// The app's state. Events from the workspace stream all come through
// handleEvent; message updates are merged at most once per frame, so a fast
// token stream costs one render per frame, not one per token.

import { create } from "zustand";
import { ApiError, CSRF_HEADER, csrfToken, http, setCSRF } from "../api/http";
import { openStream } from "../api/sse";
import type {
  Attachment,
  Bootstrap,
  Catalog,
  GitStatus,
  HubEvent,
  Message,
  PermissionMode,
  PermissionRequest,
  Project,
  QuestionRequest,
  QuestionResponse,
  RewindPreview,
  RewindResult,
  Session,
  Settings,
  WorkspaceView,
} from "../api/types";
import { badgeImage } from "../lib/badge";
import { t } from "../lib/i18n";
import { loadUI, type RailTab, type UIPrefs } from "../lib/prefs";

interface Envelope<T> {
  type: "created" | "updated" | "deleted";
  payload: T;
}

interface Optimistic {
  text: string;
  at: number;
  files: number;
}

export type DialogName = "palette" | "sessions" | "models" | "settings" | "shortcuts";
export type SettingsTab = "general" | "appearance" | "chat" | "models" | "permissions" | "integrations" | "shortcuts" | "about";

export interface Toast {
  id: number;
  text: string;
  tone?: "danger";
}

export interface RewindAsk {
  sessionId: string;
  messageId: string;
  preview: RewindPreview;
  /** Set when the message is being edited: what goes out in its place. */
  edit?: EditedMessage;
}

export interface EditedMessage {
  text: string;
  attachments: Attachment[];
}

export interface AppState {
  phase: "loading" | "ready" | "pick" | "failed";
  error: string;
  project: string;
  workspace: WorkspaceView | null;
  online: boolean;
  projects: Project[];
  sessions: Session[];
  currentId: string | null;
  messages: Record<string, Message[]>;
  busy: Record<string, boolean>;
  queued: Record<string, number>;
  unread: Record<string, boolean>;
  compacting: Record<string, boolean>;
  optimistic: Record<string, Optimistic>;
  permissions: PermissionRequest[];
  questions: QuestionRequest[];
  catalog: Catalog | null;
  settings: Settings | null;
  git: GitStatus | null;
  ui: UIPrefs;
  scheme: "light" | "dark";
  theme: "system" | "light" | "dark";
  motion: "system" | "full" | "reduce";
  notice: string;
  dialog: DialogName | null;
  settingsTab: SettingsTab;
  toasts: Toast[];
  rewind: RewindAsk | null;
  /** A file the Files tab should open. */
  fileTarget: string;
  /** A prompt waiting to land in the composer. */
  composerInsert: { text: string; seq: number } | null;

  init(): Promise<void>;
  selectSession(id: string | null): Promise<void>;
  send(text: string, attachments?: Attachment[]): Promise<void>;
  runShell(command: string): Promise<void>;
  cancel(): Promise<void>;
  deleteSession(id: string): Promise<void>;
  renameSession(id: string, title: string): Promise<void>;
  togglePin(id: string): void;
  toggleArchive(id: string): void;
  searchSessions(query: string): Promise<Session[]>;
  answerPermission(id: string, action: "allow" | "allow_session" | "deny"): Promise<void>;
  answerQuestion(id: string, responses: QuestionResponse[]): Promise<void>;
  cancelQuestion(id: string): Promise<void>;
  selectModel(provider: string, model: string, options?: { type?: "large" | "small"; effort?: string; think?: boolean }): Promise<void>;
  loadSettings(): Promise<void>;
  setPermissionMode(mode: PermissionMode): Promise<void>;
  setSessionMode(mode: string): Promise<void>;
  setCompaction(change: { enabled?: boolean; percent?: number }): Promise<void>;
  summarize(): Promise<void>;
  askRewind(messageId: string): Promise<void>;
  editMessage(messageId: string, edit: EditedMessage): Promise<void>;
  confirmRewind(): Promise<void>;
  closeRewind(): void;
  setUI<K extends keyof UIPrefs>(key: K, value: UIPrefs[K]): void;
  toggleSidebar(): void;
  toggleRail(tab?: RailTab): void;
  openFile(path: string): void;
  openDialog(dialog: DialogName | null): void;
  openSettings(tab?: SettingsTab): void;
  insertInComposer(text: string): void;
  setTheme(theme: "system" | "light" | "dark"): void;
  toggleTheme(): void;
  setMotion(motion: "system" | "full" | "reduce"): void;
  setScheme(scheme: "light" | "dark"): void;
  toast(text: string, tone?: "danger"): void;
  dropToast(id: number): void;
  refreshGit(): Promise<void>;
  reconnect(): void;
  reloadCatalog(): Promise<void>;
}

const RUN_NOTIFY_MS = 30_000;
const TOAST_MS = 4200;

let instance = "";
let stream: ReturnType<typeof openStream> | null = null;
let toastSeq = 0;
let insertSeq = 0;
const runStarted = new Map<string, number>();

const ws = (): string => `/api/workspaces/${encodeURIComponent(useApp.getState().workspace?.id ?? "")}`;
const sess = (sid: string): string => `${ws()}/sessions/${encodeURIComponent(sid)}`;

const isRootSession = (s: Session) => !s.parent_session_id;
const newestFirst = (a: Session, b: Session) => b.updated_at - a.updated_at;
const message = (err: unknown) => (err instanceof Error ? err.message : String(err));

/** RFC 4122 v4 id; the gateway uses it to start a run exactly once. */
const runId = () => crypto.randomUUID();

// Message updates waiting for the next frame, newest version per message.
const queued = new Map<string, Message>();
const deleted: Message[] = [];
let frame = 0;

function flushMessages() {
  frame = 0;
  const updates = [...queued.values()];
  queued.clear();
  const removals = deleted.splice(0);
  useApp.setState((state) => {
    const messages = { ...state.messages };
    const optimistic = { ...state.optimistic };
    const compacting = { ...state.compacting };
    for (const m of updates) {
      const list = messages[m.session_id];
      if (m.is_summary_message) delete compacting[m.session_id];
      if (!list) continue;
      const at = list.findIndex((x) => x.id === m.id);
      const next = list.slice();
      if (at >= 0) next[at] = m;
      else next.push(m);
      messages[m.session_id] = next;
      if (m.role === "user") delete optimistic[m.session_id];
    }
    for (const m of removals) {
      const list = messages[m.session_id];
      if (list) messages[m.session_id] = list.filter((x) => x.id !== m.id);
    }
    return { messages, optimistic, compacting };
  });
}

function queueMessage(m: Message) {
  queued.set(m.id, m);
  if (!frame) frame = requestAnimationFrame(flushMessages);
}

async function loadMessages(sid: string) {
  const list = await http.get<Message[] | null>(`${sess(sid)}/messages`);
  useApp.setState((s) => ({ messages: { ...s.messages, [sid]: list ?? [] } }));
}

async function loadPending() {
  const pending = await http.get<{ permissions: PermissionRequest[]; questions: QuestionRequest[] }>(`${ws()}/pending`);
  useApp.setState({ permissions: pending.permissions ?? [], questions: pending.questions ?? [] });
}

async function loadSessions() {
  const list = (await http.get<Session[] | null>(`${ws()}/sessions`)) ?? [];
  const busy: Record<string, boolean> = {};
  for (const s of list) if (s.is_busy) busy[s.id] = true;
  useApp.setState((state) => ({ sessions: list.filter(isRootSession).sort(newestFirst), busy: { ...state.busy, ...busy } }));
}

async function loadCatalog() {
  const catalog = await http.get<Catalog>(`${ws()}/models`);
  useApp.setState({ catalog });
}

/** Reloads everything the stream may have missed. */
async function resync() {
  const { currentId } = useApp.getState();
  await Promise.allSettled([loadSessions(), loadPending(), currentId ? loadMessages(currentId) : Promise.resolve()]);
  syncBadge();
}

/** The taskbar shows what waits for the person: questions and unseen results. */
function syncBadge() {
  const s = useApp.getState();
  const count = s.permissions.length + s.questions.length + Object.keys(s.unread).length;
  void window.atlas?.setBadge(count, count > 0 ? badgeImage(count) : "");
}

function titleOf(sid: string) {
  return useApp.getState().sessions.find((x) => x.id === sid)?.title ?? useApp.getState().workspace?.name ?? "Atlas";
}

function notifyWaiting(sid: string, body: string) {
  const { ui } = useApp.getState();
  if (document.hasFocus() || !ui.notifyWait) return;
  void window.atlas?.notify({ title: titleOf(sid), body, session: sid });
}

function onRunEnd(sid: string) {
  const state = useApp.getState();
  void state.refreshGit();
  const started = runStarted.get(sid) ?? Date.now();
  runStarted.delete(sid);
  const focused = document.hasFocus();
  if (sid !== state.currentId || !focused) {
    useApp.setState((s) => ({ unread: { ...s.unread, [sid]: true } }));
    syncBadge();
  }
  if (!focused && state.ui.notifyDone && Date.now() - started >= RUN_NOTIFY_MS) {
    void window.atlas?.notify({ title: titleOf(sid), body: t("notify.done"), session: sid });
  }
}

function handleEvent(ev: HubEvent) {
  const set = useApp.setState;
  switch (ev.type) {
    case "hello": {
      const hello = ev.payload as { instance: string; resync: boolean };
      const changed = instance !== "" && hello.instance !== instance;
      instance = hello.instance;
      if (hello.resync || changed) void resync();
      return;
    }
    case "resync":
      void resync();
      return;
    case "connection":
      set({ online: (ev.payload as { state: string }).state !== "offline" });
      return;
    case "workspace_replaced": {
      const { new_id: id } = ev.payload as { new_id: string };
      set((s) => ({ workspace: s.workspace ? { ...s.workspace, id } : null }));
      stream?.restart();
      void resync();
      return;
    }
    case "session_status": {
      const { session_id: sid, busy, queued: waiting } = ev.payload as { session_id: string; busy: boolean; queued?: number };
      const was = !!useApp.getState().busy[sid];
      if (busy && !was) runStarted.set(sid, Date.now());
      set((s) => ({ busy: { ...s.busy, [sid]: busy }, queued: { ...s.queued, [sid]: waiting ?? 0 } }));
      if (!busy) set((s) => ({ compacting: { ...s.compacting, [sid]: false } }));
      if (was && !busy) onRunEnd(sid);
      return;
    }
    case "message": {
      const env = ev.payload as Envelope<Message>;
      if (env.type === "deleted") {
        deleted.push(env.payload);
        if (!frame) frame = requestAnimationFrame(flushMessages);
      } else {
        queueMessage(env.payload);
      }
      return;
    }
    case "session": {
      const env = ev.payload as Envelope<Session>;
      const s = env.payload;
      if (!isRootSession(s)) return;
      set((state) => {
        const rest = state.sessions.filter((x) => x.id !== s.id);
        return { sessions: env.type === "deleted" ? rest : [s, ...rest].sort(newestFirst) };
      });
      return;
    }
    case "permission_request": {
      const req = (ev.payload as Envelope<PermissionRequest>).payload;
      set((s) => ({ permissions: [...s.permissions.filter((p) => p.id !== req.id), req] }));
      notifyWaiting(req.session_id, t("notify.permission"));
      syncBadge();
      return;
    }
    case "permission_notification": {
      const note = (ev.payload as Envelope<{ tool_call_id: string; granted: boolean; denied: boolean }>).payload;
      if (note.granted || note.denied) {
        set((s) => ({ permissions: s.permissions.filter((p) => p.tool_call_id !== note.tool_call_id) }));
        syncBadge();
      }
      return;
    }
    case "question_batch_request": {
      const req = (ev.payload as Envelope<QuestionRequest>).payload;
      set((s) => ({ questions: [...s.questions.filter((q) => q.id !== req.id), req] }));
      notifyWaiting(req.session_id, t("notify.question"));
      syncBadge();
      return;
    }
    case "question_batch_notification": {
      const note = (ev.payload as Envelope<{ batch_id: string }>).payload;
      set((s) => ({ questions: s.questions.filter((q) => q.id !== note.batch_id) }));
      syncBadge();
      return;
    }
    case "config_changed":
      void loadCatalog().catch(() => {});
      return;
  }
}

function connect() {
  stream?.stop();
  stream = openStream(
    () => useApp.getState().workspace?.id ?? "",
    () => instance,
    {
      onEvent: handleEvent,
      onOpen: () => useApp.setState({ online: true }),
      onClose: () => useApp.setState({ online: false }),
    },
  );
}

/** Lets the gateway drop its claim on the workspace when the window goes away. */
function releaseOnClose() {
  window.addEventListener("pagehide", () => {
    const id = useApp.getState().workspace?.id;
    if (!id) return;
    void fetch(`/api/workspaces/${encodeURIComponent(id)}`, {
      method: "DELETE",
      keepalive: true,
      headers: { [CSRF_HEADER]: csrfToken() },
    });
  });
}

const sessionKey = () => `session:${useApp.getState().project}`;

/** After a reload the app caused itself, the same chat comes back. */
function restoreSession() {
  const url = new URL(location.href);
  if (!url.searchParams.has("restore")) return;
  url.searchParams.delete("restore");
  history.replaceState(null, "", url);
  const id = window.__atlasBoot?.ui?.[sessionKey()];
  if (typeof id === "string" && useApp.getState().sessions.some((s) => s.id === id)) void useApp.getState().selectSession(id);
}

/** Reloads the window and comes back to the open chat. */
export function reloadKeepingChat() {
  const url = new URL(location.href);
  url.searchParams.set("restore", "1");
  location.replace(url);
}

/** Coming back to the window counts as having seen the open chat. */
function watchFocus() {
  window.addEventListener("focus", () => {
    const { currentId, unread, refreshGit } = useApp.getState();
    void refreshGit();
    if (currentId && unread[currentId]) {
      useApp.setState((s) => {
        const next = { ...s.unread };
        delete next[currentId];
        return { unread: next };
      });
    }
    syncBadge();
  });
}

/** Branches the chat from just before the message and sends the new version. */
async function sendEdited(sessionId: string, messageId: string, edit: EditedMessage) {
  const result = await http.post<RewindResult>(`${sess(sessionId)}/rewind/before`, { up_to_message_id: messageId });
  useApp.setState((s) => ({ sessions: [result.session, ...s.sessions.filter((x) => x.id !== result.session.id)] }));
  await useApp.getState().selectSession(result.session.id);
  await useApp.getState().send(edit.text, edit.attachments);
}

const editFailure = (err: unknown) => (err instanceof ApiError && err.status === 501 ? t("edit.oldServer") : message(err));

export const useApp = create<AppState>((set, get) => ({
  phase: "loading",
  error: "",
  project: new URLSearchParams(location.search).get("project") ?? "",
  workspace: null,
  online: true,
  projects: [],
  sessions: [],
  currentId: null,
  messages: {},
  busy: {},
  queued: {},
  unread: {},
  compacting: {},
  optimistic: {},
  permissions: [],
  questions: [],
  catalog: null,
  settings: null,
  git: null,
  ui: loadUI(),
  scheme: document.documentElement.dataset.colorScheme === "dark" ? "dark" : "light",
  theme: window.__atlasBoot?.theme ?? "system",
  motion: window.__atlasBoot?.motion ?? "system",
  notice: "",
  dialog: null,
  settingsTab: "general",
  toasts: [],
  rewind: null,
  fileTarget: "",
  composerInsert: null,

  async init() {
    set({ phase: "loading", error: "" });
    try {
      const boot = await http.get<Bootstrap>("/api/bootstrap");
      setCSRF(boot.csrf);
      instance = boot.instance;
      const { project } = get();
      if (!project) {
        const projects = await http.get<Project[]>("/api/projects");
        set({ phase: "pick", projects });
        return;
      }
      const workspace = await http.post<WorkspaceView>("/api/workspaces", { path: project });
      document.title = `${workspace.name} · Atlas`;
      set({ workspace });
      await Promise.all([loadSessions(), loadPending(), loadCatalog().catch(() => {}), get().loadSettings().catch(() => {}), get().refreshGit()]);
      connect();
      releaseOnClose();
      watchFocus();
      syncBadge();
      set({ phase: "ready" });
      restoreSession();
      void http.get<Project[]>("/api/projects").then((projects) => set({ projects }), () => {});
    } catch (err) {
      set({ phase: "failed", error: message(err) });
    }
  },

  async selectSession(id) {
    set((s) => {
      const unread = { ...s.unread };
      if (id) delete unread[id];
      return { currentId: id, notice: "", unread };
    });
    void window.atlas?.setUI(sessionKey(), id ?? "");
    syncBadge();
    if (id && !get().messages[id]) await loadMessages(id);
  },

  async send(text, attachments = []) {
    const prompt = text.trim() || (attachments.length > 0 ? t("composer.attachmentPrompt") : "");
    if (!prompt) return;
    set({ notice: "" });
    let sid = get().currentId;
    const running = sid ? get().busy[sid] : false;
    try {
      if (!sid) {
        const session = await http.post<Session>(`${ws()}/sessions`, {});
        sid = session.id;
        set((s) => ({
          currentId: session.id,
          messages: { ...s.messages, [session.id]: [] },
          sessions: [session, ...s.sessions.filter((x) => x.id !== session.id)],
        }));
      }
      const target = sid;
      if (!running) runStarted.set(target, Date.now());
      set((s) => ({
        busy: { ...s.busy, [target]: true },
        optimistic: { ...s.optimistic, [target]: { text: prompt, at: Date.now(), files: attachments.length } },
      }));
      const extra = attachments.length > 0 ? { attachments } : {};
      const body = running ? { prompt, follow_up: true, ...extra } : { prompt, run_id: runId(), ...extra };
      await http.post(`${sess(target)}/prompt`, body);
    } catch (err) {
      const failed = sid;
      set((s) => {
        const optimistic = { ...s.optimistic };
        if (failed) delete optimistic[failed];
        return {
          optimistic,
          busy: failed ? { ...s.busy, [failed]: running ?? false } : s.busy,
          notice: `${t("error.send")}: ${message(err)}`,
        };
      });
      throw err;
    }
  },

  async runShell(command) {
    let sid = get().currentId;
    if (!sid) {
      const session = await http.post<Session>(`${ws()}/sessions`, { title: command.slice(0, 80) });
      sid = session.id;
      set((s) => ({
        currentId: session.id,
        messages: { ...s.messages, [session.id]: [] },
        sessions: [session, ...s.sessions.filter((x) => x.id !== session.id)],
      }));
    }
    try {
      await http.post(`${sess(sid)}/run`, { command });
    } catch (err) {
      get().toast(message(err), "danger");
    }
  },

  async cancel() {
    const sid = get().currentId;
    if (sid) await http.post(`${sess(sid)}/cancel`);
  },

  async deleteSession(id) {
    await http.del(`${sess(id)}`);
    set((s) => ({
      sessions: s.sessions.filter((x) => x.id !== id),
      currentId: s.currentId === id ? null : s.currentId,
    }));
  },

  async renameSession(id, title) {
    const clean = title.trim();
    if (!clean) return;
    set((s) => ({ sessions: s.sessions.map((x) => (x.id === id ? { ...x, title: clean } : x)) }));
    try {
      await http.patch(sess(id), { title: clean });
    } catch (err) {
      get().toast(message(err), "danger");
      void loadSessions();
    }
  },

  togglePin(id) {
    const { pinned } = get().ui;
    get().setUI("pinned", pinned.includes(id) ? pinned.filter((x) => x !== id) : [id, ...pinned]);
  },

  toggleArchive(id) {
    const { archived, pinned } = get().ui;
    const archiving = !archived.includes(id);
    get().setUI("archived", archiving ? [id, ...archived] : archived.filter((x) => x !== id));
    if (archiving) {
      get().setUI("pinned", pinned.filter((x) => x !== id));
      if (get().currentId === id) void get().selectSession(null);
    }
  },

  async searchSessions(query) {
    const list = await http.get<Session[] | null>(`${ws()}/sessions?q=${encodeURIComponent(query)}`);
    return (list ?? []).filter(isRootSession);
  },

  async answerPermission(id, action) {
    set((s) => ({ permissions: s.permissions.filter((p) => p.id !== id) }));
    syncBadge();
    await http.post(`${ws()}/permissions/${encodeURIComponent(id)}`, { action });
  },

  async answerQuestion(id, responses) {
    set((s) => ({ questions: s.questions.filter((q) => q.id !== id) }));
    syncBadge();
    await http.post(`${ws()}/questions/${encodeURIComponent(id)}/answer`, { responses });
  },

  async cancelQuestion(id) {
    set((s) => ({ questions: s.questions.filter((q) => q.id !== id) }));
    syncBadge();
    await http.post(`${ws()}/questions/${encodeURIComponent(id)}/cancel`);
  },

  async selectModel(provider, model, options = {}) {
    try {
      const catalog = await http.post<Catalog>(`${ws()}/models`, {
        type: options.type ?? "large",
        provider,
        model,
        reasoning_effort: options.effort ?? "",
        think: options.think ?? false,
      });
      set({ catalog });
    } catch (err) {
      get().toast(message(err), "danger");
    }
  },

  async loadSettings() {
    const settings = await http.get<Settings>(`${ws()}/settings`);
    set({ settings });
  },

  async setPermissionMode(mode) {
    const before = get().settings;
    if (before) set({ settings: { ...before, permission_mode: mode } });
    try {
      await http.post(`${ws()}/permission-mode`, { mode });
    } catch (err) {
      set({ settings: before });
      get().toast(message(err), "danger");
    }
  },

  async setSessionMode(mode) {
    try {
      await http.post(`${ws()}/session-mode`, { mode });
      await get().loadSettings();
    } catch (err) {
      get().toast(message(err), "danger");
    }
  },

  async setCompaction(change) {
    try {
      await http.post(`${ws()}/compaction`, change);
      await get().loadSettings();
    } catch (err) {
      get().toast(message(err), "danger");
    }
  },

  async summarize() {
    const sid = get().currentId;
    if (!sid) return;
    set((s) => ({ compacting: { ...s.compacting, [sid]: true } }));
    try {
      await http.post(`${sess(sid)}/summarize`);
    } catch (err) {
      set((s) => ({ compacting: { ...s.compacting, [sid]: false } }));
      get().toast(message(err), "danger");
    }
  },

  async askRewind(messageId) {
    const sessionId = get().currentId;
    if (!sessionId) return;
    try {
      const preview = await http.post<RewindPreview>(`${sess(sessionId)}/rewind/preview`, { up_to_message_id: messageId });
      set({ rewind: { sessionId, messageId, preview } });
    } catch (err) {
      get().toast(message(err), "danger");
    }
  },

  async editMessage(messageId, edit) {
    const sessionId = get().currentId;
    if (!sessionId) return;
    try {
      const preview = await http.post<RewindPreview>(`${sess(sessionId)}/rewind/preview`, { up_to_message_id: messageId });
      // Only a change to the user's files needs their say-so.
      if (preview.files_to_write + preview.files_to_delete > 0) set({ rewind: { sessionId, messageId, preview, edit } });
      else await sendEdited(sessionId, messageId, edit);
    } catch (err) {
      get().toast(editFailure(err), "danger");
    }
  },

  async confirmRewind() {
    const ask = get().rewind;
    if (!ask) return;
    set({ rewind: null });
    if (ask.edit) {
      try {
        await sendEdited(ask.sessionId, ask.messageId, ask.edit);
      } catch (err) {
        get().toast(editFailure(err), "danger");
      }
      return;
    }
    try {
      const result = await http.post<RewindResult>(`${sess(ask.sessionId)}/rewind`, { up_to_message_id: ask.messageId });
      set((s) => ({ sessions: [result.session, ...s.sessions.filter((x) => x.id !== result.session.id)] }));
      await get().selectSession(result.session.id);
      get().toast(t("rewind.done"));
    } catch (err) {
      get().toast(message(err), "danger");
    }
  },

  closeRewind() {
    set({ rewind: null });
  },

  setUI(key, value) {
    set((s) => ({ ui: { ...s.ui, [key]: value } }));
    void window.atlas?.setUI(key, value);
  },

  toggleSidebar() {
    get().setUI("sidebarOpen", !get().ui.sidebarOpen);
  },

  toggleRail(tab) {
    const { railOpen, railTab } = get().ui;
    if (tab && tab !== railTab) {
      get().setUI("railTab", tab);
      if (!railOpen) get().setUI("railOpen", true);
      return;
    }
    get().setUI("railOpen", !railOpen);
  },

  openFile(path) {
    set({ fileTarget: path });
    get().setUI("railTab", "files");
    get().setUI("railOpen", true);
  },

  openDialog(dialog) {
    set({ dialog });
  },

  openSettings(tab) {
    set({ dialog: "settings", settingsTab: tab ?? get().settingsTab });
  },

  insertInComposer(text) {
    set({ composerInsert: { text, seq: ++insertSeq } });
  },

  setTheme(theme) {
    set({ theme });
    void window.atlas?.setTheme(theme);
  },

  toggleTheme() {
    get().setTheme(get().scheme === "dark" ? "light" : "dark");
  },

  setMotion(motion) {
    set({ motion });
    const os = matchMedia("(prefers-reduced-motion: reduce)").matches;
    document.documentElement.dataset.motion = motion === "reduce" || (motion === "system" && os) ? "reduce" : "full";
    void window.atlas?.setMotion(motion);
  },

  setScheme(scheme) {
    document.documentElement.dataset.colorScheme = scheme;
    set({ scheme });
  },

  toast(text, tone) {
    const id = ++toastSeq;
    set((s) => ({ toasts: [...s.toasts.slice(-2), { id, text, tone }] }));
    setTimeout(() => get().dropToast(id), TOAST_MS);
  },

  dropToast(id) {
    set((s) => ({ toasts: s.toasts.filter((x) => x.id !== id) }));
  },

  async refreshGit() {
    const path = get().workspace?.path ?? get().project;
    if (!path) return;
    const git = (await window.atlas?.gitStatus(path).catch(() => null)) ?? null;
    set({ git });
  },

  reconnect() {
    stream?.restart();
  },

  reloadCatalog() {
    return loadCatalog();
  },
}));

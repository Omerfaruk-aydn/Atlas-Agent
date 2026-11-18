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
import { newDesignTargets } from "../lib/design-follow";
import { moveItem, newQueueItem, type QueueItem } from "../lib/chat";
import { t } from "../lib/i18n";
import { loadUI, type UIPrefs } from "../lib/prefs";
import { usePanel, type PanelTool } from "./panel";

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
  /** Playing its exit; removed when that is over. */
  leaving?: boolean;
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

/** Something for a composer: text to put in, or a file to attach. `target` is the session whose composer it is for. */
export interface ComposerInsert {
  seq: number;
  text: string;
  append?: boolean;
  file?: { name: string; mime: string; content: string };
  target?: string;
}

export interface AppState {
  phase: "loading" | "ready" | "pick" | "failed";
  error: string;
  project: string;
  workspace: WorkspaceView | null;
  online: boolean;
  /** While the live channel is down: which try this is and when the next one starts. */
  retry: { attempt: number; at: number } | null;
  projects: Project[];
  sessions: Session[];
  /** Side chats, kept out of the chat list: they live in the side panel. */
  sideChats: Session[];
  currentId: string | null;
  messages: Record<string, Message[]>;
  busy: Record<string, boolean>;
  queued: Record<string, number>;
  /** Messages written while a turn runs, held here until it ends. */
  queue: Record<string, QueueItem[]>;
  /** A queue the person stopped: it waits for them instead of sending on. */
  queuePaused: Record<string, boolean>;
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
  /** A prompt waiting to land in the composer. */
  composerInsert: ComposerInsert | null;

  init(): Promise<void>;
  selectSession(id: string | null): Promise<void>;
  /** `into` is the session to send in; by default the open chat. */
  send(text: string, attachments?: Attachment[], into?: string): Promise<void>;
  runShell(command: string, into?: string): Promise<void>;
  cancel(into?: string): Promise<void>;
  /** Holds a message until the running turn ends. */
  enqueue(sessionId: string, text: string, attachments?: Attachment[]): void;
  updateQueued(sessionId: string, id: string, text: string): void;
  removeQueued(sessionId: string, id: string): void;
  moveQueued(sessionId: string, from: number, to: number): void;
  /** Sends a queued message into the running turn as guidance. */
  steerQueued(sessionId: string, id: string): Promise<void>;
  /** Stops the running turn and sends a queued message in its place. */
  sendQueuedNow(sessionId: string, id: string): Promise<void>;
  resumeQueue(sessionId: string): void;
  /** Copies the chat up to a message into a chat of its own. */
  branchFrom(messageId: string): Promise<void>;
  /** Puts a file back as it was before the agent's edit. */
  revertFile(file: { path: string; before: string; after: string }): Promise<boolean>;
  deleteSession(id: string): Promise<void>;
  renameSession(id: string, title: string): Promise<void>;
  togglePin(id: string): void;
  toggleArchive(id: string): void;
  searchSessions(query: string): Promise<Session[]>;
  answerPermission(id: string, action: "allow" | "allow_session" | "deny", feedback?: string): Promise<void>;
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
  toggleRail(tool?: PanelTool): void;
  openFile(path: string): void;
  openDialog(dialog: DialogName | null): void;
  openSettings(tab?: SettingsTab): void;
  insertInComposer(text: string, options?: { append?: boolean; target?: string }): void;
  /** Attaches a file (a page screenshot, say) to a composer's message. */
  attachToComposer(file: { name: string; mime: string; content: string }, target?: string): void;
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

/** The session a side chat was forked from, from the server's tag. */
export const sideOf = (s: Session): string => s.tags?.find((tag) => tag.startsWith("side-of:"))?.slice("side-of:".length) ?? "";
const isRootSession = (s: Session) => !s.parent_session_id && !sideOf(s);
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

/** Loads a session's messages again, for one whose live updates may have been missed. */
export async function refreshMessages(sid: string) {
  await loadMessages(sid);
}

const loading = new Map<string, Promise<void>>();

/** Loads a session's messages unless they are already here; one request serves every caller. */
export async function ensureMessages(sid: string) {
  if (useApp.getState().messages[sid]) return;
  let pending = loading.get(sid);
  if (!pending) {
    pending = loadMessages(sid).finally(() => loading.delete(sid));
    loading.set(sid, pending);
  }
  await pending;
}

async function loadPending() {
  const pending = await http.get<{ permissions: PermissionRequest[]; questions: QuestionRequest[] }>(`${ws()}/pending`);
  useApp.setState({ permissions: pending.permissions ?? [], questions: pending.questions ?? [] });
}

async function loadSessions() {
  const list = (await http.get<Session[] | null>(`${ws()}/sessions?side=1`)) ?? [];
  const busy: Record<string, boolean> = {};
  for (const s of list) if (s.is_busy) busy[s.id] = true;
  useApp.setState((state) => ({ sessions: list.filter(isRootSession).sort(newestFirst), sideChats: list.filter((x) => !!sideOf(x)).sort(newestFirst), busy: { ...state.busy, ...busy } }));
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
      if (was && !busy) {
        onRunEnd(sid);
        drainQueue(sid);
      }
      return;
    }
    case "message": {
      const env = ev.payload as Envelope<Message>;
      if (env.type === "deleted") {
        deleted.push(env.payload);
        if (!frame) frame = requestAnimationFrame(flushMessages);
      } else {
        queueMessage(env.payload);
        // The agent started a design: show it being made.
        if (useApp.getState().busy[env.payload.session_id]) for (const file of newDesignTargets(env.payload)) usePanel.getState().openDesign(file);
      }
      return;
    }
    case "session": {
      const env = ev.payload as Envelope<Session>;
      const s = env.payload;
      if (sideOf(s)) {
        // A session that gains its side tag leaves the chat list, wherever it was first heard of.
        set((state) => {
          const rest = state.sideChats.filter((x) => x.id !== s.id);
          return { sideChats: env.type === "deleted" ? rest : [s, ...rest].sort(newestFirst), sessions: state.sessions.filter((x) => x.id !== s.id) };
        });
        return;
      }
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
      onOpen: () => useApp.setState({ online: true, retry: null }),
      onRetry: (attempt, ms) => useApp.setState({ retry: { attempt, at: Date.now() + ms } }),
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

/** Sends the next held message once the session is idle. */
function drainQueue(sid: string) {
  const state = useApp.getState();
  const list = state.queue[sid];
  if (!list?.length || state.queuePaused[sid] || state.busy[sid]) return;
  const [next, ...rest] = list;
  useApp.setState((s) => ({ queue: { ...s.queue, [sid]: rest } }));
  void state.send(next!.text, next!.attachments, sid).catch(() => {
    // It goes back to the front and waits, so nothing the person wrote is lost.
    useApp.setState((s) => ({ queue: { ...s.queue, [sid]: [next!, ...(s.queue[sid] ?? [])] }, queuePaused: { ...s.queuePaused, [sid]: true } }));
  });
}

const editFailure = (err: unknown) => (err instanceof ApiError && err.status === 501 ? t("edit.oldServer") : message(err));

export const useApp = create<AppState>((set, get) => ({
  phase: "loading",
  error: "",
  project: new URLSearchParams(location.search).get("project") ?? "",
  workspace: null,
  online: true,
  retry: null,
  projects: [],
  sessions: [],
  sideChats: [],
  currentId: null,
  messages: {},
  busy: {},
  queued: {},
  queue: {},
  queuePaused: {},
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
    // The new thread fades in (a keyed animation, see motion.css), so a second
    // click, as in a double click to rename, is never held up by a transition.
    set((s) => {
      const unread = { ...s.unread };
      if (id) delete unread[id];
      return { currentId: id, notice: "", unread };
    });
    void window.atlas?.setUI(sessionKey(), id ?? "");
    syncBadge();
    if (id && !get().messages[id]) await loadMessages(id);
  },

  async send(text, attachments = [], into) {
    const prompt = text.trim() || (attachments.length > 0 ? t("composer.attachmentPrompt") : "");
    if (!prompt) return;
    set({ notice: "" });
    let sid = into ?? get().currentId;
    const running = sid ? get().busy[sid] : false;
    try {
      if (get().ui.autoModel && !running) {
        const targets = get().ui.autoModelTargets;
        const target = prompt.length < 240 && attachments.length === 0 ? (targets.small ?? targets.large) : targets.large;
        const current = get().catalog?.active;
        if (target && (current?.provider !== target.provider || current?.model !== target.model)) {
          const catalog = await http.post<Catalog>(`${ws()}/models`, { type: "large", ...target });
          set({ catalog });
        }
      }
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

  async runShell(command, into) {
    let sid = into ?? get().currentId;
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

  async cancel(into) {
    const sid = into ?? get().currentId;
    if (!sid) return;
    // Stopping is a decision: what was waiting does not start on its own.
    if (get().queue[sid]?.length) set((s) => ({ queuePaused: { ...s.queuePaused, [sid]: true } }));
    await http.post(`${sess(sid)}/cancel`);
  },

  enqueue(sid, text, attachments = []) {
    if (!text.trim() && attachments.length === 0) return;
    set((s) => {
      const fresh = !(s.queue[sid]?.length);
      return { queue: { ...s.queue, [sid]: [...(s.queue[sid] ?? []), newQueueItem(text, attachments)] }, queuePaused: fresh ? { ...s.queuePaused, [sid]: false } : s.queuePaused };
    });
    // The turn may have ended between the keypress and here.
    if (!get().busy[sid]) drainQueue(sid);
  },

  updateQueued(sid, id, text) {
    set((s) => ({ queue: { ...s.queue, [sid]: (s.queue[sid] ?? []).map((q) => (q.id === id ? { ...q, text } : q)) } }));
  },

  removeQueued(sid, id) {
    set((s) => ({ queue: { ...s.queue, [sid]: (s.queue[sid] ?? []).filter((q) => q.id !== id) } }));
  },

  moveQueued(sid, from, to) {
    set((s) => ({ queue: { ...s.queue, [sid]: moveItem(s.queue[sid] ?? [], from, to) } }));
  },

  async steerQueued(sid, id) {
    const item = get().queue[sid]?.find((q) => q.id === id);
    if (!item) return;
    get().removeQueued(sid, id);
    try {
      await get().send(item.text, item.attachments, sid);
    } catch {
      set((s) => ({ queue: { ...s.queue, [sid]: [item, ...(s.queue[sid] ?? [])] } }));
    }
  },

  async sendQueuedNow(sid, id) {
    const list = get().queue[sid] ?? [];
    const at = list.findIndex((q) => q.id === id);
    if (at < 0) return;
    if (!get().busy[sid]) {
      get().moveQueued(sid, at, 0);
      set((s) => ({ queuePaused: { ...s.queuePaused, [sid]: false } }));
      drainQueue(sid);
      return;
    }
    // The item goes first and the queue runs on, so the end of the stopped
    // turn is what sends it.
    get().moveQueued(sid, at, 0);
    set((s) => ({ queuePaused: { ...s.queuePaused, [sid]: false } }));
    try {
      await http.post(`${sess(sid)}/cancel`);
    } catch (err) {
      get().toast(message(err), "danger");
    }
  },

  resumeQueue(sid) {
    set((s) => ({ queuePaused: { ...s.queuePaused, [sid]: false } }));
    drainQueue(sid);
  },

  async branchFrom(messageId) {
    const sid = get().currentId;
    if (!sid) return;
    try {
      const branch = await http.post<Session>(`${sess(sid)}/branch`, { message_id: messageId });
      set((s) => ({ sessions: [branch, ...s.sessions.filter((x) => x.id !== branch.id)] }));
      await get().selectSession(branch.id);
      get().toast(t("branch.done"));
    } catch (err) {
      get().toast(err instanceof ApiError && err.status === 501 ? t("edit.oldServer") : message(err), "danger");
    }
  },

  async revertFile(file) {
    try {
      await http.post(`${ws()}/files/revert`, { path: file.path, old_content: file.before, new_content: file.after });
      void get().refreshGit();
      return true;
    } catch (err) {
      get().toast(err instanceof ApiError && err.status === 409 ? t("edits.conflict") : message(err), "danger");
      return false;
    }
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
    const list = await http.get<Session[] | null>(`${ws()}/sessions?side=1&q=${encodeURIComponent(query)}`);
    return (list ?? []).filter(isRootSession);
  },

  async answerPermission(id, action, feedback) {
    set((s) => ({ permissions: s.permissions.filter((p) => p.id !== id) }));
    syncBadge();
    await http.post(`${ws()}/permissions/${encodeURIComponent(id)}`, feedback ? { action, feedback } : { action });
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
      get().setUI("autoModel", false);
      get().setUI("recentModels", [`${provider}:${model}`, ...get().ui.recentModels.filter((id) => id !== `${provider}:${model}`)].slice(0, 8));
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

  toggleRail(tool) {
    usePanel.getState().toggle(tool);
  },

  openFile(path) {
    usePanel.getState().openFile(path);
  },

  openDialog(dialog) {
    set({ dialog });
  },

  openSettings(tab) {
    set({ dialog: "settings", settingsTab: tab ?? get().settingsTab });
  },

  insertInComposer(text, options) {
    set({ composerInsert: { text, seq: ++insertSeq, append: options?.append, target: options?.target } });
  },

  attachToComposer(file, target) {
    set({ composerInsert: { text: "", seq: ++insertSeq, file, target } });
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
    // A toast leaves in two steps: it is marked, plays its exit, and is then removed.
    if (!get().toasts.some((x) => x.id === id && !x.leaving)) return;
    set((s) => ({ toasts: s.toasts.map((x) => (x.id === id ? { ...x, leaving: true } : x)) }));
    setTimeout(() => set((s) => ({ toasts: s.toasts.filter((x) => x.id !== id) })), 200);
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

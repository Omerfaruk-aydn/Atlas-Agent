// The message box. While a turn runs the send button turns into stop; typing
// turns it back, because a message written mid-turn joins that turn. "@" looks
// for project files, "/" for commands, and a leading "!" runs a shell command.

import { useEffect, useLayoutEffect, useMemo, useRef, useState, type ClipboardEvent, type DragEvent } from "react";
import { http } from "../../api/http";
import type { SlashCommand } from "../../api/types";
import { addFiles, encode, type Pending } from "../../lib/attachments";
import { baseName } from "../../lib/format";
import { t, type Key } from "../../lib/i18n";
import { useKeys } from "../../lib/shortcuts";
import { IconArrowUp, IconBranch, IconChevronDown, IconClose, IconFile, IconLaptop, IconShield, IconStop, IconWave } from "../../primitives/icons";
import { Picker } from "../../primitives/Picker";
import { Menu, Modal, useDebounced } from "../../primitives/ui";
import { useApp } from "../../stores/app";
import { ProviderLogo } from "../models/ProviderLogo";
import { ModelPicker } from "../models/ModelPicker";
import { IconSteer } from "../thread/chatIcons";
import { QueueList } from "../thread/QueueList";
import { ContextMeter } from "./ContextMeter";
import { ContextMenu, WORK_MODES, type WorkMode } from "./ContextMenu";
import { VoiceInput } from "./VoiceInput";
import { reducedMotion, tokenMs } from "../../lib/motion";

/** Taller than this and the text has wrapped: one 22px line plus its padding. */
const ONE_LINE = 36;

interface ComposerBox {
  height: number;
  parts: ({ x: number; y: number } | null)[];
}

const MAX_FILE_SUGGESTIONS = 8;
const MAX_COMMAND_SUGGESTIONS = 40;
// The title bar above and a margin below stay free of the list.
const SUGGEST_TOP_LIMIT = 52;
const SUGGEST_BOTTOM_LIMIT = 14;
const SUGGEST_GAP = 6;
const SUGGEST_MAX_HEIGHT = 340;
const SUGGEST_MIN_HEIGHT = 170;
const DRAFT_SAVE_MS = 400;

// Drafts outlive the composer, which unmounts while a permission dock has its
// place, and the window, which the app may have to reload.
const draftKey = () => `drafts:${useApp.getState().project}`;
const drafts = new Map<string, string>(Object.entries((window.__atlasBoot?.ui?.[`drafts:${new URLSearchParams(location.search).get("project") ?? ""}`] as Record<string, string> | undefined) ?? {}));
let draftTimer = 0;

function saveDraft(key: string, value: string) {
  if (value) drafts.set(key, value);
  else drafts.delete(key);
  clearTimeout(draftTimer);
  draftTimer = window.setTimeout(() => void window.atlas?.setUI(draftKey(), Object.fromEntries(drafts)), DRAFT_SAVE_MS);
}

const PERMISSION_LABELS: Record<string, Key> = {
  manual: "mode.manual",
  auto_accept_edits: "mode.autoEdits",
  plan: "mode.plan",
  bypass: "mode.bypass",
};
const PERMISSION_HINTS: Record<string, Key> = {
  manual: "mode.manual.hint",
  auto_accept_edits: "mode.autoEdits.hint",
  plan: "mode.plan.hint",
  bypass: "mode.bypass.hint",
};

function PermissionChip() {
  const mode = useApp((s) => s.settings?.permission_mode ?? "manual");
  const setMode = useApp((s) => s.setPermissionMode);
  return (
    <Menu
      trigger={
        <button type="button" className="chip-button permission-chip" data-tone={mode === "bypass" ? "danger" : undefined} title={t("mode.title")}>
          <IconShield />
          <span>{t(PERMISSION_LABELS[mode] ?? "mode.manual")}</span>
        </button>
      }
      items={(["manual", "auto_accept_edits", "plan", "bypass"] as const).map((m) => ({
        label: t(PERMISSION_LABELS[m]),
        hint: t(PERMISSION_HINTS[m]),
        checked: m === mode,
        danger: m === "bypass",
        onSelect: () => void setMode(m),
      }))}
    />
  );
}

function ModelChip() {
  const catalog = useApp((s) => s.catalog);
  const current = catalog?.active ?? catalog?.selected?.large;
  const auto = useApp((s) => s.ui.autoModel);
  const name = catalog?.providers?.flatMap((p) => p.models).find((m) => m.id === current?.model)?.name ?? current?.model ?? t("composer.noModel");
  const effort = current?.reasoning_effort;
  const keys = useKeys("models");
  return (
    <ModelPicker trigger={
      <button type="button" className="chip-button model-trigger" title={`${t("composer.model")}${keys ? ` (${keys})` : ""}`}>
        {current?.provider && <ProviderLogo id={current.provider} size={14} />}
        <span>{auto ? t("compose.auto") : name}</span>
        {effort && <span className="chip-note">{effort}</span>}
        <IconChevronDown />
      </button>
    } />
  );
}

interface Suggestion {
  id: string;
  label: string;
  hint: string;
  file?: boolean;
  kind?: string;
  run(): void;
}

/** The token being typed at the caret, when it starts with @ or /. */
function activeToken(text: string, caret: number): { kind: "@" | "/"; query: string; from: number } | null {
  const before = text.slice(0, caret);
  const at = /(^|\s)@([^\s@]*)$/.exec(before);
  if (at) return { kind: "@", query: at[2], from: caret - at[2].length - 1 };
  const slash = /^\/([^\s]*)$/.exec(before);
  if (slash) return { kind: "/", query: slash[1], from: 0 };
  return null;
}

function CommandArguments({ command, onClose, onDone }: { command: SlashCommand | null; onClose(): void; onDone(values: Record<string, string>): void }) {
  const [values, setValues] = useState<Record<string, string>>({});
  useEffect(() => setValues({}), [command]);
  const args = command?.arguments ?? [];
  const ready = args.every((a) => !a.required || values[a.id]?.trim());
  return (
    <Modal open={!!command} onClose={onClose} title={`/${command?.name ?? ""}`} className="modal-sm">
      <form
        className="form"
        onSubmit={(e) => {
          e.preventDefault();
          if (ready) onDone(values);
        }}
      >
        {args.map((a, i) => (
          <label key={a.id} className="field">
            <span>{a.title}</span>
            <input autoFocus={i === 0} value={values[a.id] ?? ""} placeholder={a.description} onChange={(e) => setValues({ ...values, [a.id]: e.target.value })} />
          </label>
        ))}
        <div className="modal-actions">
          <button type="button" className="button" onClick={onClose}>
            {t("common.cancel")}
          </button>
          <button type="submit" className="button" data-variant="primary" disabled={!ready}>
            {t("composer.send")}
          </button>
        </div>
      </form>
    </Modal>
  );
}

export function Composer({ sessionId, autoFocus }: { sessionId: string | null; autoFocus?: boolean }) {
  const key = sessionId ?? "";
  const busy = useApp((s) => (sessionId ? !!s.busy[sessionId] : false));
  const online = useApp((s) => s.online);
  const queued = useApp((s) => (sessionId ? (s.queued[sessionId] ?? 0) : 0));
  const insert = useApp((s) => s.composerInsert);
  const workspaceId = useApp((s) => s.workspace?.id ?? "");
  const send = useApp((s) => s.send);
  const cancel = useApp((s) => s.cancel);
  const toast = useApp((s) => s.toast);
  const [text, setText] = useState(() => drafts.get(key) ?? "");
  const [pending, setPending] = useState<Pending[]>([]);
  const [caret, setCaret] = useState(0);
  const [dragging, setDragging] = useState(false);
  const [commands, setCommands] = useState<SlashCommand[]>([]);
  const [files, setFiles] = useState<string[]>([]);
  const [selected, setSelected] = useState(0);
  const [dismissed, setDismissed] = useState(false);
  const [needsArgs, setNeedsArgs] = useState<SlashCommand | null>(null);
  const [voiceActive, setVoiceActive] = useState(false);
  const [workMode, setWorkMode] = useState<WorkMode>("agent");
  const git = useApp((s) => s.git);
  const project = useApp((s) => s.project);
  const [place, setPlace] = useState<{ side: "top" | "bottom"; height: number }>({ side: "top", height: SUGGEST_MAX_HEIGHT });
  const root = useRef<HTMLDivElement>(null);
  const [wrapped, setWrapped] = useState(false);
  const wrapLength = useRef(0);
  const drawn = useRef<ComposerBox | null>(null);
  const glides = useRef<Animation[]>([]);
  const suggestList = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLTextAreaElement>(null);
  const picker = useRef<HTMLInputElement>(null);
  const voice = useRef<{ start(): void }>(null);

  useEffect(() => {
    setText(drafts.get(key) ?? "");
    if (autoFocus) input.current?.focus();
  }, [key, autoFocus]);

  // The composer stays on one row until the text really wraps there, then the
  // input takes the whole width and the controls drop below it. The wider input
  // would fit the same text again, so it only goes back once the text is
  // shorter than where it wrapped; otherwise the two layouts would flip.
  useLayoutEffect(() => {
    const el = input.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
    if (wrapped) {
      if (text.length < wrapLength.current) setWrapped(false);
    } else if (!root.current?.hasAttribute("data-expanded") && el.scrollHeight > ONE_LINE) {
      wrapLength.current = text.length;
      setWrapped(true);
    }
  }, [text, workMode, wrapped]);

  useLayoutEffect(() => {
    const el = input.current;
    if (!el) return;
    let width = el.clientWidth;
    const observer = new ResizeObserver(() => {
      if (width === el.clientWidth) return;
      width = el.clientWidth;
      el.style.height = "auto";
      el.style.height = `${el.scrollHeight}px`;
      if (!root.current?.hasAttribute("data-expanded") && el.scrollHeight > ONE_LINE) {
        wrapLength.current = el.value.length;
        setWrapped(true);
      }
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const update = (value: string) => {
    setText(value);
    setDismissed(false);
    saveDraft(key, value);
  };

  const lastInsert = useRef(insert?.seq ?? 0);
  useEffect(() => {
    if (!insert || insert.seq === lastInsert.current) return;
    lastInsert.current = insert.seq;
    if (insert.target && insert.target !== sessionId) return;
    if (insert.file) {
      const bytes = Uint8Array.from(atob(insert.file.content), (c) => c.charCodeAt(0));
      take([new File([bytes], insert.file.name, { type: insert.file.mime })]);
    } else if (insert.append) {
      const current = input.current?.value ?? "";
      update(`${current}${current.trim() ? "\n\n" : ""}${insert.text}`);
    } else {
      update(insert.text);
    }
    input.current?.focus();
    // `update` is stable in effect: it only touches this composer's state.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [insert]);

  const token = dismissed ? null : activeToken(text, caret);

  useEffect(() => {
    if (token?.kind !== "/" || commands.length > 0 || !workspaceId) return;
    http
      .get<SlashCommand[]>(`/api/workspaces/${encodeURIComponent(workspaceId)}/commands`)
      .then(setCommands)
      .catch(() => {});
  }, [token?.kind, commands.length, workspaceId]);

  useDebounced(token?.kind === "@" ? token.query : null, 90, (query) => {
    if (query === null || !workspaceId) return;
    http
      .get<{ files: string[] }>(`/api/workspaces/${encodeURIComponent(workspaceId)}/files/search?limit=${MAX_FILE_SUGGESTIONS}&q=${encodeURIComponent(query)}`)
      .then((r) => setFiles(r.files ?? []))
      .catch(() => setFiles([]));
  });

  const replaceToken = (value: string) => {
    if (!token) return;
    const next = text.slice(0, token.from) + value + text.slice(caret);
    const at = token.from + value.length;
    update(next);
    setCaret(at);
    requestAnimationFrame(() => {
      input.current?.focus();
      input.current?.setSelectionRange(at, at);
    });
  };

  const runCommand = async (command: SlashCommand, args: Record<string, string> = {}) => {
    try {
      const { prompt } = await http.post<{ prompt: string }>(`/api/workspaces/${encodeURIComponent(workspaceId)}/commands/expand`, { id: command.id, args });
      update(prompt);
      input.current?.focus();
    } catch (err) {
      toast(err instanceof Error ? err.message : String(err), "danger");
    }
  };

  const builtins = useMemo<{ name: string; label: Key; run(): void }[]>(() => {
    const app = useApp.getState;
    return [
      { name: "new", label: "slash.new", run: () => void app().selectSession(null) },
      { name: "compact", label: "slash.compact", run: () => void app().summarize() },
      { name: "model", label: "slash.model", run: () => app().openDialog("models") },
      { name: "mcp", label: "settings.mcp", run: () => app().openSettings("integrations") },
      { name: "settings", label: "slash.settings", run: () => app().openSettings() },
      { name: "help", label: "slash.help", run: () => app().openSettings("shortcuts") },
    ];
  }, []);

  const suggestions: Suggestion[] = useMemo(() => {
    if (!token) return [];
    if (token.kind === "@") {
      return files.map((f) => ({ id: f, label: baseName(f), hint: f, file: true, run: () => replaceToken(`@${f} `) }));
    }
    const q = token.query.toLowerCase();
    const local = builtins
      .filter((b) => b.name.startsWith(q))
      .map((b) => ({
        id: `builtin:${b.name}`,
        label: b.name,
        kind: "builtin",
        hint: t(b.label),
        run: () => {
          update("");
          b.run();
        },
      }));
    const custom = commands
      .filter((c) => c.name.toLowerCase().includes(q))
      .map((c) => ({
        id: c.id,
        label: c.name,
        kind: c.kind,
        hint: c.description ?? t(c.kind === "skill" ? "slash.skill" : c.kind === "workflow" ? "slash.workflow" : "slash.custom"),
        run: () => {
          if (c.arguments?.length) setNeedsArgs(c);
          else void runCommand(c);
        },
      }));
    return [...local, ...custom].slice(0, MAX_COMMAND_SUGGESTIONS);
    // replaceToken and runCommand read the latest text through this memo's deps.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token?.kind, token?.query, token?.from, files, commands, builtins, text, caret]);

  useEffect(() => setSelected(0), [token?.kind, token?.query]);

  // The list opens toward the room there is and scrolls inside it.
  const suggesting = suggestions.length > 0;
  useLayoutEffect(() => {
    if (!suggesting || !root.current) return;
    const box = root.current.getBoundingClientRect();
    const above = box.top - SUGGEST_TOP_LIMIT - SUGGEST_GAP;
    const below = innerHeight - box.bottom - SUGGEST_BOTTOM_LIMIT - SUGGEST_GAP;
    const side = above >= SUGGEST_MIN_HEIGHT || above >= below ? "top" : "bottom";
    setPlace({ side, height: Math.min(token?.kind === "/" ? 292 : SUGGEST_MAX_HEIGHT, side === "top" ? above : below) });
  }, [suggesting, token?.kind]);

  useEffect(() => {
    suggestList.current?.querySelector("[data-selected]")?.scrollIntoView({ block: "nearest" });
  }, [selected]);

  const take = (list: File[]) => {
    const { next, error } = addFiles(pending, list);
    setPending(next);
    if (error) toast(error, "danger");
  };

  const dropFile = (id: string) => {
    setPending((list) => {
      const gone = list.find((p) => p.id === id);
      if (gone?.preview) URL.revokeObjectURL(gone.preview);
      return list.filter((p) => p.id !== id);
    });
  };

  const hasContent = text.trim().length > 0 || pending.length > 0;
  const state = hasContent ? "ready" : busy ? "streaming" : "disabled";

  /** While a turn runs a message is queued, unless `steer` sends it into that turn. */
  const submit = async (steer = false) => {
    if (!hasContent || !online) return;
    const prompt = text;
    const attached = pending;
    if (prompt.startsWith("!") && prompt.length > 1 && attached.length === 0) {
      update("");
      await useApp.getState().runShell(prompt.slice(1).trim(), sessionId ?? undefined);
      return;
    }
    update("");
    setPending([]);
    try {
      const instructions: Partial<Record<WorkMode, string>> = {
        ask: "Answer the question without editing files or running commands that change the system.",
        debug: "Investigate the root cause, reproduce the issue where possible, and validate the fix with relevant checks.",
        multi: "Break the work into clearly scoped tasks, coordinate their dependencies, and validate the integrated result.",
      };
      const body = instructions[workMode] ? `${instructions[workMode]}\n\n${prompt}` : prompt;
      const files = await encode(attached);
      if (busy && sessionId && !steer) {
        useApp.getState().enqueue(sessionId, body, files);
      } else {
        await send(body, files, sessionId ?? undefined);
      }
      for (const p of attached) if (p.preview) URL.revokeObjectURL(p.preview);
    } catch {
      update(prompt);
      setPending(attached);
    }
  };

  const onPaste = (e: ClipboardEvent<HTMLTextAreaElement>) => {
    const list = [...e.clipboardData.files];
    if (list.length > 0) {
      e.preventDefault();
      take(list);
    }
  };

  const onDrop = (e: DragEvent) => {
    e.preventDefault();
    setDragging(false);
    take([...e.dataTransfer.files]);
  };

  const sessionModes = useApp((s) => s.settings?.modes ?? []);
  const sessionMode = useApp((s) => s.settings?.session_mode ?? "");
  const setSessionMode = useApp((s) => s.setSessionMode);
  const selectedMode = WORK_MODES.find((mode) => mode.id === workMode);
  const ModeIcon = selectedMode?.icon;
  const placeholder = busy ? t("composer.followUp") : workMode === "plan" ? t("compose.planPlaceholder") : workMode === "debug" ? t("compose.debugPlaceholder") : workMode === "multi" ? t("compose.multiPlaceholder") : workMode === "ask" ? t("compose.askPlaceholder") : t("composer.placeholder");

  const expanded = voiceActive || /[\r\n]/.test(text) || pending.length > 0 || wrapped;

  // Height and the moved controls glide to their new place instead of jumping.
  useLayoutEffect(() => {
    const el = root.current;
    const field = input.current;
    if (!el || !field) return;
    // Wrapped text on the single row is a state that is never painted.
    if (!expanded && field.scrollHeight > ONE_LINE) return;
    const parts = [el.querySelector<HTMLElement>(".composer-bar"), el.querySelector<HTMLElement>(".composer-add")];
    for (const a of glides.current) a.cancel();
    glides.current = [];
    const frame = el.getBoundingClientRect();
    const now: ComposerBox = {
      height: el.offsetHeight,
      parts: parts.map((part) => {
        const box = part?.getBoundingClientRect();
        return box ? { x: box.left - frame.left, y: box.top - frame.top } : null;
      }),
    };
    const before = drawn.current;
    drawn.current = now;
    if (!before || reducedMotion() || Math.abs(before.height - now.height) < 1) return;
    const timing = { duration: tokenMs("--atlas-dur-moderate", 220), easing: "cubic-bezier(0.2, 0, 0, 1)" };
    glides.current.push(el.animate([{ height: `${before.height}px` }, { height: `${now.height}px` }], timing));
    parts.forEach((part, i) => {
      const from = before.parts[i];
      const to = now.parts[i];
      if (!part || !from || !to || (from.x === to.x && from.y === to.y)) return;
      glides.current.push(part.animate([{ transform: `translate(${from.x - to.x}px, ${from.y - to.y}px)` }, { transform: "none" }], timing));
    });
  }, [text, workMode, expanded]);

  return (
    <>
    <QueueList sessionId={sessionId} />
    <div
      ref={root}
      className="composer"
      data-drag={dragging || undefined}
      data-expanded={expanded || undefined}
      data-voice={voiceActive || undefined}
      onDragOver={(e) => {
        if (!e.dataTransfer.types.includes("Files")) return;
        e.preventDefault();
        setDragging(true);
      }}
      onDragLeave={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node)) setDragging(false);
      }}
      onDrop={onDrop}
    >
      {suggestions.length > 0 && (
        <div className="suggest" ref={suggestList} data-kind={token?.kind === "/" ? "commands" : "files"} data-side={place.side} style={{ maxBlockSize: place.height }} role="listbox" aria-label={t(token?.kind === "@" ? "suggest.files" : "suggest.commands")}>
          {suggestions.map((s, i) => (
            <button
              key={s.id}
              type="button"
              role="option"
              aria-selected={i === selected}
              className="suggest-row"
              data-selected={i === selected || undefined}
              data-divider={i > 0 && s.kind !== "builtin" && suggestions[i - 1]?.kind === "builtin" || undefined}
              onMouseEnter={() => setSelected(i)}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => s.run()}
            >
              {s.file && <IconFile />}
              <span className="suggest-label">{s.label}</span>
              <span className="suggest-hint">{s.hint}</span>
              {i === selected && <span className="suggest-key" aria-hidden="true">↵</span>}
            </button>
          ))}
        </div>
      )}
      {queued > 0 && (
        <div className="queued-chip">
          {queued} {t("composer.queued")}
        </div>
      )}
      {pending.length > 0 && (
        <div className="attachments">
          {pending.map((p) => (
            <div className="attachment" key={p.id} title={p.file.name}>
              {p.preview ? <img src={p.preview} alt="" /> : <IconFile />}
              <span>{p.file.name || "image.png"}</span>
              <button type="button" className="icon-button" data-size="xs" aria-label={t("attach.remove")} onClick={() => dropFile(p.id)}>
                <IconClose />
              </button>
            </div>
          ))}
        </div>
      )}
      <div className="composer-input-row">
      <ContextMenu mode={workMode} attach={() => picker.current?.click()} commands={() => { update("/"); setCaret(1); input.current?.focus(); }} onMode={(mode) => {
        setWorkMode(mode);
        if (mode === "plan") void useApp.getState().setPermissionMode("plan");
        else if (useApp.getState().settings?.permission_mode === "plan") void useApp.getState().setPermissionMode("manual");
        input.current?.focus();
      }} />
      <div className="composer-editor">
      {selectedMode && ModeIcon && <div className="compose-mode-tag" data-mode={workMode}><ModeIcon /><span>{t(selectedMode.label)}</span><button type="button" className="compose-mode-clear" aria-label={t("compose.clearMode")} onClick={() => { setWorkMode("agent"); if (workMode === "plan") void useApp.getState().setPermissionMode("manual"); input.current?.focus(); }}><IconClose /></button></div>}
      <textarea
        ref={input}
        rows={1}
        value={text}
        placeholder={placeholder}
        aria-label={t("composer.placeholder")}
        data-shell={text.startsWith("!") || undefined}
        onChange={(e) => {
          update(e.target.value);
          setCaret(e.target.selectionStart);
        }}
        onSelect={(e) => setCaret(e.currentTarget.selectionStart)}
        onPaste={onPaste}
        onKeyDown={(e) => {
          if (suggestions.length > 0) {
            if (e.key === "ArrowDown" || e.key === "ArrowUp") {
              e.preventDefault();
              const step = e.key === "ArrowDown" ? 1 : -1;
              setSelected((selected + step + suggestions.length) % suggestions.length);
              return;
            }
            if (e.key === "Enter" || e.key === "Tab") {
              e.preventDefault();
              suggestions[selected]?.run();
              return;
            }
            if (e.key === "Escape") {
              e.preventDefault();
              e.stopPropagation();
              setDismissed(true);
              return;
            }
          }
          if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing && !voiceActive) {
            e.preventDefault();
            void submit(e.ctrlKey || e.metaKey);
          }
        }}
      />
      </div>
      <div className="composer-bar">
        <input
          ref={picker}
          type="file"
          multiple
          hidden
          onChange={(e) => {
            take([...(e.target.files ?? [])]);
            e.target.value = "";
          }}
        />
        <ModelChip />
        <VoiceInput startRef={voice} onActive={setVoiceActive} onText={(value) => { const current = input.current?.value ?? ""; update(`${current}${current.trim() ? " " : ""}${value}`); requestAnimationFrame(() => input.current?.focus()); }} />
        {busy && hasContent && (
          <button type="button" className="steer-button" data-action="steer" title={t("queue.steerHint")} aria-label={`${t("composer.steer")}: ${t("queue.steerHint")}`} disabled={voiceActive || !online} onClick={() => void submit(true)}>
            <IconSteer />
            <span>{t("composer.steer")}</span>
          </button>
        )}
        <button
          type="button"
          className="send"
          data-state={state}
          disabled={voiceActive || !online}
          aria-label={state === "streaming" ? t("composer.stop") : state === "disabled" ? t("compose.record") : busy ? t("composer.queue") : t("composer.send")}
          title={state === "streaming" ? t("composer.stop") : state === "disabled" ? t("compose.record") : busy ? `${t("composer.queue")} · ${t("composer.busyHint")}` : t("composer.send")}
          onClick={() => (state === "streaming" ? void cancel(sessionId ?? undefined) : state === "disabled" ? voice.current?.start() : void submit(false))}
        >
          {state === "streaming" ? <IconStop key="stop" className="icon-in stop-icon" /> : state === "disabled" ? <IconWave /> : <IconArrowUp key="send" className="icon-in" />}
        </button>
      </div>
      </div>
      <CommandArguments
        command={needsArgs}
        onClose={() => setNeedsArgs(null)}
        onDone={(values) => {
          const command = needsArgs;
          setNeedsArgs(null);
          if (command) void runCommand(command, values);
        }}
      />
    </div>
    <div className="composer-meta">
      {git && <button type="button" className="composer-meta-project" title={git.branch} onClick={() => useApp.getState().toggleRail("changes")}><IconBranch /><span>{git.branch}</span><IconChevronDown /></button>}
      <button type="button" className="composer-meta-project" title={project} onClick={() => void window.atlas?.reveal(project)}><IconLaptop /><span>{t("compose.local")}</span><IconChevronDown /></button>
      <PermissionChip />
      {sessionModes.length > 1 && (
          <Picker
            label={t("mode.agent")}
            value={sessionMode}
            side="top"
            align="start"
            searchLabel={t("mode.search")}
            emptyLabel={t("sidebar.noResults")}
            onChange={(mode) => void setSessionMode(mode)}
            trigger={
              <button type="button" className="chip-button" title={t("mode.agent")}>
                <span>{sessionMode || t("mode.agentDefault")}</span>
                <IconChevronDown />
              </button>
            }
            options={[{ value: "", label: t("mode.agentDefault") }, ...sessionModes.map((m) => ({ value: m.name, label: m.name, hint: m.description }))]}
          />
        )}
        <span className="composer-gap" />
        <ContextMeter sessionId={sessionId} />
    </div>
    </>
  );
}

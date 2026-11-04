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
import { IconArrowUp, IconChevronDown, IconClose, IconFile, IconPaperclip, IconShield, IconSparkle, IconStop } from "../../primitives/icons";
import { Picker } from "../../primitives/Picker";
import { Menu, Modal, Tip, useDebounced } from "../../primitives/ui";
import { useApp } from "../../stores/app";
import { ProviderLogo } from "../models/ProviderLogo";
import { ContextMeter } from "./ContextMeter";

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
        <button type="button" className="chip-button" data-tone={mode === "bypass" ? "danger" : undefined} title={t("mode.title")}>
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
  const open = useApp((s) => s.openDialog);
  const current = catalog?.active ?? catalog?.selected?.large;
  const name = catalog?.providers?.flatMap((p) => p.models).find((m) => m.id === current?.model)?.name ?? current?.model ?? t("composer.noModel");
  const effort = current?.reasoning_effort;
  const keys = useKeys("models");
  return (
    <Tip label={t("composer.model")} keys={keys}>
      <button type="button" className="chip-button" onClick={() => open("models")}>
        {current?.provider && <ProviderLogo id={current.provider} size={14} />}
        <span>{name}</span>
        {effort && <span className="chip-note">{effort}</span>}
        <IconChevronDown />
      </button>
    </Tip>
  );
}

interface Suggestion {
  id: string;
  label: string;
  hint: string;
  file?: boolean;
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
  const [place, setPlace] = useState<{ side: "top" | "bottom"; height: number }>({ side: "top", height: SUGGEST_MAX_HEIGHT });
  const root = useRef<HTMLDivElement>(null);
  const suggestList = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLTextAreaElement>(null);
  const picker = useRef<HTMLInputElement>(null);

  useEffect(() => {
    setText(drafts.get(key) ?? "");
    if (autoFocus) input.current?.focus();
  }, [key, autoFocus]);

  useLayoutEffect(() => {
    const el = input.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
  }, [text]);

  const update = (value: string) => {
    setText(value);
    setDismissed(false);
    saveDraft(key, value);
  };

  const lastInsert = useRef(insert?.seq ?? 0);
  useEffect(() => {
    if (!insert || insert.seq === lastInsert.current) return;
    lastInsert.current = insert.seq;
    update(insert.text);
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
        label: `/${b.name}`,
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
        label: `/${c.name}`,
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
    setPlace({ side, height: Math.min(SUGGEST_MAX_HEIGHT, side === "top" ? above : below) });
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

  const submit = async () => {
    if (!hasContent || !online) return;
    const prompt = text;
    const attached = pending;
    if (prompt.startsWith("!") && prompt.length > 1 && attached.length === 0) {
      update("");
      await useApp.getState().runShell(prompt.slice(1).trim());
      return;
    }
    update("");
    setPending([]);
    try {
      await send(prompt, await encode(attached));
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

  return (
    <div
      ref={root}
      className="composer"
      data-drag={dragging || undefined}
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
        <div className="suggest" ref={suggestList} data-side={place.side} style={{ maxBlockSize: place.height }} role="listbox" aria-label={t(token?.kind === "@" ? "suggest.files" : "suggest.commands")}>
          {suggestions.map((s, i) => (
            <button
              key={s.id}
              type="button"
              role="option"
              aria-selected={i === selected}
              className="suggest-row"
              data-selected={i === selected || undefined}
              onMouseEnter={() => setSelected(i)}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => s.run()}
            >
              {s.file ? <IconFile /> : <IconSparkle />}
              <span className="suggest-label">{s.label}</span>
              <span className="suggest-hint">{s.hint}</span>
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
      <textarea
        ref={input}
        rows={1}
        value={text}
        placeholder={busy ? t("composer.followUp") : t("composer.placeholder")}
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
          if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault();
            void submit();
          }
        }}
      />
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
        <Tip label={t("attach.add")}>
          <button type="button" className="icon-button" aria-label={t("attach.add")} onClick={() => picker.current?.click()}>
            <IconPaperclip />
          </button>
        </Tip>
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
        <ModelChip />
        <span className="composer-gap" />
        <ContextMeter sessionId={sessionId} />
        <button
          type="button"
          className="send"
          data-state={state}
          disabled={state === "disabled" || (state === "ready" && !online)}
          aria-label={state === "streaming" ? t("composer.stop") : t("composer.send")}
          title={state === "streaming" ? t("composer.stop") : t("composer.send")}
          onClick={() => (state === "streaming" ? void cancel() : void submit())}
        >
          {state === "streaming" ? <IconStop key="stop" className="icon-in stop-icon" /> : <IconArrowUp key="send" className="icon-in" />}
        </button>
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
  );
}

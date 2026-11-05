// One user message and everything the agent did for it. While it runs the
// steps flow in the open; once done, long turns fold under "Worked for 2m 34s"
// and only the closing answer and the changed files stay visible.

import { memo, useEffect, useLayoutEffect, useRef, useState } from "react";
import type { Message } from "../../api/types";
import { baseName, clock, duration } from "../../lib/format";
import { lang, t } from "../../lib/i18n";
import { category } from "../../lib/tools";
import { finalTextStart, toolCalls, userAttachments, userFiles, userText, type Step, type Turn, type UserFile } from "../../lib/turns";
import { IconAlert, IconChevron, IconFile, IconPencil, IconRetry, IconRewind } from "../../primitives/icons";
import { Collapse, Shimmer, useElapsed } from "../../primitives/motion";
import { Pop, Tip } from "../../primitives/ui";
import { useApp } from "../../stores/app";
import { CopyButton, Markdown } from "./Markdown";
import { SiteIcon } from "./SiteIcon";
import { ThinkingRow, ToolGroupRow, ToolRow } from "./ToolRow";

const FOLD_MIN_TOOLS = 3;
const FOLD_MIN_MS = 15_000;
const STUCK_HEIGHT = 34;
const EDITOR_MAX_HEIGHT = 320;

interface BubbleProps {
  text: string;
  files?: UserFile[];
  pendingFiles?: number;
  enter?: boolean;
  /** The stored message; without it the bubble offers no actions. */
  message?: Message;
}

function FileList({ files, pending = 0 }: { files: UserFile[]; pending?: number }) {
  return (
    <div className="user-files">
      {files.map((f, i) =>
        f.image ? (
          <img key={i} className="user-image" src={f.image} alt={f.name} />
        ) : (
          <span key={i} className="user-file">
            <IconFile />
            {f.name}
          </span>
        ),
      )}
      {pending > 0 && (
        <span className="user-file">
          <IconFile />
          {pending}
        </span>
      )}
    </div>
  );
}

/** The message turned into an input. Sending branches the chat from here. */
function MessageEditor({ text, files, onCancel, onSend }: { text: string; files: UserFile[]; onCancel(): void; onSend(text: string): void }) {
  const [value, setValue] = useState(text);
  const input = useRef<HTMLTextAreaElement>(null);
  const empty = !value.trim() && files.length === 0;

  useLayoutEffect(() => {
    const el = input.current;
    if (!el) return;
    el.style.blockSize = "auto";
    el.style.blockSize = `${Math.min(el.scrollHeight, EDITOR_MAX_HEIGHT)}px`;
  }, [value]);

  useEffect(() => {
    const el = input.current;
    el?.focus();
    el?.setSelectionRange(el.value.length, el.value.length);
  }, []);

  return (
    <div className="user-edit">
      {files.length > 0 && <FileList files={files} />}
      <textarea
        ref={input}
        rows={1}
        value={value}
        aria-label={t("edit.action")}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            e.preventDefault();
            e.stopPropagation();
            onCancel();
          } else if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault();
            if (!empty) onSend(value);
          }
        }}
      />
      <div className="user-edit-bar">
        <span className="user-edit-hint">{t("edit.hint")}</span>
        <button type="button" className="button" onClick={onCancel}>
          {t("common.cancel")}
        </button>
        <button type="button" className="button" data-variant="primary" disabled={empty} onClick={() => onSend(value)}>
          {t("edit.send")}
        </button>
      </div>
    </div>
  );
}

export function UserBubble({ text, files = [], pendingFiles = 0, enter, message }: BubbleProps) {
  const row = useRef<HTMLDivElement>(null);
  const sentinel = useRef<HTMLDivElement>(null);
  const bubble = useRef<HTMLDivElement>(null);
  const body = useRef<HTMLDivElement>(null);
  const atTopRef = useRef(false);
  const [atTop, setAtTop] = useState(false);
  const [saved, setSaved] = useState(0);
  const [editing, setEditing] = useState(false);
  const [clipped, setClipped] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const askRewind = useApp((s) => s.askRewind);
  const editMessage = useApp((s) => s.editMessage);
  const busy = useApp((s) => !!s.currentId && !!s.busy[s.currentId]);
  const stuck = atTop && !editing;

  // The bubble shrinks to one line once it sticks to the top of the thread.
  // The height it gives up moves into its margin, so the thread below stays
  // put; otherwise the shrink itself would unstick it and it would flicker.
  useEffect(() => {
    const el = sentinel.current;
    const root = el?.closest(".thread-scroll");
    if (!el || !root) return;
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (!entry) return;
        const rootTop = entry.rootBounds?.top ?? 0;
        const top = entry.boundingClientRect.top;
        const stuck = atTopRef.current;
        // Stay collapsed until the sentinel is clearly back in view. A
        // one-pixel anchoring nudge used to flip the bubble, change its
        // height, and shake the thread so a scroll never landed.
        const next = stuck ? top <= rootTop + 24 : top < rootTop - 1 && !entry.isIntersecting;
        if (next === stuck) return;
        if (next) setSaved(Math.max(0, (bubble.current?.offsetHeight ?? 0) - STUCK_HEIGHT));
        atTopRef.current = next;
        setAtTop(next);
      },
      { root },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  // Long text folds to a few lines. An off-screen turn has no layout yet, so
  // the size is watched rather than read once.
  useEffect(() => {
    const el = body.current;
    if (!el) return;
    const observer = new ResizeObserver(() => {
      if (!el.hasAttribute("data-open")) setClipped(el.scrollHeight > el.clientHeight + 1);
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [text, editing]);

  if (editing && message) {
    return (
      <>
        <div ref={sentinel} className="user-sentinel" />
        <div className="user-row" ref={row} data-editing="">
          <MessageEditor
            text={text}
            files={files}
            onCancel={() => setEditing(false)}
            onSend={(next) => {
              setEditing(false);
              void editMessage(message.id, { text: next, attachments: userAttachments(message) });
            }}
          />
        </div>
      </>
    );
  }

  return (
    <>
      <div ref={sentinel} className="user-sentinel" />
      <div className="user-row" ref={row} data-stuck={stuck || undefined} style={stuck ? { marginBlockEnd: `calc(${saved}px - var(--atlas-space-2))` } : undefined}>
        {message && !stuck && (
          <div className="user-actions">
            <Tip label={t("rewind.action")}>
              <button type="button" className="icon-button" data-size="sm" aria-label={t("rewind.action")} onClick={() => void askRewind(message.id)}>
                <IconRewind />
              </button>
            </Tip>
            {!busy && (
              <Tip label={t("edit.action")}>
                <button type="button" className="icon-button" data-size="sm" aria-label={t("edit.action")} onClick={() => setEditing(true)}>
                  <IconPencil />
                </button>
              </Tip>
            )}
            <CopyButton text={text} />
          </div>
        )}
        <div
          ref={bubble}
          className="user-bubble"
          data-enter={enter || undefined}
          onClick={() => {
            if (stuck) row.current?.closest(".turn")?.scrollIntoView({ block: "start", behavior: "smooth" });
          }}
        >
          {(files.length > 0 || pendingFiles > 0) && !stuck && <FileList files={files} pending={pendingFiles} />}
          <div ref={body} className="user-text" data-open={expanded || undefined}>
            {text}
          </div>
          {(clipped || expanded) && !stuck && (
            <button type="button" className="user-more" aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>
              {t(expanded ? "thread.showLess" : "thread.showMore")}
            </button>
          )}
        </div>
      </div>
    </>
  );
}

export function Working({ from }: { from: number }) {
  const elapsed = useElapsed(from);
  return (
    <div className="working" role="status">
      <Shimmer text={t("thread.working")} />
      {elapsed >= 2000 && <span className="working-time">{clock(elapsed)}</span>}
    </div>
  );
}

function StepView({ step, active, last, waiting, showThinking }: { step: Step; active: boolean; last: boolean; waiting: Set<string>; showThinking: boolean }) {
  switch (step.kind) {
    case "text":
      return <Markdown text={step.text} streaming={active && last} />;
    case "thinking":
      return showThinking ? <ThinkingRow text={step.text} ms={step.ms} live={active && step.open} /> : null;
    case "tool":
      return <ToolRow call={step.call} result={step.result} active={active} waiting={waiting.has(step.call.id)} />;
    case "group":
      return <ToolGroupRow cat={step.category} calls={step.calls} active={active} waiting={waiting} />;
    case "shell":
      return (
        <div className="tool-body">
          <pre className="tool-command">$ {step.command}</pre>
          {step.output && <pre className="tool-output">{step.output}</pre>}
        </div>
      );
  }
}

/** "3 edits, 9 files read, 1 command". */
function summarize(turn: Turn): string {
  const counts: Record<string, number> = {};
  for (const { call } of toolCalls(turn.steps)) {
    const cat = category(call.name);
    counts[cat] = (counts[cat] ?? 0) + 1;
  }
  const tr = lang === "tr";
  const parts: string[] = [];
  const add = (cat: string, trLabel: string, enLabel: string) => {
    const n = counts[cat];
    if (n) parts.push(`${n} ${tr ? trLabel : enLabel}`);
  };
  add("edit", "düzenleme", "edits");
  add("write", "yeni dosya", "new files");
  add("read", "dosya okundu", "files read");
  add("search", "arama", "searches");
  add("shell", "komut", "commands");
  add("test", "kontrol", "checks");
  add("web", "web araması", "web searches");
  add("fetch", "sayfa", "pages");
  add("agent", "alt ajan", "subagents");
  return parts.join(", ");
}

function FilesChanged({ turn }: { turn: Turn }) {
  const toggleRail = useApp((s) => s.toggleRail);
  const railOpen = useApp((s) => s.ui.railOpen && s.ui.railTab === "changes");
  const n = turn.changes.length;
  return (
    <div className="files-card">
      <button type="button" className="files-head" onClick={() => !railOpen && toggleRail("changes")}>
        <span>
          {n} {n === 1 ? t("thread.fileChanged") : t("thread.filesChanged")}
        </span>
        <IconChevron className="chevron" />
      </button>
      {turn.changes.map((f) => (
        <div className="files-row" key={f.path} title={f.path}>
          <span className="files-name">{baseName(f.path)}</span>
          {!!f.additions && <span className="add">+{f.additions}</span>}
          {!!f.removals && <span className="del">−{f.removals}</span>}
        </div>
      ))}
    </div>
  );
}

function Sources({ turn }: { turn: Turn }) {
  const shown = turn.sources.slice(0, 3);
  return (
    <Pop
      className="sources"
      side="top"
      trigger={
        <button type="button" className="sources-chip">
          <span className="sources-stack">
            {shown.map((s) => (
              <SiteIcon key={s.url} host={s.host} />
            ))}
          </span>
          {turn.sources.length} {t("thread.sources")}
        </button>
      }
    >
      <div className="sources-list">
        {turn.sources.map((s) => (
          <button key={s.url} type="button" className="source-row" title={s.url} onClick={() => void window.atlas?.openExternal(s.url)}>
            <SiteIcon host={s.host} />
            <span className="source-title">{s.title}</span>
            <span className="source-host">{s.host}</span>
          </button>
        ))}
      </div>
    </Pop>
  );
}

function answerText(turn: Turn): string {
  return turn.steps
    .slice(finalTextStart(turn.steps))
    .flatMap((s) => (s.kind === "text" ? [s.text] : []))
    .join("\n\n");
}

function modelName(id: string): string {
  const catalog = useApp.getState().catalog;
  return catalog?.providers?.flatMap((p) => p.models).find((m) => m.id === id)?.name ?? id;
}

function TurnMeta({ turn, worked, last }: { turn: Turn; worked: number; last: boolean }) {
  const send = useApp((s) => s.send);
  const answer = answerText(turn);
  const prompt = turn.user ? userText(turn.user) : "";
  return (
    <div className="turn-meta">
      {answer && <CopyButton text={answer} />}
      {last && prompt && (
        <Tip label={t("thread.retry")}>
          <button type="button" className="icon-button" data-size="sm" aria-label={t("thread.retry")} onClick={() => void send(prompt)}>
            <IconRetry />
          </button>
        </Tip>
      )}
      {turn.sources.length > 0 && <Sources turn={turn} />}
      <span className="turn-meta-text">
        {turn.model && modelName(turn.model)}
        {turn.model && worked >= 1000 && " · "}
        {worked >= 1000 && duration(worked)}
      </span>
    </div>
  );
}

/** The conversation was folded into a summary to free context. */
function Compaction({ turn, running }: { turn: Turn; running: boolean }) {
  const [open, setOpen] = useState(false);
  const summary = turn.steps.flatMap((s) => (s.kind === "text" ? [s.text] : [])).join("\n\n");
  return (
    <div className="compaction-wrap">
      <div className="compaction" data-running={running || undefined} role="status">
        <span className="compaction-line" />
        <span className="compaction-label">{running ? t("compact.running") : t("compact.done")}</span>
        {!running && summary && (
          <button type="button" className="compaction-link" aria-expanded={open} onClick={() => setOpen(!open)}>
            {t("compact.show")}
          </button>
        )}
        <span className="compaction-line" />
      </div>
      <Collapse open={open}>{open && <Markdown text={summary} streaming={false} />}</Collapse>
    </div>
  );
}

export function CompactionRunning() {
  return (
    <div className="compaction" data-running="" role="status">
      <span className="compaction-line" />
      <span className="compaction-label">{t("compact.running")}</span>
      <span className="compaction-line" />
    </div>
  );
}

interface TurnProps {
  turn: Turn;
  index: number;
  active: boolean;
  last: boolean;
  waiting: Set<string>;
  unfoldAll: boolean;
}

function TurnBase({ turn, index, active, last, waiting, unfoldAll }: TurnProps) {
  const foldTurns = useApp((s) => s.ui.foldTurns);
  const showThinking = useApp((s) => s.ui.showThinking);
  const [unfolded, setUnfolded] = useState<boolean | null>(null);
  const worked = turn.endedAt - turn.startedAt;
  const foldable = foldTurns && !active && (turn.tools >= FOLD_MIN_TOOLS || (turn.tools > 0 && worked >= FOLD_MIN_MS));
  const finalAt = finalTextStart(turn.steps);
  // A turn that ended without an answer keeps its steps in view: they are the
  // only account of what happened.
  const open = unfolded ?? (unfoldAll || finalAt === turn.steps.length);
  const failed = turn.finish?.reason === "error";
  const stopped = turn.finish?.reason === "canceled";

  if (turn.summary) return <Compaction turn={turn} running={active} />;

  const renderSteps = (steps: Step[]) =>
    steps.map((step, i) => <StepView key={step.id} step={step} active={active} last={i === steps.length - 1} waiting={waiting} showThinking={showThinking} />);

  return (
    <section className="turn" data-turn={index}>
      {turn.user && userText(turn.user) && <UserBubble text={userText(turn.user)} files={userFiles(turn.user)} message={active ? undefined : turn.user} />}
      <div className="turn-body">
        {foldable ? (
          <>
            <button type="button" className="worked" aria-expanded={open} onClick={() => setUnfolded(!open)}>
              {worked >= 1000 && <span className="worked-time">{lang === "tr" ? `${duration(worked)} ${t("thread.worked")}` : `${t("thread.worked")} ${duration(worked)}`}</span>}
              <span className="worked-summary">{summarize(turn)}</span>
              <IconChevron className="chevron" />
            </button>
            <Collapse open={open}>{open && <div className="worked-steps">{renderSteps(turn.steps.slice(0, finalAt))}</div>}</Collapse>
            {renderSteps(turn.steps.slice(finalAt))}
          </>
        ) : (
          renderSteps(turn.steps)
        )}
        {active && <Working from={turn.startedAt} />}
        {!active && stopped && <div className="note-inline">{t("thread.stopped")}</div>}
        {!active && failed && (
          <div className="turn-error" role="alert">
            <IconAlert />
            <div>
              <div>{turn.finish?.message || t("tool.error")}</div>
              {turn.finish?.details && <div className="turn-error-details">{turn.finish.details}</div>}
            </div>
          </div>
        )}
        {!active && turn.changes.length > 0 && <FilesChanged turn={turn} />}
        {!active && turn.model && <TurnMeta turn={turn} worked={worked} last={last} />}
      </div>
    </section>
  );
}

const sameMessages = (a: TurnProps, b: TurnProps) =>
  a.active === b.active &&
  a.last === b.last &&
  a.index === b.index &&
  a.unfoldAll === b.unfoldAll &&
  a.waiting === b.waiting &&
  a.turn.messages.length === b.turn.messages.length &&
  a.turn.messages.every((m, i) => m === b.turn.messages[i]);

export const TurnView = memo(TurnBase, sameMessages);

// A tool call as one quiet line of text: a verb and a dim argument. Details
// open on click; errors open by themselves.

import { structuredPatch } from "diff";
import { memo, useMemo, useRef, useState } from "react";
import type { ToolCall, ToolResult } from "../../api/types";
import { clock, duration } from "../../lib/format";
import { languageOfFile } from "../../lib/highlight";
import { t } from "../../lib/i18n";
import { category, groupLabel, isDenied, toolArg, toolCommand, toolFile, toolMeta, toolVerb, type ToolMeta } from "../../lib/tools";
import { IconAlert, IconBan, IconChevron } from "../../primitives/icons";
import { Collapse, Shimmer, useElapsed } from "../../primitives/motion";
import { Code } from "./CodeView";
import { Markdown } from "./Markdown";

type State = "running" | "waiting" | "success" | "error" | "denied" | "stopped";

const MAX_PREVIEW_LINES = 40;

interface DiffLine {
  kind: "add" | "del" | "ctx" | "hunk";
  text: string;
}

function diffLines(meta: ToolMeta): DiffLine[] {
  if (meta.old_content !== undefined || meta.new_content !== undefined) {
    const patch = structuredPatch("", "", meta.old_content ?? "", meta.new_content ?? "", "", "", { context: 2 });
    return patch.hunks.flatMap((hunk) => [
      { kind: "hunk" as const, text: `@@ ${hunk.oldStart} @@` },
      ...hunk.lines.map((line): DiffLine => ({ kind: line[0] === "+" ? "add" : line[0] === "-" ? "del" : "ctx", text: line.slice(1) })),
    ]);
  }
  return (meta.diff ?? "")
    .split("\n")
    .filter((line) => !line.startsWith("---") && !line.startsWith("+++") && !line.startsWith("Index:") && !line.startsWith("==="))
    .map((line): DiffLine => {
      if (line.startsWith("@@")) return { kind: "hunk", text: line };
      if (line.startsWith("+")) return { kind: "add", text: line.slice(1) };
      if (line.startsWith("-")) return { kind: "del", text: line.slice(1) };
      return { kind: "ctx", text: line.slice(1) };
    });
}

export function Diff({ meta, full }: { meta: ToolMeta; full?: boolean }) {
  const lines = useMemo(() => diffLines(meta), [meta]);
  const shown = full ? lines : lines.slice(0, MAX_PREVIEW_LINES);
  return (
    <div className="diff">
      {shown.map((line, i) => (
        <div key={i} data-kind={line.kind}>
          {line.text || " "}
        </div>
      ))}
      {lines.length > shown.length && (
        <div data-kind="hunk">
          {lines.length - shown.length} {t("thread.moreLines")}
        </div>
      )}
    </div>
  );
}

function Preview({ text, language = "" }: { text: string; language?: string }) {
  const lines = text.split("\n");
  const shown = lines.slice(0, MAX_PREVIEW_LINES).join("\n");
  const rest = lines.length - MAX_PREVIEW_LINES;
  return (
    <pre className="tool-output">
      {language ? <Code code={shown} language={language} delay={0} /> : shown}
      {rest > 0 && `\n… ${rest} ${t("thread.moreLines")}`}
    </pre>
  );
}

/** The view tool numbers its lines and wraps them in tags; the file is what reads. */
function viewedText(content: string): string {
  return content
    .replace(/^<file>\n?/, "")
    .replace(/\n?<\/file>[\s\S]*$/, "")
    .split("\n")
    .map((line) => line.replace(/^\s*\d+\|/, ""))
    .join("\n");
}

function Body({ call, result, meta }: { call: ToolCall; result?: ToolResult; meta: ToolMeta }) {
  const cat = category(call.name);
  if ((cat === "edit" || cat === "write") && !result?.is_error && (meta.diff || meta.new_content !== undefined)) return <Diff meta={meta} />;
  if (cat === "shell") {
    const command = toolCommand(call);
    return (
      <>
        {command && <pre className="tool-command">$ {command}</pre>}
        {(meta.output ?? result?.content) ? <Preview text={meta.output ?? result?.content ?? ""} /> : null}
      </>
    );
  }
  if (!result?.content) return null;
  if (cat === "read" && !result.is_error) return <Preview text={viewedText(result.content)} language={languageOfFile(toolFile(call))} />;
  return <Preview text={result.content} />;
}

/** Shows after two seconds, so quick calls never flash a number. */
function LiveTime({ from }: { from: number }) {
  const elapsed = useElapsed(from);
  return elapsed >= 2000 ? <span className="tool-time">{clock(elapsed)}</span> : null;
}

export const ToolRow = memo(function ToolRow({ call, result, active, waiting }: { call: ToolCall; result?: ToolResult; active: boolean; waiting: boolean }) {
  const state: State = waiting ? "waiting" : result ? (isDenied(result) ? "denied" : result.is_error ? "error" : "success") : active ? "running" : "stopped";
  const [open, setOpen] = useState<boolean | null>(null);
  const expanded = open ?? state === "error";
  const meta = useMemo(() => toolMeta(result), [result]);
  const verb = toolVerb(call.name, state !== "running" && state !== "waiting");
  const arg = toolArg(call);
  const expandable = !!result || !!toolCommand(call);
  const exit = meta.exit_code;
  const started = useRef(Date.now());

  return (
    <div className="tool" data-state={state}>
      <button type="button" className="tool-row" aria-expanded={expanded} disabled={!expandable} onClick={() => setOpen(!expanded)}>
        {state === "error" && <IconAlert className="tool-ind" />}
        {state === "denied" && <IconBan className="tool-ind" />}
        <span className="tool-verb">{state === "running" ? <Shimmer text={verb} /> : verb}</span>
        {arg && <span className="tool-arg">{arg}</span>}
        <span className="tool-meta">
          {state === "running" && <LiveTime from={started.current} />}
          {!!meta.additions && <span className="add">+{meta.additions}</span>}
          {!!meta.removals && <span className="del">−{meta.removals}</span>}
          {exit !== undefined && exit !== 0 && <span className="chip" data-tone="danger">exit {exit}</span>}
          {state === "waiting" && <span className="chip" data-tone="warning">{t("tool.waiting")}</span>}
          {state === "denied" && <span className="chip">{t("tool.denied")}</span>}
          {state === "stopped" && <span className="chip">{t("tool.stopped")}</span>}
          {state === "error" && <span className="chip" data-tone="danger">{t("tool.error")}</span>}
        </span>
        {expandable && <IconChevron className="chevron" />}
      </button>
      <Collapse open={expanded}>{expanded && <div className="tool-body"><Body call={call} result={result} meta={meta} /></div>}</Collapse>
    </div>
  );
});

interface GroupCall {
  id: string;
  call: ToolCall;
  result?: ToolResult;
}

/** Calls made at once: one line with a count, each call one step in. */
export const ToolGroupRow = memo(function ToolGroupRow({ cat, calls, active, waiting }: { cat: string; calls: GroupCall[]; active: boolean; waiting: Set<string> }) {
  const done = calls.filter((c) => c.result).length;
  const failed = calls.some((c) => c.result?.is_error);
  const running = active && done < calls.length;
  const [open, setOpen] = useState<boolean | null>(null);
  const expanded = open ?? (failed || calls.some((c) => waiting.has(c.call.id)));
  const ordered = failed ? [...calls].sort((a, b) => Number(!!b.result?.is_error) - Number(!!a.result?.is_error)) : calls;
  const label = groupLabel(cat, calls[0].call.name, calls.length, !running);

  return (
    <div className="tool" data-state={failed ? "error" : running ? "running" : "success"}>
      <button type="button" className="tool-row" aria-expanded={expanded} onClick={() => setOpen(!expanded)}>
        {failed && <IconAlert className="tool-ind" />}
        <span className="tool-verb">{running ? <Shimmer text={label} /> : label}</span>
        <span className="tool-meta">{running && <span className="tool-time">{done}/{calls.length}</span>}</span>
        <IconChevron className="chevron" />
      </button>
      <Collapse open={expanded}>
        {expanded && (
          <div className="tool-group">
            {ordered.map((c) => (
              <ToolRow key={c.id} call={c.call} result={c.result} active={active} waiting={waiting.has(c.call.id)} />
            ))}
          </div>
        )}
      </Collapse>
    </div>
  );
});

export function ThinkingRow({ text, ms, live }: { text: string; ms: number; live: boolean }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="tool" data-state={live ? "running" : "success"}>
      <button type="button" className="tool-row" aria-expanded={open} onClick={() => setOpen(!open)}>
        <span className="tool-verb">{live ? <Shimmer text={t("thread.thinking")} /> : t("thread.thought")}</span>
        {!live && ms >= 1000 && <span className="tool-arg">{duration(ms)}</span>}
        <span className="tool-meta" />
        <IconChevron className="chevron" />
      </button>
      <Collapse open={open}>
        {open && (
          <div className="tool-body thinking-body">
            <Markdown text={text} streaming={false} />
          </div>
        )}
      </Collapse>
    </div>
  );
}

// One step of a turn, drawn by its kind. A call that hands work to a
// sub-agent opens onto what that sub-agent did, step by step, with the same
// rows the main chat uses: it is its own session on the server, so its
// messages are loaded and kept live like any chat's.

import { useEffect, useMemo, useRef, useState } from "react";
import type { ToolCall, ToolResult } from "../../api/types";
import { t } from "../../lib/i18n";
import { agentSessionId, feedOf, latestActivity, MAX_AGENT_DEPTH, runDuration, runReport, startedInBackground, stepCount, type RunReport } from "../../lib/subagent";
import { category } from "../../lib/tools";
import type { Step } from "../../lib/turns";
import { ensureMessages, refreshMessages, useApp } from "../../stores/app";
import { Markdown } from "./Markdown";
import { ThinkingRow, ToolGroupRow, ToolRow } from "./ToolRow";

interface StepProps {
  step: Step;
  active: boolean;
  last: boolean;
  waiting: Set<string>;
  showThinking: boolean;
  /** How many sub-agents this step is already inside. */
  depth?: number;
}

export function StepView({ step, active, last, waiting, showThinking, depth = 0 }: StepProps) {
  switch (step.kind) {
    case "text":
      return <Markdown text={step.text} streaming={active && last} />;
    case "thinking":
      return showThinking ? <ThinkingRow text={step.text} ms={step.ms} live={active && step.open} /> : null;
    case "tool":
      if (category(step.call.name) === "agent" && depth < MAX_AGENT_DEPTH) {
        return <AgentRow stepId={step.id} call={step.call} result={step.result} active={active} waiting={waiting} depth={depth} />;
      }
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

/** The sub-agent's steps, from the messages kept for its session. */
function useFeed(sid: string) {
  const messages = useApp((s) => s.messages[sid]);
  const steps = useMemo(() => feedOf(messages), [messages]);
  return { loaded: messages !== undefined, steps };
}

interface AgentRowProps {
  stepId: string;
  call: ToolCall;
  result?: ToolResult;
  active: boolean;
  waiting: Set<string>;
  depth: number;
}

/** What a finished row says: how many steps, and what the server measured of the run. */
function finishedHint(count: number, report: RunReport | null): string {
  const parts: string[] = [];
  if (count > 0) parts.push(t(count === 1 ? "subagent.step" : "subagent.steps").replace("{0}", String(count)));
  if (report) {
    const took = runDuration(report.durationMs);
    if (took) parts.push(took);
    if (report.filesChanged > 0) parts.push(t(report.filesChanged === 1 ? "subagent.file" : "subagent.files").replace("{0}", String(report.filesChanged)));
  }
  return parts.join(" · ");
}

/** A delegated call: its row, open while the sub-agent works, with the work beneath. */
function AgentRow({ stepId, call, result, active, waiting, depth }: AgentRowProps) {
  const sid = agentSessionId(stepId, call.id);
  const running = active && !result;
  const { loaded, steps } = useFeed(sid);

  // Its work is read as soon as its row is on screen: while it works the row says
  // what it is doing, and once it is done the row says how many steps it took.
  useEffect(() => {
    void ensureMessages(sid).catch(() => {});
  }, [sid]);

  // Updates that arrived before the first load are not replayed, so a
  // sub-agent that just ended is read once more to be complete.
  const wasRunning = useRef(running);
  useEffect(() => {
    if (wasRunning.current && !running && loaded) void refreshMessages(sid).catch(() => {});
    wasRunning.current = running;
  }, [running, loaded, sid]);

  const count = stepCount(steps);
  const background = startedInBackground(result?.content);
  const hint = running
    ? latestActivity(steps)
    : background
      ? [t("subagent.background"), finishedHint(count, null)].filter(Boolean).join(" · ")
      : finishedHint(count, runReport(result?.metadata));
  return (
    <ToolRow
      call={call}
      result={result}
      active={active}
      waiting={waiting.has(call.id)}
      detail={<AgentFeed sid={sid} running={running} result={result} waiting={waiting} depth={depth} />}
      hint={hint}
      defaultOpen={running}
    />
  );
}

interface FeedProps {
  sid: string;
  running: boolean;
  result?: ToolResult;
  waiting: Set<string>;
  depth: number;
}

/** The sub-agent's steps in a box that follows the newest one while it works. */
function AgentFeed({ sid, running, result, waiting, depth }: FeedProps) {
  const { loaded, steps } = useFeed(sid);
  const box = useRef<HTMLDivElement>(null);
  const near = useRef(true);
  const showThinking = useApp((s) => s.ui.showThinking);
  const [missing, setMissing] = useState(false);

  // Opened, so its work is needed whether or not it is still running.
  useEffect(() => {
    setMissing(false);
    void ensureMessages(sid).catch(() => setMissing(true));
  }, [sid]);

  useEffect(() => {
    const el = box.current;
    if (el && running && near.current) el.scrollTop = el.scrollHeight;
  }, [steps, running]);

  if (steps.length === 0) {
    // A sub-agent that has just started has no session yet; that is not "nothing kept".
    if (running || (!loaded && !missing)) return <div className="subagent-note" role="status">{t("subagent.loading")}</div>;
    // Nothing kept for it (an old chat, or its session was deleted): its answer is what is left.
    return result?.content ? <Markdown text={result.content} streaming={false} /> : <div className="subagent-note">{t("subagent.empty")}</div>;
  }
  return (
    <div
      ref={box}
      className="subagent"
      data-running={running || undefined}
      onScroll={(e) => {
        const el = e.currentTarget;
        near.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
      }}
    >
      {steps.map((step, i) => (
        <StepView key={step.id} step={step} active={running} last={i === steps.length - 1} waiting={waiting} showThinking={showThinking} depth={depth + 1} />
      ))}
    </div>
  );
}

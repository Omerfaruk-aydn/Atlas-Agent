// What a sub-agent's work needs on this side. The server keeps each one as a
// session of its own, named after the message and the call that started it;
// its messages are ordinary messages, so the same step folding that draws a
// turn draws a sub-agent. Pure, so it is tested on its own.

import type { Message, ToolCall } from "../api/types";
import { buildTurns, toolCalls, type Step } from "./turns";
import { toolArg, toolVerb } from "./tools";

/** How deep sub-agents are drawn inside sub-agents; deeper ones stay a single line. */
export const MAX_AGENT_DEPTH = 3;

/**
 * The session a delegated call ran in: "<message id>$$<call id>", where the
 * message is the one that holds the call. A step id is "<message id>:<part>".
 */
export function agentSessionId(stepId: string, callId: string): string {
  return `${stepId.split(":")[0]}$$${callId}`;
}

/** What the sub-agent did, in order, without the task it was given. */
export function feedOf(messages: Message[] | undefined): Step[] {
  if (!messages?.length) return [];
  return buildTurns(messages).flatMap((turn) => turn.steps);
}

/** How many tool calls the sub-agent made, parallel groups opened up. */
export function stepCount(steps: Step[]): number {
  return toolCalls(steps).length;
}

/** The newest thing it is doing or did: "Reading main.go", for a collapsed row. */
export function latestActivity(steps: Step[]): string {
  const last = toolCalls(steps).at(-1);
  if (!last) return "";
  return activityLine(last.call, !!last.result);
}

export function activityLine(call: ToolCall, done: boolean): string {
  const verb = toolVerb(call.name, done);
  const arg = toolArg(call);
  return arg ? `${verb} ${arg}` : verb;
}

/** What the server measured of one finished run, from the call's result metadata. */
export interface RunReport {
  durationMs: number;
  costUsd: number;
  filesChanged: number;
}

/** True when the call only started the sub-agent: its answer is read later with agent_result. */
export function startedInBackground(content: string | undefined): boolean {
  return /^Started in the background as agent-[0-9a-f]{8}(?![0-9a-z])/.test(content ?? "");
}

/** Reads the report out of a result's metadata; null when there is none or it is unreadable. */
export function runReport(metadata: string | undefined): RunReport | null {
  if (!metadata) return null;
  try {
    const run = (JSON.parse(metadata) as { run?: { duration_ms?: number; cost_usd?: number; files_changed?: string[] } }).run;
    if (!run) return null;
    return { durationMs: Math.max(0, run.duration_ms ?? 0), costUsd: Math.max(0, run.cost_usd ?? 0), filesChanged: run.files_changed?.length ?? 0 };
  } catch {
    return null;
  }
}

/** "1m 05s" for a long run, "12s" for a short one, "" under a second. */
export function runDuration(ms: number): string {
  const seconds = Math.round(ms / 1000);
  if (seconds < 1) return "";
  if (seconds < 60) return `${seconds}s`;
  return `${Math.floor(seconds / 60)}m ${String(seconds % 60).padStart(2, "0")}s`;
}

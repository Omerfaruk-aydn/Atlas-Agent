// Folds the flat message list into turns: one user message and everything the
// agent did for it, in order.

import type { Attachment, Message, ToolCall, ToolResult } from "../api/types";
import { toMs } from "./format";
import { category, toolFile, toolMeta } from "./tools";

export type Step =
  | { kind: "text"; id: string; text: string }
  | { kind: "thinking"; id: string; text: string; ms: number; open: boolean }
  | { kind: "tool"; id: string; call: ToolCall; result?: ToolResult }
  | { kind: "group"; id: string; category: string; calls: { id: string; call: ToolCall; result?: ToolResult }[] }
  | { kind: "shell"; id: string; command: string; output: string; exitCode: number };

export interface ChangedFile {
  path: string;
  additions: number;
  removals: number;
}

export interface Source {
  url: string;
  host: string;
  title: string;
}

export interface Turn {
  id: string;
  user?: Message;
  messages: Message[];
  steps: Step[];
  startedAt: number;
  endedAt: number;
  finish?: { reason: string; message?: string; details?: string };
  summary: boolean;
  changes: ChangedFile[];
  sources: Source[];
  tools: number;
  model: string;
}

export function userText(m: Message): string {
  return m.parts.flatMap((p) => (p.type === "text" ? [p.data.text] : [])).join("\n");
}

export interface UserFile {
  name: string;
  image?: string;
}

/** Files that went along with a user message. */
export function userFiles(m: Message): UserFile[] {
  return m.parts.flatMap((p): UserFile[] => {
    if (p.type === "image_url") return [{ name: p.data.url, image: p.data.url }];
    if (p.type !== "binary") return [];
    const name = (p.data.Path ?? "").split(/[\\/]/).pop() || p.data.MIMEType || "file";
    const image = p.data.MIMEType?.startsWith("image/") && p.data.Data ? `data:${p.data.MIMEType};base64,${p.data.Data}` : undefined;
    return [{ name, image }];
  });
}

/** A user message's files, ready to go out again with an edited version. */
export function userAttachments(m: Message): Attachment[] {
  return m.parts.flatMap((p): Attachment[] => {
    if (p.type !== "binary" || !p.data.Data) return [];
    const mime = p.data.MIMEType ?? "";
    return [{ file_name: (p.data.Path ?? "").split(/[\\/]/).pop() || "file", mime_type: mime, content: p.data.Data }];
  });
}

function buildSteps(messages: Message[]): Step[] {
  const results = new Map<string, ToolResult>();
  for (const m of messages) {
    for (const p of m.parts) if (p.type === "tool_result") results.set(p.data.tool_call_id, p.data);
  }
  const steps: Step[] = [];
  for (const m of messages) {
    m.parts.forEach((p, i) => {
      // Of a user message, only a command they ran is a step; the rest is the bubble.
      if (m.role === "user" && p.type !== "shell_command") return;
      const id = `${m.id}:${i}`;
      switch (p.type) {
        case "reasoning": {
          if (!p.data.thinking.trim()) break;
          const { started_at: from, finished_at: to } = p.data;
          steps.push({ kind: "thinking", id, text: p.data.thinking, ms: from && to ? toMs(to) - toMs(from) : 0, open: !to });
          break;
        }
        case "text":
          if (p.data.text.trim()) steps.push({ kind: "text", id, text: p.data.text });
          break;
        case "tool_call":
          steps.push({ kind: "tool", id, call: p.data, result: results.get(p.data.id) });
          break;
        case "shell_command":
          steps.push({ kind: "shell", id, command: p.data.command, output: p.data.output, exitCode: p.data.exit_code });
          break;
      }
    });
  }
  return steps;
}

/** Calls the model made at once read as one row; agents keep their own. */
function groupParallel(steps: Step[]): Step[] {
  const out: Step[] = [];
  let i = 0;
  while (i < steps.length) {
    const first = steps[i];
    if (first.kind !== "tool") {
      out.push(first);
      i++;
      continue;
    }
    const cat = category(first.call.name);
    const message = first.id.split(":")[0];
    let end = i + 1;
    while (end < steps.length) {
      const next = steps[end];
      if (next.kind !== "tool" || next.id.split(":")[0] !== message || category(next.call.name) !== cat) break;
      end++;
    }
    if (end - i >= 2 && cat !== "agent" && cat !== "ask" && cat !== "plan") {
      const calls = (steps.slice(i, end) as Extract<Step, { kind: "tool" }>[]).map(({ id, call, result }) => ({ id, call, result }));
      out.push({ kind: "group", id: first.id, category: cat, calls });
    } else {
      out.push(...steps.slice(i, end));
    }
    i = end;
  }
  return out;
}

/** Every tool call of a step list, with parallel groups opened up. */
export function toolCalls(steps: Step[]): { call: ToolCall; result?: ToolResult }[] {
  return steps.flatMap((s) => (s.kind === "tool" ? [s] : s.kind === "group" ? s.calls : []));
}

function hostOf(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, "");
  } catch {
    return "";
  }
}

/** The pages a turn looked at: fetched URLs and web search results. */
function sourcesOf(steps: Step[]): Source[] {
  const seen = new Map<string, Source>();
  const add = (url: string, title: string) => {
    const host = hostOf(url);
    if (host && !seen.has(url)) seen.set(url, { url, host, title: title || host });
  };
  for (const { call, result } of toolCalls(steps)) {
    const cat = category(call.name);
    if (cat === "fetch") {
      try {
        add(String((JSON.parse(call.input) as { url?: string }).url ?? ""), "");
      } catch {
        // A call still streaming has no complete input yet.
      }
    } else if (cat === "web" && result && !result.is_error) {
      for (const m of result.content.matchAll(/^\d+\.\s+(.+)\n\s+URL:\s+(\S+)/gm)) add(m[2], m[1]);
    }
  }
  return [...seen.values()];
}

function changesOf(steps: Step[]): ChangedFile[] {
  const files = new Map<string, ChangedFile>();
  for (const s of toolCalls(steps)) {
    if (!s.result || s.result.is_error) continue;
    const cat = category(s.call.name);
    if (cat !== "edit" && cat !== "write") continue;
    const path = toolFile(s.call);
    if (!path) continue;
    const meta = toolMeta(s.result);
    const entry = files.get(path) ?? { path, additions: 0, removals: 0 };
    entry.additions += meta.additions ?? 0;
    entry.removals += meta.removals ?? 0;
    files.set(path, entry);
  }
  return [...files.values()];
}

export function buildTurns(messages: Message[]): Turn[] {
  const groups: Message[][] = [];
  for (const m of messages) {
    // A summary stands on its own, not as the answer of the turn before it.
    if (m.role === "user" || m.is_summary_message || groups.length === 0) groups.push([m]);
    else groups[groups.length - 1].push(m);
  }
  return groups.map((group) => {
    const first = group[0];
    const steps = groupParallel(buildSteps(group));
    let finish: Turn["finish"];
    let endedAt = toMs(first.created_at);
    for (const m of group) {
      endedAt = Math.max(endedAt, toMs(m.updated_at));
      for (const p of m.parts) {
        if (p.type === "finish") {
          finish = p.data;
          endedAt = Math.max(endedAt, toMs(p.data.time));
        }
      }
    }
    return {
      id: first.id,
      user: first.role === "user" ? first : undefined,
      messages: group,
      steps,
      startedAt: toMs(first.created_at),
      endedAt,
      finish,
      summary: group.some((m) => m.is_summary_message),
      changes: changesOf(steps),
      sources: sourcesOf(steps),
      tools: toolCalls(steps).length,
      model: group.findLast((m) => m.role === "assistant" && m.model)?.model ?? "",
    };
  });
}

/** What a turn was about, for a one-glance preview. */
export function turnPreview(turn: Turn): { title: string; body: string } {
  const plain = (text: string) => text.replace(/[`*_#>]/g, "").replace(/\s+/g, " ").trim();
  const answer = turn.steps.findLast((s) => s.kind === "text");
  return { title: plain(turn.user ? userText(turn.user) : ""), body: answer?.kind === "text" ? plain(answer.text) : "" };
}

/** Index of the first step of the closing answer: the text after the last tool. */
export function finalTextStart(steps: Step[]): number {
  let i = steps.length;
  while (i > 0 && steps[i - 1].kind === "text") i--;
  return i;
}

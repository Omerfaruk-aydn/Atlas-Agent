// Follows the agent's design work: the first time a run edits a design file,
// the design tool opens on it, so the person watches the design being made.

import type { Message } from "../api/types";

export const DESIGN_TOOL = "design_canvas";
const WRITES = new Set(["create", "apply"]);

/** The design file a tool call writes, or "" when it is not a design write. */
export function designTarget(name: string, input: string): string {
  if (name !== DESIGN_TOOL) return "";
  try {
    const value = JSON.parse(input) as { action?: unknown; file?: unknown };
    if (typeof value.action !== "string" || !WRITES.has(value.action)) return "";
    return typeof value.file === "string" && /^[a-z0-9][a-z0-9._-]{0,63}$/.test(value.file) ? value.file : "";
  } catch {
    return "";
  }
}

const seen = new Set<string>();

/** Files a live assistant message starts writing for the first time in its session. */
export function newDesignTargets(message: Message): string[] {
  if (message.role !== "assistant") return [];
  const out: string[] = [];
  for (const part of message.parts) {
    if (part.type !== "tool_call") continue;
    const file = designTarget(part.data.name, part.data.input);
    const key = `${message.session_id}:${file}`;
    if (!file || seen.has(key)) continue;
    seen.add(key);
    out.push(file);
  }
  return out;
}

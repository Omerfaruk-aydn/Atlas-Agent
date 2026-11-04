// What fills the context window. The server reports one total; the shares are
// estimated from the messages since the last summary, at about four characters
// a token. Whatever the messages do not explain is the system prompt, tool
// definitions, rules, skills and MCP tools the model is sent with every turn.

import type { Message } from "../api/types";

export type ContextPart = "system" | "summary" | "user" | "assistant" | "tools";

export interface ContextShare {
  part: ContextPart;
  tokens: number;
}

const CHARS_PER_TOKEN = 4;
const IMAGE_TOKENS = 1200;

const estimate = (text: string) => Math.ceil(text.length / CHARS_PER_TOKEN);

function tally(m: Message, totals: Record<ContextPart, number>) {
  const own: ContextPart = m.is_summary_message ? "summary" : m.role === "user" ? "user" : "assistant";
  for (const p of m.parts) {
    switch (p.type) {
      case "text":
        totals[own] += estimate(p.data.text);
        break;
      case "reasoning":
        totals[own] += estimate(p.data.thinking);
        break;
      case "tool_call":
        totals.tools += estimate(p.data.input);
        break;
      case "tool_result":
        totals.tools += estimate(p.data.content);
        break;
      case "shell_command":
        totals.tools += estimate(p.data.command + p.data.output);
        break;
      case "binary":
      case "image_url":
        totals[own] += IMAGE_TOKENS;
        break;
    }
  }
}

/** Shares of `used` tokens in a fixed order, with empty ones left out. */
export function contextShares(messages: Message[], used: number): ContextShare[] {
  const lastSummary = messages.findLastIndex((m) => m.is_summary_message);
  const totals: Record<ContextPart, number> = { system: 0, summary: 0, user: 0, assistant: 0, tools: 0 };
  for (const m of messages.slice(Math.max(0, lastSummary))) tally(m, totals);

  const counted = totals.summary + totals.user + totals.assistant + totals.tools;
  // Estimates can overshoot the real total; the shares still have to add up.
  const scale = counted > used ? used / counted : 1;
  const shares: ContextShare[] = (["summary", "user", "assistant", "tools"] as const).map((part) => ({ part, tokens: Math.round(totals[part] * scale) }));
  const rest = used - shares.reduce((sum, s) => sum + s.tokens, 0);
  return [{ part: "system" as const, tokens: Math.max(0, rest) }, ...shares].filter((s) => s.tokens > 0);
}

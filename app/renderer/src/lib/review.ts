// What a review of a turn's changes needs on this side: telling findings
// apart, turning one into a request to the agent, and remembering reviews on
// this computer. Pure, so it is tested on its own.

import type { Review, ReviewFinding, ReviewSeverity } from "../api/types";
import { t } from "./i18n";

export const SEVERITIES: readonly ReviewSeverity[] = ["critical", "high", "medium", "low", "info"];

export function severityRank(s: ReviewSeverity): number {
  const at = SEVERITIES.indexOf(s);
  return at < 0 ? SEVERITIES.length : at;
}

/** A finding's identity within one review: stable across reloads, so a dismissal sticks. */
export function findingKey(f: ReviewFinding): string {
  return `${f.severity}\u0000${f.file ?? ""}\u0000${f.line ?? 0}\u0000${f.title}`;
}

/** "file.ts:42", or the file alone, or nothing. */
export function locationOf(f: ReviewFinding): string {
  if (!f.file) return "";
  return f.line ? `${f.file}:${f.line}` : f.file;
}

/** How many findings of each severity, worst first, skipping the ones with none. */
export function countBySeverity(findings: readonly ReviewFinding[]): { severity: ReviewSeverity; count: number }[] {
  return SEVERITIES.map((severity) => ({ severity, count: findings.filter((f) => f.severity === severity).length })).filter((x) => x.count > 0);
}

function block(f: ReviewFinding): string {
  const lines = [`[${f.severity}] ${f.title}`];
  const where = locationOf(f);
  if (where) lines.push(`${t("review.fix.where")}: ${where}`);
  if (f.explanation) lines.push(`${t("review.fix.problem")}: ${f.explanation}`);
  if (f.suggestion) lines.push(`${t("review.fix.suggested")}: ${f.suggestion}`);
  return lines.join("\n");
}

/** The request that hands one finding to the agent that made the changes. */
export function fixPrompt(f: ReviewFinding): string {
  return `${t("review.fix.one")}\n\n${block(f)}`;
}

/** The request that hands several findings over at once, worst first. */
export function fixAllPrompt(findings: readonly ReviewFinding[]): string {
  const ordered = [...findings].sort((a, b) => severityRank(a.severity) - severityRank(b.severity));
  return `${t("review.fix.many")}\n\n${ordered.map((f, i) => `${i + 1}. ${block(f).replace(/\n/g, "\n   ")}`).join("\n\n")}`;
}

// ---- Remembered on this computer ------------------------------------------

/** What the page remembers about the review of one turn. */
export interface Remembered {
  /** The server's id, while the review runs; used to pick polling up again after a reload. */
  reviewId?: string;
  /** The finished review, so it can be read again without the server. */
  review?: Review;
  /** Findings the person dismissed. */
  dismissed: string[];
  /** Findings already handed to the agent. */
  sent: string[];
}

const KEY = "atlas:reviews";
const KEEP = 40;

export function emptyMemory(): Remembered {
  return { dismissed: [], sent: [] };
}

function read(): Record<string, Remembered> {
  try {
    const raw = localStorage.getItem(KEY);
    return raw ? (JSON.parse(raw) as Record<string, Remembered>) : {};
  } catch {
    return {};
  }
}

export function loadMemory(turnId: string): Remembered {
  const found = read()[turnId];
  return found ? { ...emptyMemory(), ...found } : emptyMemory();
}

/** Saves one turn's memory, keeping only the newest turns. */
export function saveMemory(turnId: string, value: Remembered): void {
  try {
    const all = read();
    delete all[turnId];
    all[turnId] = value;
    const ids = Object.keys(all);
    for (const old of ids.slice(0, Math.max(0, ids.length - KEEP))) delete all[old];
    localStorage.setItem(KEY, JSON.stringify(all));
  } catch {
    // A private window or a full disk leaves the review for this session only.
  }
}

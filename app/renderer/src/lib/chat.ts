// Pure helpers for the chat extras: message actions, the queue, permission
// sentences and the edit cards. Nothing here touches the DOM or the store, so
// all of it is unit tested on its own.

import type { Attachment, PermissionRequest } from "../api/types";
import { baseName } from "./format";
import { t } from "./i18n";
import { category, toolFile, toolMeta } from "./tools";
import { toolCalls, type Step } from "./turns";

// ---- Text ------------------------------------------------------------------

/** Markdown reduced to the words a reader sees: no fences, markers or link syntax. */
export function plainText(markdown: string): string {
  return markdown
    .replace(/```[^\n]*\n([\s\S]*?)```/g, (_, code: string) => code.replace(/\n$/, ""))
    .replace(/^ {0,3}#{1,6}\s+/gm, "")
    .replace(/^ {0,3}>\s?/gm, "")
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .replace(/(\*\*|__)(.+?)\1/g, "$2")
    .replace(/(^|[^*\w])\*(?!\s)([^*\n]+?)\*(?!\w)/g, "$1$2")
    .replace(/(^|[^_\w])_(?!\s)([^_\n]+?)_(?!\w)/g, "$1$2")
    .replace(/~~(.+?)~~/g, "$1")
    .replace(/`([^`\n]+)`/g, "$1")
    .replace(/^ {0,3}([-*_])( ?\1){2,}\s*$/gm, "")
    .replace(/\n{3,}/g, "\n\n")
    .trim();
}

// ---- Lists -----------------------------------------------------------------

/** The list with the item at `from` moved so it ends up at index `to`. */
export function moveItem<T>(list: readonly T[], from: number, to: number): T[] {
  if (from === to || from < 0 || from >= list.length) return list.slice();
  const target = Math.max(0, Math.min(list.length - 1, to));
  const next = list.slice();
  const [item] = next.splice(from, 1);
  next.splice(target, 0, item!);
  return next;
}

/**
 * Where a dragged item lands, given the gap the pointer is over. Gaps count
 * from 0 (above the first item) to `length` (below the last).
 */
export function dropIndex(from: number, gap: number): number {
  return gap > from ? gap - 1 : gap;
}

// ---- Queue -----------------------------------------------------------------

export interface QueueItem {
  id: string;
  text: string;
  attachments: Attachment[];
}

let queueSeq = 0;
export const newQueueItem = (text: string, attachments: Attachment[] = []): QueueItem => ({ id: `q${Date.now().toString(36)}${(queueSeq++).toString(36)}`, text, attachments });

// ---- Permission sentences --------------------------------------------------

export interface PermissionSentence {
  /** "Run this command?" */
  title: string;
  /** The command, file or address the sentence is about. */
  detail: string;
  /** What "Always allow" would cover. */
  scope: string;
}

function params(req: PermissionRequest): Record<string, unknown> {
  return req.params && typeof req.params === "object" ? (req.params as Record<string, unknown>) : {};
}

const text = (v: unknown): string => (typeof v === "string" ? v : "");

function hostOf(url: string): string {
  try {
    return new URL(url).hostname;
  } catch {
    return url;
  }
}

/** The action a permission request asks for, as one question the person can answer. */
export function permissionSentence(req: PermissionRequest): PermissionSentence {
  const p = params(req);
  const file = text(p.file_path) || text(p.path) || req.path;
  const name = file ? baseName(file) : "";
  const fill = (key: Parameters<typeof t>[0], value: string) => t(key).replace("{0}", value);
  const scope = fill("approve.scope", req.tool_name);
  switch (category(req.tool_name)) {
    case "shell":
    case "test":
      return { title: t("approve.run"), detail: text(p.command) || req.description, scope };
    case "edit":
      return { title: name ? fill("approve.edit", name) : t("approve.editAny"), detail: file, scope };
    case "write":
      return { title: name ? fill("approve.write", name) : t("approve.writeAny"), detail: file, scope };
    case "read":
    case "list":
    case "search":
      return { title: name ? fill("approve.read", name) : t("approve.readAny"), detail: file, scope };
    case "fetch": {
      const url = text(p.url);
      return { title: url ? fill("approve.open", hostOf(url)) : t("approve.openAny"), detail: url, scope };
    }
    case "web":
      return { title: fill("approve.search", text(p.query)), detail: "", scope };
    default:
      return { title: fill("approve.use", req.tool_name), detail: text(p.command) || text(p.url) || file || "", scope };
  }
}

// ---- Edit cards ------------------------------------------------------------

export interface FileEdit {
  path: string;
  additions: number;
  removals: number;
  /** The file as the turn found it; "" when the turn created it. */
  before: string;
  /** The file as the turn left it. */
  after: string;
  created: boolean;
  /** False when a tool result did not carry both versions. */
  undoable: boolean;
}

/** One entry per file the steps edited, with the first old and last new text. */
export function fileEdits(steps: Step[]): FileEdit[] {
  const files = new Map<string, FileEdit>();
  for (const { call, result } of toolCalls(steps)) {
    if (!result || result.is_error) continue;
    const cat = category(call.name);
    if (cat !== "edit" && cat !== "write") continue;
    const path = toolFile(call);
    if (!path) continue;
    const meta = toolMeta(result);
    const hasBoth = typeof meta.old_content === "string" || typeof meta.new_content === "string";
    const entry = files.get(path);
    if (!entry) {
      files.set(path, {
        path,
        additions: meta.additions ?? 0,
        removals: meta.removals ?? 0,
        before: meta.old_content ?? "",
        after: meta.new_content ?? "",
        created: !(meta.old_content ?? ""),
        undoable: hasBoth && typeof meta.new_content === "string",
      });
    } else {
      entry.additions += meta.additions ?? 0;
      entry.removals += meta.removals ?? 0;
      entry.after = meta.new_content ?? entry.after;
      entry.undoable = entry.undoable && hasBoth && typeof meta.new_content === "string";
    }
  }
  return [...files.values()];
}

export type Review = "kept" | "undone";

// ---- Small per-person memory ----------------------------------------------

function read<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T) : fallback;
  } catch {
    return fallback;
  }
}

function write(key: string, value: unknown) {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // Private windows and full disks leave the choice for this session only.
  }
}

export type Feedback = "up" | "down";

const FEEDBACK_KEY = "atlas:feedback";
const REVIEW_KEY = "atlas:edit-review";
const MEMORY_LIMIT = 500;

/** The thumbs a person gave to messages, by message id. Kept on this computer only. */
export function loadFeedback(): Record<string, Feedback> {
  return read<Record<string, Feedback>>(FEEDBACK_KEY, {});
}

export function saveFeedback(id: string, value: Feedback | null): Record<string, Feedback> {
  const all = loadFeedback();
  if (value) all[id] = value;
  else delete all[id];
  const keys = Object.keys(all);
  for (const old of keys.slice(0, Math.max(0, keys.length - MEMORY_LIMIT))) delete all[old];
  write(FEEDBACK_KEY, all);
  return all;
}

export const reviewKey = (turnId: string, path: string) => `${turnId}\u0000${path}`;

export function loadReviews(): Record<string, Review> {
  return read<Record<string, Review>>(REVIEW_KEY, {});
}

export function saveReview(key: string, value: Review): Record<string, Review> {
  const all = loadReviews();
  all[key] = value;
  const keys = Object.keys(all);
  for (const old of keys.slice(0, Math.max(0, keys.length - MEMORY_LIMIT))) delete all[old];
  write(REVIEW_KEY, all);
  return all;
}

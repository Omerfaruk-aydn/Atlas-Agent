// How a tool call reads in the thread: a verb, one dim argument, nothing else.
// Verbs turn from present to past when the call ends.

import { lang } from "./i18n";
import { baseName } from "./format";
import type { ToolCall, ToolResult } from "../api/types";

type Verb = { tr: [string, string]; en: [string, string] };

const VERBS: Record<string, Verb> = {
  read: { tr: ["Okuyor", "Okudu"], en: ["Reading", "Read"] },
  search: { tr: ["Arıyor", "Aradı"], en: ["Searching", "Searched"] },
  list: { tr: ["Listeliyor", "Listeledi"], en: ["Listing", "Listed"] },
  lsp: { tr: ["İnceliyor", "İnceledi"], en: ["Inspecting", "Inspected"] },
  edit: { tr: ["Düzenliyor", "Düzenledi"], en: ["Editing", "Edited"] },
  write: { tr: ["Oluşturuyor", "Oluşturdu"], en: ["Creating", "Created"] },
  shell: { tr: ["Çalıştırıyor", "Çalıştırdı"], en: ["Running", "Ran"] },
  test: { tr: ["Test ediyor", "Test etti"], en: ["Testing", "Tested"] },
  web: { tr: ["Web'de arıyor", "Web'de aradı"], en: ["Searching the web", "Searched the web"] },
  fetch: { tr: ["Sayfayı okuyor", "Sayfayı okudu"], en: ["Reading page", "Read page"] },
  browser: { tr: ["Tarayıcıda", "Tarayıcıda gezindi"], en: ["Browsing", "Browsed"] },
  computer: { tr: ["Bilgisayarda çalışıyor", "Bilgisayarda çalıştı"], en: ["Using the computer", "Used the computer"] },
  agent: { tr: ["Alt ajan çalışıyor", "Alt ajan bitti"], en: ["Delegating", "Delegated"] },
  plan: { tr: ["Planı güncelliyor", "Planı güncelledi"], en: ["Updating plan", "Updated plan"] },
  ask: { tr: ["Soruyor", "Sordu"], en: ["Asking", "Asked"] },
  memory: { tr: ["Hatırlıyor", "Hatırladı"], en: ["Recalling", "Recalled"] },
  security: { tr: ["Tarıyor", "Taradı"], en: ["Scanning", "Scanned"] },
};

const CATEGORIES: Record<string, string> = {
  view: "read",
  inspect_file: "read",
  read_mcp_resource: "read",
  grep: "search",
  glob: "search",
  semantic_code_search: "search",
  code_query: "search",
  references: "search",
  ls: "list",
  code_map: "list",
  project_model: "list",
  edit: "edit",
  multiedit: "edit",
  lsp_rename: "edit",
  lsp_replace_symbol: "edit",
  write: "write",
  bash: "shell",
  job_output: "shell",
  job_kill: "shell",
  test_run: "test",
  lint_run: "test",
  coverage_report: "test",
  mutation_test: "test",
  web_search: "web",
  design_search: "web",
  fetch: "fetch",
  api_probe: "fetch",
  site_monitor: "fetch",
  browser: "browser",
  computer: "computer",
  isolated_desktop: "computer",
  android: "computer",
  local_app: "computer",
  agent: "agent",
  subagent: "agent",
  task: "agent",
  team_send: "agent",
  todos: "plan",
  exit_plan_mode: "plan",
  question: "ask",
  memory: "memory",
  facts: "memory",
  session_search: "memory",
  security_scan: "security",
  scan_secrets: "security",
  dep_audit: "security",
};

export function category(name: string): string {
  return CATEGORIES[name] ?? (name.startsWith("lsp_") ? "lsp" : "other");
}

function humanize(name: string): string {
  const words = name.replace(/^mcp_/, "").replace(/_/g, " ");
  return words.charAt(0).toUpperCase() + words.slice(1);
}

export function toolVerb(name: string, done: boolean): string {
  const verb = VERBS[category(name)];
  return verb ? verb[lang][done ? 1 : 0] : humanize(name);
}

const NOUNS: Record<string, { tr: string; en: [string, string] }> = {
  read: { tr: "dosya", en: ["file", "files"] },
  edit: { tr: "dosya", en: ["file", "files"] },
  write: { tr: "dosya", en: ["file", "files"] },
  list: { tr: "klasör", en: ["folder", "folders"] },
  search: { tr: "arama", en: ["search", "searches"] },
  shell: { tr: "komut", en: ["command", "commands"] },
  test: { tr: "kontrol", en: ["check", "checks"] },
  web: { tr: "arama", en: ["search", "searches"] },
  fetch: { tr: "sayfa", en: ["page", "pages"] },
};

/** "Read 3 files" / "Okudu · 3 dosya", for calls made at once. */
export function groupLabel(cat: string, name: string, count: number, done: boolean): string {
  const noun = NOUNS[cat];
  const verb = toolVerb(name, done);
  if (!noun) return `${verb} · ${count}`;
  return lang === "tr" ? `${verb} · ${count} ${noun.tr}` : `${verb} ${count} ${noun.en[count === 1 ? 0 : 1]}`;
}

function parseInput(input: string): Record<string, unknown> {
  try {
    const value = JSON.parse(input) as unknown;
    return typeof value === "object" && value !== null ? (value as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

const str = (v: unknown) => (typeof v === "string" ? v : "");

/** The dim part of the row: a file and range, a pattern, a command, a domain. */
export function toolArg(call: ToolCall): string {
  const input = parseInput(call.input);
  const file = str(input.file_path) || str(input.path);
  switch (category(call.name)) {
    case "read": {
      if (!file) break;
      const offset = Number(input.offset) || 0;
      const limit = Number(input.limit) || 0;
      return limit ? `${baseName(file)} L${offset + 1}-${offset + limit}` : baseName(file);
    }
    case "edit":
    case "write":
      return file ? baseName(file) : "";
    case "search": {
      const pattern = str(input.pattern) || str(input.query);
      const where = str(input.path);
      return where ? `${pattern} · ${baseName(where)}` : pattern;
    }
    case "list":
      return file ? baseName(file) : "";
    case "shell":
      return str(input.description) || str(input.command) || str(input.shell_id);
    case "fetch":
    case "web": {
      const url = str(input.url);
      if (url) {
        try {
          return new URL(url).hostname;
        } catch {
          return url;
        }
      }
      return str(input.query);
    }
    case "agent":
      return str(input.description) || str(input.prompt);
    default:
      break;
  }
  return file || str(input.query) || str(input.description) || str(input.command) || str(input.url) || str(input.action);
}

export function toolCommand(call: ToolCall): string {
  return str(parseInput(call.input).command);
}

export function toolFile(call: ToolCall): string {
  const input = parseInput(call.input);
  return str(input.file_path) || str(input.path);
}

export interface ToolMeta {
  additions?: number;
  removals?: number;
  old_content?: string;
  new_content?: string;
  diff?: string;
  exit_code?: number;
  output?: string;
  background?: boolean;
}

export function toolMeta(result: ToolResult | undefined): ToolMeta {
  if (!result?.metadata) return {};
  try {
    return (JSON.parse(result.metadata) ?? {}) as ToolMeta;
  } catch {
    return {};
  }
}

export function isDenied(result: ToolResult | undefined): boolean {
  return !!result?.is_error && /permission denied|user denied|denied by/i.test(result.content);
}

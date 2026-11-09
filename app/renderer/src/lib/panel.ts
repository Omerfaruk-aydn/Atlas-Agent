// What the side panel is made of, and the rules for it. Pure functions: the
// store (stores/panel.ts) keeps the state, the components draw it.

export type PanelTool = "changes" | "files" | "terminal" | "side" | "jobs" | "design";
/** Views of the browser's own data, one tab each. */
export type BrowserView = "history" | "passwords" | "extensions" | "addresses" | "settings";
export type TabKind = "web" | PanelTool | BrowserView;

export interface PanelTab {
  id: string;
  kind: TabKind;
  /** The last known page title of a web tab, or the name a tool tab shows (the shell path, the open file). */
  title?: string;
  /** The page a web tab shows; empty for the new tab page. */
  url?: string;
  /** The side chat a "side" tab shows. */
  sessionId?: string;
}

export interface ProjectPanel {
  open: boolean;
  width: number;
  expanded: boolean;
  tabs: PanelTab[];
  active: string;
}

export interface RecentSite {
  url: string;
  host: string;
  title: string;
}

export const PANEL_MIN = 360;
export const THREAD_MIN = 320;
export const PANEL_DEFAULT = 520;
export const MAX_TABS = 20;
const MAX_RECENT = 12;
const ENGINES: Record<string, string> = {
  duckduckgo: "https://duckduckgo.com/?q=",
  google: "https://www.google.com/search?q=",
  bing: "https://www.bing.com/search?q=",
};
/** Where a search goes; follows the browser's settings (a live binding). */
export let SEARCH_URL = ENGINES.duckduckgo!;
export function setSearchEngine(id: string): void {
  SEARCH_URL = ENGINES[id] ?? ENGINES.duckduckgo!;
}

/** Sites offered next to the person's own recent ones. */
export const DEFAULT_SITES: RecentSite[] = [
  { url: "https://github.com/", host: "github.com", title: "GitHub" },
  { url: "https://developer.mozilla.org/", host: "developer.mozilla.org", title: "MDN Web Docs" },
  { url: "https://en.wikipedia.org/", host: "en.wikipedia.org", title: "Wikipedia" },
  { url: "https://news.ycombinator.com/", host: "news.ycombinator.com", title: "Hacker News" },
];

const TOOLS: ReadonlySet<string> = new Set(["changes", "files", "terminal", "side", "jobs", "design", "history", "passwords", "extensions", "addresses", "settings"]);

export const emptyPanel = (): ProjectPanel => ({ open: false, width: PANEL_DEFAULT, expanded: false, tabs: [], active: "" });

export const projectKey = (path: string | undefined): string => (path ?? "").replaceAll("\\", "/").replace(/\/$/, "").toLowerCase();

export const newTabId = (): string => crypto.randomUUID();

export function hostOf(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, "");
  } catch {
    return "";
  }
}

/** The page an address bar entry means: a URL, a host, or a search. Empty for blank input. */
export function normalizeAddress(input: string): string {
  const text = input.trim();
  if (!text) return "";
  if (/^https?:\/\//i.test(text)) {
    try {
      return new URL(text).toString();
    } catch {
      return SEARCH_URL + encodeURIComponent(text);
    }
  }
  // Another scheme (javascript:, file:, data:) is a search, never a page.
  if (/^[a-z][a-z0-9+.-]*:/i.test(text) && !/^[^\s/:]+:\d+([/?#].*)?$/.test(text)) return SEARCH_URL + encodeURIComponent(text);
  const hostLike = /^(localhost|\d{1,3}(\.\d{1,3}){3}|\[[0-9a-f:]+\]|([a-z0-9-]+\.)+[a-z]{2,})(:\d+)?([/?#].*)?$/i;
  if (!/\s/.test(text) && hostLike.test(text)) {
    const local = /^(localhost|\d{1,3}(\.\d{1,3}){3}|\[)/i.test(text);
    try {
      return new URL(`${local ? "http" : "https"}://${text}`).toString();
    } catch {
      // Falls through to a search.
    }
  }
  return SEARCH_URL + encodeURIComponent(text);
}

export const isWebUrl = (url: string): boolean => /^https?:\/\/[^/\s]/i.test(url);

/** The panel as saved, with anything unexpected dropped. */
export function sanitizePanel(raw: unknown): ProjectPanel {
  const base = emptyPanel();
  if (typeof raw !== "object" || raw === null) return base;
  const r = raw as Record<string, unknown>;
  const seen = new Set<string>();
  const tabs: PanelTab[] = [];
  if (Array.isArray(r.tabs)) {
    for (const item of r.tabs.slice(0, MAX_TABS)) {
      if (typeof item !== "object" || item === null) continue;
      const t = item as Record<string, unknown>;
      const id = typeof t.id === "string" ? t.id.slice(0, 80) : "";
      const kind = t.kind === "web" || (typeof t.kind === "string" && TOOLS.has(t.kind)) ? (t.kind as TabKind) : null;
      if (!id || !kind || seen.has(id)) continue;
      seen.add(id);
      const tab: PanelTab = { id, kind };
      if (kind === "web") {
        if (typeof t.url === "string" && isWebUrl(t.url)) tab.url = t.url.slice(0, 2048);
        if (typeof t.title === "string") tab.title = t.title.slice(0, 200);
      }
      if ((kind === "terminal" || kind === "files" || kind === "design") && typeof t.title === "string") tab.title = t.title.slice(0, 400);
      if (kind === "side") {
        if (typeof t.sessionId !== "string" || !t.sessionId) continue;
        tab.sessionId = t.sessionId;
      }
      tabs.push(tab);
    }
  }
  const width = typeof r.width === "number" && Number.isFinite(r.width) ? Math.min(2000, Math.max(PANEL_MIN, Math.round(r.width))) : PANEL_DEFAULT;
  const active = typeof r.active === "string" && seen.has(r.active) ? r.active : (tabs[0]?.id ?? "");
  return { open: r.open === true && tabs.length > 0, width, expanded: r.expanded === true && tabs.length > 0, tabs, active };
}

export function sanitizeRecent(raw: unknown): RecentSite[] {
  if (!Array.isArray(raw)) return [];
  const out: RecentSite[] = [];
  for (const item of raw) {
    if (typeof item !== "object" || item === null) continue;
    const r = item as Record<string, unknown>;
    if (typeof r.url !== "string" || !isWebUrl(r.url)) continue;
    const host = hostOf(r.url);
    if (!host || out.some((x) => x.host === host)) continue;
    out.push({ url: r.url.slice(0, 2048), host, title: typeof r.title === "string" && r.title ? r.title.slice(0, 120) : host });
    if (out.length >= MAX_RECENT) break;
  }
  return out;
}

/** Puts a visited page first in the recent list, one entry per host. */
export function rememberSite(recent: RecentSite[], url: string, title: string): RecentSite[] {
  const host = hostOf(url);
  if (!host || !isWebUrl(url)) return recent;
  const origin = new URL(url).origin + "/";
  const entry: RecentSite = { url: origin, host, title: title.trim() || host };
  return [entry, ...recent.filter((x) => x.host !== host)].slice(0, MAX_RECENT);
}

/** Up to `limit` tiles: recent sites first, then defaults that are not already there. */
export function suggestedSites(recent: RecentSite[], limit = 8): RecentSite[] {
  const hosts = new Set(recent.map((x) => x.host));
  return [...recent, ...DEFAULT_SITES.filter((x) => !hosts.has(x.host))].slice(0, limit);
}

/** Width the panel may take in a container, leaving the thread its minimum. */
export function clampWidth(width: number, container: number): number {
  const max = Math.max(PANEL_MIN, container - THREAD_MIN);
  return Math.round(Math.min(max, Math.max(PANEL_MIN, width)));
}

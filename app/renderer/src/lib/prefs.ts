// What the app remembers about how it is used. It lives in the main process,
// because the gateway's port, and with it this page's origin, changes on every
// start.

type ModelTarget = { provider: string; model: string; reasoning_effort?: string; think?: boolean };

export interface UIPrefs {
  sidebarOpen: boolean;
  sidebarW: number;
  /** The side panel, by project (see lib/panel.ts). */
  panels: Record<string, unknown>;
  /** Sites opened in the panel browser, newest first. */
  recentSites: unknown[];
  pinned: string[];
  archived: string[];
  foldTurns: boolean;
  showThinking: boolean;
  notifyDone: boolean;
  notifyWait: boolean;
  lang: "system" | "tr" | "en";
  onboarded: boolean;
  /** Keys the person changed, by shortcut id. */
  shortcuts: Record<string, string[]>;
  recentModels: string[];
  autoModel: boolean;
  autoModelTargets: { large?: ModelTarget; small?: ModelTarget };
}

export const defaultUI: UIPrefs = {
  sidebarOpen: true,
  sidebarW: 264,
  panels: {},
  recentSites: [],
  pinned: [],
  archived: [],
  foldTurns: true,
  showThinking: true,
  notifyDone: true,
  notifyWait: true,
  lang: "system",
  onboarded: false,
  shortcuts: {},
  recentModels: [],
  autoModel: false,
  autoModelTargets: {},
};

export function loadUI(): UIPrefs {
  const saved = (window.__atlasBoot?.ui ?? {}) as Partial<UIPrefs>;
  return { ...defaultUI, ...saved };
}

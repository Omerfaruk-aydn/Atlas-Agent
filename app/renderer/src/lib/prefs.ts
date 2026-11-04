// What the app remembers about how it is used. It lives in the main process,
// because the gateway's port, and with it this page's origin, changes on every
// start.

export type RailTab = "changes" | "files" | "jobs";

export interface UIPrefs {
  sidebarOpen: boolean;
  sidebarW: number;
  railOpen: boolean;
  railTab: RailTab;
  railW: number;
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
}

export const defaultUI: UIPrefs = {
  sidebarOpen: true,
  sidebarW: 264,
  railOpen: false,
  railTab: "changes",
  railW: 380,
  pinned: [],
  archived: [],
  foldTurns: true,
  showThinking: true,
  notifyDone: true,
  notifyWait: true,
  lang: "system",
  onboarded: false,
  shortcuts: {},
};

export function loadUI(): UIPrefs {
  const saved = (window.__atlasBoot?.ui ?? {}) as Partial<UIPrefs>;
  return { ...defaultUI, ...saved };
}

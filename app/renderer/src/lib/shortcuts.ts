// Every keyboard shortcut in one list: the listener below runs them and
// Settings > Shortcuts prints them, so the two cannot drift apart. The keys
// here are the defaults; ui.shortcuts holds what the person changed.

import { useEffect } from "react";
import type { PermissionMode } from "../api/types";
import { useApp } from "../stores/app";
import type { Key } from "./i18n";
import { emitThread } from "./thread-bus";

export interface Shortcut {
  id: string;
  keys: string[];
  label: Key;
  /** Listed but handled elsewhere, like Enter in the composer. */
  info?: boolean;
  run?(e: KeyboardEvent): void;
}

const MODES: PermissionMode[] = ["manual", "auto_accept_edits", "plan"];

function cyclePermissionMode() {
  const { settings, setPermissionMode } = useApp.getState();
  if (!settings) return;
  const at = MODES.indexOf(settings.permission_mode);
  void setPermissionMode(MODES[(at + 1) % MODES.length]);
}

export const SHORTCUTS: Shortcut[] = [
  { id: "palette", keys: ["Ctrl+K", "Ctrl+Shift+P"], label: "shortcut.palette", run: () => useApp.getState().openDialog("palette") },
  { id: "sessions", keys: ["Ctrl+P"], label: "shortcut.sessions", run: () => useApp.getState().openDialog("sessions") },
  { id: "new", keys: ["Ctrl+N"], label: "shortcut.new", run: () => void useApp.getState().selectSession(null) },
  { id: "sidebar", keys: ["Ctrl+B"], label: "shortcut.sidebar", run: () => useApp.getState().toggleSidebar() },
  { id: "rail", keys: ["Ctrl+Alt+B"], label: "shortcut.rail", run: () => useApp.getState().toggleRail() },
  { id: "rail1", keys: ["Ctrl+Alt+1"], label: "shortcut.railChanges", run: () => useApp.getState().toggleRail("changes") },
  { id: "rail2", keys: ["Ctrl+Alt+2"], label: "shortcut.railFiles", run: () => useApp.getState().toggleRail("files") },
  { id: "rail3", keys: ["Ctrl+Alt+3"], label: "shortcut.railJobs", run: () => useApp.getState().toggleRail("jobs") },
  { id: "focus", keys: ["Ctrl+L"], label: "shortcut.focus", run: () => document.querySelector<HTMLTextAreaElement>(".composer textarea")?.focus() },
  { id: "models", keys: ["Ctrl+/"], label: "shortcut.models", run: () => useApp.getState().openDialog("models") },
  { id: "settings", keys: ["Ctrl+,"], label: "shortcut.settings", run: () => useApp.getState().openSettings() },
  { id: "help", keys: ["Ctrl+Shift+/"], label: "shortcut.help", run: () => useApp.getState().openSettings("shortcuts") },
  { id: "mode", keys: ["Shift+Tab"], label: "shortcut.mode", run: cyclePermissionMode },
  { id: "prev", keys: ["Alt+ArrowUp"], label: "shortcut.prevTurn", run: () => emitThread("prev") },
  { id: "next", keys: ["Alt+ArrowDown"], label: "shortcut.nextTurn", run: () => emitThread("next") },
  { id: "fold", keys: ["Ctrl+."], label: "shortcut.fold", run: () => emitThread("fold") },
  { id: "send", keys: ["Enter"], label: "shortcut.send", info: true },
  { id: "newline", keys: ["Shift+Enter"], label: "shortcut.newline", info: true },
  { id: "stop", keys: ["Esc"], label: "shortcut.stop", info: true },
  { id: "allow", keys: ["Enter", "Ctrl+Enter", "Esc"], label: "shortcut.permission", info: true },
];

/** "Ctrl+Shift+/" for a key event, in the form SHORTCUTS uses. */
export function chord(e: KeyboardEvent): string {
  const parts: string[] = [];
  if (e.ctrlKey || e.metaKey) parts.push("Ctrl");
  if (e.altKey) parts.push("Alt");
  if (e.shiftKey) parts.push("Shift");
  // Shift turns "/" into "?" and "1" into "!"; the physical key names them all.
  const digit = /^Digit(\d)$/.exec(e.code)?.[1];
  const key = digit ?? (e.code === "Slash" ? "/" : e.code === "Comma" ? "," : e.code === "Period" ? "." : e.key.length === 1 ? e.key.toUpperCase() : e.key);
  parts.push(key);
  return parts.join("+");
}

export function displayKeys(keys: string): string {
  return keys.replace("ArrowUp", "↑").replace("ArrowDown", "↓");
}

type Overrides = Record<string, string[]>;

/** The keys a shortcut answers to now. */
export function keysOf(shortcut: Shortcut, overrides: Overrides): string[] {
  return overrides[shortcut.id] ?? shortcut.keys;
}

/** The shortcut that holds a chord, leaving out the one being changed. */
export function holderOf(keys: string, overrides: Overrides, except?: string): Shortcut | undefined {
  return SHORTCUTS.find((s) => s.id !== except && keysOf(s, overrides).includes(keys));
}

/** A chord worth binding: plain keys belong to typing, Esc and Enter to dialogs. */
export function canBind(e: Pick<KeyboardEvent, "key" | "ctrlKey" | "metaKey" | "altKey" | "shiftKey">): boolean {
  if (["Control", "Shift", "Alt", "Meta", "AltGraph", "CapsLock", "Escape"].includes(e.key)) return false;
  return e.ctrlKey || e.metaKey || e.altKey || /^F\d{1,2}$/.test(e.key) || (e.key === "Tab" && e.shiftKey);
}

/** Gives a chord to a shortcut and takes it from whoever held it. */
export function rebind(id: string, keys: string, overrides: Overrides): Overrides {
  const next = { ...overrides, [id]: [keys] };
  for (const s of SHORTCUTS) {
    if (s.id !== id && !s.info && keysOf(s, next).includes(keys)) next[s.id] = keysOf(s, next).filter((k) => k !== keys);
  }
  return next;
}

/** The first key of a shortcut, for the hint beside what it drives. */
export function useKeys(id: string): string {
  return useApp((s) => {
    const shortcut = SHORTCUTS.find((x) => x.id === id);
    return shortcut ? (keysOf(shortcut, s.ui.shortcuts)[0] ?? "") : "";
  });
}

let bound: { overrides: Overrides; map: Map<string, Shortcut> } | null = null;

function byChord(overrides: Overrides) {
  if (bound?.overrides !== overrides) {
    bound = { overrides, map: new Map(SHORTCUTS.flatMap((s) => (s.info ? [] : keysOf(s, overrides).map((k) => [k, s] as const)))) };
  }
  return bound.map;
}

export function useShortcuts() {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const state = useApp.getState();
      if (state.phase !== "ready" || e.isComposing) return;
      const hit = byChord(state.ui.shortcuts).get(chord(e));
      if (hit) {
        // Dialogs own the keyboard; the palette may still be toggled closed.
        if (state.dialog && hit.id !== "palette") return;
        if (state.dialog === "palette" && hit.id === "palette") {
          e.preventDefault();
          state.openDialog(null);
          return;
        }
        e.preventDefault();
        hit.run?.(e);
        return;
      }
      if (e.key === "Escape" && !state.dialog && state.currentId && state.busy[state.currentId]) {
        if (document.querySelector("[data-radix-popper-content-wrapper], .suggest")) return;
        void state.cancel();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
}

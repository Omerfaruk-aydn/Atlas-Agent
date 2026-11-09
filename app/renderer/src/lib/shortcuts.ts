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
  { id: "sessions", keys: ["Ctrl+Shift+F"], label: "shortcut.sessions", run: () => useApp.getState().openDialog("sessions") },
  { id: "new", keys: ["Ctrl+N"], label: "shortcut.new", run: () => void useApp.getState().selectSession(null) },
  { id: "sidebar", keys: ["Ctrl+B"], label: "shortcut.sidebar", run: () => useApp.getState().toggleSidebar() },
  { id: "rail", keys: ["Ctrl+Alt+B"], label: "shortcut.rail", run: () => useApp.getState().toggleRail() },
  { id: "rail1", keys: ["Ctrl+Shift+G"], label: "shortcut.railChanges", run: () => useApp.getState().toggleRail("changes") },
  { id: "railTerminal", keys: ["Ctrl+`"], label: "shortcut.railTerminal", run: () => useApp.getState().toggleRail("terminal") },
  { id: "railSide", keys: ["Ctrl+Shift+Y"], label: "shortcut.railSide", run: () => useApp.getState().toggleRail("side") },
  { id: "rail2", keys: ["Ctrl+P"], label: "shortcut.railFiles", run: () => useApp.getState().toggleRail("files") },
  { id: "rail3", keys: ["Ctrl+Shift+J"], label: "shortcut.railJobs", run: () => useApp.getState().toggleRail("jobs") },
  { id: "railDesign", keys: ["Ctrl+Shift+E"], label: "shortcut.railDesign", run: () => useApp.getState().toggleRail("design") },
  { id: "summary", keys: ["Ctrl+Shift+U"], label: "shortcut.summary", run: () => document.querySelector<HTMLButtonElement>("[data-summary-trigger]")?.click() },
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
  { id: "steer", keys: ["Ctrl+Enter"], label: "shortcut.steer", info: true },
  { id: "queueMove", keys: ["Alt+ArrowUp", "Alt+ArrowDown"], label: "shortcut.queueMove", info: true },
  { id: "allow", keys: ["Enter", "Ctrl+Enter", "Esc"], label: "shortcut.permission", info: true },
];

const CODE_KEYS: Record<string, string> = { Slash: "/", Comma: ",", Period: ".", Backquote: "`" };

/** The key a physical key stands for, whatever the layout typed with it. */
function keyOfCode(code: string): string {
  const letter = /^Key([A-Z])$/.exec(code);
  if (letter) return letter[1]!;
  const digit = /^Digit(\d)$/.exec(code);
  if (digit) return digit[1]!;
  return CODE_KEYS[code] ?? "";
}

/**
 * Whether the press is AltGr typing a character. On Windows AltGr arrives as
 * Ctrl+Alt: when that pair typed something other than the key's own character
 * (@, €, {, [, ], \, |, ~, # on a Turkish layout), it is text and never a
 * shortcut. The browser also names it through the AltGraph modifier.
 */
export function isAltGrText(e: Pick<KeyboardEvent, "key" | "code" | "ctrlKey" | "altKey"> & { getModifierState?: (key: string) => boolean }): boolean {
  if (e.key === "AltGraph") return true;
  if (e.getModifierState?.("AltGraph")) return true;
  if (!(e.ctrlKey && e.altKey) || e.key.length !== 1) return false;
  const own = keyOfCode(e.code);
  return own ? e.key !== own && e.key !== own.toLowerCase() : /[^\w]/.test(e.key);
}

/** "Ctrl+Shift+/" for a key event, in the form SHORTCUTS uses; "" for AltGr text. */
export function chord(e: KeyboardEvent): string {
  if (isAltGrText(e)) return "";
  const parts: string[] = [];
  if (e.ctrlKey || e.metaKey) parts.push("Ctrl");
  if (e.altKey) parts.push("Alt");
  if (e.shiftKey) parts.push("Shift");
  // Shift turns "/" into "?" and "1" into "!"; the physical key names them all.
  const key = keyOfCode(e.code) || (e.key.length === 1 ? e.key.toUpperCase() : e.key);
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
export function canBind(e: Pick<KeyboardEvent, "key" | "ctrlKey" | "metaKey" | "altKey" | "shiftKey"> & { code?: string; getModifierState?: (key: string) => boolean }): boolean {
  if (isAltGrText({ key: e.key, code: e.code ?? "", ctrlKey: e.ctrlKey, altKey: e.altKey, getModifierState: e.getModifierState?.bind(e) })) return false;
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

/**
 * Whether a registered chord pressed in a terminal belongs to the app. A
 * terminal keeps the plain Ctrl chords (Ctrl+P is history, Ctrl+L clears,
 * Ctrl+K kills a line); chords with Alt or Shift, and Ctrl+` which a shell has
 * no use for, reach the app.
 */
export function terminalLeavesToApp(e: Pick<KeyboardEvent, "altKey" | "shiftKey" | "key">): boolean {
  return e.altKey || e.shiftKey || e.key === "`";
}

/** Whether the key press in a terminal is one of the app's shortcuts that the terminal gives up. */
export function isAppChord(e: KeyboardEvent): boolean {
  return byChord(useApp.getState().ui.shortcuts).has(chord(e)) && terminalLeavesToApp(e);
}

/** Every chord the app answers to; the browser view lets exactly these through to the app. */
export function allChords(overrides: Record<string, string[]>): string[] {
  return SHORTCUTS.filter((s) => !s.info).flatMap((s) => keysOf(s, overrides));
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

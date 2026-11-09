// The terminals of the panel. An xterm instance outlives the tab being on
// screen: switching tabs must not lose what a shell printed, so instances live
// here, apart from React, until their tab closes.

import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { isAppChord } from "../../lib/shortcuts";
import { useApp } from "../../stores/app";

export type TermStatus = "starting" | "running" | "exited" | "failed";

export interface TermEntry {
  id: string;
  term: Terminal;
  fit: FitAddon;
  status: TermStatus;
  detail: string;
  subscribers: Set<() => void>;
  opened: boolean;
  started: boolean;
}

const entries = new Map<string, TermEntry>();
let wired = false;

const css = (name: string) => getComputedStyle(document.documentElement).getPropertyValue(name).trim();

const DARK = { black: "#1f1f22", red: "#ff6b63", green: "#3dd68c", yellow: "#f5b83d", blue: "#7ea2ff", magenta: "#b4a0ed", cyan: "#5fd0d6", white: "#d6d6da", brightBlack: "#6b6b73", brightRed: "#ff8a83", brightGreen: "#6fe3a9", brightYellow: "#f8cb6b", brightBlue: "#a1bcff", brightMagenta: "#cbbcf3", brightCyan: "#8fe0e4", brightWhite: "#ffffff" };
const LIGHT = { black: "#1a1917", red: "#c9362f", green: "#17804f", yellow: "#9a6400", blue: "#2f5bd3", magenta: "#7154b5", cyan: "#0e7c86", white: "#a9a59d", brightBlack: "#736f68", brightRed: "#d9534c", brightGreen: "#2a9a66", brightYellow: "#b07a10", brightBlue: "#4a74e8", brightMagenta: "#8a6fd0", brightCyan: "#1d96a2", brightWhite: "#d8d5ce" };

export function termTheme() {
  const dark = document.documentElement.dataset.colorScheme === "dark";
  return { ...(dark ? DARK : LIGHT), background: css("--atlas-surface"), foreground: css("--atlas-ink"), cursor: css("--atlas-ink"), cursorAccent: css("--atlas-surface"), selectionBackground: dark ? "rgba(126,162,255,0.30)" : "rgba(47,91,211,0.22)" };
}

function notify(entry: TermEntry) {
  for (const fn of entry.subscribers) fn();
}

function wire() {
  if (wired || !window.atlas) return;
  wired = true;
  window.atlas.terminal.onData(({ id, data }) => entries.get(id)?.term.write(data));
  window.atlas.terminal.onExit(({ id, code }) => {
    const entry = entries.get(id);
    if (!entry) return;
    entry.status = "exited";
    entry.detail = String(code);
    notify(entry);
  });
}

/** Starts (or restarts) the shell behind an entry in the project folder. */
export async function startShell(entry: TermEntry) {
  const cwd = useApp.getState().workspace?.path ?? useApp.getState().project;
  entry.status = "starting";
  notify(entry);
  const result = await window.atlas?.terminal.create({ id: entry.id, cwd, cols: entry.term.cols, rows: entry.term.rows });
  if (result?.ok) {
    entry.status = "running";
    entry.detail = result.shell;
  } else {
    entry.status = "failed";
    entry.detail = result?.error ?? "";
  }
  notify(entry);
}

export function terminalFor(id: string): TermEntry {
  wire();
  let entry = entries.get(id);
  if (entry) return entry;
  const term = new Terminal({
    fontFamily: css("--atlas-font-mono") || "monospace",
    fontSize: 12.5,
    lineHeight: 1.25,
    cursorBlink: true,
    scrollback: 5000,
    allowProposedApi: false,
    theme: termTheme(),
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  entry = { id, term, fit, status: "starting", detail: "", subscribers: new Set(), opened: false, started: false };
  const made = entry;
  term.onData((data) => void window.atlas?.terminal.write(id, data));
  term.onResize(({ cols, rows }) => void window.atlas?.terminal.resize(id, cols, rows));
  term.attachCustomKeyEventHandler((e) => {
    if (e.type !== "keydown") return true;
    if (isAppChord(e)) return false;
    if ((e.ctrlKey && e.key.toLowerCase() === "c" && term.hasSelection()) || (e.ctrlKey && e.shiftKey && e.key.toLowerCase() === "c")) {
      void navigator.clipboard.writeText(term.getSelection());
      return false;
    }
    return true;
  });
  entries.set(id, made);
  return made;
}

export function disposeTerminal(id: string) {
  const entry = entries.get(id);
  if (!entry) return;
  entries.delete(id);
  entry.term.dispose();
}

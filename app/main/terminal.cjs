"use strict";

// Real terminals for the side panel: one pseudo-terminal per tab, started in
// the project folder with node-pty (ConPTY on Windows). Output is batched so a
// busy build costs a few messages a frame, not one per chunk.

const fs = require("node:fs");
const path = require("node:path");

const MAX_TERMINALS = 8;
const FLUSH_MS = 8;

let pty = null;
let loadError = "";

function loadPty() {
  if (pty || loadError) return pty;
  try {
    pty = require("node-pty");
  } catch (err) {
    loadError = err instanceof Error ? err.message : String(err);
  }
  return pty;
}

/** The person's own shell: PowerShell on Windows, $SHELL elsewhere. */
function shellCommand() {
  if (process.platform === "win32") {
    const system = process.env.SystemRoot ?? "C:\\Windows";
    const powershell = path.join(system, "System32", "WindowsPowerShell", "v1.0", "powershell.exe");
    if (fs.existsSync(powershell)) return { file: powershell, args: ["-NoLogo"] };
    return { file: process.env.ComSpec ?? path.join(system, "System32", "cmd.exe"), args: [] };
  }
  return { file: process.env.SHELL || "/bin/sh", args: [] };
}

class TerminalHost {
  /** @param {import("electron").BrowserWindow} win */
  constructor(win) {
    this.win = win;
    /** @type {Map<string, {proc: any, buffer: string, timer: NodeJS.Timeout | null}>} */
    this.terms = new Map();
  }

  send(channel, value) {
    if (!this.win.isDestroyed()) this.win.webContents.send(channel, value);
  }

  /** @returns {{ok: true, shell: string} | {ok: false, error: string}} */
  create({ id, cwd, cols, rows }) {
    if (typeof id !== "string" || id.length > 80) return { ok: false, error: "bad id" };
    if (this.terms.has(id)) return { ok: true, shell: shellCommand().file };
    if (this.terms.size >= MAX_TERMINALS) return { ok: false, error: "too many terminals" };
    const native = loadPty();
    if (!native) return { ok: false, error: loadError };
    const { file, args } = shellCommand();
    try {
      const proc = native.spawn(file, args, {
        name: "xterm-256color",
        cols: Math.min(500, Math.max(2, Math.floor(cols) || 80)),
        rows: Math.min(300, Math.max(1, Math.floor(rows) || 24)),
        cwd,
        env: { ...process.env, TERM: "xterm-256color", COLORTERM: "truecolor", TERM_PROGRAM: "Atlas" },
        useConpty: true,
      });
      const term = { proc, buffer: "", timer: null };
      this.terms.set(id, term);
      proc.onData((data) => {
        term.buffer += data;
        term.timer ??= setTimeout(() => this.flush(id), FLUSH_MS);
      });
      proc.onExit(({ exitCode }) => {
        this.flush(id);
        this.terms.delete(id);
        this.send("atlas:terminal-exit", { id, code: exitCode });
      });
      return { ok: true, shell: file };
    } catch (err) {
      return { ok: false, error: err instanceof Error ? err.message : String(err) };
    }
  }

  flush(id) {
    const term = this.terms.get(id);
    if (!term) return;
    if (term.timer) clearTimeout(term.timer);
    term.timer = null;
    if (term.buffer) this.send("atlas:terminal-data", { id, data: term.buffer });
    term.buffer = "";
  }

  write(id, data) {
    if (typeof data === "string" && data.length <= 1 << 20) this.terms.get(id)?.proc.write(data);
  }

  resize(id, cols, rows) {
    const term = this.terms.get(id);
    if (!term || !Number.isFinite(cols) || !Number.isFinite(rows)) return;
    try {
      term.proc.resize(Math.min(500, Math.max(2, Math.floor(cols))), Math.min(300, Math.max(1, Math.floor(rows))));
    } catch {
      // The shell is already gone.
    }
  }

  kill(id) {
    const term = this.terms.get(id);
    if (!term) return;
    this.terms.delete(id);
    if (term.timer) clearTimeout(term.timer);
    try {
      term.proc.kill();
    } catch {
      // Already exited.
    }
  }

  destroy() {
    for (const id of [...this.terms.keys()]) this.kill(id);
  }
}

module.exports = { TerminalHost };

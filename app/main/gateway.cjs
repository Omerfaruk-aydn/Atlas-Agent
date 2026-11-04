"use strict";

// Owns the hidden `atlas-agent app serve` child. The child prints one JSON
// line when it is ready and exits when its stdin closes, so it can never
// outlive the app.

const { spawn } = require("node:child_process");
const os = require("node:os");
const readline = require("node:readline");

const READY_TIMEOUT_MS = 60_000;
const STOP_TIMEOUT_MS = 6_000;

class Gateway {
  /** @param {{bin: string, renderer: string, onCrash: (err: Error) => void}} opts */
  constructor(opts) {
    this.opts = opts;
    this.child = null;
    this.origin = "";
    this.version = "";
    this.link = "";
    this.stopping = false;
  }

  /** Starts the child and resolves once it serves. */
  start() {
    return new Promise((resolve, reject) => {
      const child = spawn(this.opts.bin, ["app", "serve", "--renderer", this.opts.renderer, "--port", "0"], {
        cwd: os.homedir(),
        stdio: ["pipe", "pipe", "pipe"],
        windowsHide: true,
      });
      this.child = child;
      let settled = false;
      let lastError = "";
      const settle = (fn, value) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        fn(value);
      };
      const timer = setTimeout(() => {
        settle(reject, new Error("The Atlas core did not start in time."));
        child.kill();
      }, READY_TIMEOUT_MS);

      child.on("error", (err) => settle(reject, new Error(`Could not start ${this.opts.bin}: ${err.message}`)));
      child.stdin.on("error", () => {});
      child.stderr.on("data", (chunk) => {
        lastError = String(chunk).trim().slice(-400) || lastError;
      });

      readline.createInterface({ input: child.stdout }).on("line", (line) => {
        let ev;
        try {
          ev = JSON.parse(line);
        } catch {
          return;
        }
        if (ev.type === "ready") {
          this.origin = `http://127.0.0.1:${ev.port}`;
          this.version = ev.version || "";
          this.link = ev.link;
          settle(resolve, this);
        } else if (ev.type === "error") {
          lastError = ev.message || lastError;
        }
      });

      child.on("exit", (code) => {
        if (this.child === child) this.child = null;
        const err = new Error(lastError || `The Atlas core exited with code ${code}.`);
        if (!settled) {
          settle(reject, err);
        } else if (!this.stopping) {
          this.opts.onCrash(err);
        }
      });
    });
  }

  /** Returns the one-time sign-in link once; afterwards the cookie is enough. */
  takeLink() {
    const link = this.link;
    this.link = "";
    return link;
  }

  /** Closes stdin and waits for the child to shut down cleanly. */
  stop() {
    const child = this.child;
    this.stopping = true;
    if (!child) return Promise.resolve();
    return new Promise((resolve) => {
      const timer = setTimeout(() => {
        child.kill();
        resolve();
      }, STOP_TIMEOUT_MS);
      child.once("exit", () => {
        clearTimeout(timer);
        resolve();
      });
      child.stdin.end();
    });
  }
}

module.exports = { Gateway };

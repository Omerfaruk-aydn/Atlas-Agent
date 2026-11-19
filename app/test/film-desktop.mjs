// Records a real window the agent drives outside the app: Chrome for the
// browser tool, Notepad for desktop control.
//
//   node test/film-desktop.mjs <browser|desktop> <project> <out-dir> --bin <exe>
//
// The agent runs headless (`atlas-agent run`) and a second Electron process
// records only the target window.

import { spawn, spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { electronExecutable } from "./electron-bin.mjs";

const [scene, project, outRoot] = process.argv.slice(2);
const flag = (n) => { const i = process.argv.indexOf(n); return i > 0 ? process.argv[i + 1] : ""; };
const bin = flag("--bin");
const here = path.dirname(fileURLToPath(import.meta.url));
const frames = path.join(path.resolve(outRoot), scene);
fs.rmSync(frames, { recursive: true, force: true });
const stop = path.join(os.tmpdir(), `film-stop-${process.pid}`);
fs.rmSync(stop, { force: true });

const scenes = {
  browser: {
    title: "Shop",
    prompt: "Serve the site folder on port 8099 in the background, open http://localhost:8099/index.html in the browser tool, click every link and fix the one that is broken. Reload and click it again to prove it works.",
    setup: () => spawn("python", ["-m", "http.server", "8099", "--directory", path.join(project, "site")], { stdio: "ignore", detached: false }),
  },
  desktop: {
    title: "Not Defteri",
    prompt: "Use the computer tool: open Notepad, type a three-line release note for version 0.20.5 (a headline, one fix and one feature), then save it as release-notes-0.20.5.txt in D:\demo\out and tell me it is saved.",
    setup: () => { fs.mkdirSync("D:\demo\out", { recursive: true }); fs.rmSync("D:\demo\out\release-notes-0.20.5.txt", { force: true }); return null; },
  },
};
const s = scenes[scene];
const helper = spawn(electronExecutable(), [path.join(here, "film-window", "main.cjs"), `--title=${s.title}`, `--out=${frames}`, `--stop=${stop}`], { stdio: ["ignore", "inherit", "inherit"] });
const server = s.setup();
const agent = spawnSync(bin, ["run", "--yolo", "-m", "claude/claude-sonnet-5-5", s.prompt], { cwd: project, stdio: "inherit", timeout: 600000, env: { ...process.env } });
await new Promise((r) => setTimeout(r, 3500));
fs.writeFileSync(stop, "1");
await new Promise((r) => helper.on("exit", r));
server?.kill();
console.log("agent exit", agent.status);

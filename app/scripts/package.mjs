// Builds the desktop app that ships with the CLI: the renderer, the Electron
// folder, and the zip the installers download into <bin>/app.

import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const app = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const dist = path.join(app, "dist");
const unpacked = path.join(dist, "win-unpacked");
const archive = path.join(dist, "atlas-app-windows-x64.zip");

function run(command, args) {
  const result = spawnSync(command, args, { cwd: app, stdio: "inherit", shell: process.platform === "win32" });
  if (result.status !== 0) process.exit(result.status ?? 1);
}

fs.rmSync(dist, { recursive: true, force: true });
run("npm", ["run", "build"]);
run("npx", ["electron-builder", "--win", "--dir", "--publish", "never"]);
// Windows' own tar writes zip files; one from Git or MSYS would not.
const tar = path.join(process.env.SystemRoot ?? "C:/Windows", "System32", "tar.exe");
const zipped = spawnSync(tar, ["-a", "-c", "-f", archive, "-C", unpacked, "."], { stdio: "inherit" });
if (zipped.status !== 0) process.exit(zipped.status ?? 1);

const mb = (fs.statSync(archive).size / 1024 / 1024).toFixed(1);
console.log(`Packaged ${archive} (${mb} MB)`);

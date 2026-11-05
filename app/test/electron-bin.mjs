// The Electron package writes path.txt from its postinstall script. A clean
// npm ci that skips lifecycle scripts, or a runner that never finished the
// download, leaves the file absent and the suite cannot even load. Install
// the binary the same way the package does, then resolve the executable.

import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";
import fs from "node:fs";
import path from "node:path";

const require = createRequire(import.meta.url);

export function electronExecutable() {
  const electronDir = path.dirname(require.resolve("electron/package.json"));
  const marker = path.join(electronDir, "path.txt");
  if (!fs.existsSync(marker)) {
    const install = path.join(electronDir, "install.js");
    const result = spawnSync(process.execPath, [install], { cwd: electronDir, stdio: "inherit" });
    if (result.status !== 0 || !fs.existsSync(marker)) {
      throw new Error(`Electron did not install a binary at ${marker}`);
    }
  }
  return path.join(electronDir, "dist", fs.readFileSync(marker, "utf8").trim());
}

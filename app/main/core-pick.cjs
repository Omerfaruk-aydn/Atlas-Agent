"use strict";

// Which Atlas core the app starts when it was not told. The folder of a staged
// build can hold many `atlas-agent*.exe` (old test builds, renamed copies), so
// the choice must not depend on the order a directory happens to list them in.

const fs = require("node:fs");
const path = require("node:path");

const EXACT = ["atlas-agent.exe", "atlas-agent"];
const CORE = /^atlas-agent.*\.exe$/i;
// "atlas-agent-test.old1234.exe" is a build that was moved aside.
const MOVED_ASIDE = /\.(old|bak|tmp)\d*\.|\.exe~$|\.old$/i;

/**
 * @param {string} dir
 * @param {{readdirSync: Function, statSync: Function}} [files]
 * @returns {string} the full path, or "" when the folder holds none
 */
function pickCore(dir, files = fs) {
  let names;
  try {
    names = files.readdirSync(dir);
  } catch {
    return "";
  }
  const exact = EXACT.find((name) => names.some((n) => n.toLowerCase() === name));
  if (exact) return path.join(dir, names.find((n) => n.toLowerCase() === exact));
  const candidates = names
    .filter((n) => CORE.test(n) && !MOVED_ASIDE.test(n))
    .map((n) => {
      try {
        const stat = files.statSync(path.join(dir, n));
        return stat.isFile() ? { n, at: stat.mtimeMs } : null;
      } catch {
        return null;
      }
    })
    .filter(Boolean)
    // Newest first; the name breaks a tie, so the order is the same every time.
    .sort((a, b) => b.at - a.at || a.n.localeCompare(b.n));
  return candidates[0] ? path.join(dir, candidates[0].n) : "";
}

module.exports = { pickCore };

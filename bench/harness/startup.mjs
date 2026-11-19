// Startup, memory and install-size tests for coding-agent harnesses. None of
// them needs a login or a model, so every harness can be measured the same way
// on the same machine.
//
//   node bench/harness/startup.mjs [--runs 15] [--only atlas,codex]
//
// Latency is the wall time of `<command> --version`, from spawn to exit, the
// way a user meets it, including any launcher shim. Size is the folder the
// command's package lives in.

import { spawn, spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const arg = (n, d) => { const i = process.argv.indexOf(`--${n}`); return i > 0 ? process.argv[i + 1] : d; };
const runs = Number(arg("runs", "15"));
const only = arg("only", "");
const config = JSON.parse(fs.readFileSync(path.join(here, "harnesses.json"), "utf8"));

// One entry per tool, not per model.
const tools = {};
for (const [name, h] of Object.entries(config.harnesses)) if (!tools[h.command]) tools[h.command] = { name: h.label === "Atlas Agent" ? "Atlas Agent" : h.label, command: h.command };
// A locally built binary can be measured next to the installed ones.
const dev = arg("dev", "");
if (dev) tools.__dev = { name: "Atlas Agent (this build)", command: dev };
if (only) for (const k of Object.keys(tools)) if (!only.split(",").includes(k)) delete tools[k];

const median = (xs) => { const s = [...xs].sort((a, b) => a - b); return s.length % 2 ? s[(s.length - 1) / 2] : (s[s.length / 2 - 1] + s[s.length / 2]) / 2; };
const pct = (xs, p) => [...xs].sort((a, b) => a - b)[Math.min(xs.length - 1, Math.floor(xs.length * p))];

function resolve(cmd) {
  const r = spawnSync("where.exe", [cmd], { encoding: "utf8" });
  const hits = r.stdout.split(/\r?\n/).filter(Boolean);
  return hits.find((h) => /\.(exe|cmd)$/i.test(h)) ?? hits[0] ?? "";
}
const viaCmd = (file) => /\.(cmd|bat)$/i.test(file);
function runOnce(file, args) {
  const t0 = process.hrtime.bigint();
  const r = viaCmd(file) ? spawnSync("cmd.exe", ["/d", "/s", "/c", `"${file}" ${args.join(" ")}`], { encoding: "utf8", windowsVerbatimArguments: true }) : spawnSync(file, args, { encoding: "utf8" });
  return { ms: Number(process.hrtime.bigint() - t0) / 1e6, ok: r.status === 0, out: (r.stdout || r.stderr || "").trim().split(/\r?\n/)[0] };
}

function folderSize(dir) {
  let total = 0;
  const walk = (d) => { for (const e of fs.readdirSync(d, { withFileTypes: true })) { const p = path.join(d, e.name); try { e.isDirectory() ? walk(p) : (total += fs.statSync(p).size); } catch {} } };
  try { walk(dir); } catch {}
  return total;
}
// The package folder behind a command: npm global installs keep it in node_modules next to the shim.
function installDir(file) {
  const dir = path.dirname(file);
  const modules = path.join(dir, "node_modules");
  const base = path.basename(file).replace(/\.(cmd|exe|bat)$/i, "");
  if (viaCmd(file) && fs.existsSync(modules)) {
    const hits = [];
    for (const e of fs.readdirSync(modules, { withFileTypes: true })) {
      if (!e.isDirectory()) continue;
      if (e.name.startsWith("@")) for (const s of fs.readdirSync(path.join(modules, e.name))) { if (s.includes(base)) hits.push(path.join(modules, e.name, s)); }
      else if (e.name.includes(base)) hits.push(path.join(modules, e.name));
    }
    if (hits.length) return hits;
  }
  return [file];
}

const rows = [];
for (const t of Object.values(tools)) {
  const file = fs.existsSync(t.command) ? path.resolve(t.command) : resolve(t.command);
  if (!file) { console.error(`${t.command}: not installed, skipped`); continue; }
  const args = ["--version"];
  runOnce(file, args); // warm the disk cache so every tool is measured warm
  const times = [];
  let version = "";
  for (let i = 0; i < runs; i++) { const r = runOnce(file, args); if (r.ok) { times.push(r.ms); version = r.out; } }
  const size = installDir(file).reduce((n, d) => n + (fs.statSync(d).isDirectory() ? folderSize(d) : fs.statSync(d).size), 0);
  rows.push({ tool: t.name, command: t.command, version, runs: times.length, median_ms: Math.round(median(times)), p95_ms: Math.round(pct(times, 0.95)), install_mb: Math.round(size / 1048576) });
  console.log(`${t.name.padEnd(12)} ${version.slice(0, 22).padEnd(22)} start ${Math.round(median(times))} ms (p95 ${Math.round(pct(times, 0.95))})  install ${Math.round(size / 1048576)} MB`);
}
const out = path.join(here, "results", `startup-${new Date().toISOString().replace(/[:.]/g, "-")}.json`);
fs.mkdirSync(path.dirname(out), { recursive: true });
fs.writeFileSync(out, JSON.stringify({ started: new Date().toISOString(), runs, machine: `${os.cpus()[0].model.trim()}, ${Math.round(os.totalmem() / 2 ** 30)} GB, ${os.platform()} ${os.release()}`, rows }, null, 2));
console.log(out);

// Runs the same bench tasks through several coding-agent harnesses and
// scores each attempt with the task's hidden checks.
//
//   node bench/harness/run.mjs --harnesses atlas,claude-code --tasks 10 --repeats 1
//
// Every attempt starts from a fresh copy of the task repository in a
// temporary git repository. The harness gets only the task prompt. When it
// exits, the hidden checks are copied in and the task's check command
// decides pass or fail, exactly as `atlas eval bench` does. Results are
// written to bench/harness/results/<stamp>.json and summarized by chart.mjs.

import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const suite = path.resolve(here, "..", "tasks");

function arg(name, fallback) {
  const i = process.argv.indexOf(`--${name}`);
  return i > 0 ? process.argv[i + 1] : fallback;
}

const config = JSON.parse(fs.readFileSync(arg("config", path.join(here, "harnesses.json")), "utf8"));
const wanted = arg("harnesses", Object.keys(config.harnesses).join(",")).split(",");
const repeats = Number(arg("repeats", "1"));
const only = arg("only", "");
let tasks = fs.readdirSync(suite).filter((d) => fs.existsSync(path.join(suite, d, "task.json")));
if (only) tasks = tasks.filter((t) => only.split(",").includes(t));
tasks = tasks.slice(0, Number(arg("tasks", String(tasks.length))));

const stamp = new Date().toISOString().replace(/[:.]/g, "-");
const outFile = path.join(here, "results", `${stamp}.json`);
fs.mkdirSync(path.dirname(outFile), { recursive: true });
const results = { started: new Date().toISOString(), config, tasks, runs: [] };

function copyDir(from, to) {
  fs.cpSync(from, to, { recursive: true });
}

// npm installs commands as .cmd shims on Windows, which only cmd.exe can
// start. Quote every argument for cmd so a prompt stays one argument and
// characters such as ^ & | < > reach the program unchanged.
const winQuote = (a) => `"${String(a).replaceAll('"', '""').replaceAll("%", "%%")}"`;

function run(cmd, args, cwd, timeoutMs, env = {}) {
  const t0 = Date.now();
  const viaCmd = process.platform === "win32" && !/\.(exe|com)$/i.test(cmd);
  const opts = { cwd, timeout: timeoutMs, encoding: "utf8", env: { ...process.env, ...env }, maxBuffer: 64 * 1024 * 1024, windowsVerbatimArguments: viaCmd };
  const r = viaCmd ? spawnSync("cmd.exe", ["/d", "/s", "/c", `"${[cmd, ...args].map(winQuote).join(" ")}"`], opts) : spawnSync(cmd, args, opts);
  return { code: r.status, timedOut: r.error?.code === "ETIMEDOUT", seconds: (Date.now() - t0) / 1000, stdout: r.stdout ?? "", stderr: r.stderr ?? "" };
}

// Usage numbers each harness prints in its JSON output, when it prints any.
function usage(name, stdout) {
  const last = stdout.trim().split("\n").reverse().find((l) => l.trim().startsWith("{"));
  if (!last) return {};
  try {
    const j = JSON.parse(last);
    if (name.startsWith("atlas")) return { cost: j.cost, tokens: (j.prompt_tokens ?? 0) + (j.completion_tokens ?? 0) || undefined };
    if (name.startsWith("claude")) return { cost: j.total_cost_usd, tokens: j.usage ? (j.usage.input_tokens ?? 0) + (j.usage.output_tokens ?? 0) + (j.usage.cache_read_input_tokens ?? 0) + (j.usage.cache_creation_input_tokens ?? 0) : undefined };
  } catch {}
  return {};
}

for (const name of wanted) {
  const h = config.harnesses[name];
  if (!h) throw new Error(`unknown harness ${name}`);
  for (const id of tasks) {
    const task = JSON.parse(fs.readFileSync(path.join(suite, id, "task.json"), "utf8"));
    for (let rep = 0; rep < repeats; rep++) {
      const work = fs.mkdtempSync(path.join(os.tmpdir(), `hb-${name}-${id}-`));
      copyDir(path.join(suite, id, "repo"), work);
      run("git", ["init", "-q"], work, 30000);
      run("git", ["add", "-A"], work, 30000);
      run("git", ["-c", "user.name=bench", "-c", "user.email=bench@example.invalid", "commit", "-qm", "task"], work, 30000);

      const args = h.args.map((a) => a.replaceAll("{prompt}", task.prompt).replaceAll("{cwd}", work));
      const agent = run(h.command, args, work, (config.timeout_seconds ?? 600) * 1000, h.env);

      // A harness that changed nothing and exited with an error never got to
      // work (no login, unknown model). That is a setup error, not a failed
      // task, and it is reported apart so it cannot skew the pass rate.
      const changed = run("git", ["status", "--porcelain"], work, 30000).stdout.trim() !== "";
      const setupError = !changed && agent.code !== 0;

      copyDir(path.join(suite, id, "hidden"), work);
      const check = task.check.map((a) => a.replaceAll("{suite}", suite));
      const verdict = run(check[0], check.slice(1), work, (task.timeout_seconds ?? 180) * 1000);
      const entry = { harness: name, task: id, repeat: rep, pass: !setupError && verdict.code === 0, error: setupError ? (agent.stderr + agent.stdout).trim().slice(-400) : undefined, seconds: agent.seconds, timedOut: agent.timedOut, agentExit: agent.code, ...usage(name, agent.stdout), checkTail: (verdict.stdout + verdict.stderr).slice(-600) };
      results.runs.push(entry);
      fs.writeFileSync(outFile, JSON.stringify(results, null, 2));
      console.log(`${name.padEnd(14)} ${id.padEnd(34)} ${entry.error ? "ERROR" : entry.pass ? "PASS" : "fail"} ${entry.seconds.toFixed(0)}s`);
      fs.rmSync(work, { recursive: true, force: true });
    }
  }
}
results.finished = new Date().toISOString();
fs.writeFileSync(outFile, JSON.stringify(results, null, 2));
console.log(`\n${outFile}`);

// Takes the README screenshots against the fake gateway with an English
// session: `node test/readme-shots.mjs ../docs/assets`, after `npm run build`.

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { _electron as electron } from "playwright-core";
import { electronExecutable } from "./electron-bin.mjs";
import { assistantMessage, session, startFakeGateway, userMessage } from "./fake-gateway.mjs";

const out = path.resolve(process.argv[2] ?? "shots");
fs.mkdirSync(out, { recursive: true });
const appDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

const gateway = await startFakeGateway({ renderer: path.join(appDir, "renderer", "dist") });
const call = (id, name, input) => ({ type: "tool_call", data: { id, name, input: JSON.stringify(input), finished: true } });
const result = (id, name, content) => ({ type: "tool_result", data: { tool_call_id: id, name, content, metadata: "", is_error: false } });
const text = (t) => ({ type: "text", data: { text: t } });

const messages = [
  userMessage("u1", "s1", "TestParseConfig is failing on CI. Find out why and fix it."),
  assistantMessage("a1", "s1", [
    text("I'll start by running the failing test to see the exact error."),
    call("c1", "bash", { command: "go test ./internal/config -run TestParseConfig" }),
    result("c1", "bash", "--- FAIL: TestParseConfig (0.00s)\n    config_test.go:42: expected timeout 30s, got 0s\nFAIL"),
    call("c2", "grep", { pattern: "Timeout", path: "internal/config" }),
    result("c2", "grep", "internal/config/parse.go:57:\tcfg.Timeout = raw.TimeoutSecs * time.Second"),
    call("c3", "edit", { file_path: "internal/config/parse.go", old_string: "raw.TimeoutSecs * time.Second", new_string: "time.Duration(raw.TimeoutSecs) * time.Second" }),
    result("c3", "edit", "Edited internal/config/parse.go"),
    call("c4", "bash", { command: "go test ./internal/config" }),
    result("c4", "bash", "ok  \tgithub.com/example/app/internal/config\t0.012s"),
    text("**Fixed.** `TestParseConfig` failed because `TimeoutSecs` was multiplied by `time.Second` without converting it to a `time.Duration` first, so the timeout was parsed as zero.\n\nI changed `internal/config/parse.go:57` and the whole `internal/config` package passes now. Want me to add a regression test for a zero value?"),
  ]),
];
await gateway.seed({ sessions: [session("s1", "Fix the failing config test", { prompt_tokens: 48200, completion_tokens: 1900, cost: 0.12 })], messages: { s1: messages } });
await gateway.modes(
  [
    ["orchestrator", "Plans the work, runs workers in parallel and has the result verified."],
    ["review", "Reviews changed code for correctness, security and maintainability."],
    ["research", "Reads the code and the web, then reports what it found."],
    ["security", "Looks for vulnerabilities and unsafe patterns."],
    ["test", "Writes and repairs tests."],
  ].map(([name, description]) => ({ name, description })),
);

const userData = fs.mkdtempSync(path.join(os.tmpdir(), "atlas-readme-"));
fs.writeFileSync(path.join(userData, "prefs.json"), JSON.stringify({ theme: "dark", ui: { lang: "en", onboarded: true } }));
const project = path.join(os.tmpdir(), "atlas-e2e-project");
fs.mkdirSync(project, { recursive: true });
const app = await electron.launch({ executablePath: electronExecutable(), args: [appDir, `--project=${project}`], env: { ...process.env, ATLAS_APP_DEV_GATEWAY: gateway.origin, ATLAS_APP_USER_DATA: userData } });
const page = await app.firstWindow();
await page.setViewportSize({ width: 1440, height: 900 });
await page.waitForLoadState();
await page.waitForSelector(".composer textarea");
await page.click(".session-main");
await page.waitForTimeout(800);
await page.screenshot({ path: path.join(out, "hero.png") });
await page.click('[title="Agent mode"]').catch(() => page.click('[title="Ajan modu"]'));
await page.waitForTimeout(400);
await page.screenshot({ path: path.join(out, "modes.png") });
await app.close();
await gateway.close?.();

// Deterministic web regressions driven by Atlas's own Go browser-use
// pipeline. Playwright is not used as a second execution engine here; the
// existing Playwright suite remains independent until coverage is equivalent.
import { execFileSync } from "node:child_process"
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { chromiumPath, launchURL, repo, startStack, type Stack } from "./stack.ts"

type Step = Record<string, unknown>

const visible = (id: string, selector: string): Step => ({ id, action: "assert", target: { selector, condition: "visible", timeout_ms: 20000 } })
const text = (id: string, selector: string, expected: string): Step => ({ id, action: "wait_for", target: { selector, condition: "text_contains", expected, timeout_ms: 30000 } })
const click = (id: string, selector: string): Step => ({ id, action: "click", target: { selector } })
const type = (id: string, selector: string, value: string): Step => ({ id, action: "type", target: { selector, text: value } })
const key = (id: string, value: string): Step => ({ id, action: "key", key: value })
const composer = "[data-component='composer'] [role='textbox']"
const timeline = "[data-slot='session-timeline-scroll']"
const home = "[data-slot='home-panel']"

function signedIn(url: string): Step[] {
  return [{ id: "sign-in", action: "navigate", url }, visible("home", home)]
}

function newSession(prefix = "draft"): Step[] {
  return [click(`${prefix}-new`, "[data-action='home-new-session']"), visible(`${prefix}-composer`, composer)]
}

function send(id: string, prompt: string): Step[] {
  return [type(`${id}-fill`, composer, prompt), key(`${id}-enter`, "enter")]
}

function scenarios(stack: Stack, links: string[]): { name: string; workflow: { steps: Step[] } }[] {
  const marker = `browser-use-${Date.now()}`
  return [
    {
      name: "session-stream-tool-and-reopen",
      workflow: { steps: [
        ...signedIn(links[0]), ...newSession(), ...send("tool-prompt", `${marker} [[bash]]`),
        text("streaming-tool-result", timeline, "Tool finished. Result:"),
        key("reload", "ctrl+r"), text("reopened-tool-result", timeline, "Tool finished. Result:"),
        { id: "single-user-message", action: "assert", target: { selector: "[data-component='user-message']", condition: "count", expected: "1" } },
        { id: "single-tool-result", action: "assert", target: { selector: "[data-component='markdown']", condition: "text_contains", expected: "pipeline-output" } },
      ] },
    },
    {
      name: "cancel-and-model-error",
      workflow: { steps: [
        ...signedIn(links[1]), ...newSession(), ...send("slow-prompt", "[[slow]]"),
        text("stream-progress", timeline, "tick 3"),
        click("stop", "[data-action='composer-submit']"),
        { id: "stopped", action: "wait_for", target: { selector: "[data-action='composer-submit'][aria-label='Stop']", condition: "hidden", timeout_ms: 15000 } },
        ...send("error-prompt", "[[error]]"),
        { id: "visible-model-error", action: "wait_for", target: { selector: timeline, condition: "text_contains", expected: "Bad Request", timeout_ms: 30000 } },
      ] },
    },
    {
      name: "project-and-workspace-isolation",
      workflow: { steps: [
        // The projects dropdown exists only below 768px. The regression
        // browser is wider, so the sidebar add button is the control on screen.
        ...signedIn(links[2]),
        click("add-project", "[data-action='home-add-project']"),
        visible("add-project-dialog", "[role='dialog'] input"),
        type("project-path", "[role='dialog'] input", stack.second),
        click("open-project", "[role='dialog'] button[type='submit']"),
        { id: "second-project", action: "wait_for", target: { role: "button", name: "S second", match: "exact", condition: "visible", timeout_ms: 20000 } },
        ...newSession("isolated"), ...send("isolated-prompt", `${marker}-second`),
        text("second-response", timeline, `Echo: ${marker}-second`),
        { id: "other-session-not-visible", action: "assert", target: { selector: "[data-component='user-message']", condition: "count", expected: "1" } },
      ] },
    },
  ]
}

function cleanupDir(path: string) {
  try {
    rmSync(path, { recursive: true, force: true, maxRetries: 20, retryDelay: 250 })
  } catch (error) {
    console.warn(`Temporary directory cleanup failed for ${path}: ${String(error)}`)
  }
}

async function main() {
  const chromium = chromiumPath()
  if (!chromium) throw new Error("ATLAS_E2E_CHROMIUM or a supported Playwright Chromium installation is required")
  const stack = await startStack()
  const artifacts = join(repo, "web", "test-results", "browser-use")
  mkdirSync(artifacts, { recursive: true })
  try {
    const runner = join(stack.root, process.platform === "win32" ? "webregress.exe" : "webregress")
    execFileSync("go", ["build", "-o", runner, "./internal/webui/webtest/webregress"], { cwd: repo, stdio: "inherit" })
    const links = await Promise.all([0, 1, 2].map(() => launchURL(stack)))
    for (const [index, scenario] of scenarios(stack, links).entries()) {
      const root = mkdtempSync(join(tmpdir(), "atlas-browser-use-"))
      try {
        const scenarioPath = join(root, "scenario.json")
        writeFileSync(scenarioPath, JSON.stringify(scenario), { mode: 0o600 })
        execFileSync(runner, [
          "-scenario", scenarioPath,
          "-chromium", chromium,
          "-profile", join(root, "profile"),
          "-working-dir", join(root, "state"),
          "-artifacts", join(artifacts, `${index + 1}-${scenario.name}`),
        ], { cwd: repo, stdio: "inherit", timeout: 150_000, env: { ...process.env, ATLAS_E2E: "1" } })
      } finally {
        cleanupDir(root)
      }
    }
  } finally {
    for (const pid of [...stack.pids].reverse()) {
      try { process.kill(pid) } catch { /* Already stopped. */ }
    }
    cleanupDir(stack.root)
  }
}

await main()

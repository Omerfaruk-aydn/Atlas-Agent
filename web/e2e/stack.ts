// Starts the real Atlas stack for browser tests: the scripted model
// (internal/webui/webtest/fakellm), an isolated `atlas-agent server` on a
// private TCP port, and `atlas-agent web` in front of it. Nothing touches
// the user's own server, config or data: every directory is a temp dir and
// the server listens on a port of its own.

import { execFileSync, spawn, type ChildProcess } from "node:child_process"
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, writeFileSync } from "node:fs"
import { createServer } from "node:net"
import { tmpdir, homedir } from "node:os"
import { dirname, join, resolve } from "node:path"
import { fileURLToPath } from "node:url"

export const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..")
export const exe = process.env.ATLAS_E2E_EXE ?? join(repo, "dist", "dev", process.platform === "win32" ? "atlas-web-dev.exe" : "atlas-web-dev")
export const stateFile = join(repo, "web", "test-results", "stack.json")

export interface Stack {
  root: string
  project: string
  second: string
  serverHost: string
  serverURL: string
  base: string
  port: string
  env: Record<string, string>
  pids: number[]
}

function freePort(): Promise<number> {
  return new Promise((res, rej) => {
    const srv = createServer()
    srv.listen(0, "127.0.0.1", () => {
      const port = (srv.address() as { port: number }).port
      srv.close(() => res(port))
    })
    srv.on("error", rej)
  })
}

function waitFor<T>(fn: () => T | undefined, ms: number, what: string): Promise<T> {
  const until = Date.now() + ms
  return new Promise((res, rej) => {
    const tick = () => {
      try {
        const v = fn()
        if (v !== undefined) return res(v)
      } catch {
        // Not ready yet.
      }
      if (Date.now() > until) return rej(new Error(`timed out waiting for ${what}`))
      setTimeout(tick, 100)
    }
    tick()
  })
}

/** chromiumPath finds an installed Playwright Chromium, newest first, so
 * the tests run without downloading a browser. */
export function chromiumPath(): string | undefined {
  if (process.env.ATLAS_E2E_CHROMIUM) return process.env.ATLAS_E2E_CHROMIUM
  const dir = join(process.env.LOCALAPPDATA ?? join(homedir(), "AppData", "Local"), "ms-playwright")
  if (!existsSync(dir)) return undefined
  const builds = readdirSync(dir)
    .filter((d) => /^chromium-\d+$/.test(d))
    .sort((a, b) => Number(b.split("-")[1]) - Number(a.split("-")[1]))
  for (const b of builds) {
    const p = join(dir, b, "chrome-win64", "chrome.exe")
    if (existsSync(p)) return p
  }
  return undefined
}

export async function startStack(): Promise<Stack> {
  if (!existsSync(exe)) throw new Error(`${exe} is missing; run \`task build:web-dev\` first`)
  const root = mkdtempSync(join(tmpdir(), "atlas-web-e2e-"))
  const project = join(root, "project")
  const second = join(root, "second")
  for (const d of [project, second]) mkdirSync(d, { recursive: true })
  writeFileSync(join(project, "README.md"), "# Demo project\n\nUsed by the web UI end-to-end tests.\n")
  writeFileSync(join(project, "main.go"), 'package main\n\nfunc main() {\n\tprintln("hello")\n}\n')
  writeFileSync(join(second, "notes.txt"), "second project\n")

  const fakellm = join(root, process.platform === "win32" ? "fakellm.exe" : "fakellm")
  execFileSync("go", ["build", "-o", fakellm, "./internal/webui/webtest/fakellm"], {
    cwd: repo,
    stdio: "inherit",
    env: { ...process.env, CGO_ENABLED: "0" },
  })
  const pids: number[] = []
  const track = (p: ChildProcess, name: string) => {
    if (p.pid) pids.push(p.pid)
    p.on("exit", (code) => code && code !== 0 && console.error(`${name} exited with ${code}`))
    return p
  }

  let llmOut = ""
  const llm = track(spawn(fakellm, [join(root, "atlas")], { stdio: ["ignore", "pipe", "inherit"] }), "fakellm")
  llm.stdout!.on("data", (b) => (llmOut += b))
  const { env } = await waitFor(() => (llmOut.includes("\n") ? (JSON.parse(llmOut.split("\n")[0]) as { env: Record<string, string> }) : undefined), 15_000, "fake model")

  const serverPort = await freePort()
  const webPort = await freePort()
  const serverHost = `tcp://127.0.0.1:${serverPort}`
  const childEnv: NodeJS.ProcessEnv = { ...process.env, ...env, ATLAS_AGENT_SERVER_IDLE_TIMEOUT: "600" }
  delete childEnv.ATLAS_AGENT_CLIENT_SERVER

  track(spawn(exe, ["server", "--host", serverHost], { env: childEnv, stdio: "ignore" }), "server")
  await new Promise((r) => setTimeout(r, 1500))

  let webOut = ""
  const web = track(
    spawn(exe, ["web", "--host", serverHost, "--port", String(webPort), "--open=false", "--cwd", project], { env: childEnv, stdio: ["ignore", "pipe", "pipe"] }),
    "web",
  )
  web.stdout!.on("data", (b) => (webOut += b))
  web.stderr!.on("data", (b) => (webOut += b))
  await waitFor(() => (/Open: (http\S+)/.test(webOut) ? true : undefined), 60_000, `web UI (${webOut})`)

  const stack: Stack = {
    root,
    project,
    second,
    serverHost,
    serverURL: `http://127.0.0.1:${serverPort}`,
    base: `http://127.0.0.1:${webPort}`,
    port: String(webPort),
    env: childEnv as Record<string, string>,
    pids,
  }
  mkdirSync(join(repo, "web", "test-results"), { recursive: true })
  writeFileSync(stateFile, JSON.stringify(stack, null, 2))
  return stack
}

export function loadStack(): Stack {
  return JSON.parse(readFileSync(stateFile, "utf8"))
}

/** launchURL asks the running web UI for a fresh one-time sign-in link,
 * the way a second `atlas-agent web` does. */
export async function launchURL(stack: Stack): Promise<string> {
  const control = JSON.parse(readFileSync(join(stack.env.ATLAS_AGENT_CACHE_DIR, "web", `gateway-${stack.port}.json`), "utf8"))
  const rsp = await fetch(`${stack.base}/control/launch`, { method: "POST", headers: { "X-Atlas-Control": control.secret } })
  if (!rsp.ok) throw new Error(`control launch: ${rsp.status}`)
  return ((await rsp.json()) as { url: string }).url
}

/** server calls the Atlas server's own API as another client would. */
export async function server<T = any>(stack: Stack, method: string, path: string, body?: unknown): Promise<T> {
  const rsp = await fetch(`${stack.serverURL}/v1${path}`, {
    method,
    headers: body === undefined ? {} : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!rsp.ok) throw new Error(`${method} ${path}: ${rsp.status} ${await rsp.text()}`)
  const text = await rsp.text()
  return (text ? JSON.parse(text) : undefined) as T
}

/** cli runs the real terminal client non-interactively against the stack's
 * server, as `ATLAS_AGENT_CLIENT_SERVER=1 atlas-agent run` does. */
export function cli(stack: Stack, args: string[]): string {
  return execFileSync(exe, ["--host", stack.serverHost, "--cwd", stack.project, ...args], {
    env: { ...stack.env, ATLAS_AGENT_CLIENT_SERVER: "1" },
    encoding: "utf8",
    timeout: 120_000,
  })
}

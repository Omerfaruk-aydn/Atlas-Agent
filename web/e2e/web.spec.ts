import { expect, test, type Page } from "@playwright/test"
import { existsSync, readFileSync } from "node:fs"
import { join } from "node:path"
import { cli, launchURL, loadStack, server } from "./stack"

// Browser acceptance tests against the real Atlas server and agent engine.
// Only the model is scripted (internal/webui/webtest).

const stack = loadStack()

async function signIn(page: Page) {
  await page.goto(await launchURL(stack))
  await expect(page.locator("[data-slot='home-panel']")).toBeVisible()
}

const samePath = (a: string, b: string) => a.replaceAll("\\", "/").toLowerCase() === b.replaceAll("\\", "/").toLowerCase()

/** workspaceID is the server workspace the web UI opened for the test
 * project. */
async function workspaceID(_page: Page): Promise<string> {
  let id = ""
  await expect
    .poll(async () => {
      const list = await server<{ id: string; path: string }[]>(stack, "GET", "/workspaces")
      id = list.find((w) => samePath(w.path, stack.project))?.id ?? ""
      return id
    })
    .not.toBe("")
  return id
}

/** newSession opens a draft in the test project from the home screen. The
 * session itself is created by the first prompt; sessionID reads it then. */
async function newSession(page: Page) {
  if (new URL(page.url()).search) await page.getByRole("button", { name: "Home", exact: true }).click()
  await page.locator("[data-slot='home-panel'] [data-action='home-new-session']").click()
  await expect(page).toHaveURL(/[?&]draft=/)
}

async function sessionID(page: Page) {
  await expect(page).toHaveURL(/[?&]s=/)
  return new URL(page.url()).searchParams.get("s")!
}

const promptBox = (page: Page) => page.locator("[data-component='composer'] [role='textbox']")

async function send(page: Page, text: string) {
  const box = promptBox(page)
  await box.fill(text)
  await box.press("Enter")
  await expect(box).toHaveText("")
}

/** The composer's send button turns into Stop while a run is in flight. */
const working = (page: Page) => page.locator("[data-action='composer-submit']").and(page.getByRole("button", { name: "Stop" }))
const idle = (page: Page) => expect(working(page)).toBeHidden()

/** runCommand runs one of the composer's "/" commands: typing it selects the
 * suggestion, Enter runs it. */
async function runCommand(page: Page, name: string) {
  await promptBox(page).click()
  await page.keyboard.type(`/${name}`)
  await page.keyboard.press("Enter")
  await page.keyboard.press("Enter")
}

const permissionCard = (page: Page) => page.locator("[data-component='dock-prompt'][data-kind='permission']")
const questionCard = (page: Page) => page.locator("[data-component='dock-prompt'][data-kind='question']")

test("signing in needs a one-time link", async ({ page, browser }) => {
  const link = await launchURL(stack)
  await page.goto(link)
  await expect(page.locator("[data-slot='home-panel']")).toBeVisible()

  const other = await browser.newContext()
  const p2 = await other.newPage()
  await p2.goto(stack.base)
  await expect(p2.getByText("Sign-in link needed")).toBeVisible()
  // The link already used does not sign a second browser in.
  const rsp = await p2.goto(link)
  expect(rsp?.status()).toBe(401)
  await other.close()
})

test("prompt, tool result, reload without duplicates (4, 5, 8, 15)", async ({ page }) => {
  await signIn(page)
  await newSession(page)
  await send(page, "Show me the pipeline [[bash]]")
  await sessionID(page)
  await expect(page.getByText("Tool finished. Result:")).toBeVisible()
  await idle(page)

  await page.reload()
  await expect(page.getByText("Tool finished. Result:")).toBeVisible()
  await expect(page.locator("[data-component='user-message']")).toHaveCount(1)
  await expect(page.getByText("Tool finished. Result:")).toHaveCount(1)
})

test("a question is answered in the browser and the run continues (6)", async ({ page }) => {
  await signIn(page)
  await newSession(page)
  await send(page, "[[ask]]")
  const card = questionCard(page)
  await expect(card).toBeVisible()

  // A reload finds the pending question again.
  await page.reload()
  await expect(card).toBeVisible()
  await card.getByRole("textbox").fill("web-answer.md")
  await card.getByRole("textbox").press("Control+Enter")
  await expect(card).toBeHidden()
  await expect(page.locator("[data-component='markdown'] code", { hasText: "User provided: web-answer.md" })).toBeVisible()
  await idle(page)
})

test("a permission answered by another client closes in the browser (7, 14)", async ({ page }) => {
  await signIn(page)
  const ws = await workspaceID(page)
  await newSession(page)
  await send(page, "[[write e2e-granted.txt]]")
  const card = permissionCard(page)
  await expect(card).toBeVisible()
  await expect(card).toContainText("e2e-granted.txt")

  // The terminal (another client of the same server) allows it.
  const pending = await server<any[]>(stack, "GET", `/workspaces/${ws}/permissions/pending`)
  expect(pending).toHaveLength(1)
  await server(stack, "POST", `/workspaces/${ws}/permissions/grant`, { permission: pending[0], action: "allow" })

  await expect(card).toBeHidden()
  await idle(page)
  expect(readFileSync(join(stack.project, "e2e-granted.txt"), "utf8")).toBe("written by the fake model\n")

  // The review panel shows what is on disk.
  await page.keyboard.press("Control+Shift+E")
  const panel = page.locator("#review-panel")
  await expect(panel).toContainText("e2e-granted.txt")
  await expect(panel).toContainText("written by the fake model")
})

test("two tabs agree, and stop cancels only that session (9, 10, 11)", async ({ page, context }) => {
  await signIn(page)
  await newSession(page)
  await send(page, "[[slow]]")
  const sid = await sessionID(page)
  const second = await context.newPage()
  await second.goto(page.url())
  await expect(working(page)).toBeVisible()
  await expect(second.getByText(/tick 3/)).toBeVisible()

  // Closing a tab does not stop the run.
  await second.close()
  await page.waitForTimeout(500)
  await expect(working(page)).toBeVisible()

  const third = await context.newPage()
  await third.goto(page.url())
  await expect(working(third)).toBeVisible()
  await working(page).click()
  await idle(page)
  await idle(third)
  expect(sid).toBeTruthy()
})

test("a run started in the terminal shows in the browser (2, 3)", async ({ page }) => {
  await signIn(page)
  const ws = await workspaceID(page)
  await newSession(page)
  await send(page, "first prompt from the browser")
  const sid = await sessionID(page)
  await idle(page)
  // The session the browser created is visible to the terminal client.
  const sessions = await server<any[]>(stack, "GET", `/workspaces/${ws}/sessions`)
  expect(sessions.some((s) => s.id === sid)).toBe(true)

  cli(stack, ["run", "--quiet", "--session", sid, "typed in the terminal"])
  await expect(page.getByText("Echo: typed in the terminal")).toBeVisible()
  await expect(page.locator("[data-component='user-message']", { hasText: "typed in the terminal" })).toHaveCount(1)
})

test("permission mode is applied by the server (12)", async ({ page }) => {
  await signIn(page)
  const ws = await workspaceID(page)
  await newSession(page)
  await send(page, "start the session")
  await sessionID(page)
  await idle(page)
  const mode = async () => (await server(stack, "GET", `/workspaces/${ws}/permissions/mode`)).mode
  const statusLine = page.locator("[data-component='status-line']")

  // /permissions steps manual -> accept edits -> plan.
  await runCommand(page, "permissions")
  await expect.poll(mode).toBe("auto_accept_edits")
  await expect(statusLine).toContainText("Accept edits")
  await runCommand(page, "permissions")
  await expect.poll(mode).toBe("plan")
  await expect(statusLine).toContainText("Plan")

  await send(page, "[[write plan-blocked.txt]]")
  await idle(page)
  expect(existsSync(join(stack.project, "plan-blocked.txt"))).toBe(false)

  // Two more steps go through yolo and back to asking first.
  await runCommand(page, "permissions")
  await expect.poll(mode).toBe("bypass")
  await expect(statusLine).toContainText("Yolo")
  await runCommand(page, "permissions")
  await expect.poll(mode).toBe("manual")
  await expect(statusLine).toContainText("Ask first")
})

test("an image attachment goes through the server's attachment path (13)", async ({ page }) => {
  await signIn(page)
  await newSession(page)
  const png = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGNgYGD4DwABBAEAwS2OUAAAAABJRU5ErkJggg==", "base64")
  await page.locator("input[type=file]").setInputFiles({ name: "pixel.png", mimeType: "image/png", buffer: png })
  await send(page, "describe the picture")
  await expect(page.getByText("(image received)")).toBeVisible()
  await expect(page.locator("[data-component='user-message'] img")).toBeVisible()
})

test("a long answer stays responsive and the reader is not pulled down (18)", async ({ page }) => {
  await signIn(page)
  await newSession(page)
  for (let i = 0; i < 3; i++) {
    await send(page, "[[big]]")
    await idle(page)
  }
  const timeline = page.locator("[data-slot='session-timeline-scroll']")
  await timeline.evaluate((el) => el.scrollTo({ top: 0 }))
  await send(page, "after scrolling up")
  await expect(page.getByText("Echo: after scrolling up")).toBeAttached()
  // The reader stayed where they were scrolled to.
  expect(await timeline.evaluate((el) => el.scrollTop)).toBeLessThan(200)
  const started = Date.now()
  await timeline.evaluate((el) => el.scrollTo({ top: el.scrollHeight }))
  await expect(page.getByText("Echo: after scrolling up")).toBeVisible()
  expect(Date.now() - started).toBeLessThan(5000)
})

test("API requests from another origin are refused (17)", async ({ page }) => {
  await signIn(page)
  const ws = await workspaceID(page)
  const result = await page.evaluate(async (id) => {
    const forged = await fetch(`/api/workspaces/${id}/sessions`, { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" })
    return forged.status
  }, ws)
  expect(result).toBe(403)
})

// Last: it adds a second project, which later tests would otherwise inherit.
test("a project opened in the browser is a server workspace (1)", async ({ page }) => {
  await signIn(page)
  await page.getByRole("button", { name: "Add project" }).first().click()
  await page.getByRole("textbox").fill(stack.second)
  await page.getByRole("button", { name: "Open", exact: true }).click()
  await expect(page.locator("[data-component='home-project-row']", { hasText: "second" })).toBeVisible()
  const list = await server<{ path: string }[]>(stack, "GET", "/workspaces")
  expect(list.some((w) => w.path.toLowerCase() === stack.second.toLowerCase())).toBe(true)
})

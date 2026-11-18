// End-to-end tests: the real Electron shell and renderer against the fake
// gateway. Run with `npm run test:e2e` after `npm run build`.

import { spawn, spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { _electron as electron } from "playwright-core";
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, test } from "vitest";
import { electronExecutable } from "./electron-bin.mjs";
import { assistantMessage, session, startFakeGateway, userMessage } from "./fake-gateway.mjs";

const appDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const electronBin = electronExecutable();
const axeSource = fs.readFileSync(path.join(appDir, "node_modules", "axe-core", "axe.min.js"), "utf8");

let gateway;
let app;
let page;
let userData;

async function launch(prefs = { ui: { onboarded: true, lang: "en" } }, extraArgs = []) {
  userData = fs.mkdtempSync(path.join(os.tmpdir(), "atlas-e2e-"));
  fs.writeFileSync(path.join(userData, "prefs.json"), JSON.stringify(prefs));
  app = await electron.launch({
    executablePath: electronBin,
    args: [appDir, `--project=${path.join(os.tmpdir(), "atlas-e2e-project")}`, ...extraArgs],
    env: { ...process.env, ATLAS_APP_DEV_GATEWAY: gateway.origin, ATLAS_APP_USER_DATA: userData, ATLAS_APP_TEST_CONFIRM: process.env.ATLAS_APP_TEST_CONFIRM ?? "yes" },
  });
  page = await app.firstWindow();
  await page.setViewportSize?.({ width: 1280, height: 800 });
}

const ready = () => page.waitForSelector(".composer textarea");

function parts(text) {
  return [{ type: "text", data: { text } }];
}

/** A chat with a finished first turn, open in the window. */
async function openChat() {
  await gateway.seed({
    sessions: [session("s1", "First chat")],
    messages: { s1: [userMessage("u1", "s1", "Hello"), assistantMessage("a1", "s1", parts("Hi there."))] },
  });
  await launch();
  await page.click(".session-main");
  await ready();
}

/** Presses Escape until the element is gone: under load a key can reach the window before the menu has focus. */
async function escapeUntilGone(selector) {
  for (let i = 0; i < 6; i++) {
    await page.keyboard.press("Escape");
    try {
      await page.waitForSelector(selector, { state: "detached", timeout: 3000 });
      return;
    } catch {
      // Not closed yet: press again.
    }
  }
  await page.waitForSelector(selector, { state: "detached" });
}

beforeAll(async () => {
  fs.mkdirSync(path.join(os.tmpdir(), "atlas-e2e-project"), { recursive: true });
  gateway = await startFakeGateway({ renderer: path.join(appDir, "renderer", "dist") });
});

afterAll(async () => {
  await gateway.close();
});

beforeEach(async () => {
  await gateway.seed({ sessions: [], messages: {} });
  await gateway.refuse(false);
});

afterEach(async () => {
  const proc = app?.process();
  const closed = await Promise.race([app?.close().then(() => true), new Promise((r) => setTimeout(() => r(false), 8000))]);
  expect(closed, "the app quits when asked").toBe(true);
  if (proc?.exitCode === null) spawnSync("taskkill", ["/T", "/F", "/PID", String(proc.pid)]);
  try {
    fs.rmSync(userData, { recursive: true, force: true, maxRetries: 8, retryDelay: 250 });
  } catch {
    // Chromium's helpers can hold the folder a moment longer; it is a temp folder.
  }
});

describe("first launch", () => {
  test("a second launch during startup waits for the gateway instead of opening a blank window", async () => {
    userData = fs.mkdtempSync(path.join(os.tmpdir(), "atlas-e2e-startup-"));
    fs.writeFileSync(path.join(userData, "prefs.json"), JSON.stringify({ ui: { onboarded: true, lang: "en" } }));
    const project = path.join(os.tmpdir(), "atlas-e2e-project");
    const env = { ...process.env, ATLAS_APP_DEV_GATEWAY: gateway.origin, ATLAS_APP_USER_DATA: userData, ATLAS_APP_DEV_START_DELAY_MS: "5000" };
    app = await electron.launch({ executablePath: electronBin, args: [appDir, `--project=${project}`], env });
    const second = spawn(electronBin, [appDir, `--project=${project}`], { env, windowsHide: true, stdio: "ignore" });
    await new Promise((resolve, reject) => { second.once("exit", resolve); second.once("error", reject); });
    expect(await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows().length)).toBe(0);
    page = await app.firstWindow();
    await ready();
    expect(await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows().length)).toBe(1);
    expect(new URL(page.url()).origin).toBe(gateway.origin);
  });
  test("shows the welcome steps once and remembers them", async () => {
    await launch({ ui: { lang: "en" } });
    await page.waitForSelector(".onboarding");
    await page.click("text=Get started");
    await page.click("text=Continue");
    await page.click("text=Start using Atlas");
    await ready();
    expect(await page.locator(".onboarding").count()).toBe(0);
  });
});

describe("window layout", () => {
  test("long titles and messages stay inside restored and maximized windows", async () => {
    const title = "A very long conversation title ".repeat(30);
    await gateway.seed({
      sessions: [session("s1", title)],
      messages: { s1: [userMessage("u1", "s1", "A long message ".repeat(40)), assistantMessage("a1", "s1", parts("Answer ".repeat(80)))] },
    });
    await launch({ theme: "dark", ui: { onboarded: true, lang: "en" } });
    await page.click(".session-main");
    await ready();
    await page.mouse.move(0, 699);
    for (const [width, height] of [[760, 520], [960, 700], [1360, 700], [1920, 700]]) {
      await page.setViewportSize({ width, height });
      await expect.poll(() => page.evaluate(() => {
        const bounds = (selector) => document.querySelector(selector).getBoundingClientRect();
        const fits = (selector) => {
          const box = bounds(selector);
          return box.left >= 0 && box.right <= innerWidth + 1;
        };
        const bar = bounds(".titlebar");
        const actions = bounds(".titlebar-end");
        const composer = bounds(".composer");
        const send = bounds(".send");
        return document.documentElement.scrollWidth <= innerWidth &&
          [".app", ".body", ".card", ".thread-head-title", ".composer", ".user-bubble"].every(fits) &&
          bounds(".crumbs").right <= actions.left + 1 && actions.right <= bar.right - 140 &&
          send.right <= composer.right && send.bottom <= composer.bottom;
      })).toBe(true);
      if (process.env.ATLAS_LAYOUT_SHOTS) {
        fs.mkdirSync(process.env.ATLAS_LAYOUT_SHOTS, { recursive: true });
        await page.screenshot({ path: path.join(process.env.ATLAS_LAYOUT_SHOTS, `layout-${width}.png`) });
      }
    }
    await page.setViewportSize({ width: 760, height: 520 });
    await page.click('.titlebar [aria-label="Toggle panel"]');
    await expect.poll(() => page.evaluate(() => {
      const card = document.querySelector(".card").getBoundingClientRect();
      const content = document.querySelector(".card-content").getBoundingClientRect();
      return content.width >= card.width - 1;
    })).toBe(true);
  });
});

describe("composer menus", () => {
  test("mode chips keep their icon and draft inline in wide and narrow composers", async () => {
    await gateway.seed({ sessions: [] });
    await launch();
    await ready();
    await page.fill(".composer textarea", "Keep this draft");
    for (const width of [1280, 760]) {
      await page.setViewportSize({ width, height: 700 });
      for (const mode of ["plan", "debug", "multi", "ask"]) {
        await page.click(".composer-add");
        await page.click(`.context-menu-row[data-mode="${mode}"]`);
        await page.waitForSelector(`.compose-mode-tag[data-mode="${mode}"] > svg`);
        await expect.poll(() => page.evaluate(() => {
          const chip = document.querySelector(".compose-mode-tag").getBoundingClientRect();
          const input = document.querySelector(".composer textarea").getBoundingClientRect();
          const add = document.querySelector(".composer-add").getBoundingClientRect();
          return chip.left >= add.right && input.left >= chip.right && chip.top < input.bottom && chip.bottom > input.top && document.documentElement.scrollWidth <= innerWidth;
        })).toBe(true);
        expect(await page.inputValue(".composer textarea")).toBe("Keep this draft");
        await page.click('[aria-label="Remove selected mode"]');
        await page.waitForSelector(".compose-mode-tag", { state: "detached" });
        expect(await page.inputValue(".composer textarea")).toBe("Keep this draft");
      }
    }
    expect((await gateway.state()).prompts).toHaveLength(0);
  });
  test("the composer drops its controls below once the text wraps, and only then", async () => {
    await gateway.seed({ sessions: [] });
    await launch();
    await ready();
    await page.setViewportSize({ width: 760, height: 700 });
    const shape = () => page.evaluate(() => {
      const input = document.querySelector(".composer textarea").getBoundingClientRect();
      const bar = document.querySelector(".composer-bar").getBoundingClientRect();
      return { expanded: document.querySelector(".composer").hasAttribute("data-expanded"), below: bar.top >= input.bottom - 8 };
    });
    await page.fill(".composer textarea", "short");
    await expect.poll(shape).toEqual({ expanded: false, below: false });
    // Under the old 90 character rule this wrapped beside the controls.
    await page.fill(".composer textarea", "bana elevenlabs mcp ile bir ses yaparmısın atatürk sesi gibi bir ses olsun");
    await expect.poll(shape).toEqual({ expanded: true, below: true });
    // One character less still fits the wide input, and must not flip back and forth.
    await page.locator(".composer textarea").press("End");
    await page.keyboard.type("x");
    const flips = await page.evaluate(() => new Promise((done) => {
      let count = 0;
      const observer = new MutationObserver(() => { count += 1; });
      observer.observe(document.querySelector(".composer"), { attributes: true, attributeFilter: ["data-expanded"] });
      setTimeout(() => { observer.disconnect(); done(count); }, 300);
    }));
    expect(flips).toBe(0);
    await page.fill(".composer textarea", "short");
    await expect.poll(shape).toEqual({ expanded: false, below: false });
  });

  test("the plus menu opens MCP, changes a real toggle and returns to the draft", async () => {
    await gateway.seed({ sessions: [], messages: {}, mcp: [{ name: "Docs", state: "connected", enabled: true, tools: 3 }, { name: "Browser", state: "disabled", enabled: false }] });
    await launch();
    await ready();
    await page.fill(".composer textarea", "Keep this draft");
    await page.click(".composer-add");
    await page.waitForSelector(".context-menu");
    await page.click('.context-menu-row:has-text("MCP")');
    await page.waitForSelector(".mcp-server");
    await page.click('.mcp-switch[aria-label="Docs: Enabled"]');
    await expect.poll(async () => (await gateway.state()).mcp.find((s) => s.name === "Docs").enabled).toBe(false);
    await page.fill('.context-menu-search input', "Browser");
    expect(await page.locator(".mcp-server").count()).toBe(1);
    await page.keyboard.press("Escape");
    expect(await page.inputValue(".composer textarea")).toBe("Keep this draft");
  });

  test("slash skills filter and expand using the keyboard", async () => {
    await gateway.seed({ sessions: [], commands: [{ id: "skill:review", name: "code-review", kind: "skill", description: "Review a change" }, { id: "skill:test", name: "test", kind: "skill", description: "Test changes" }] });
    await launch();
    await ready();
    await page.fill(".composer textarea", "/code");
    await page.waitForSelector('.suggest-row:has-text("code-review")');
    await page.keyboard.press("Enter");
    await expect.poll(() => page.inputValue(".composer textarea")).toBe("Expanded skill:review");
    expect((await gateway.state()).prompts).toHaveLength(0);
  });

  test("the anchored model menu offers auto and preserves its keyboard picker", async () => {
    await openChat();
    await page.click(".model-trigger");
    await page.waitForSelector(".model-menu");
    const anchored = await page.evaluate(() => {
      const menu = document.querySelector(".model-menu").getBoundingClientRect();
      const trigger = document.querySelector(".model-trigger").getBoundingClientRect();
      return menu.bottom <= trigger.top && Math.abs(menu.right - trigger.right) < 16;
    });
    expect(anchored).toBe(true);
    await page.click('.model-auto [role="switch"]');
    await escapeUntilGone(".model-menu");
    await expect.poll(() => page.evaluate(() => document.activeElement?.classList.contains("model-trigger"))).toBe(true);
    expect(await page.locator(".model-trigger").innerText()).toContain("Auto");
    await page.click(".model-trigger");
    await page.fill(".model-menu input", "other");
    await page.keyboard.press("Enter");
    await page.waitForSelector(".model-menu", { state: "detached" });
    await expect.poll(() => page.locator(".model-trigger").innerText()).toContain("Other Model");
  });

  test("microphone capture renders a wave, discards safely and transcribes on confirmation", async () => {
    await gateway.seed({ sessions: [] });
    await launch({ ui: { onboarded: true, lang: "en" } }, ["--use-fake-device-for-media-stream", "--use-fake-ui-for-media-stream"]);
    await ready();
    await page.fill(".composer textarea", "Draft");
    await page.click(".voice-trigger");
    await page.waitForSelector('.voice-capture[data-phase="recording"]');
    await page.waitForTimeout(400);
    await page.click('[aria-label="Discard recording"]');
    await page.waitForSelector(".voice-trigger");
    expect((await gateway.state()).speechRequests).toHaveLength(0);
    expect(await page.inputValue(".composer textarea")).toBe("Draft");
    await page.click(".voice-trigger");
    await page.waitForSelector('.voice-capture[data-phase="recording"]');
    await page.waitForTimeout(600);
    await page.click('[aria-label="Use recording"]');
    await expect.poll(() => page.inputValue(".composer textarea")).toBe("Draft Dictated words.");
    const state = await gateway.state();
    expect(state.speechRequests).toHaveLength(1);
    expect(state.speechRequests[0].header).toBe("RIFF");
    expect(state.speechRequests[0].rate).toBe(16000);
    expect(state.speechRequests[0].size).toBeGreaterThan(44);
    expect(state.prompts).toHaveLength(0);
  });
});

describe("a turn", () => {
  test("shows Working within 200 ms of sending, then streams and folds into the answer", async () => {
    await openChat();
    await page.fill(".composer textarea", "Do the thing");
    const shown = await page.evaluate(
      () =>
        new Promise((resolve) => {
          const started = performance.now();
          const observer = new MutationObserver(() => {
            if (document.querySelector(".working")) {
              observer.disconnect();
              resolve(performance.now() - started);
            }
          });
          observer.observe(document.body, { subtree: true, childList: true });
          document.querySelector(".send").click();
        }),
    );
    expect(shown).toBeLessThan(200);

    await gateway.emit("session_status", { session_id: "s1", busy: true });
    const prompts = (await gateway.state()).prompts;
    expect(prompts.at(-1).prompt).toBe("Do the thing");

    await gateway.emit("message", { type: "created", payload: userMessage("u2", "s1", "Do the thing") });
    await gateway.emit("message", { type: "created", payload: assistantMessage("a2", "s1", parts("Streaming text"), false) });
    await page.waitForSelector("text=Streaming text");
    await gateway.emit("message", { type: "updated", payload: assistantMessage("a2", "s1", parts("Streaming text, done.")) });
    await gateway.emit("session_status", { session_id: "s1", busy: false });
    await page.waitForSelector("text=Streaming text, done.");
    expect(await page.locator(".turn").count()).toBe(2);
    expect(await page.locator(".working").count()).toBe(0);
  });

  test("groups calls made at once into one row", async () => {
    const call = (id) => ({ type: "tool_call", data: { id, name: "view", input: JSON.stringify({ file_path: `${id}.txt` }), finished: true } });
    const result = (id) => ({ type: "tool_result", data: { tool_call_id: id, name: "view", content: "x", metadata: "", is_error: false } });
    await gateway.seed({
      sessions: [session("s1", "Reads")],
      messages: { s1: [userMessage("u1", "s1", "Read"), assistantMessage("a1", "s1", [call("c1"), call("c2"), result("c1"), result("c2"), ...parts("Done")])] },
    });
    await launch();
    await page.click(".session-main");
    await page.waitForSelector("text=Read 2 files");
    expect(await page.locator(".tool").count()).toBe(1);
  });
});

describe("the agent asks", () => {
  const permission = { id: "p1", session_id: "s1", tool_call_id: "t1", tool_name: "bash", description: "Run a command", action: "execute", params: { command: "go test" }, path: "" };

  test("permission dock answers from the keyboard and gives the draft back", async () => {
    await openChat();
    await page.fill(".composer textarea", "my draft");
    await gateway.emit("permission_request", { type: "created", payload: permission });
    await page.waitForSelector(".dock");
    expect(await page.locator(".composer").count()).toBe(0);

    await page.keyboard.press("Enter");
    await page.waitForSelector(".composer textarea");
    expect((await gateway.state()).permissionAnswers.at(-1)).toBe("allow");
    expect(await page.inputValue(".composer textarea")).toBe("my draft");

    await gateway.emit("permission_request", { type: "created", payload: { ...permission, id: "p2" } });
    await page.waitForSelector(".dock");
    await page.keyboard.press("Escape");
    await page.waitForSelector(".composer textarea");
    expect((await gateway.state()).permissionAnswers.at(-1)).toBe("deny");

    await gateway.emit("permission_request", { type: "created", payload: { ...permission, id: "p3" } });
    await page.waitForSelector(".dock");
    await page.keyboard.press("Control+Enter");
    await page.waitForSelector(".composer textarea");
    expect((await gateway.state()).permissionAnswers.at(-1)).toBe("allow_session");
  });

  test("a plan can be sent back with a comment", async () => {
    await openChat();
    const plan = { ...permission, id: "plan1", tool_name: "exit_plan_mode", description: "[MODIFY] a.go", action: "exit_plan_mode", params: {} };
    await gateway.emit("permission_request", { type: "created", payload: plan });
    await page.waitForSelector(".dock");
    expect(await page.locator('[data-action="changes"]').count()).toBe(1);
    await page.click('[data-action="changes"]');
    await page.fill(".dock-comment", "Also cover the Windows path");
    await page.keyboard.press("Control+Enter");
    await page.waitForSelector(".composer textarea");
    const state = await gateway.state();
    expect(state.permissionAnswers.at(-1)).toBe("deny");
    expect(state.permissionFeedback.at(-1)).toBe("Also cover the Windows path");

    // An ordinary permission has no change request.
    await gateway.emit("permission_request", { type: "created", payload: { ...permission, id: "p9" } });
    await page.waitForSelector(".dock");
    expect(await page.locator('[data-action="changes"]').count()).toBe(0);
    await page.keyboard.press("Escape");
    await page.waitForSelector(".composer textarea");
  });

  test("an MCP app page opens in a locked frame and can call its server's tools", async () => {
    const html = `<button id="go" onclick="window.atlas.callTool('refresh', {n: 1}).then(function(r){document.getElementById('out').textContent = r.content}).catch(function(e){document.getElementById('out').textContent = 'error: ' + e.message})">Refresh</button><div id="out"></div>`;
    await gateway.seed({
      sessions: [session("s1", "Apps")],
      messages: {
        s1: [
          userMessage("u1", "s1", "Show the board"),
          assistantMessage("a1", "s1", [
            { type: "tool_call", data: { id: "c1", name: "mcp_docs_board", input: "{}", finished: true } },
            { type: "tool_result", data: { tool_call_id: "c1", name: "mcp_docs_board", content: "board ready", metadata: "", is_error: false } },
          ]),
        ],
      },
      mcpApp: { server: "docs", tool: "board", uri: "ui://docs/board", html },
    });
    await launch();
    await page.click(".session-main");
    await ready();
    await page.click(".tool-row");
    await page.click('[data-action="open-mcp-app"]');
    const frame = page.frameLocator(".mcp-app-frame");
    await frame.locator("#go").click();
    await expect.poll(() => frame.locator("#out").textContent(), { timeout: 5000 }).toBe("ran refresh");
    const calls = (await gateway.state()).mcpCalls;
    expect(calls).toEqual([{ tool: "mcp_docs_board", call: "refresh", arguments: { n: 1 } }]);
    // The frame has scripts but no same-origin access.
    expect(await page.getAttribute(".mcp-app-frame", "sandbox")).toBe("allow-scripts");
  });

  test("the summary shows the agent's plan", async () => {
    await gateway.seed({
      sessions: [session("s1", "Planned", { todos: [{ id: "1", content: "Read the code", status: "completed", active_form: "Reading" }, { id: "2", content: "Write the fix", status: "in_progress", active_form: "Writing the fix" }, { id: "3", content: "Run the tests", status: "pending", active_form: "Running" }] })],
      messages: { s1: [userMessage("u1", "s1", "Fix it"), assistantMessage("a1", "s1", parts("On it."))] },
    });
    await launch();
    await page.click(".session-main");
    await ready();
    await page.click("[data-summary-trigger]");
    await page.waitForSelector(".plan-step");
    expect(await page.locator(".plan-step").count()).toBe(3);
    expect(await page.locator('.plan-step[data-status="completed"]').count()).toBe(1);
    expect(await page.textContent(".summary-pop")).toContain("Writing the fix");
    expect(await page.textContent(".summary-pop")).toContain("1/3");
    await page.keyboard.press("Escape");
  });

  test("question dock collects an answer", async () => {
    await openChat();
    await gateway.emit("question_batch_request", {
      type: "created",
      payload: { id: "q1", session_id: "s1", tool_call_id: "t1", questions: [{ id: "i1", type: "single_choice", question: "Which colour?", choices: [{ id: "a", label: "Blue" }, { id: "b", label: "Green" }] }] },
    });
    await page.waitForSelector(".dock");
    await page.click("text=Green");
    await page.click("text=Answer");
    await page.waitForSelector(".composer textarea");
    expect((await gateway.state()).questionAnswers.at(-1)).toEqual([{ request_id: "i1", selected_ids: ["b"] }]);
  });
});

describe("connection", () => {
  test("the banner shows when the stream drops and leaves when it returns; the draft stays", async () => {
    await openChat();
    await page.fill(".composer textarea", "keep me");
    await gateway.refuse(true);
    await gateway.drop();
    await page.waitForSelector(".collapse[data-open] .conn-banner", { timeout: 1000 });
    expect(await page.inputValue(".composer textarea")).toBe("keep me");
    await gateway.refuse(false);
    await page.waitForSelector(".collapse:not([data-open]) .conn-banner", { state: "attached", timeout: 8000 });
    expect(await page.inputValue(".composer textarea")).toBe("keep me");
  });
});

describe("keyboard", () => {
  test("the palette opens fast, filters, and runs from the keyboard", async () => {
    await openChat();
    const opened = await page.evaluate(
      () =>
        new Promise((resolve) => {
          const started = performance.now();
          const observer = new MutationObserver(() => {
            if (document.querySelector(".palette")) {
              observer.disconnect();
              resolve(performance.now() - started);
            }
          });
          observer.observe(document.body, { subtree: true, childList: true });
          window.dispatchEvent(new KeyboardEvent("keydown", { key: "k", code: "KeyK", ctrlKey: true, bubbles: true, cancelable: true }));
        }),
    );
    expect(opened).toBeLessThan(100);
    await page.keyboard.type("sidebar");
    await page.waitForSelector(".palette-row[data-selected='true']");
    await page.keyboard.press("Enter");
    await page.waitForSelector(".body[data-sidebar='closed']");
  });

  test("settings trap focus, close with Esc and hand focus back", async () => {
    await openChat();
    await page.focus(".composer textarea");
    await page.keyboard.press("Control+,");
    await page.waitForSelector(".settings");
    for (let i = 0; i < 12; i++) await page.keyboard.press("Tab");
    expect(await page.evaluate(() => !!document.activeElement?.closest(".settings"))).toBe(true);
    await escapeUntilGone(".settings");
    expect(await page.evaluate(() => document.activeElement?.tagName)).toBe("TEXTAREA");
  });

  test("the model picker switches the model", async () => {
    await openChat();
    await page.keyboard.press("Control+/");
    await page.waitForSelector(".palette");
    await page.keyboard.type("other");
    await page.keyboard.press("Enter");
    await page.waitForSelector(".chip-button:has-text('Other Model')");
  });
});

describe("motion", () => {
  test("reduced motion stops every animation", async () => {
    await gateway.seed({ sessions: [session("s1", "Busy", { is_busy: true })], messages: { s1: [userMessage("u1", "s1", "Hi"), assistantMessage("a1", "s1", parts("Working on it"), false)] } });
    await launch({ motion: "reduce", ui: { onboarded: true, lang: "en" } });
    await page.click(".session-main");
    await page.waitForSelector(".working");
    const running = await page.evaluate(() =>
      document
        .getAnimations()
        .filter((a) => a.playState === "running" && a.effect?.getTiming().iterations === Infinity)
        .map((a) => a.animationName ?? a.constructor.name),
    );
    // Busy indicators keep a slow fade of opacity so work still reads as alive; nothing moves.
    expect(running.filter((name) => name !== "calm-pulse")).toEqual([]);
  });
});

describe("accessibility", () => {
  const audit = async () => {
    await page.evaluate(axeSource);
    const results = await page.evaluate(async () => {
      const { violations } = await window.axe.run(document, { resultTypes: ["violations"] });
      return violations.map((v) => ({ id: v.id, impact: v.impact, nodes: v.nodes.slice(0, 3).map((n) => n.target.join(" ")) }));
    });
    return results.filter((v) => v.impact === "critical" || v.impact === "serious");
  };

  test.each(["light", "dark"])("no serious violations on the main screens (%s)", async (theme) => {
    await gateway.seed({ sessions: [session("s1", "First chat")], messages: { s1: [userMessage("u1", "s1", "Hello"), assistantMessage("a1", "s1", parts("Hi **there**."))] } });
    await launch({ theme, ui: { onboarded: true, lang: "en" } });
    await page.waitForSelector(".home");
    await page.waitForTimeout(400);
    expect(await audit()).toEqual([]);

    await page.click(".session-main");
    await page.waitForSelector(".turn");
    // A hover card fading in would be measured at part opacity.
    await page.mouse.move(700, 400);
    await page.waitForTimeout(700);
    expect(await audit()).toEqual([]);

    await page.keyboard.press("Control+,");
    await page.waitForSelector(".settings");
    await page.waitForTimeout(400);
    expect(await audit()).toEqual([]);
  });
});

describe("long chats", () => {
  test("200 turns open with a small DOM and scroll smoothly", async () => {
    const messages = [];
    for (let i = 0; i < 200; i++) {
      messages.push(userMessage(`u${i}`, "s1", `Question number ${i}`));
      messages.push(assistantMessage(`a${i}`, "s1", parts(`## Answer ${i}\n\nSome text with **bold** and \`code\`.\n\n- one\n- two\n\n\`\`\`go\nfunc main() {}\n\`\`\``)));
    }
    await gateway.seed({ sessions: [session("s1", "Long")], messages: { s1: messages } });
    await launch();
    await page.click(".session-main");
    await page.waitForSelector(".turn");
    await page.waitForTimeout(500);
    const nodes = await page.evaluate(() => document.querySelectorAll(".thread *").length);
    // Only the newest turns are mounted, and each stays within a budget: a turn here
    // holds its message actions, a code block with its tools, and the turn's own actions.
    const mounted = await page.locator(".turn").count();
    expect(mounted).toBeLessThanOrEqual(40);
    expect(nodes / mounted).toBeLessThan(100);
    expect(nodes).toBeLessThan(3000);

    const fps = await page.evaluate(
      () =>
        new Promise((resolve) => {
          const el = document.querySelector(".thread-scroll");
          let frames = 0;
          const started = performance.now();
          const step = () => {
            el.scrollTop -= 60;
            frames++;
            if (performance.now() - started < 2000) requestAnimationFrame(step);
            else resolve(frames / 2);
          };
          requestAnimationFrame(step);
        }),
    );
    console.log(`scroll: ${Math.round(fps)} fps, ${nodes} nodes`);
    expect(fps).toBeGreaterThan(50);

    await page.keyboard.press("Alt+ArrowUp");
    expect(await page.locator(".tick").count()).toBeGreaterThan(10);
  });
});

describe("sidebar", () => {
  test("rename, pin and search", async () => {
    await gateway.seed({ sessions: [session("s1", "Alpha"), session("s2", "Beta")], messages: { s1: [], s2: [] } });
    await launch();
    await page.waitForSelector(".session-main");
    await page.dblclick(".session-main >> nth=0");
    await page.fill(".rename", "Renamed");
    await page.keyboard.press("Enter");
    await page.waitForSelector("text=Renamed");

    await page.hover(".session-row >> nth=1");
    await page.click(".session-row >> nth=1 >> .session-tools button >> nth=0");
    await page.waitForSelector("text=Pinned");

    await page.fill(".search input", "Beta");
    await page.waitForSelector("text=Results");
    expect(await page.locator(".session-main").count()).toBe(1);
  });
});

describe("the sidebar's buttons", () => {
  test("the sun switches the theme and the gear opens Settings", async () => {
    await launch();
    await ready();
    const scheme = () => page.evaluate(() => document.documentElement.dataset.colorScheme);
    const before = await scheme();
    await page.click(before === "dark" ? '[aria-label="Switch to light theme"]' : '[aria-label="Switch to dark theme"]');
    await page.waitForFunction((b) => document.documentElement.dataset.colorScheme !== b, before);
    expect(await page.locator(".settings").count()).toBe(0);
    await page.click('.sidebar-foot [aria-label="Settings"]');
    await page.waitForSelector(".settings");
  });

  test("picking a folder moves this window instead of opening another", async () => {
    await launch();
    await ready();
    const other = path.join(os.tmpdir(), "atlas-e2e-other");
    fs.mkdirSync(other, { recursive: true });
    await page.evaluate((p) => window.atlas.openProject(p), other);
    await page.waitForURL((url) => url.searchParams.get("project") === other);
    await ready();
    expect(app.windows()).toHaveLength(1);
  });
});

describe("connecting a provider", () => {
  test("an account signs in through a code, not a key", async () => {
    await launch();
    await ready();
    await page.keyboard.press("Control+,");
    await page.click('.settings-tab:has-text("Models & providers")');
    const row = page.locator('[data-provider="kimi-code"]');
    await row.locator("text=Sign in").click();
    await page.waitForSelector(".login-code");
    expect(await page.locator(".login-code").innerText()).toBe("ABCD-1234");
    expect((await gateway.state()).logins).toContain("kimi-code");
    await gateway.login({ state: "done" });
    await page.waitForSelector('.login-status[data-tone="success"]');
    await page.waitForSelector(".login", { state: "detached" });
  });

  test("a key provider still takes a key", async () => {
    await launch();
    await ready();
    await page.keyboard.press("Control+,");
    await page.click('.settings-tab:has-text("Models & providers")');
    await page.locator('[data-provider="zai"]').locator("text=Connect").click();
    await page.waitForSelector('[data-provider="zai"] input[type="password"]');
  });

  test("the model picker searches and picks", async () => {
    await launch();
    await ready();
    await page.keyboard.press("Control+,");
    await page.click('.settings-tab:has-text("Models & providers")');
    await page.click('.picker-button[aria-label="Main model"]');
    await page.fill(".picker-search input", "other");
    await page.keyboard.press("Enter");
    await page.waitForFunction(() => document.querySelector(".picker-value")?.textContent === "Other Model");
    expect(await page.locator("select").count()).toBe(0);
  });
});

describe("keyboard shortcuts", () => {
  test("a shortcut takes new keys and the old ones stop working", async () => {
    await launch();
    await ready();
    await page.keyboard.press("Control+,");
    await page.click('.settings-tab:has-text("Keyboard shortcuts")');
    await page.click('[aria-label^="Search chats:"]');
    await page.waitForSelector("text=Press keys…");
    await page.keyboard.press("Control+Alt+J");
    await page.waitForSelector('[aria-label="Search chats: Ctrl+Alt+J"]');
    await escapeUntilGone(".settings");

    await page.keyboard.press("Control+p");
    await page.waitForTimeout(150);
    expect(await page.locator(".palette").count()).toBe(0);
    await page.keyboard.press("Control+Alt+J");
    await page.waitForSelector(".palette");
  });

  test("a chord in use asks first, and Esc cancels recording without closing Settings", async () => {
    await launch();
    await ready();
    await page.keyboard.press("Control+,");
    await page.click('.settings-tab:has-text("Keyboard shortcuts")');
    await page.click('[aria-label^="Search chats:"]');
    await escapeUntilGone("text=Press keys…");
    expect(await page.locator(".settings").count()).toBe(1);

    await page.click('[aria-label^="Search chats:"]');
    await page.keyboard.press("Control+b");
    await page.waitForSelector(".shortcut-clash");
    await page.click("text=Use here instead");
    await page.waitForSelector('[aria-label="Search chats: Ctrl+B"]');
    await page.waitForSelector('[aria-label="Toggle sidebar: Not set"]');
  });
});

describe("what floats stays in the window", () => {
  test("a long mode list scrolls inside the window", async () => {
    await gateway.modes(Array.from({ length: 16 }, (_, i) => ({ name: `mode-${i}`, description: "Long description ".repeat(6) })));
    await launch();
    await ready();
    await page.click('[title="Agent mode"]');
    await page.waitForSelector(".picker");
    const box = await page.locator(".picker").boundingBox();
    expect(box.y).toBeGreaterThanOrEqual(40);
    await page.fill(".picker-search input", "mode-15");
    await page.waitForFunction(() => document.querySelectorAll(".picker-row").length === 1);
    await gateway.modes([]);
  });
});

describe("the context meter", () => {
  test("hovering sums it up and clicking breaks it down", async () => {
    await openChat();
    await page.hover(".context-ring");
    await page.waitForSelector(".context-hover");
    expect(await page.locator(".context-hover").innerText()).toContain("Tokens");
    await page.click(".context-ring");
    await page.waitForSelector(".context-panel");
    await page.waitForSelector(".context-hover", { state: "detached" });
    expect(await page.locator(".context-legend li").count()).toBeGreaterThanOrEqual(2);
    await page.click('.context-panel [aria-label="Close"]');
    await page.waitForSelector(".context-panel", { state: "detached" });
  });
});

describe("the thread rail", () => {
  test("hovering a tick shows the message and its answer", async () => {
    await gateway.seed({
      sessions: [session("s1", "Two turns")],
      messages: {
        s1: [userMessage("u1", "s1", "First question"), assistantMessage("a1", "s1", parts("First answer")), userMessage("u2", "s1", "Second question"), assistantMessage("a2", "s1", parts("Second answer"))],
      },
    });
    await launch();
    await page.click(".session-main");
    await ready();
    await page.locator(".tick").first().hover();
    await page.waitForSelector(".tick-card");
    expect(await page.locator(".tick-title").innerText()).toBe("First question");
    expect(await page.locator(".tick-body").innerText()).toBe("First answer");
  });
});

describe("a sent message", () => {
  const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==";

  async function edit(text) {
    await page.hover(".user-row");
    await page.click('.user-actions [aria-label="Edit message"]');
    await page.fill(".user-edit textarea", text);
  }

  test("an edit branches from just before it and goes out in its place", async () => {
    await openChat();
    await edit("Hello again");
    await page.keyboard.press("Enter");
    await expect.poll(async () => (await gateway.state()).prompts.at(-1)?.prompt).toBe("Hello again");
    const state = await gateway.state();
    expect(state.forks).toEqual([{ from: "s1", target: "u1" }]);
    expect(state.prompts.at(-1).sid).toBe("s2");
  });

  test("Esc leaves the message as it was", async () => {
    await openChat();
    await edit("Never mind");
    await escapeUntilGone(".user-edit");
    expect(await page.locator(".user-text").innerText()).toBe("Hello");
    expect((await gateway.state()).forks).toEqual([]);
  });

  test("an edit that would change files asks first", async () => {
    await gateway.seed({
      sessions: [session("s1", "First chat")],
      messages: { s1: [userMessage("u1", "s1", "Hello"), assistantMessage("a1", "s1", parts("Hi there."))] },
      rewindPreview: { files_to_write: 2, files_to_delete: 1 },
    });
    await launch();
    await page.click(".session-main");
    await ready();
    await edit("Hello again");
    await page.click('.user-edit .button:has-text("Send")');
    await page.waitForSelector('.modal-actions .button:has-text("Edit and send")');
    expect((await gateway.state()).forks).toEqual([]);
    await page.click('.modal-actions .button:has-text("Edit and send")');
    await expect.poll(async () => (await gateway.state()).prompts.at(-1)?.prompt).toBe("Hello again");
  });

  test("an image keeps the text under it whole, and an edit sends it again", async () => {
    const text = "What do you see in this picture?";
    const user = userMessage("u1", "s1", text);
    user.parts.unshift({ type: "binary", data: { Path: "photo.png", MIMEType: "image/png", Data: png } });
    await gateway.seed({ sessions: [session("s1", "Picture")], messages: { s1: [user, assistantMessage("a1", "s1", parts("A dot."))] } });
    await launch();
    await page.click(".session-main");
    await ready();
    await page.waitForSelector(".user-image");
    await page.evaluate(() => {
      const image = document.querySelector(".user-image");
      image.style.blockSize = "120px";
      image.style.inlineSize = "180px";
    });
    const clipped = await page.evaluate(() => {
      const bubble = document.querySelector(".user-bubble").getBoundingClientRect();
      const words = document.querySelector(".user-text").getBoundingClientRect();
      return words.bottom > bubble.bottom;
    });
    expect(clipped).toBe(false);
    await edit("And now?");
    await page.keyboard.press("Enter");
    await expect.poll(async () => (await gateway.state()).prompts.at(-1)?.attachments?.[0]?.content).toBe(png);
  });

  test("the side panel's styles do not change the chat rows or the home chips", async () => {
    await launch();
    await ready();
    const seen = await page.evaluate(() => {
      const make = (tag, cls) => { const el = document.createElement(tag); el.className = cls; el.textContent = "Tool search"; document.body.append(el); const cs = getComputedStyle(el); const out = { h: Math.round(el.getBoundingClientRect().height), bg: cs.backgroundColor, pad: parseFloat(cs.paddingLeft) }; el.remove(); return out; };
      return { row: make("button", "tool-row"), chip: make("button", "pill") };
    });
    // Chat rows are flat, one 24px line; the panel's launch rows have their own class.
    expect(seen.row.h).toBeLessThanOrEqual(26);
    expect(seen.row.bg).toBe("rgba(0, 0, 0, 0)");
    expect(seen.chip.pad).toBeGreaterThanOrEqual(10);
  });

  test("a chat row gets no grey fill on hover", async () => {
    await openChat();
    await page.waitForSelector(".tool-row, .worked-summary", { timeout: 5000 }).catch(() => {});
    const bg = await page.evaluate(() => {
      const row = document.createElement("button");
      row.className = "tool-row";
      row.textContent = "Tool search";
      document.body.append(row);
      return new Promise((done) => { requestAnimationFrame(() => done(row)); }).then(() => row);
    }).then(async () => {
      const box = await page.locator("button.tool-row").last().boundingBox();
      await page.mouse.move(box.x + 8, box.y + 8);
      await page.waitForTimeout(300);
      return page.evaluate(() => { const el = [...document.querySelectorAll("button.tool-row")].pop(); const out = getComputedStyle(el).backgroundColor; el.remove(); return out; });
    });
    expect(bg).toBe("rgba(0, 0, 0, 0)");
  });

  test("recent projects keep long names on one line", async () => {
    await launch();
    await ready();
    const rows = await page.evaluate(() => {
      const list = document.createElement("div");
      list.className = "recent";
      list.style.cssText = "position:fixed;inset-block-start:40px;inset-inline-start:40px;inline-size:440px;z-index:999;background:#111";
      for (const name of ["chatgpt_gpt-6.1-sol-long-project-name-here", "nvidia-nim_moonshotai_kimi-k2.6-long-project", "atlas"]) {
        const row = document.createElement("button");
        row.className = "recent-row";
        row.innerHTML = `<span class="recent-name">${name}</span><span class="recent-path">D:\Atlas\dist\dev\capability-acceptance\${name}</span>`;
        list.append(row);
      }
      document.body.append(list);
      return [...list.children].map((row) => {
        const [title, where] = [...row.children].map((el) => el.getBoundingClientRect());
        const box = row.getBoundingClientRect();
        return { height: title.height, inside: title.top >= box.top && title.bottom <= box.bottom, apart: where.left >= title.right, pathWidth: where.width };
      });
    });
    await page.screenshot({ path: path.join(os.tmpdir(), "atlas-recent-check.png") });
    for (const row of rows) {
      expect(row.height).toBeLessThan(26);
      expect(row.inside).toBe(true);
      expect(row.apart).toBe(true);
      expect(row.pathWidth).toBeGreaterThan(80);
    }
  });

  test("long text folds and opens", async () => {
    const long = Array.from({ length: 30 }, (_, i) => `Line ${i}`).join(String.fromCharCode(10));
    await gateway.seed({ sessions: [session("s1", "Long")], messages: { s1: [userMessage("u1", "s1", long), assistantMessage("a1", "s1", parts("Noted."))] } });
    await launch();
    await page.click(".session-main");
    await ready();
    await page.click(".user-more");
    await page.waitForSelector(".user-text[data-open]");
    expect(await page.locator(".user-more").innerText()).toBe("Show less");
  });
});

describe("the side panel", () => {
  const project = path.join(os.tmpdir(), "atlas-e2e-project");
  const panelKey = project.replaceAll("\\", "/").replace(/\/$/, "").toLowerCase();
  const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==";

  const editCall = (id, file, before, after, add, del) => [
    { type: "tool_call", data: { id, name: "edit", input: JSON.stringify({ file_path: file }), finished: true } },
    { type: "tool_result", data: { tool_call_id: id, name: "edit", content: "ok", metadata: JSON.stringify({ additions: add, removals: del, old_content: before, new_content: after }), is_error: false } },
  ];
  const viewCall = (id, file) => [
    { type: "tool_call", data: { id, name: "view", input: JSON.stringify({ file_path: file }), finished: true } },
    { type: "tool_result", data: { tool_call_id: id, name: "view", content: "x", metadata: "", is_error: false } },
  ];

  /** A chat that changed two files, read one, and carries an image. */
  async function openWorkChat(extra = {}) {
    await gateway.seed({
      sessions: [session("s1", "Work chat")],
      messages: {
        s1: [
          { ...userMessage("u1", "s1", "Look at the files"), parts: [...parts("Look at the files"), { type: "image_url", data: { url: `data:image/png;base64,${png}` } }] },
          assistantMessage("a1", "s1", [
            ...viewCall("v1", "app/main/gateway.cjs"),
            ...editCall("e1", "app/main/launch-queue.cjs", "const a = 1;\nconst b = 2;\n", "const a = 1;\nconst b = 3;\nconst c = 4;\n", 2, 1),
            ...editCall("e2", "app/renderer/src/app.tsx", "x\n", "x\ny\nz\n", 2, 0),
            ...parts("Fixed."),
          ]),
        ],
      },
      ...extra,
    });
    await launch();
    await page.click(".session-main");
    await page.waitForSelector(".turn");
  }

  const toggle = '[aria-label="Toggle panel"]';
  const tabTitles = () => page.locator('.ptab-main .ptab-title').allInnerTexts();
  // The slot is what grows while the panel opens, so it is what settles.
  const panelWidth = async () => (await page.locator(".panel-slot").boundingBox()).width;
  // Views that are drawn: a view parked behind a menu is still in the window but not shown.
  // The page is a <webview> in the app's root layer: "shown" means the tag is visible.
  const viewCount = () => page.evaluate(() => [...document.querySelectorAll("webview")].filter((w) => getComputedStyle(w).visibility === "visible").length);
  const guestCount = () => app.evaluate(({ webContents }) => webContents.getAllWebContents().filter((w) => w.getType() === "webview" && !w.isDestroyed()).length);
  const viewBox = () =>
    page.evaluate(() => {
      const w = [...document.querySelectorAll("webview")].find((x) => getComputedStyle(x).visibility === "visible");
      if (!w) return null;
      const r = w.getBoundingClientRect();
      return { x: r.x, y: r.y, width: r.width, height: r.height };
    });
  /** The colour of one pixel of the window as drawn, read from a screenshot of it. */
  const pixelAt = async (x, y) => {
    const shot = (await page.screenshot()).toString("base64");
    return page.evaluate(async ([b64, px, py]) => {
      const blob = await (await fetch(`data:image/png;base64,${b64}`)).blob();
      const bmp = await createImageBitmap(blob);
      const c = document.createElement("canvas");
      c.width = bmp.width;
      c.height = bmp.height;
      const ctx = c.getContext("2d", { willReadFrequently: true });
      ctx.drawImage(bmp, 0, 0);
      return [...ctx.getImageData(Math.round(px), Math.round(py), 1, 1).data.slice(0, 3)].join(",");
    }, [shot, x, y]);
  };
  const pages = () => app.evaluate(({ webContents }) => webContents.getAllWebContents().filter((w) => /\/__test\//.test(w.getURL())).map((w) => ({ url: w.getURL(), title: w.getTitle() })));
  const inView = (code) =>
    app.evaluate(({ webContents }, source) => {
      const view = webContents.getAllWebContents().find((w) => w.getURL().includes("/__test/"));
      return view ? view.executeJavaScript(source, true) : null;
    }, code);

  /** A real click inside the page view, so the page has a user gesture. */
  const clickInView = (elementId) =>
    app.evaluate(async ({ webContents }, id) => {
      const view = webContents.getAllWebContents().find((w) => w.getURL().includes("/__test/"));
      const box = JSON.parse(await view.executeJavaScript(`JSON.stringify(document.getElementById(${JSON.stringify(id)}).getBoundingClientRect())`));
      const x = Math.round(box.x + box.width / 2);
      const y = Math.round(box.y + box.height / 2);
      view.sendInputEvent({ type: "mouseDown", x, y, button: "left", clickCount: 1 });
      view.sendInputEvent({ type: "mouseUp", x, y, button: "left", clickCount: 1 });
    }, elementId);

  async function relaunch() {
    await app.close();
    app = await electron.launch({
      executablePath: electronBin,
      args: [appDir, `--project=${project}`],
      env: { ...process.env, ATLAS_APP_DEV_GATEWAY: gateway.origin, ATLAS_APP_USER_DATA: userData, ATLAS_APP_TEST_CONFIRM: process.env.ATLAS_APP_TEST_CONFIRM ?? "yes" },
    });
    page = await app.firstWindow();
  }

  async function goTo(address) {
    await page.fill('input[aria-label="Search or enter URL"]', address);
    await page.keyboard.press("Enter");
  }

  test("opens and closes from the title bar, the keyboard and its close button", async () => {
    await openChat();
    expect(await page.locator(".side-panel").count()).toBe(0);
    await page.click(toggle);
    await page.waitForSelector(".side-panel .newtab");
    expect(await page.locator(toggle).getAttribute("aria-pressed")).toBe("true");
    await page.click('[aria-label="Close panel"]');
    await page.waitForSelector(".side-panel", { state: "detached" });
    expect(await page.locator(toggle).getAttribute("aria-pressed")).toBe("false");
    await page.keyboard.press("Control+Alt+b");
    await page.waitForSelector(".side-panel");
    await page.keyboard.press("Control+Alt+b");
    await page.waitForSelector(".side-panel", { state: "detached" });
  });

  test("tabs are added and closed, and closing the last one closes the panel", async () => {
    await openChat();
    await page.click(toggle);
    await page.waitForSelector(".side-panel");
    await page.click('.side-panel [aria-label="New tab"]');
    await expect.poll(() => tabTitles()).toEqual(["New tab", "New tab"]);
    expect(await page.locator('.ptab-main[aria-current="true"]').count()).toBe(1);
    await page.locator(".ptab-close").nth(1).click();
    await expect.poll(() => tabTitles()).toEqual(["New tab"]);
    await page.locator(".ptab-close").first().click();
    await page.waitForSelector(".side-panel", { state: "detached" });
  });

  // The box of an element once two reads a frame apart agree, so a pointer
  // aimed at it does not land where a transition has just moved it from.
  async function settledBox(selector) {
    let last = null;
    for (let i = 0; i < 60; i++) {
      const box = await page.locator(selector).boundingBox();
      if (last && box && Math.abs(box.x - last.x) < 0.5 && Math.abs(box.y - last.y) < 0.5) return box;
      last = box;
      await page.waitForTimeout(50);
    }
    throw new Error(`${selector} did not stop moving`);
  }

  test("dragging the left edge resizes live, keeps both sides usable, persists, and double click resets", async () => {
    await openChat();
    await page.click(toggle);
    await page.waitForSelector(".side-panel");
    await expect.poll(async () => Math.round(await panelWidth())).toBe(520);
    // The panel is 520 wide at once but slides in as its grid column opens, so
    // wait for the handle to stop moving before grabbing it.
    const edge = await settledBox(".panel-handle");
    const y = edge.y + 300;
    await page.mouse.move(edge.x + 5, y);
    await page.mouse.down();
    await page.mouse.move(edge.x - 45, y, { steps: 4 });
    // The panel follows the pointer a frame later, so a busy machine needs a moment.
    await expect.poll(async () => Math.round(await panelWidth())).toBe(570);
    await page.mouse.move(edge.x - 95, y, { steps: 4 });
    await page.mouse.up();
    await expect.poll(async () => Math.round(await panelWidth())).toBe(620);

    // Neither side goes below its minimum.
    const grip = await page.locator(".panel-handle").boundingBox();
    await page.mouse.move(grip.x + 3, y);
    await page.mouse.down();
    await page.mouse.move(0, y, { steps: 6 });
    await page.mouse.up();
    const thread = await page.locator(".card").boundingBox();
    expect(thread.width).toBeGreaterThanOrEqual(319);
    const body = await page.locator(".body").boundingBox();
    const left = await page.locator(".panel-handle").boundingBox();
    await page.mouse.move(left.x + 3, y);
    await page.mouse.down();
    await page.mouse.move(body.x + body.width, y, { steps: 6 });
    await page.mouse.up();
    expect(Math.round(await panelWidth())).toBe(360);
    // Put it back to 620 for the restart check.
    const again = await page.locator(".panel-handle").boundingBox();
    await page.mouse.move(again.x + 3, y);
    await page.mouse.down();
    await page.mouse.move(again.x - 257, y, { steps: 6 });
    await page.mouse.up();
    expect(Math.round(await panelWidth())).toBe(620);

    await expect.poll(() => JSON.parse(fs.readFileSync(path.join(userData, "prefs.json"), "utf8")).ui?.panels?.[panelKey]?.width).toBe(620);
    await relaunch();
    await page.waitForSelector(".side-panel");
    await expect.poll(async () => Math.round(await panelWidth())).toBe(620);

    await page.locator(".panel-handle").dblclick({ position: { x: 3, y: 200 } });
    await expect.poll(async () => Math.round(await panelWidth())).toBe(520);
  });

  test("expand gives the panel the whole main area and brings the thread back", async () => {
    await openChat();
    await page.click(toggle);
    await page.waitForSelector(".side-panel");
    await page.click('[aria-label="Expand panel"]');
    await page.waitForSelector(".body[data-expanded]");
    // The panel covers the sidebar too: the whole body, under the title bar.
    await expect.poll(async () => {
      const body = await page.locator(".body").boundingBox();
      const panel = await page.locator(".side-panel").boundingBox();
      return Math.round(Math.abs(body.width - panel.width) + Math.abs(body.y - panel.y));
    }).toBe(0);
    expect(await page.locator(".card").getAttribute("inert")).not.toBeNull();
    expect(await page.locator(".sidebar-slot").getAttribute("inert")).not.toBeNull();
    expect(await page.locator('[aria-label="Restore panel size"]').getAttribute("aria-pressed")).toBe("true");
    expect(await page.locator(".panel-handle").count()).toBe(0);
    await page.click('[aria-label="Restore panel size"]');
    await expect.poll(async () => Math.round(await panelWidth())).toBe(520);
    expect(await page.locator(".card").getAttribute("inert")).toBeNull();
    expect(await page.locator(".body[data-expanded]").count()).toBe(0);
  });

  test("the new tab page lists the tools with their shortcuts and suggests real sites", async () => {
    await openChat();
    await page.click(toggle);
    await page.waitForSelector(".newtab");
    expect(await page.locator(".launch-row .launch-row-name").allInnerTexts()).toEqual(["Changes", "Terminal", "Side chat", "Files", "Design"]);
    expect(await page.locator(".launch-row .kbds").allTextContents()).toEqual(["CtrlShiftG", "Ctrl`", "CtrlShiftY", "CtrlP", "CtrlShiftE"]);
    expect(await page.locator(".site-tile-name").allInnerTexts()).toEqual(["GitHub", "MDN Web Docs", "Wikipedia", "Hacker News"]);

    await goTo(`${gateway.origin}/__test/page`);
    await page.waitForSelector('.ptab-main:has-text("Atlas test page")');
    await page.click('.side-panel [aria-label="New tab"]');
    await page.waitForSelector(".newtab");
    expect((await page.locator(".site-tile-name").allInnerTexts())[0]).toBe("Atlas test page");
    expect(await page.locator(".site-tile-name").count()).toBe(4);
  });

  test("Changes lists the session's files with counts, a branch pill and a diff beside a filterable tree", async () => {
    await openWorkChat();
    await page.click(toggle);
    await page.click('.launch-row[data-tool="changes"]');
    await page.waitForSelector(".work-tree .tree-row");
    expect(await tabTitles()).toEqual(["Changes"]);
    expect(await page.locator(".work-tree .tree-name").allInnerTexts()).toEqual(["app", "main", "launch-queue.cjs", "renderer", "src", "app.tsx"]);
    const pill = await page.locator(".work-bar .bar-pill").first().innerText();
    expect(pill).toContain("+4");
    expect(pill).toContain("−1");
    // Per-file counts and a status mark.
    expect(await page.locator('.work-tree .tree-row:has-text("launch-queue.cjs") .add').innerText()).toBe("+2");
    expect(await page.locator('.work-tree .tree-row:has-text("launch-queue.cjs") .status-mark').getAttribute("data-status")).toBe("modified");
    expect(await page.locator(".work-tree .file-icon").first().innerText()).toBe("JS");
    // The first file's diff is on the left.
    expect(await page.locator(".work-diff .diff [data-kind=add]").allInnerTexts()).toEqual(["const b = 3;", "const c = 4;"]);
    expect(await page.locator(".work-diff .diff [data-kind=del]").allInnerTexts()).toEqual(["const b = 2;"]);
    await page.click('.work-tree .tree-row:has-text("app.tsx")');
    await expect.poll(() => page.locator(".work-diff .diff [data-kind=add]").allInnerTexts()).toEqual(["y", "z"]);
    // The filter narrows the tree.
    await page.fill(".work-filter input", "launch");
    await expect.poll(() => page.locator(".work-tree .tree-name").allInnerTexts()).toEqual(["app", "main", "launch-queue.cjs"]);

    // From a closed panel the shortcut opens it on Changes.
    await page.click('[aria-label="Close panel"]');
    await page.keyboard.press("Control+Shift+g");
    await page.waitForSelector(".work-tree .tree-row");
  });

  test("Files browses the project and opens a file", async () => {
    await openChat();
    await page.keyboard.press("Control+p");
    await page.waitForSelector(".work-tree .tree-row");
    expect(await tabTitles()).toEqual(["Open file"]);
    expect(await page.locator(".work-empty").innerText()).toContain("Pick a file from the workspace tree");
    expect(await page.locator(".work-all").innerText()).toBe("All changes");
    await page.click('.work-tree .tree-row:has-text("main.go")');
    await page.waitForSelector(".file-code");
    expect(await page.locator(".file-code").innerText()).toContain("package main");
    expect(await tabTitles()).toEqual(["main.go"]);
    await page.click('.file-head [aria-label="Close"]');
    await page.waitForSelector(".work-empty");
    expect(await tabTitles()).toEqual(["Open file"]);
    // "All changes" goes to the Changes tool.
    await page.click(".work-all");
    await page.waitForSelector(".work-bar .bar-pill");
    await expect.poll(() => tabTitles()).toEqual(["Open file", "Changes"]);
  });

  test.runIf(process.platform === "win32")("Terminal is a real shell in the project folder and keeps its output across tabs", async () => {
    await openChat();
    await page.click(toggle);
    await page.click('.launch-row[data-tool="terminal"]');
    await page.waitForSelector(".xterm");
    await expect.poll(async () => (await tabTitles())[0].toLowerCase()).toContain("powershell");
    const screen = () => page.locator(".xterm-rows").innerText().then((text) => text.replace(/\s+/g, ""));
    await page.locator(".terminal-host").click();
    await page.keyboard.type('Write-Output ("atlas-" + (6*7)); Write-Output ("in-" + (Get-Item .).Name)');
    await page.keyboard.press("Enter");
    await expect.poll(screen, { timeout: 20_000 }).toContain("in-atlas-e2e-project");
    expect(await screen()).toContain("atlas-42");

    await page.click('.side-panel [aria-label="New tab"]');
    await page.waitForSelector(".newtab");
    await page.locator('.ptab-main[title*="owershell"]').click();
    await page.waitForSelector(".xterm");
    expect(await screen()).toContain("atlas-42");

    // Closing the tab ends the shell.
    await page.locator('.ptab-close[aria-label*="owershell"]').click();
    await expect.poll(() => tabTitles()).toEqual(["New tab"]);
  });

  test("Side chat forks the open chat, leaves the main thread alone, and is listed", async () => {
    await openWorkChat();
    await page.keyboard.press("Control+Shift+y");
    await page.waitForSelector(".side-chat .turn");
    expect(await tabTitles()).toEqual(["Side chat"]);
    await expect.poll(async () => (await gateway.state()).sideChats).toEqual([{ from: "s1", id: "side2" }]);
    expect(await page.locator(".side-chat .user-text").first().innerText()).toBe("Look at the files");
    // The side chat is not a chat of the sidebar.
    expect(await page.locator(".session-main").count()).toBe(1);

    // The new side chat has the focus: what the person types next goes there,
    // not into the main thread's composer.
    await expect.poll(() => page.evaluate(() => !!document.activeElement?.closest(".side-chat .composer"))).toBe(true);
    await page.keyboard.type("Only in the side chat");
    expect(await page.locator(".conversation:not(.side-chat) .composer textarea").inputValue()).toBe("");
    await page.keyboard.press("Enter");
    await expect.poll(async () => (await gateway.state()).prompts.at(-1)).toMatchObject({ sid: "side2", prompt: "Only in the side chat" });
    expect((await gateway.state()).prompts.filter((p) => p.prompt === "Only in the side chat").map((p) => p.sid)).toEqual(["side2"]);
    expect(await page.locator(".conversation:not(.side-chat) .user-text").allInnerTexts()).toEqual(["Look at the files"]);

    await page.click('[aria-label="Chat summary"]');
    await page.waitForSelector(".summary-pop");
    expect(await page.locator(".summary-pop .summary-row", { hasText: "Work chat" }).count()).toBe(1);
    await page.locator(".summary-pop .summary-row", { hasText: "Work chat" }).click();
    await page.waitForSelector(".summary-pop", { state: "detached" });
    expect(await tabTitles()).toEqual(["Side chat"]);
  });

  test("a side chat says so when the server is too old, and when there is no chat to fork", async () => {
    await launch();
    await ready();
    await page.keyboard.press("Control+Shift+y");
    await page.waitForSelector('.toast:has-text("Open a chat first")');
    await gateway.seed({ sessions: [session("s1", "First chat")], messages: { s1: [userMessage("u1", "s1", "Hello"), assistantMessage("a1", "s1", parts("Hi."))] }, noSideChat: true });
    await relaunch();
    await page.click(".session-main");
    await page.waitForSelector(".turn");
    await page.keyboard.press("Control+Shift+y");
    await page.waitForSelector('.toast:has-text("too old")');
    expect(await page.locator(".side-chat").count()).toBe(0);
  });

  test("the browser loads pages in an isolated view, with history, a title, and popups as tabs", async () => {
    await openChat();
    const before = await viewCount();
    await page.click(toggle);
    await page.waitForSelector(".newtab");
    await goTo("   ");
    await goTo(`${gateway.origin}/__test/page`);
    await page.waitForSelector('.ptab-main:has-text("Atlas test page")');
    await expect.poll(viewCount).toBe(before + 1);
    expect((await pages())[0]).toMatchObject({ url: `${gateway.origin}/__test/page`, title: "Atlas test page" });

    // No Node, sandboxed, a profile of its own.
    const facts = await app.evaluate(({ webContents, session: electronSession }) => {
      const all = webContents.getAllWebContents();
      const view = all.find((w) => w.getURL().endsWith("/__test/page"));
      const shell = all.find((w) => !w.getURL().includes("/__test/"));
      const prefs = view.getLastWebPreferences();
      return {
        sandbox: prefs.sandbox,
        node: prefs.nodeIntegration,
        isolated: prefs.contextIsolation,
        ownProfile: view.session === electronSession.fromPartition("persist:atlas-browser") && view.session !== shell.session,
      };
    });
    expect(facts).toEqual({ sandbox: true, node: false, isolated: true, ownProfile: true });
    expect(await inView("typeof require + typeof process + typeof window.atlas")).toBe("undefinedundefinedundefined");

    // History.
    await inView("document.getElementById('next').click()");
    await page.waitForSelector('.ptab-main:has-text("Second page")');
    await page.click('[aria-label="Back"]');
    await page.waitForSelector('.ptab-main:has-text("Atlas test page")');
    await page.waitForSelector('[aria-label="Forward"]:not([disabled])');
    await page.click('[aria-label="Forward"]');
    await page.waitForSelector('.ptab-main:has-text("Second page")');
    await page.click('[aria-label="Back"]');
    await page.waitForSelector('.ptab-main:has-text("Atlas test page")');

    // Only http and https load, whoever asks.
    const tabId = await page.evaluate(() => document.querySelector(".web-view").dataset.tab);
    for (const bad of ["file:///C:/Windows/win.ini", "javascript:alert(1)", "data:text/html,x", "about:blank"]) {
      expect(await page.evaluate(([id, url]) => window.atlas.browser.navigate(id, url), [tabId, bad])).toBe(false);
    }
    await inView("location.href = 'file:///C:/Windows/win.ini'");
    await page.waitForTimeout(600);
    expect((await pages()).some((p) => p.url.startsWith("file:"))).toBe(false);
    expect((await pages())[0].url).toBe(`${gateway.origin}/__test/page`);

    // A page that opens windows on its own gets none, and takes nothing from the tab in front.
    await goTo(`${gateway.origin}/__test/spam`);
    await page.waitForSelector('.ptab-main:has-text("Spam page")');
    await page.waitForTimeout(1500);
    expect(await tabTitles()).toEqual(["Spam page"]);
    await goTo(`${gateway.origin}/__test/page`);
    await page.waitForSelector('.ptab-main:has-text("Atlas test page")');
    // A popup after a click becomes a tab of the panel, next to its opener.
    await clickInView("pop");
    await page.waitForSelector('.ptab-main[aria-current="true"]:has-text("Popup page")');
    expect(await tabTitles()).toEqual(["Atlas test page", "Popup page"]);

    // Closing the tab closes its view.
    await page.locator('[aria-label="Close tab: Popup page"]').click();
    await expect.poll(async () => (await pages()).length).toBe(1);
  });

  test("the page stays where it is under a menu and a popover, follows the panel's width, and lives on when the panel closes", async () => {
    await openWorkChat();
    await page.click(toggle);
    await goTo(`${gateway.origin}/__test/page`);
    await page.waitForSelector('.ptab-main:has-text("Atlas test page")');
    await expect.poll(viewCount).toBe(1);
    const sameAsBox = async () => {
      const view = await viewBox();
      const box = await page.locator(".web-view").boundingBox();
      return !!view && !!box && Math.max(Math.abs(view.x - box.x), Math.abs(view.y - box.y), Math.abs(view.width - box.width), Math.abs(view.height - box.height)) <= 1;
    };
    // The page sits exactly on the box the panel reserved for it, also while the panel slides in.
    await expect.poll(sameAsBox).toBe(true);
    await inView("window.__alive = 7");

    // A popover and a menu draw over the page: it stays, nothing stands in for it, and they are on top.
    const onTop = (selector) =>
      page.evaluate((sel) => {
        const el = document.querySelector(sel);
        const r = el.getBoundingClientRect();
        const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
        return !!hit?.closest(sel);
      }, selector);
    await page.click('[aria-label="Chat summary"]');
    await page.waitForSelector(".summary-pop");
    expect(await viewCount()).toBe(1);
    expect(await page.locator(".web-snapshot").count()).toBe(0);
    expect(await onTop(".summary-pop")).toBe(true);
    await escapeUntilGone(".summary-pop");
    await page.click('.side-panel [aria-label="More"]');
    await page.waitForSelector(".menu");
    expect(await viewCount()).toBe(1);
    expect(await onTop(".menu")).toBe(true);
    await escapeUntilGone(".menu");
    expect(await viewCount()).toBe(1);

    // A drag of the panel's edge follows the pointer live, also across the page.
    const edge = await page.locator(".panel-handle").boundingBox();
    const start = (await viewBox()).width;
    await page.mouse.move(edge.x + 5, edge.y + 300);
    await page.mouse.down();
    await page.mouse.move(edge.x - 38, edge.y + 300, { steps: 3 });
    await expect.poll(async () => Math.abs((await viewBox()).width - (start + 43))).toBeLessThanOrEqual(2);
    // Over the page the drag keeps going: the page does not take the pointer.
    expect(await page.evaluate(() => getComputedStyle(document.querySelector("webview")).pointerEvents)).toBe("none");
    await page.mouse.move(edge.x - 150, edge.y + 300, { steps: 4 });
    await expect.poll(async () => Math.abs((await viewBox()).width - (start + 155))).toBeLessThanOrEqual(2);
    await page.mouse.up();
    expect(await page.evaluate(() => getComputedStyle(document.querySelector("webview")).pointerEvents)).toBe("auto");
    await expect.poll(sameAsBox).toBe(true);

    // Closing the panel hides the page and keeps it alive; opening it brings the same page back.
    await page.click('[aria-label="Close panel"]');
    await expect.poll(viewCount).toBe(0);
    expect(await guestCount()).toBe(1);
    expect(await inView("window.__alive")).toBe(7);
    await page.click(toggle);
    await expect.poll(viewCount).toBe(1);
    expect(await inView("window.__alive")).toBe(7);
    expect((await pages())[0].url).toBe(`${gateway.origin}/__test/page`);
  });

  test("tabs, their order and the page they show come back after a restart", async () => {
    await openChat();
    await page.click(toggle);
    await goTo(`${gateway.origin}/__test/page`);
    await page.waitForSelector('.ptab-main:has-text("Atlas test page")');
    await page.click('.side-panel [aria-label="New tab"]');
    await page.click('.launch-row[data-tool="files"]');
    await page.waitForSelector(".tree-row");
    await page.locator('.ptab-main:has-text("Atlas test page")').click();
    await page.waitForSelector(".web-view");
    await expect.poll(() => JSON.parse(fs.readFileSync(path.join(userData, "prefs.json"), "utf8")).ui?.panels?.[panelKey]?.tabs?.length).toBe(2);

    await relaunch();
    await page.waitForSelector(".side-panel");
    // A restored page names its tab once it has loaded, so the names are waited for.
    await expect.poll(tabTitles, { timeout: 20_000 }).toEqual(["Atlas test page", "Open file"]);
    await expect.poll(() => page.locator('.ptab-main[aria-current="true"]').innerText()).toContain("Atlas test page");
    await expect.poll(async () => (await pages())[0]?.url, { timeout: 20_000 }).toBe(`${gateway.origin}/__test/page`);
  });

  test("the summary popover shows changes, side chats and sources, and closes like a popover", async () => {
    await openWorkChat();
    const trigger = page.locator('[aria-label="Chat summary"]');
    await trigger.click();
    await page.waitForSelector(".summary-pop");
    const pop = page.locator(".summary-pop");
    expect(await pop.locator(".summary-project").innerText()).toBe("demo");
    expect(await pop.locator(".summary-changes").innerText()).toMatch(/Changes\s+\+4\s+−1/);
    // Side chats and agents have nothing yet, so only Sources has a heading.
    expect(await pop.locator(".summary-section h3").allInnerTexts()).toEqual(["Sources"]);
    expect(await pop.locator(".source-thumb").count()).toBe(1);
    expect(await pop.locator(".summary-sources .summary-name").allInnerTexts()).toEqual(["app.tsx", "launch-queue.cjs", "gateway.cjs", "Image"]);
    const box = await pop.boundingBox();
    expect(box.x + box.width).toBeLessThanOrEqual(await page.evaluate(() => innerWidth));
    expect(box.y).toBeGreaterThanOrEqual(40);
    // Focus moves into it; Esc closes it and returns focus to the button.
    expect(await page.evaluate(() => !!document.activeElement?.closest(".summary-pop"))).toBe(true);
    await escapeUntilGone(".summary-pop");
    expect(await page.evaluate(() => document.activeElement?.getAttribute("aria-label"))).toBe("Chat summary");

    // The keyboard opens it, and the shortcut does as well.
    await page.keyboard.press("Enter");
    await page.waitForSelector(".summary-pop");
    await escapeUntilGone(".summary-pop");
    await page.keyboard.press("Control+Shift+u");
    await page.waitForSelector(".summary-pop");

    // An outside click closes it.
    await page.waitForTimeout(400);
    await page.mouse.click(130, 600);
    await page.waitForSelector(".summary-pop", { state: "detached" });

    // The Changes row opens the Changes tab.
    await trigger.click();
    await page.click(".summary-changes");
    await page.waitForSelector(".summary-pop", { state: "detached" });
    await page.waitForSelector(".work-tree .tree-row");
    expect(await tabTitles()).toEqual(["Changes"]);

    // A file of the sources opens in Files.
    await trigger.click();
    await page.click('.summary-pop .summary-row:has-text("gateway.cjs")');
    await page.waitForSelector(".file-code");
    expect(await tabTitles()).toEqual(["Changes", "gateway.cjs"]);
  });

  test("the summary menu, attach button and the long list of sources work", async () => {
    const many = Array.from({ length: 6 }, (_, i) => viewCall(`v${i}`, `src/file${i}.go`)).flat();
    await gateway.seed({ sessions: [session("s1", "Many")], messages: { s1: [userMessage("u1", "s1", "Read them"), assistantMessage("a1", "s1", [...many, ...parts("Read.")])] } });
    await launch();
    await page.click(".session-main");
    await page.waitForSelector(".turn");
    await page.click('[aria-label="Chat summary"]');
    await page.waitForSelector(".summary-pop");
    expect(await page.locator(".summary-sources .summary-row").count()).toBe(4);
    await page.click(".summary-all");
    expect(await page.locator(".summary-sources .summary-row").count()).toBe(6);
    expect(await page.locator(".summary-all").innerText()).toBe("Show fewer");
    await page.click(".summary-all");
    expect(await page.locator(".summary-sources .summary-row").count()).toBe(4);

    await page.click('.summary-pop [aria-label="Project menu"]');
    await page.waitForSelector(".menu");
    expect(await page.locator(".menu .menu-item").allInnerTexts()).toEqual(["Open terminal", "Show folder in Explorer", "Copy path"]);
    await page.click('.menu-item:has-text("Copy path")');
    await expect.poll(() => app.evaluate(({ clipboard }) => clipboard.readText())).toBe(project);
    await page.waitForSelector(".menu", { state: "detached" });
    const attach = fs.mkdtempSync(path.join(os.tmpdir(), "atlas-e2e-attach-"));
    const file = path.join(attach, "notes.txt");
    fs.writeFileSync(file, "notes");
    // The button opens the composer's own file picker.
    await page.evaluate(() => {
      window.__picked = 0;
      document.querySelector(".composer input[type=file]").addEventListener("click", (e) => {
        e.preventDefault();
        window.__picked++;
      });
    });
    await page.click('.summary-pop [aria-label="Attach a file"]');
    await expect.poll(() => page.evaluate(() => window.__picked)).toBe(1);
    await page.waitForSelector(".summary-pop", { state: "detached" });
    await page.setInputFiles(".composer input[type=file]", file);
    await page.waitForSelector('.attachment[title="notes.txt"]');
  });

  test.each(["light", "dark"])("the panel and the popover have no serious accessibility violations (%s)", async (theme) => {
    await gateway.seed({
      sessions: [session("s1", "Work chat")],
      messages: { s1: [userMessage("u1", "s1", "Hi"), assistantMessage("a1", "s1", [...editCall("e1", "a.go", "a\n", "b\n", 1, 1), ...editCall("e2", "b.go", "a\n", "b\n", 1, 1), ...editCall("e3", "c.go", "a\n", "b\n", 1, 1), ...parts("Done.")])] },
    });
    await launch({ theme, ui: { onboarded: true, lang: "en" } });
    await page.click(".session-main");
    await page.waitForSelector(".turn");
    await page.click(toggle);
    await page.waitForSelector(".newtab");
    await page.mouse.move(300, 400);
    await page.waitForTimeout(500);
    const audit = async () => {
      await page.evaluate(axeSource);
      const results = await page.evaluate(async () => {
        const { violations } = await window.axe.run(document, { resultTypes: ["violations"] });
        return violations.map((v) => ({ id: v.id, impact: v.impact, nodes: v.nodes.slice(0, 3).map((n) => `${n.target.join(" ")}: ${n.any.map((c) => c.message).join("; ")}`) }));
      });
      return results.filter((v) => v.impact === "critical" || v.impact === "serious");
    };
    expect(await audit()).toEqual([]);
    await page.click('.launch-row[data-tool="changes"]');
    await page.waitForSelector(".work-tree .tree-row");
    await page.locator(".work-tree .tree-row").first().click();
    // The diff fades in; measure it when it has settled.
    await page.waitForTimeout(600);
    expect(await audit()).toEqual([]);
    await page.click('[aria-label="Chat summary"]');
    await page.waitForSelector(".summary-pop");
    await page.waitForTimeout(500);
    expect(await audit()).toEqual([]);
  });
  test("Changes: branch and upstream pills and a working toolbar; the tree folds away; the active tab is the wider one", async () => {
    await openWorkChat();
    await page.click(toggle);
    // A picture in a message is a real thumbnail, never a speck beside the text.
    const thumb = await page.locator(".user-image").first().boundingBox();
    expect(thumb.width).toBeGreaterThanOrEqual(56);
    expect(thumb.height).toBeGreaterThanOrEqual(56);
    await page.click('.launch-row[data-tool="changes"]');
    await page.waitForSelector(".work-tree .tree-row");
    expect(await page.locator(".work-bar .bar-pill").count()).toBeGreaterThanOrEqual(2);
    if (await page.locator(".work-bar .bar-pill-upstream").count()) expect(await page.locator(".work-bar .bar-pill-upstream").innerText()).toContain("→");
    const tools = await page.locator(".work-tools button").evaluateAll((els) => els.map((e) => e.getAttribute("aria-label")));
    expect(tools).toEqual(["Refresh", "Copy diff", "Wrap lines", "Show file in folder", "File tree"]);
    await page.click('.work-tools [aria-label="Copy diff"]');
    await expect.poll(() => app.evaluate(({ clipboard }) => clipboard.readText())).toContain("+const c = 4;");
    await page.click('.work-tools [aria-label="Wrap lines"]');
    await page.waitForSelector(".work-diff[data-wrap]");
    const before = (await page.locator(".work-tree").boundingBox()).width;
    await page.click('.work-tools [aria-label="File tree"]');
    await expect.poll(async () => (await page.locator(".work-tree").boundingBox()).width, { timeout: 3000 }).toBeLessThan(5);
    expect(before).toBeGreaterThan(100);
    await page.click('.work-tools [aria-label="File tree"]');
    await expect.poll(async () => Math.round((await page.locator(".work-tree").boundingBox()).width)).toBeGreaterThan(100);

    // Tabs: the active one is wider than the others once they have to share.
    for (let i = 0; i < 3; i++) await page.click(".side-panel .ptab-add");
    await expect.poll(() => page.locator(".ptab").count()).toBeGreaterThanOrEqual(4);
    await page.waitForTimeout(500);
    const widths = await page.locator(".ptab").evaluateAll((els) => els.map((e) => [e.hasAttribute("data-active"), e.getBoundingClientRect().width]));
    const active = widths.find((w) => w[0])[1];
    for (const [isActive, w] of widths) if (!isActive) expect(active).toBeGreaterThan(w);
  });

});

describe("the panel browser's features", () => {
  const project = path.join(os.tmpdir(), "atlas-e2e-project");
  const panelKey = project.replaceAll("\\", "/").replace(/\/$/, "").toLowerCase();
  const toggle = '[aria-label="Toggle panel"]';
  const tabTitles = () => page.locator(".ptab-main .ptab-title").allInnerTexts();
  const png1 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==";
  const editCall = (id, file, before, after, add, del) => [
    { type: "tool_call", data: { id, name: "edit", input: JSON.stringify({ file_path: file }), finished: true } },
    { type: "tool_result", data: { tool_call_id: id, name: "edit", content: "ok", metadata: JSON.stringify({ additions: add, removals: del, old_content: before, new_content: after }), is_error: false } },
  ];
  async function openWorkChat() {
    await gateway.seed({
      sessions: [session("s1", "Work chat")],
      messages: { s1: [userMessage("u1", "s1", "Look"), assistantMessage("a1", "s1", [...editCall("e1", "app/main/launch-queue.cjs", "a\n", "b\n", 1, 1), ...parts("Fixed.")])] },
    });
    await launch();
    await page.click(".session-main");
    await page.waitForSelector(".turn");
  }
  // Views that are drawn: a view parked behind a menu is still in the window but not shown.
  // The page is a <webview> in the app's root layer: "shown" means the tag is visible.
  const viewCount = () => page.evaluate(() => [...document.querySelectorAll("webview")].filter((w) => getComputedStyle(w).visibility === "visible").length);
  const guestCount = () => app.evaluate(({ webContents }) => webContents.getAllWebContents().filter((w) => w.getType() === "webview" && !w.isDestroyed()).length);
  const viewBox = () =>
    page.evaluate(() => {
      const w = [...document.querySelectorAll("webview")].find((x) => getComputedStyle(x).visibility === "visible");
      if (!w) return null;
      const r = w.getBoundingClientRect();
      return { x: r.x, y: r.y, width: r.width, height: r.height };
    });
  /** The colour of one pixel of the window as drawn, read from a screenshot of it. */
  const pixelAt = async (x, y) => {
    const shot = (await page.screenshot()).toString("base64");
    return page.evaluate(async ([b64, px, py]) => {
      const blob = await (await fetch(`data:image/png;base64,${b64}`)).blob();
      const bmp = await createImageBitmap(blob);
      const c = document.createElement("canvas");
      c.width = bmp.width;
      c.height = bmp.height;
      const ctx = c.getContext("2d", { willReadFrequently: true });
      ctx.drawImage(bmp, 0, 0);
      return [...ctx.getImageData(Math.round(px), Math.round(py), 1, 1).data.slice(0, 3)].join(",");
    }, [shot, x, y]);
  };
  const withEnv = async (vars, run) => {
    const before = Object.fromEntries(Object.keys(vars).map((k) => [k, process.env[k]]));
    Object.assign(process.env, vars);
    try {
      return await run();
    } finally {
      for (const [k, v] of Object.entries(before)) {
        if (v === undefined) delete process.env[k];
        else process.env[k] = v;
      }
    }
  };
  /** Runs code in the first page view and returns its result. */
  const inView = (code) =>
    app.evaluate(({ webContents }, source) => {
      const view = webContents.getAllWebContents().find((w) => w.getURL().includes("/__test/"));
      return view ? view.executeJavaScript(source, true) : null;
    }, code);
  const viewInfo = () =>
    app.evaluate(({ webContents }) => {
      const view = webContents.getAllWebContents().find((w) => w.getURL().includes("/__test/"));
      return view ? { url: view.getURL(), zoom: view.getZoomFactor(), agent: view.getUserAgent() } : null;
    });
  /** A real key press inside the page view: it passes through the main process like a person's would. */
  const keyInView = (keyCode, modifiers = []) =>
    app.evaluate(({ webContents }, [code, mods]) => {
      const view = webContents.getAllWebContents().find((w) => w.getURL().includes("/__test/"));
      view.focus();
      view.sendInputEvent({ type: "keyDown", keyCode: code, modifiers: mods });
      view.sendInputEvent({ type: "keyUp", keyCode: code, modifiers: mods });
    }, [keyCode, modifiers]);
  const typeInView = (text) =>
    app.evaluate(({ webContents }, chars) => {
      const view = webContents.getAllWebContents().find((w) => w.getURL().includes("/__test/"));
      for (const ch of chars) view.sendInputEvent({ type: "char", keyCode: ch });
    }, text);
  const clickInView = (elementId) =>
    app.evaluate(async ({ webContents }, id) => {
      const view = webContents.getAllWebContents().find((w) => w.getURL().includes("/__test/"));
      const box = JSON.parse(await view.executeJavaScript(`JSON.stringify(document.getElementById(${JSON.stringify(id)}).getBoundingClientRect())`));
      const x = Math.round(box.x + box.width / 2);
      const y = Math.round(box.y + box.height / 2);
      view.sendInputEvent({ type: "mouseDown", x, y, button: "left", clickCount: 1 });
      view.sendInputEvent({ type: "mouseUp", x, y, button: "left", clickCount: 1 });
    }, elementId);
  async function openPage(pathname, title, options = {}) {
    if (!options.keepPanel) {
      await page.click(toggle);
      await page.waitForSelector(".side-panel");
    }
    await page.fill('input[aria-label="Search or enter URL"]', `${options.origin ?? gateway.origin}${pathname}`);
    await page.keyboard.press("Enter");
    await page.waitForSelector(`.ptab-main:has-text("${title}")`);
    await expect.poll(async () => (await viewInfo())?.url ?? "").toContain(pathname);
    // The page is a visible <webview> once its guest is attached.
    await expect.poll(viewCount).toBeGreaterThan(0);
  }
  async function relaunch() {
    await app.close();
    app = await electron.launch({ executablePath: electronBin, args: [appDir, `--project=${project}`], env: { ...process.env, ATLAS_APP_DEV_GATEWAY: gateway.origin, ATLAS_APP_USER_DATA: userData, ATLAS_APP_TEST_CONFIRM: process.env.ATLAS_APP_TEST_CONFIRM ?? "yes" } });
    page = await app.firstWindow();
  }
  const menu = async (...path) => {
    await page.waitForSelector(".web-menu", { state: "detached" });
    await page.click('.side-panel [aria-label="More"]');
    await page.waitForSelector(".web-menu");
    for (const name of path.slice(0, -1)) {
      await page.locator(".menu-item", { hasText: name }).first().hover();
      await page.waitForTimeout(250);
    }
    // The last name is matched whole when some item has exactly that text.
    const exact = page.locator(".menu-item").filter({ has: page.getByText(path.at(-1), { exact: true }) });
    await ((await exact.count()) > 0 ? exact : page.locator(".menu-item", { hasText: path.at(-1) })).last().click();
  };

  test("with a page repainting behind it, the ... menu, Add note, Extensions and Downloads open over it without changing a pixel of the page, and it keeps repainting", async () => {
    await gateway.seed({ sessions: [] });
    await launch();
    await ready();
    await openPage("/__test/paint", "Paint page");
    await expect.poll(viewCount).toBe(1);
    const view = await viewBox();
    // One pixel in the page's fixed column and one in the part that repaints each frame;
    // both are on the left of the panel, clear of every menu.
    const fixed = [view.x + 20, view.y + 200];
    const moving = [view.x + 150, view.y + 200];
    const before = await pixelAt(...fixed);
    expect(before).toBe("204,51,102");
    const opens = [
      ["the ... menu", async () => page.click('.side-panel [aria-label="More"]'), ".menu"],
      ["Add note", async () => page.click('[aria-label="Add note"]'), ".note-pop, .web-pop"],
      ["Extensions", async () => page.click('[aria-label="Extensions"]'), ".ext-pop"],
      ["Downloads", async () => page.click('[aria-label="Downloads"]'), ".downloads-pop"],
    ];
    for (const [name, open, selector] of opens) {
      await open();
      await page.waitForSelector(selector);
      await page.waitForTimeout(350);
      // The page is not parked, captured or replaced: still the same tag, still visible.
      expect(await viewCount(), name).toBe(1);
      expect(await page.locator(".web-snapshot").count(), name).toBe(0);
      const seen = new Set();
      for (let i = 0; i < 8; i++) {
        expect(await pixelAt(...fixed), `${name}: the fixed pixel`).toBe(before);
        seen.add(await pixelAt(...moving));
        await page.waitForTimeout(60);
      }
      expect(seen.size, `${name}: the page repaints behind it`).toBeGreaterThanOrEqual(4);
      await page.keyboard.press("Escape");
      await page.waitForSelector(selector, { state: "detached" });
      expect(await pixelAt(...fixed), `${name}: after it closes`).toBe(before);
    }
  });

  test("find in page counts matches, steps through them and closes with Esc; Ctrl+F in the page opens it", async () => {
    await openChat();
    await openPage("/__test/find", "Find page");
    expect(await page.locator(".find-bar").count()).toBe(0);
    await keyInView("F", ["control"]);
    await page.waitForSelector(".find-bar input");
    await page.fill(".find-bar input", "needle");
    await expect.poll(() => page.locator(".find-count").innerText()).toMatch(/^[1-5]\/5$/);
    const at = Number((await page.locator(".find-count").innerText()).split("/")[0]);
    await page.click('[aria-label="Next match"]');
    await expect.poll(() => page.locator(".find-count").innerText()).toBe(`${(at % 5) + 1}/5`);
    await page.click('[aria-label="Previous match"]');
    await expect.poll(() => page.locator(".find-count").innerText()).toBe(`${at}/5`);
    await page.fill(".find-bar input", "zzzz");
    await expect.poll(() => page.locator(".find-count").innerText()).toBe("No matches");
    expect(await page.locator('[aria-label="Next match"]').isDisabled()).toBe(true);
    await escapeUntilGone(".find-bar");
    // The menu opens it as well.
    await menu("Find in page");
    await page.waitForSelector(".find-bar input");
  });

  test("zoom: the menu, Ctrl +, - and 0 in the page, and it is kept per site across tabs and restarts", async () => {
    await openChat();
    await openPage("/__test/page", "Atlas test page");
    await page.click('.side-panel [aria-label="More"]');
    await page.click('[aria-label="Zoom in"]');
    await page.click('[aria-label="Zoom in"]');
    await expect.poll(() => page.locator(".zoom-value").innerText()).toBe("125%");
    await escapeUntilGone(".web-menu");
    expect((await viewInfo()).zoom).toBeCloseTo(1.25, 2);
    expect(await page.locator(".address-chip").innerText()).toBe("125%");
    await keyInView("=", ["control"]);
    await expect.poll(async () => (await viewInfo()).zoom).toBeCloseTo(1.5, 2);
    await keyInView("-", ["control"]);
    await keyInView("-", ["control"]);
    await expect.poll(async () => (await viewInfo()).zoom).toBeCloseTo(1.1, 2);
    await keyInView("0", ["control"]);
    await expect.poll(async () => (await viewInfo()).zoom).toBeCloseTo(1, 2);
    await page.waitForTimeout(400);
    await page.click('.side-panel [aria-label="More"]');
    await page.waitForSelector(".web-menu");
    await page.click('[aria-label="Zoom in"]');
    await page.click('[aria-label="Zoom in"]');
    await escapeUntilGone(".web-menu");

    // The same site in another page keeps it; so does a new tab.
    await page.fill('input[aria-label="Search or enter URL"]', `${gateway.origin}/__test/second-same-host`);
    await page.keyboard.press("Enter");
    await page.waitForSelector('.ptab-main:has-text("Same host")');
    await expect.poll(async () => (await viewInfo()).zoom).toBeCloseTo(1.25, 2);
    // After a restart the saved zoom is applied again.
    await relaunch();
    await page.waitForSelector(".side-panel");
    await expect.poll(async () => (await viewInfo())?.zoom ?? 0, { timeout: 20_000 }).toBeCloseTo(1.25, 2);
    // Reset in the menu forgets the site.
    await page.click('.side-panel [aria-label="More"]');
    await page.click('[aria-label="Reset zoom"]');
    await expect.poll(async () => (await viewInfo()).zoom).toBeCloseTo(1, 2);
  });

  test("screenshots go to a chosen file or into the message", async () => {
    await openChat();
    await openPage("/__test/page", "Atlas test page");
    const out = path.join(userData, "shot.png");
    await app.close();
    await withEnv({ ATLAS_APP_TEST_SAVE: out }, async () => {
      await relaunch();
      await page.waitForSelector(".side-panel");
      await expect.poll(async () => (await viewInfo())?.url ?? "", { timeout: 20_000 }).toContain("/__test/page");
      await menu("Take screenshot", "Save as");
      await expect.poll(() => fs.existsSync(out) && fs.statSync(out).size, { timeout: 15_000 }).toBeGreaterThan(8);
      expect(fs.readFileSync(out).subarray(0, 8).toString("hex")).toBe("89504e470d0a1a0a");
      await page.waitForSelector('.toast:has-text("Saved")');
    });
    await menu("Take screenshot", "Add to message");
    await page.waitForSelector('.attachment[title^="page-"]');
    await page.waitForSelector('.toast:has-text("Screenshot attached")');
  });

  test("downloads are listed with progress, can pause, resume and cancel, and land in the chosen folder", async () => {
    const folder = fs.mkdtempSync(path.join(os.tmpdir(), "atlas-e2e-downloads-"));
    await openChat();
    await app.close();
    await withEnv({ ATLAS_APP_DOWNLOADS: folder }, async () => {
      await relaunch();
      await page.click(toggle);
      await page.waitForSelector(".side-panel");
      await page.fill('input[aria-label="Search or enter URL"]', `${gateway.origin}/__test/file`);
      await page.keyboard.press("Enter");
      await page.waitForSelector('.download-row[data-state="completed"]');
      expect(fs.existsSync(path.join(folder, "atlas-test.txt"))).toBe(true);
      expect(fs.statSync(path.join(folder, "atlas-test.txt")).size).toBe(2000);
      expect(await page.locator(".download-name").first().innerText()).toBe("atlas-test.txt");
      expect(await page.locator('[aria-label="Show in folder"]').count()).toBe(1);

      // A slow one: progress, pause, resume, finish; a name that is taken gets a number.
      await page.fill('input[aria-label="Search or enter URL"]', `${gateway.origin}/__test/slow`);
      await page.keyboard.press("Enter");
      await page.waitForSelector('.download-row[data-state="progressing"]');
      expect(await page.locator("progress.download-bar").count()).toBe(1);
      await page.click('[aria-label="Pause download"]');
      await page.waitForSelector('.download-row[data-state="paused"]');
      await page.click('[aria-label="Resume download"]');
      await page.waitForSelector('.download-row[data-state="completed"] >> text=atlas-test (1).txt', { timeout: 20_000 });
      expect(fs.statSync(path.join(folder, "atlas-test (1).txt")).size).toBe(400_000);

      // And one that is cancelled.
      await page.fill('input[aria-label="Search or enter URL"]', `${gateway.origin}/__test/slow`);
      await page.keyboard.press("Enter");
      await page.waitForSelector('.download-row[data-state="progressing"]');
      await page.click('[aria-label="Cancel download"]');
      await page.waitForSelector('.download-row[data-state="cancelled"]');
      await page.click('.download-row[data-state="cancelled"] [aria-label="Remove from list"]');
      await page.waitForSelector('.download-row[data-state="cancelled"]', { state: "detached" });
    });
  });

  test("device mode shows the page at a device's size with its user agent", async () => {
    await openChat();
    await openPage("/__test/size", "Size page");
    await menu("Device toolbar", "iPhone 15");
    await expect.poll(async () => await inView("document.getElementById('w').textContent")).toBe("393");
    expect(await inView("document.getElementById('ua').textContent")).toContain("iPhone");
    expect(await page.locator(".address-chip").first().innerText()).toContain("iPhone 15");
    await menu("Device toolbar", "Turn off device mode");
    await expect.poll(async () => await inView("document.getElementById('w').textContent")).not.toBe("393");
    expect(await inView("document.getElementById('ua').textContent")).not.toContain("iPhone");
    // A custom size.
    await menu("Device toolbar", "Custom size");
    await page.fill('label:has-text("Width") input', "500");
    await page.fill('label:has-text("Height") input', "700");
    await page.click('.modal .button[data-variant="primary"]');
    await expect.poll(async () => await inView("document.getElementById('w').textContent")).toBe("500");
  });

  test("history records loaded pages only, searches, deletes and clears", async () => {
    await openChat();
    await openPage("/__test/page", "Atlas test page");
    await page.fill('input[aria-label="Search or enter URL"]', `${gateway.origin}/__test/second`);
    await page.keyboard.press("Enter");
    await page.waitForSelector('.ptab-main:has-text("Second page")');
    // A load that fails is not history.
    await page.fill('input[aria-label="Search or enter URL"]', `${gateway.origin}/__test/fail`);
    await page.keyboard.press("Enter");
    await page.waitForTimeout(1200);
    await menu("History");
    await page.waitForSelector(".data-view .data-row");
    const rows = await page.locator(".data-row .data-title").allInnerTexts();
    expect(rows).toEqual(["Second page", "Atlas test page"]);
    expect(rows.join()).not.toContain("fail");
    await page.fill(".data-search input", "second");
    await expect.poll(() => page.locator(".data-row").count()).toBe(1);
    await page.fill(".data-search input", "");
    await expect.poll(() => page.locator(".data-row").count()).toBe(2);
    await page.click('[aria-label^="Remove from history: Second page"]');
    await expect.poll(() => page.locator(".data-row").count()).toBe(1);
    await page.click(".data-head .button");
    await page.click('.data-head .button[data-variant="primary"]');
    await page.waitForSelector(".data-row", { state: "detached" });
    expect(await page.locator(".rail-empty").innerText()).toContain("No pages visited yet");
  });

  test("clear browsing data asks what to clear and clears exactly that", async () => {
    await openChat();
    await openPage("/__test/page", "Atlas test page");
    await inView("document.cookie = 'fixture=1; path=/'; localStorage.setItem('k', 'v')");
    const cookies = () => app.evaluate(async ({ session }) => (await session.fromPartition("persist:atlas-browser").cookies.get({})).map((c) => c.name));
    expect(await cookies()).toContain("fixture");
    await menu("Clear browsing data");
    await page.waitForSelector(".modal");
    // Only history is ticked for cookies off: nothing of cookies goes.
    expect(await page.locator('.check-row:has-text("Cookies") input').isChecked()).toBe(false);
    await page.uncheck('.check-row:has-text("Browsing history") input');
    await page.uncheck('.check-row:has-text("Cached images") input');
    expect(await page.locator(".modal .button[data-variant=primary]").isDisabled()).toBe(true);
    await page.check('.check-row:has-text("Cookies") input');
    await page.selectOption(".modal select", { label: "Last hour" });
    await page.click(".modal .button[data-variant=primary]");
    await page.waitForSelector('.toast:has-text("Browsing data cleared")');
    expect(await cookies()).not.toContain("fixture");
    // History is untouched: it was not ticked.
    await page.keyboard.press("Escape");
    await menu("History");
    await page.waitForSelector(".data-row");
  });

  test("a note is tied to the page and the selected text, goes into the composer, and can be edited and deleted", async () => {
    await openChat();
    await openPage("/__test/page", "Atlas test page");
    await inView("getSelection().selectAllChildren(document.querySelector('h1'))");
    await page.click('[aria-label="Add note"]');
    await page.waitForSelector(".note-pop");
    expect(await page.locator(".note-quote").first().innerText()).toBe("Hello from the test page");
    await page.fill(".note-pop .note-input", "Check this heading");
    await page.click('.note-pop .button[data-variant="primary"]');
    await page.waitForSelector('.toast:has-text("Note added")');
    const composer = await page.inputValue(".composer textarea");
    expect(composer).toContain("> Hello from the test page");
    expect(composer).toContain("Check this heading");
    expect(composer).toContain(`${gateway.origin}/__test/page`);
    // The composer took focus, which closes the popover; open it again to see the note.
    await page.waitForSelector(".note-pop", { state: "detached" });
    await page.click('[aria-label="Add note"]');
    await page.waitForSelector(".note-item");
    expect(await page.locator(".note-item").count()).toBe(1);
    await page.click('.note-item [aria-label="Edit note"]');
    await page.fill(".note-item .note-input", "Changed text");
    await page.click(".note-item .button:has-text('Save')");
    await expect.poll(() => page.locator(".note-text").innerText()).toBe("Changed text");
    // It stays after a restart, on this page only.
    await page.keyboard.press("Escape");
    await relaunch();
    await page.waitForSelector(".side-panel");
    await expect.poll(async () => (await viewInfo())?.url ?? "", { timeout: 20_000 }).toContain("/__test/page");
    await page.click('[aria-label="Add note"]');
    await expect.poll(() => page.locator(".note-text").innerText()).toBe("Changed text");
    await page.click('.note-item [aria-label="Delete note"]');
    await page.waitForSelector(".note-item", { state: "detached" });
    // Not on other pages.
    await page.keyboard.press("Escape");
    await page.fill('input[aria-label="Search or enter URL"]', `${gateway.origin}/__test/second`);
    await page.keyboard.press("Enter");
    await page.waitForSelector('.ptab-main:has-text("Second page")');
    await page.click('[aria-label="Add note"]');
    expect(await page.locator(".note-item").count()).toBe(0);
  });

  test("an unpacked extension is confirmed with its permissions, runs in pages, can be switched off and on, survives a restart and is removed", async () => {
    const fixture = path.join(appDir, "test", "fixtures", "extension");
    await openChat();
    await app.close();
    await withEnv({ ATLAS_APP_TEST_EXTENSION_DIR: fixture }, async () => {
      await relaunch();
      await page.click(toggle);
      await page.waitForSelector(".side-panel");
      await page.click('[aria-label="Extensions"]');
      await page.waitForSelector(".ext-pop");
      expect(await page.locator(".ext-pop .web-pop-title").innerText()).toBe("Extensions (0)");
      expect(await page.locator(".ext-empty").innerText()).toContain("No extensions installed");
      await page.click('.ext-pop .summary-row:has-text("Manage extensions")');
      await page.waitForSelector(".data-view");
      // Cancel: nothing is loaded.
      await page.click('.data-head .button:has-text("Load unpacked extension")');
      await page.waitForSelector(".modal");
      const asked = await page.locator(".permission-list").innerText();
      expect(asked).toContain("Store its own data");
      expect(asked).toContain("http://127.0.0.1/*");
      await page.click('.modal .button:has-text("Cancel")');
      await page.waitForSelector(".modal", { state: "detached" });
      expect(await app.evaluate(({ session }) => session.fromPartition("persist:atlas-browser").extensions.getAllExtensions().length)).toBe(0);
      // Allow.
      await page.click('.data-head .button:has-text("Load unpacked extension")');
      await page.click('.modal .button:has-text("Allow and add")');
      await page.waitForSelector(".ext-card");
      expect(await page.locator(".ext-card .data-title").innerText()).toContain("Atlas Test Extension");

      await page.click(".side-panel .ptab-add");
      await page.fill('input[aria-label="Search or enter URL"]', `${gateway.origin}/__test/page`);
      await page.keyboard.press("Enter");
      await page.waitForSelector('.ptab-main:has-text("Atlas test page")');
      await expect.poll(() => inView("document.documentElement.getAttribute('data-atlas-ext')")).toBe("ran");

      // The toolbar shows its action; the popup is the extension's own page.
      await page.click('[aria-label="Atlas test action"]');
      await expect.poll(() => app.evaluate(({ webContents }) => webContents.getAllWebContents().filter((w) => w.getURL().startsWith("chrome-extension://")).length)).toBeGreaterThan(0);
      await expect
        .poll(() => app.evaluate(({ webContents }) => webContents.getAllWebContents().find((w) => w.getURL().endsWith("/popup.html"))?.executeJavaScript("document.getElementById('p').textContent")))
        .toBe("popup ran");
      await page.keyboard.press("Escape");

      // Hide from the toolbar, show again.
      await page.click('[aria-label="Extensions"]');
      await page.click('.ext-pop .button:has-text("Hide from toolbar")');
      await page.waitForSelector('[aria-label="Atlas test action"]', { state: "detached" });
      await page.click('.ext-pop .button:has-text("Show in toolbar")');
      await page.waitForSelector('[aria-label="Atlas test action"]');
      await page.keyboard.press("Escape");

      // Switch off, then on again, which asks again.
      await page.locator('.ptab-main[title="Extensions"]').click();
      await page.click('.ext-card .button:has-text("Switch off")');
      await expect.poll(() => app.evaluate(({ session }) => session.fromPartition("persist:atlas-browser").extensions.getAllExtensions().length)).toBe(0);
      await page.click('.ext-card .button:has-text("Switch on")');
      await page.waitForSelector(".modal .permission-list");
      await page.click('.modal .button:has-text("Allow and add")');
      await expect.poll(() => app.evaluate(({ session }) => session.fromPartition("persist:atlas-browser").extensions.getAllExtensions().length)).toBe(1);
    });

    // It is there after a restart, with no confirmation needed again.
    await relaunch();
    await page.waitForSelector(".side-panel");
    await expect.poll(() => app.evaluate(({ session }) => session.fromPartition("persist:atlas-browser").extensions.getAllExtensions().map((e) => e.name))).toEqual(["Atlas Test Extension"]);
    await page.locator('.ptab-main[title="Extensions"]').click();
    await page.click('.ext-card [aria-label^="Remove extension"]');
    await page.waitForSelector(".ext-card", { state: "detached" });
    expect(await app.evaluate(({ session }) => session.fromPartition("persist:atlas-browser").extensions.getAllExtensions().length)).toBe(0);
  });

  test("a changed manifest is not trusted: the extension comes back switched off", async () => {
    const copy = fs.mkdtempSync(path.join(os.tmpdir(), "atlas-e2e-ext-"));
    for (const f of fs.readdirSync(path.join(appDir, "test", "fixtures", "extension"))) fs.copyFileSync(path.join(appDir, "test", "fixtures", "extension", f), path.join(copy, f));
    await openChat();
    await app.close();
    await withEnv({ ATLAS_APP_TEST_EXTENSION_DIR: copy }, async () => {
      await relaunch();
      await page.click(toggle);
      await page.click('[aria-label="Extensions"]');
      await page.click('.ext-pop .summary-row:has-text("Manage extensions")');
      await page.click('.data-head .button:has-text("Load unpacked extension")');
      await page.click('.modal .button:has-text("Allow and add")');
      await page.waitForSelector(".ext-card");
    });
    // The extension now asks for more than was confirmed.
    const manifest = JSON.parse(fs.readFileSync(path.join(copy, "manifest.json"), "utf8"));
    manifest.permissions.push("cookies");
    fs.writeFileSync(path.join(copy, "manifest.json"), JSON.stringify(manifest));
    await relaunch();
    await page.waitForSelector(".side-panel");
    await page.locator('.ptab-main[title="Extensions"]').click();
    await page.waitForSelector(".ext-card");
    expect(await app.evaluate(({ session }) => session.fromPartition("persist:atlas-browser").extensions.getAllExtensions().length)).toBe(0);
    await page.click('.ext-card .button:has-text("Switch on")');
    expect(await page.locator(".modal .permission-list").innerText()).toContain("Read and change cookies");
  });

  test("passwords: offered on a submitted login, saved encrypted, filled only on its exact origin and only when asked", async () => {
    await openChat();
    await openPage("/__test/login", "Login page");
    const loginUrl = (await viewInfo()).url;
    const origin = new URL(loginUrl).origin;
    await inView("document.getElementById('user').focus()");
    await typeInView("fixture-user");
    await inView("document.getElementById('pass').focus()");
    await typeInView("fixture-secret-9");
    await clickInView("go");
    await page.waitForSelector(".login-bar");
    expect(await page.locator(".login-bar").innerText()).toContain("127.0.0.1");
    await expect.poll(() => page.locator(".login-bar").innerText()).toContain("fixture-user");
    // The app is told who and where, never the password.
    expect(await page.locator(".login-bar").innerText()).not.toContain("fixture-secret-9");
    await page.click('.login-bar .button[data-variant="primary"]');
    await page.waitForSelector('.toast:has-text("Password saved")');

    // Encrypted on disk; not in prefs; not in what the renderer can list.
    const vault = fs.readFileSync(path.join(userData, "vault.json"), "utf8");
    expect(vault).not.toContain("fixture-secret-9");
    expect(vault).toContain(origin);
    const prefsText = fs.readFileSync(path.join(userData, "prefs.json"), "utf8");
    expect(prefsText).not.toContain("fixture-secret-9");
    const listed = await page.evaluate(() => window.atlas.data.vault.list("").then((l) => JSON.stringify(l)));
    expect(listed).not.toContain("fixture-secret-9");

    // Back on the login page: nothing is filled by itself; the key button fills when pressed.
    await page.fill('input[aria-label="Search or enter URL"]', loginUrl);
    await page.keyboard.press("Enter");
    await page.waitForSelector(".address-key");
    await page.waitForTimeout(500);
    expect(await inView("document.getElementById('pass').value")).toBe("");
    await page.click(".address-key");
    await page.click('.web-pop .summary-row:has-text("fixture-user")');
    await expect.poll(() => inView("document.getElementById('pass').value")).toBe("fixture-secret-9");
    expect(await inView("document.getElementById('user').value")).toBe("fixture-user");
    // A field the person cannot see is left alone.
    expect(await inView("document.getElementById('hidden').value")).toBe("");

    // Another origin of the same machine gets nothing.
    const other = origin.replace("127.0.0.1", "localhost");
    await page.fill('input[aria-label="Search or enter URL"]', `${other}/__test/login`);
    await page.keyboard.press("Enter");
    await expect.poll(async () => (await viewInfo())?.url ?? "").toContain("localhost");
    await page.waitForTimeout(400);
    await page.waitForSelector(".address-key");
    await page.click(".address-key");
    await expect.poll(() => page.locator(".web-pop").innerText()).toContain("No saved login for this site");
    await page.keyboard.press("Escape");
    expect(await inView("document.getElementById('pass').value")).toBe("");

    // The manager: reveal, edit, delete.
    await menu("Passwords and autofill", "Passwords");
    await page.waitForSelector(".password-row");
    expect(await page.locator(".password-row").innerText()).toContain("fixture-user");
    expect(await page.locator(".password-secret").count()).toBe(0);
    await page.click('.password-row [aria-label^="Show password"]');
    expect(await page.locator(".password-secret").innerText()).toBe("fixture-secret-9");
    await page.click('.password-row [aria-label^="Edit login"]');
    await page.fill('.password-edit input[type="password"]', "fixture-secret-10");
    await page.click(".password-edit .button:has-text('Save')");
    await page.click('.password-row [aria-label^="Show password"]');
    await expect.poll(() => page.locator(".password-secret").innerText()).toBe("fixture-secret-10");
    await page.fill(".data-search input", "nothing-matches");
    await page.waitForSelector(".password-row", { state: "detached" });
    await page.fill(".data-search input", "127");
    await page.click('.password-row [aria-label^="Delete login"]');
    await page.waitForSelector(".password-row", { state: "detached" });
    expect(await page.evaluate(() => window.atlas.data.vault.list("").then((l) => l.length))).toBe(0);
  });

  test("never for this site stops the offer; saving the same login again changes nothing", async () => {
    await openChat();
    await openPage("/__test/login", "Login page");
    const loginUrl = (await viewInfo()).url;
    const submit = async () => {
      await inView("document.getElementById('user').value = ''; document.getElementById('pass').value = ''; document.getElementById('user').focus()");
      await typeInView("fixture-user");
      await inView("document.getElementById('pass').focus()");
      await typeInView("fixture-secret-9");
      await clickInView("go");
    };
    await submit();
    await page.waitForSelector(".login-bar");
    await page.click(".login-bar .button:has-text('Never for this site')");
    await page.waitForSelector(".login-bar", { state: "detached" });
    await page.fill('input[aria-label="Search or enter URL"]', loginUrl);
    await page.keyboard.press("Enter");
    await page.waitForSelector(".address-key");
    await submit();
    await page.waitForTimeout(1500);
    expect(await page.locator(".login-bar").count()).toBe(0);
    expect(await page.evaluate(() => window.atlas.data.vault.list("").then((l) => l.length))).toBe(0);
  });

  test("passwords import from a CSV export with a warning, a summary and nothing from a file that is not one", async () => {
    await openChat();
    await withEnv({ ATLAS_APP_TEST_CSV: path.join(appDir, "test", "fixtures", "passwords-chrome.csv"), ATLAS_APP_TEST_NO_PROFILES: "1" }, async () => {
      await app.close();
      await relaunch();
      await page.click(toggle);
      await page.waitForSelector(".side-panel");
      await menu("Import cookies and passwords");
      await page.waitForSelector(".modal");
      // No other browser is looked into during a test.
      expect(await page.locator(".modal").innerText()).toContain("No Chrome, Edge or Firefox profile was found");
      expect(await page.locator(".import-warning").innerText()).toContain("Delete it now");
      await page.click('.modal .button:has-text("Choose CSV file")');
      await page.waitForSelector(".import-result");
      expect(await page.locator(".import-result").innerText()).toContain("Passwords imported: 1");
      await page.click('.modal .button:has-text("Close")');
    });
    expect(await page.evaluate(() => window.atlas.data.vault.list("").then((l) => l.map((e) => `${e.origin} ${e.username}`)))).toEqual(["https://example.test fixture-user"]);
    expect(fs.readFileSync(path.join(userData, "vault.json"), "utf8")).not.toContain("fixture-pass-1");
  });

  test("the app's own shortcuts work with the page in front; editing keys and everything else go to the page", async () => {
    await openChat();
    await openPage("/__test/keys", "Keys page");
    await keyInView("Left", ["control"]);
    await keyInView("Backspace", ["control"]);
    await keyInView("Z", ["control", "shift"]);
    await keyInView("V", ["control", "shift"]);
    await expect.poll(() => inView("document.documentElement.dataset.keys")).toContain("CSV|");
    const keys = await inView("document.documentElement.dataset.keys");
    expect(keys).toContain("CArrowLeft|");
    expect(keys).toContain("CBackspace|");
    expect(keys).toContain("CSZ|");
    expect(keys).toContain("CSV|");
    // The panel shortcut does not reach the page: it toggles the panel.
    await keyInView("B", ["control", "alt"]);
    await page.waitForSelector(".side-panel", { state: "detached" });
    expect((await inView("document.documentElement.dataset.keys")) ?? "").not.toContain("CAB|");
  });

  test("a redirect to a forbidden address is blocked, loading ends, and the address bar shows what the view shows", async () => {
    await openChat();
    await page.click(toggle);
    await page.fill('input[aria-label="Search or enter URL"]', `${gateway.origin}/__test/redirect`);
    await page.keyboard.press("Enter");
    // The view shows the page it could not follow, and nothing is left loading.
    await page.waitForSelector('button[aria-label="Reload"]');
    expect(await page.inputValue(".address input")).toBe(new URL(gateway.origin).hostname);
    expect(await app.evaluate(({ webContents }) => webContents.getAllWebContents().some((w) => w.getURL().startsWith("file:")))).toBe(false);
    // From a page that is showing, the same.
    await page.fill('input[aria-label="Search or enter URL"]', `${gateway.origin}/__test/page`);
    await page.keyboard.press("Enter");
    await page.waitForSelector('.ptab-main:has-text("Atlas test page")');
    await page.fill('input[aria-label="Search or enter URL"]', `${gateway.origin}/__test/redirect`);
    await page.keyboard.press("Enter");
    await page.waitForSelector('button[aria-label="Reload"]');
    await expect.poll(() => page.inputValue(".address input")).toBe(new URL(gateway.origin).hostname);
    expect(await page.locator('button[aria-label="Stop loading"]').count()).toBe(0);
  });

  test("a page cannot make the app fetch an icon from inside the network", async () => {
    await openChat();
    await openPage("/__test/favicon", "Icon page");
    await page.waitForTimeout(2000);
    expect((await gateway.state()).iconHits).toBe(0);
    expect(await page.locator(".ptab-main img.favicon").count()).toBe(0);
  });

  test("a toast shows over the page, and Esc closes a popover every time", async () => {
    await openChat();
    await openPage("/__test/page", "Atlas test page");
    await expect.poll(viewCount).toBe(1);
    for (let i = 0; i < 6; i++) {
      await page.click('[aria-label="Chat summary"]');
      await page.waitForSelector(".summary-pop");
      await escapeUntilGone(".summary-pop");
    }
    expect(await viewCount()).toBe(1);
    await menu("Copy address");
    await page.waitForSelector('.toast:has-text("Copied")');
    // The page stays, and the toast is drawn above it where it can be read.
    expect(await viewCount()).toBe(1);
    const toastOnTop = await page.evaluate(() => {
      const el = document.querySelector(".toast");
      const r = el.getBoundingClientRect();
      return !!document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2)?.closest(".toast");
    });
    expect(toastOnTop).toBe(true);
    await page.waitForSelector(".toast", { state: "detached", timeout: 8000 });
    expect(await viewCount()).toBe(1);
  });

  test("Esc closes a menu inside the popover, and the next Esc closes the popover", async () => {
    await openChat();
    await page.click('button[aria-label="Chat summary"]');
    await page.waitForSelector(".summary-pop");
    await page.click('.summary-pop [aria-label="Project menu"]');
    await page.waitForSelector(".menu");
    // The menu takes focus a frame after it appears; an Esc before that has
    // nothing to close it, so press it once focus is inside, as a person would.
    await page.waitForFunction(() => document.activeElement?.closest(".menu"));
    await page.keyboard.press("Escape");
    await page.waitForSelector(".menu", { state: "detached" });
    expect(await page.locator(".summary-pop").count()).toBe(1);
    await page.waitForFunction(() => document.activeElement?.closest(".summary-pop"));
    await page.keyboard.press("Escape");
    await page.waitForSelector(".summary-pop", { state: "detached" });
  });

  test("a chat's preview card stays shut after its row is clicked, and comes back on the next hover", async () => {
    await openChat();
    // The pointer is still on the row it clicked: no preview may open late on
    // top of what comes next, where it would take that menu's Esc.
    await page.waitForTimeout(900);
    expect(await page.locator(".hover-card").count()).toBe(0);
    const row = await page.locator(".session-main").boundingBox();
    await page.mouse.move(row.x + row.width + 300, row.y + 300);
    await page.mouse.move(row.x + 20, row.y + row.height / 2, { steps: 3 });
    await page.waitForSelector(".hover-card");
  });

  test("the side panel saves a project's panel without erasing another project's", async () => {
    fs.mkdirSync(path.join(os.tmpdir(), "atlas-e2e-project"), { recursive: true });
    const other = { open: true, width: 700, expanded: false, tabs: [{ id: "x1", kind: "terminal" }], active: "x1" };
    await gateway.seed({ sessions: [session("s1", "First chat")], messages: { s1: [userMessage("u1", "s1", "Hello"), assistantMessage("a1", "s1", parts("Hi."))] } });
    await launch({ ui: { onboarded: true, lang: "en", panels: { "c:/some/other/project": other } } });
    await page.click(".session-main");
    await ready();
    await page.click(toggle);
    await page.waitForSelector(".side-panel");
    await page.click('.side-panel [aria-label="New tab"]');
    await expect.poll(() => JSON.parse(fs.readFileSync(path.join(userData, "prefs.json"), "utf8")).ui?.panels?.[panelKey]?.tabs?.length).toBe(2);
    const saved = JSON.parse(fs.readFileSync(path.join(userData, "prefs.json"), "utf8")).ui;
    expect(saved.panels["c:/some/other/project"]).toEqual(other);
    // The generic writer cannot overwrite the panels wholesale.
    await page.evaluate(() => window.atlas.setUI("panels", {}));
    await page.waitForTimeout(300);
    expect(Object.keys(JSON.parse(fs.readFileSync(path.join(userData, "prefs.json"), "utf8")).ui.panels).sort()).toEqual(["c:/some/other/project", panelKey].sort());
  });

  test("a side chat is never in the chat list, even when the server first announced it untagged", async () => {
    await gateway.seed({ sessions: [session("s1", "First chat")], messages: { s1: [userMessage("u1", "s1", "Hello"), assistantMessage("a1", "s1", parts("Hi."))] }, twoStepSideChat: true });
    await launch();
    await page.click(".session-main");
    await ready();
    await page.keyboard.press("Control+Shift+y");
    await page.waitForSelector(".side-chat .turn");
    await page.waitForTimeout(600);
    expect(await page.locator(".session-main").count()).toBe(1);
    await page.click('[aria-label="Chat summary"]');
    expect(await page.locator(".summary-section h3").allInnerTexts()).toContain("Side chats");
  });

  test("Jobs has a way in from the new tab page", async () => {
    await openChat();
    await page.click(toggle);
    await page.click('.tool-cell [aria-label="More"]');
    await page.click('.menu-item:has-text("Jobs")');
    await expect.poll(() => tabTitles()).toEqual(["Jobs"]);
  });

  test("the panel is laid out like the reference: header on one row, flush panel, tabs, popover, thread minimum", async () => {
    await openWorkChat();
    await page.click(toggle);
    await page.waitForSelector(".side-panel");
    const head = await page.locator(".thread-head").boundingBox();
    const panelHead = await page.locator(".panel-head").boundingBox();
    expect(Math.round(head.y)).toBe(Math.round(panelHead.y));
    expect(Math.round(head.height)).toBe(Math.round(panelHead.height));
    // No card around the panel: it runs to the window's edge, with a 1px divider.
    const panel = await page.locator(".side-panel").boundingBox();
    const win = await page.evaluate(() => ({ w: innerWidth, h: innerHeight }));
    expect(Math.round(panel.x + panel.width)).toBe(win.w);
    expect(Math.round(panel.y + panel.height)).toBe(win.h);
    expect(await page.locator(".side-panel").evaluate((el) => getComputedStyle(el).borderInlineStartWidth)).toBe("1px");
    // The active tab is a bordered card; the plus follows the last tab.
    expect(await page.locator(".ptab[data-active]").evaluate((el) => getComputedStyle(el).borderTopWidth)).toBe("1px");
    await page.click('.side-panel [aria-label="New tab"]');
    // The new tab slides in with a view transition, so the plus settles a moment later.
    await expect
      .poll(async () => {
        const last = await page.locator(".ptab").last().boundingBox();
        const plus = await page.locator(".ptab-add").boundingBox();
        return plus.x - (last.x + last.width);
      })
      .toBeLessThan(12);
    // While the panel is open the header has no plus; closed, it does and it opens the panel.
    expect(await page.locator('.thread-head [aria-label="Open side panel"]').count()).toBe(0);
    await page.click('[aria-label="Close panel"]');
    await page.click('.thread-head [aria-label="Open side panel"]');
    await page.waitForSelector(".side-panel");
    // The summary popover sits under its toggle, right-aligned, and the toggle stays lit.
    await page.click('button[aria-label="Chat summary"]');
    await page.waitForSelector(".summary-pop");
    const toggleBox = await page.locator('button[aria-label="Chat summary"]').boundingBox();
    const pop = await page.locator(".summary-pop").boundingBox();
    expect(pop.y).toBeGreaterThan(toggleBox.y + toggleBox.height);
    expect(Math.abs(pop.x + pop.width - (toggleBox.x + toggleBox.width))).toBeLessThan(16);
    expect(await page.locator('button[aria-label="Chat summary"]').getAttribute("data-on")).not.toBeNull();
    await page.keyboard.press("Escape");
    // Dragged as wide as it goes, the thread keeps 320px and the composer drops its labels.
    const edge = await page.locator(".panel-handle").boundingBox();
    await page.mouse.move(edge.x + 4, edge.y + 300);
    await page.mouse.down();
    await page.mouse.move(0, edge.y + 300, { steps: 6 });
    await page.mouse.up();
    const thread = await page.locator(".card").boundingBox();
    expect(Math.round(thread.width)).toBeGreaterThanOrEqual(319);
    expect(Math.round(thread.width)).toBeLessThanOrEqual(345);
    expect(await page.locator(".composer-meta-project > span").first().isVisible()).toBe(false);
  });

  test("the popover lists the agents a chat started, its web sources and attachments, which open", async () => {
    const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==";
    const call = (id, name, input) => ({ type: "tool_call", data: { id, name, input: JSON.stringify(input), finished: true } });
    const result = (id, name) => ({ type: "tool_result", data: { tool_call_id: id, name, content: "ok", metadata: "", is_error: false } });
    await gateway.seed({
      sessions: [session("s1", "Research")],
      messages: {
        s1: [
          { ...userMessage("u1", "s1", "Look"), parts: [...parts("Look"), { type: "image_url", data: { url: `data:image/png;base64,${png}` } }, { type: "binary", data: { Path: "notes.txt", MIMEType: "text/plain", Data: Buffer.from("fixture notes text").toString("base64") } }] },
          assistantMessage("a1", "s1", [
            call("g1", "fetch", { url: "https://github.com/example/repo/issues/1" }),
            result("g1", "fetch"),
            call("w1", "web_search", { query: "atlas fixture" }),
            result("w1", "web_search"),
            call("t1", "agent", { description: "one" }),
            result("t1", "agent"),
            call("t2", "agent", { description: "two" }),
            result("t2", "agent"),
            call("t3", "agent", { description: "three" }),
            ...parts("Done."),
          ], false),
        ],
      },
    });
    await launch();
    await page.click(".session-main");
    await page.waitForSelector(".turn");
    await page.click('[aria-label="Chat summary"]');
    await page.waitForSelector(".summary-pop");
    expect(await page.locator(".summary-section h3").allInnerTexts()).toEqual(["Subagents", "Sources"]);
    expect(await page.locator(".summary-pop").innerText()).toContain("1 running, 2 finished");
    const names = await page.locator(".summary-sources .summary-name").allInnerTexts();
    expect(names).toContain("github.com/example/repo/issues/1");
    expect(names).toContain("Web search");
    // A link opens in the panel browser.
    await page.click('.summary-sources .summary-row:has-text("github.com/example")');
    await page.waitForSelector(".side-panel");
    // An attachment opens as it was sent.
    await page.click('[aria-label="Chat summary"]');
    await page.click('.summary-sources .summary-row:has-text("notes.txt")');
    await page.waitForSelector(".preview-text");
    expect(await page.locator(".preview-text").innerText()).toBe("fixture notes text");
    await escapeUntilGone(".preview-text");
  });

  // ----- Round three -----

  /** Saves a login straight into the vault, as the manager's Add does. */
  const addLogin = (site, user, pass) => page.evaluate(([a, b, c]) => window.atlas.data.vault.add(a, b, c), [site, user, pass]);

  test("showing or copying a password asks in a native window; copy never gives the secret to the page and clears itself; asks are rate limited", async () => {
    await openChat();
    expect((await addLogin("https://vault.test", "fixture-user", "fixture-secret-77")).ok).toBe(true);
    await app.close();
    await withEnv({ ATLAS_APP_TEST_CONFIRM: "no", ATLAS_APP_CLIPBOARD_CLEAR_MS: "700" }, async () => {
      await relaunch();
      await page.waitForSelector(".composer textarea");
      // Cancelled: nothing comes back.
      const denied = await page.evaluate(async () => {
        const id = (await window.atlas.data.vault.list(""))[0].id;
        return [await window.atlas.data.vault.reveal(id), await window.atlas.data.vault.copy(id)];
      });
      expect(denied[0].ok).toBe(false);
      expect(denied[0].password).toBeUndefined();
      expect(denied[1].ok).toBe(false);
    });
    await app.close();
    await withEnv({ ATLAS_APP_TEST_CONFIRM: "yes", ATLAS_APP_CLIPBOARD_CLEAR_MS: "700" }, async () => {
      await relaunch();
      await page.waitForSelector(".composer textarea");
      await app.evaluate(({ clipboard }) => clipboard.writeText("before"));
      const copied = await page.evaluate(async () => {
        const id = (await window.atlas.data.vault.list(""))[0].id;
        return await window.atlas.data.vault.copy(id);
      });
      // The page learns that it worked, never the value.
      expect(copied).toEqual({ ok: true });
      expect(await app.evaluate(({ clipboard }) => clipboard.readText())).toBe("fixture-secret-77");
      await expect.poll(() => app.evaluate(({ clipboard }) => clipboard.readText()), { timeout: 5000 }).toBe("");
      // Something else copied since is left alone.
      await page.evaluate(async () => window.atlas.data.vault.copy((await window.atlas.data.vault.list(""))[0].id));
      await app.evaluate(({ clipboard }) => clipboard.writeText("someone else"));
      await page.waitForTimeout(1200);
      expect(await app.evaluate(({ clipboard }) => clipboard.readText())).toBe("someone else");
      // Five asks a minute: the rest are refused whatever the answer would be.
      const results = await page.evaluate(async () => {
        const id = (await window.atlas.data.vault.list(""))[0].id;
        const out = [];
        for (let i = 0; i < 6; i++) out.push((await window.atlas.data.vault.reveal(id)).ok);
        return out;
      });
      expect(results.filter(Boolean).length).toBeLessThan(6);
      expect(results.at(-1)).toBe(false);
    });
  });

  test("the find bar opening and closing resizes the page in the DOM: the page follows its box frame by frame, with no picture", async () => {
    await openChat();
    await openPage("/__test/page", "Atlas test page");
    await expect.poll(viewCount).toBe(1);
    const before = await viewBox();
    // Every animation frame: the page's height against its box's height.
    await page.evaluate(() => {
      window.__frames = [];
      const tick = () => {
        const w = [...document.querySelectorAll("webview")].find((x) => getComputedStyle(x).visibility === "visible");
        const box = document.querySelector(".web-view");
        if (w && box) window.__frames.push([w.getBoundingClientRect().height, box.getBoundingClientRect().height]);
        window.__run && requestAnimationFrame(tick);
      };
      window.__run = true;
      requestAnimationFrame(tick);
    });
    await page.keyboard.press("Control+f");
    await page.waitForSelector(".find-bar");
    await page.waitForTimeout(500);
    await escapeUntilGone(".find-bar");
    await page.waitForTimeout(500);
    const frames = await page.evaluate(() => {
      window.__run = false;
      return window.__frames;
    });
    // The box did change, and the page was never more than one frame behind it.
    expect(new Set(frames.map((f) => Math.round(f[1]))).size).toBeGreaterThan(1);
    for (let i = 1; i < frames.length; i++) expect(Math.abs(frames[i][0] - frames[i][1]) < 1.5 || Math.abs(frames[i][0] - frames[i - 1][1]) < 1.5, `frame ${i}: ${frames[i]}`).toBe(true);
    expect(await page.locator(".web-snapshot").count()).toBe(0);
    expect((await viewBox()).height).toBeCloseTo(before.height, 0);
  });

  test("the attach gate: a <webview> nobody announced is refused; an announced one is rewritten to our terms; a bare id is never trusted", async () => {
    await openChat();
    await page.click(toggle);
    await page.waitForSelector(".side-panel");
    // Page content that managed to put a <webview> into the app (markdown, model output) gets no guest.
    await page.evaluate((origin) => {
      const w = document.createElement("webview");
      w.setAttribute("src", `${origin}/__test/page`);
      w.setAttribute("data-tab", "injected");
      document.body.append(w);
    }, gateway.origin);
    await page.waitForTimeout(1500);
    expect(await guestCount()).toBe(0);
    await page.evaluate(() => document.querySelector('webview[data-tab="injected"]')?.remove());

    // A bare id is no proof: nothing was announced, and a real guest is no use for the wrong tab.
    expect(await page.evaluate(() => window.atlas.browser.attach("nope", 1, "https://example.com/"))).toBe(null);
    expect(await page.evaluate(() => window.atlas.browser.expect(""))).toBe(false);

    // Announced, but asking for a file:// address: refused.
    expect(await page.evaluate(() => window.atlas.browser.expect("file-tab"))).toBe(true);
    await page.evaluate(() => {
      const w = document.createElement("webview");
      w.setAttribute("src", "file:///C:/Windows/win.ini");
      document.body.append(w);
    });
    await page.waitForTimeout(1200);
    expect(await guestCount()).toBe(0);

    // Announced, and asking for everything it should not have: preload, popups, another partition, Node, no sandbox.
    await page.evaluate(() => {
      const w = document.createElement("webview");
      w.setAttribute("src", "about:blank");
      w.setAttribute("preload", "file:///C:/Windows/evil.js");
      w.setAttribute("allowpopups", "");
      w.setAttribute("partition", "persist:other");
      w.setAttribute("nodeintegration", "");
      w.setAttribute("disablewebsecurity", "");
      w.setAttribute("webpreferences", "sandbox=no,nodeIntegration=yes,contextIsolation=no,webSecurity=no");
      w.setAttribute("data-tab", "file-tab");
      document.body.append(w);
    });
    await expect.poll(guestCount, { timeout: 8000 }).toBe(1);
    const facts = await app.evaluate(({ webContents, session: electronSession }) => {
      const guest = webContents.getAllWebContents().find((w) => w.getType() === "webview");
      const prefs = guest.getLastWebPreferences();
      return {
        sandbox: prefs.sandbox,
        nodeIntegration: prefs.nodeIntegration,
        nodeIntegrationInSubFrames: prefs.nodeIntegrationInSubFrames,
        contextIsolation: prefs.contextIsolation,
        webSecurity: prefs.webSecurity,
        webviewTag: prefs.webviewTag,
        plugins: prefs.plugins,
        inBrowserProfile: guest.session === electronSession.fromPartition("persist:atlas-browser"),
        inOtherProfile: guest.session === electronSession.fromPartition("persist:other"),
      };
    });
    expect(facts).toMatchObject({ sandbox: true, nodeIntegration: false, nodeIntegrationInSubFrames: false, contextIsolation: true, webSecurity: true, webviewTag: false, inBrowserProfile: true, inOtherProfile: false });
    expect(facts.plugins).toBeFalsy();
    // The guest is attached only for the announced tab, by a host that is the sender.
    const id = await page.evaluate(() => document.querySelector('webview[data-tab="file-tab"]').getWebContentsId());
    expect(await page.evaluate(([i]) => window.atlas.browser.attach("other-tab", i, ""), [id])).toBe(null);
    expect(await page.evaluate(([i]) => window.atlas.browser.attach("file-tab", i, ""), [id])).not.toBe(null);
    // And it is not a way into the app: no window.atlas, no Node, and the gateway wants its cookie.
    await page.evaluate(() => window.atlas.browser.navigate("file-tab", location.origin + "/__test/page"));
    await expect.poll(async () => (await viewInfo())?.url ?? "", { timeout: 20_000 }).toContain("/__test/page");
    const reach = await inView(`JSON.stringify({ atlas: typeof window.atlas, req: typeof require, proc: typeof process })`);
    expect(JSON.parse(reach)).toEqual({ atlas: "undefined", req: "undefined", proc: "undefined" });
    const status = await inView(`fetch("${gateway.origin}/api/state").then((r) => r.status).catch(() => -1)`);
    expect([401, 403, 404, -1]).toContain(status);
  });

  test("a zoom is on disk the moment it changes, even if the app is killed", async () => {
    await openChat();
    await openPage("/__test/page", "Atlas test page");
    await page.click('.side-panel [aria-label="More"]');
    await page.click('[aria-label="Zoom in"]');
    await expect.poll(async () => (await viewInfo()).zoom).toBeGreaterThan(1);
    const file = path.join(userData, "browser-zoom.json");
    await expect.poll(() => fs.existsSync(file) && fs.readFileSync(file, "utf8")).toContain("127.0.0.1");
    app.process().kill();
    expect(Object.entries(JSON.parse(fs.readFileSync(file, "utf8")).zoom).find(([host]) => host.startsWith("127.0.0.1"))?.[1]).toBeGreaterThan(1);
  });

  test("a login offer is forgotten when it is old; a script's requestSubmit never makes one", async () => {
    await openChat();
    await app.close();
    await withEnv({ ATLAS_APP_OFFER_TTL_MS: "1500" }, async () => {
      await relaunch();
      await page.click(toggle);
      await page.waitForSelector(".side-panel");
      // A page that submits by itself, with no press of the person's, makes no offer.
      await openPage("/__test/login-script", "Script login", { keepPanel: true });
      await page.waitForTimeout(3500);
      expect(await page.locator(".login-bar").count()).toBe(0);
      expect(await page.evaluate(() => window.atlas.data.vault.list(""))).toHaveLength(0);

      // A real one is offered; answered late it is gone, and nothing is saved.
      await page.click('.side-panel [aria-label="New tab"]');
      await page.waitForSelector(".newtab");
      await page.waitForTimeout(400);
      await page.fill('input[aria-label="Search or enter URL"]', `${gateway.origin}/__test/login`);
      await page.keyboard.press("Enter");
      await page.waitForSelector('.ptab-main:has-text("Login page")');
      await expect.poll(() => viewCount()).toBeGreaterThan(0);
      await page.waitForTimeout(600);
      await inView("document.getElementById('user').focus()");
      await typeInView("late-user");
      await inView("document.getElementById('pass').focus()");
      await typeInView("late-secret");
      await clickInView("go");
      await page.waitForSelector(".login-bar");
      await page.waitForTimeout(2200);
      await page.click(".login-bar .button[data-variant='primary']");
      await page.waitForSelector(".login-bar", { state: "detached" });
      await page.waitForTimeout(500);
      expect(await page.evaluate(() => window.atlas.data.vault.list(""))).toHaveLength(0);
    });
  });

  test("the ... menu follows the reference: items, order, separators; its tooltip stays shut while it is open", async () => {
    await openChat();
    await openPage("/__test/page", "Atlas test page");
    await page.hover('.side-panel [aria-label="More"]');
    await page.click('.side-panel [aria-label="More"]');
    await page.waitForSelector(".web-menu");
    await page.waitForTimeout(700);
    expect(await page.locator('.tip:has-text("More")').count()).toBe(0);
    const rows = await page.evaluate(() => {
      const menu = document.querySelector(".web-menu");
      return [...menu.children].map((el) => (el.classList.contains("menu-sep") ? "---" : el.querySelector(".menu-text")?.textContent ?? ""));
    });
    expect(rows).toEqual(["Find in page", "Print", "Zoom", "---", "Device toolbar", "Take screenshot", "---", "Import cookies and passwords...", "Passwords and autofill", "Downloads", "History", "Clear browsing data", "---", "Browser settings", "---", "Open in your browser", "Copy address"]);
    // No icons on the items, as in the reference.
    expect(await page.locator(".web-menu > .menu-item:not([aria-haspopup]) svg").count()).toBe(0);
  });

  test("browser settings: the search engine, offering to save passwords, never-save sites and the shortcuts all work", async () => {
    await openChat();
    await openPage("/__test/login", "Login page");
    await menu("Browser settings");
    await page.waitForSelector(".settings-view");
    expect(await tabTitles()).toContain("Browser settings");
    // The engine is what an address that is not a page goes to.
    await page.selectOption('.settings-view select[aria-label="Search engine"]', "google");
    await expect.poll(() => fs.existsSync(path.join(userData, "browser-settings.json")) && fs.readFileSync(path.join(userData, "browser-settings.json"), "utf8")).toContain("google");
    expect(await page.evaluate(() => window.atlas.data.settings.get().then((x) => x.searchEngine))).toBe("google");
    // Off: a submitted login is not offered.
    await page.locator('.ptab-main[title="Browser settings"]').click();
    await page.click('.settings-view [aria-label="Offer to save passwords"]');
    await expect.poll(() => page.evaluate(() => window.atlas.data.settings.get().then((s) => s.offerSave))).toBe(false);
    // The never list starts empty.
    expect(await page.locator(".settings-view .never-row").count()).toBe(0);
  });

  test("addresses: saved encrypted, offered on an address form, filled only on the button, only visible fields, and off when switched off", async () => {
    await openChat();
    await page.click(toggle);
    await page.waitForSelector(".side-panel");
    await openPage("/__test/address", "Address page", { keepPanel: true });
    await menu("Passwords and autofill", "Addresses");
    await page.waitForSelector(".data-view");
    await page.click('.data-head .button:has-text("Add address")');
    for (const [label, value] of [["Full name", "Ada Fixture"], ["Street address", "1 Test Road"], ["City", "Testville"], ["Postal code", "99999"], ["Country", "TR"], ["Phone", "+90 555 0100"]]) await page.fill(`.address-form [aria-label="${label}"]`, value);
    await page.click('.address-form .button:has-text("Save address")');
    await page.waitForSelector(".address-row");
    expect(await page.locator(".address-row").innerText()).toContain("Ada Fixture");
    // On disk it is encrypted, never plain text.
    const file = path.join(userData, "addresses.json");
    await expect.poll(() => fs.existsSync(file)).toBe(true);
    expect(fs.readFileSync(file, "utf8")).not.toContain("Ada");
    expect(fs.readFileSync(file, "utf8")).not.toContain("Testville");

    // Back on the form: nothing is filled until the button is used.
    await page.locator('.ptab-main:has-text("Address page")').click();
    await page.waitForSelector(".address-pin");
    expect(await inView("document.getElementById('aname').value")).toBe("");
    await page.click(".address-pin");
    await page.click('.web-pop .summary-row:has-text("Ada Fixture")');
    await expect.poll(() => inView("document.getElementById('aname').value")).toBe("Ada Fixture");
    expect(await inView("document.getElementById('astreet').value")).toBe("1 Test Road");
    expect(await inView("document.getElementById('acity').value")).toBe("Testville");
    expect(await inView("document.getElementById('apostal').value")).toBe("99999");
    expect(await inView("document.getElementById('acountry').value")).toBe("TR");
    expect(await inView("document.getElementById('aphone').value")).toBe("+90 555 0100");
    // A field the person cannot see is left alone.
    expect(await inView("document.getElementById('ahidden').value")).toBe("");

    // Switched off: no button.
    await page.evaluate(() => window.atlas.data.settings.set("addressAutofill", false));
    await page.click('.side-panel [aria-label="Reload"]');
    await page.waitForTimeout(1500);
    expect(await page.locator(".address-pin").count()).toBe(0);
  });

  test("an extension button with no popup is drawn and its click reaches the extension in the page in front", async () => {
    const fixture = path.join(appDir, "test", "fixtures", "extension-click");
    await openChat();
    await app.close();
    await withEnv({ ATLAS_APP_TEST_EXTENSION_DIR: fixture }, async () => {
      await relaunch();
      await page.click(toggle);
      await page.click('[aria-label="Extensions"]');
      await page.click('.ext-pop .summary-row:has-text("Manage extensions")');
      await page.click('.data-head .button:has-text("Load unpacked extension")');
      await page.click('.modal .button:has-text("Allow and add")');
      await page.waitForSelector(".ext-card");
      await page.click(".side-panel .ptab-add");
      await page.fill('input[aria-label="Search or enter URL"]', `${gateway.origin}/__test/page`);
      await page.keyboard.press("Enter");
      await page.waitForSelector('.ptab-main:has-text("Atlas test page")');
      await page.waitForSelector('[aria-label="Atlas click action"]');
      expect(await inView("document.documentElement.getAttribute('data-atlas-ext-click')")).toBe(null);
      await page.waitForTimeout(800);
      await page.click('[aria-label="Atlas click action"]');
      await expect.poll(() => inView("document.documentElement.getAttribute('data-atlas-ext-click')")).toBe("yes");
    });
  });

  test("printing says so when it fails; closing the dialog is not a failure; the clear dialog warns at the point of choosing", async () => {
    await openChat();
    await openPage("/__test/page", "Atlas test page");
    await app.close();
    for (const [mode, text] of [["none", "No printer is available"], ["fail", "Printing failed"]]) {
      await withEnv({ ATLAS_APP_TEST_PRINT: mode }, async () => {
        await relaunch();
        await page.waitForSelector(".side-panel");
        await expect.poll(async () => (await viewInfo())?.url ?? "", { timeout: 20_000 }).toContain("/__test/page");
        await menu("Print");
        await page.waitForSelector(`.toast:has-text("${text}")`);
        await app.close();
      });
    }
    await withEnv({ ATLAS_APP_TEST_PRINT: "cancel" }, async () => {
      await relaunch();
      await page.waitForSelector(".side-panel");
      await expect.poll(async () => (await viewInfo())?.url ?? "", { timeout: 20_000 }).toContain("/__test/page");
      await menu("Print");
      await page.waitForTimeout(800);
      expect(await page.locator(".toast").count()).toBe(0);
    });
    await menu("Clear browsing data");
    await page.waitForSelector(".modal");
    const warn = page.locator('[data-testid="clear-whole-warning"]');
    const open = () => page.locator(".reveal").getAttribute("data-open");
    expect(await open()).toBe(null);
    await page.check('.modal .check-row:has-text("Cookies") input');
    await page.selectOption(".modal select", { index: 0 });
    await expect.poll(open).not.toBe(null);
    expect(await warn.innerText()).toContain("removes all of them");
    await page.selectOption(".modal select", { index: 4 });
    await expect.poll(open).toBe(null);
  });

  test("the address pill shows the host, the whole address only while it is edited; a picture is a real thumbnail", async () => {
    await openChat();
    await openPage("/__test/page", "Atlas test page");
    const input = page.locator('input[aria-label="Search or enter URL"]');
    expect(await input.inputValue()).toBe("127.0.0.1");
    await input.click();
    await expect.poll(() => input.inputValue()).toContain("/__test/page");
    await page.keyboard.press("Escape");
    await expect.poll(() => input.inputValue()).toBe("127.0.0.1");
    // The page sits flush: no inset card around it.
    expect(await page.evaluate(() => getComputedStyle(document.querySelector(".web-view")).margin)).toBe("0px");
  });
});

describe("importing cookies from another browser", () => {
  test("a fixture Firefox and Chrome profile import what may be read, and say what is app-bound", async () => {
    const { DatabaseSync } = await import("node:sqlite");
    const crypto = await import("node:crypto");
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "atlas-e2e-profiles-"));
    const roaming = path.join(root, "Roaming");
    const local = path.join(root, "Local");
    // Firefox.
    const ff = path.join(roaming, "Mozilla", "Firefox", "Profiles", "fix.default");
    fs.mkdirSync(ff, { recursive: true });
    fs.writeFileSync(path.join(roaming, "Mozilla", "Firefox", "profiles.ini"), "[Profile0]\nName=fixture-firefox\nIsRelative=1\nPath=Profiles/fix.default\n");
    const fdb = new DatabaseSync(path.join(ff, "cookies.sqlite"));
    fdb.exec("CREATE TABLE moz_cookies (name TEXT, value TEXT, host TEXT, path TEXT, expiry INTEGER, isSecure INTEGER, isHttpOnly INTEGER, sameSite INTEGER, originAttributes TEXT)");
    fdb.prepare("INSERT INTO moz_cookies VALUES (?,?,?,?,?,?,?,?,?)").run("ff_cookie", "fixture-1", "example.test", "/", 4102444800, 0, 0, 0, "");
    fdb.close();
    // Chrome, with one value the user's own key opens and one that is app-bound.
    const key = crypto.randomBytes(32);
    const chrome = path.join(local, "Google", "Chrome", "User Data");
    fs.mkdirSync(path.join(chrome, "Default", "Network"), { recursive: true });
    fs.writeFileSync(path.join(chrome, "Local State"), JSON.stringify({ os_crypt: { encrypted_key: Buffer.concat([Buffer.from("DPAPI"), Buffer.from("fake")]).toString("base64") }, profile: { info_cache: { Default: { name: "Fixture Person" } } } }));
    const nonce = crypto.randomBytes(12);
    const cipher = crypto.createCipheriv("aes-256-gcm", key, nonce);
    const sealed = Buffer.concat([Buffer.from("v10"), nonce, cipher.update("fixture-2", "utf8"), cipher.final(), cipher.getAuthTag()]);
    const cdb = new DatabaseSync(path.join(chrome, "Default", "Network", "Cookies"));
    cdb.exec("CREATE TABLE cookies (host_key TEXT, name TEXT, value TEXT, encrypted_value BLOB, path TEXT, expires_utc INTEGER, is_secure INTEGER, is_httponly INTEGER, samesite INTEGER)");
    const put = cdb.prepare("INSERT INTO cookies VALUES (?,?,?,?,?,?,?,?,?)");
    put.run("example.test", "chrome_cookie", "", sealed, "/", (4102444800 + 11_644_473_600) * 1_000_000, 0, 0, -1);
    put.run("example.test", "bound_cookie", "", Buffer.concat([Buffer.from("v20"), crypto.randomBytes(40)]), "/", 0, 1, 0, 0);
    cdb.close();

    await gateway.seed({ sessions: [session("s1", "First chat")], messages: { s1: [userMessage("u1", "s1", "Hello"), assistantMessage("a1", "s1", parts("Hi."))] } });
    const before = { r: process.env.ATLAS_APP_TEST_PROFILE_ROOT, k: process.env.ATLAS_APP_TEST_DPAPI_KEY };
    process.env.ATLAS_APP_TEST_PROFILE_ROOT = root;
    process.env.ATLAS_APP_TEST_DPAPI_KEY = key.toString("hex");
    try {
      await launch();
      await page.click(".session-main");
      await ready();
      await page.click('[aria-label="Toggle panel"]');
      await page.click('.side-panel [aria-label="More"]');
      await page.click('.menu-item:has-text("Import cookies and passwords")');
      await page.waitForSelector(".modal select");
      expect(await page.locator(".modal select option").allInnerTexts()).toEqual(["Firefox: fixture-firefox", "Google Chrome: Fixture Person"]);
      // Firefox first: plain text, nothing app-bound.
      await page.click('.modal .button:has-text("Import cookies")');
      await page.waitForSelector(".import-result");
      expect(await page.locator(".import-result").innerText()).toContain("Cookies imported: 1");
      expect(await page.locator(".import-result").innerText()).not.toContain("app-bound");
      // Then Chrome: one value opens with the user's own key, one is app-bound and is left alone.
      await page.selectOption(".modal select", { index: 1 });
      await page.click('.modal .button:has-text("Import cookies")');
      await expect.poll(() => page.locator(".import-result").innerText()).toContain("app-bound): 1");
      const chromeSummary = await page.locator(".import-result").innerText();
      expect(chromeSummary).toContain("Cookies imported: 1");
      expect(chromeSummary).toContain("Atlas does not break that protection");
      const names = await app.evaluate(async ({ session: s }) => (await s.fromPartition("persist:atlas-browser").cookies.get({})).map((c) => `${c.name}=${c.value}`).sort());
      expect(names).toEqual(["chrome_cookie=fixture-2", "ff_cookie=fixture-1"]);
    } finally {
      for (const [name, v] of [["ATLAS_APP_TEST_PROFILE_ROOT", before.r], ["ATLAS_APP_TEST_DPAPI_KEY", before.k]]) {
        if (v === undefined) delete process.env[name];
        else process.env[name] = v;
      }
    }
  });
});

describe("the review of a turn's changes", () => {
  const editCall = (id, file, before, after, add, del) => [
    { type: "tool_call", data: { id, name: "edit", input: JSON.stringify({ file_path: file }), finished: true } },
    { type: "tool_result", data: { tool_call_id: id, name: "edit", content: "ok", metadata: JSON.stringify({ additions: add, removals: del, old_content: before, new_content: after }), is_error: false } },
  ];
  const findings = [
    { severity: "low", title: "Unclear name", file: "src/app.ts", line: 2, explanation: "The name hides what it holds.", suggestion: "Rename it." },
    { severity: "high", title: "Empty input panics", file: "src/app.ts", line: 7, explanation: "An empty list indexes element 0.", suggestion: "Check the length first." },
  ];

  async function openEditedChat(review) {
    await gateway.seed({
      sessions: [session("s1", "Edited chat")],
      messages: {
        s1: [
          userMessage("u1", "s1", "Add the parser"),
          assistantMessage("a1", "s1", [...editCall("e1", "src/app.ts", "a\n", "a\nb\n", 1, 0), ...parts("Done.")]),
        ],
      },
      review,
    });
    await launch();
    await page.click(".session-main");
    await page.waitForSelector(".edit-card");
  }

  test("runs on the changed files and the task, then lists findings worst first", async () => {
    await openEditedChat({ polls: 2, findings });
    await page.click('[data-action="review"]');
    await page.waitForSelector(".review-state");
    expect(await page.locator(".review-state").innerText()).toContain("Reviewing");
    await page.waitForSelector(".finding");
    const severities = await page.locator(".finding").evaluateAll((els) => els.map((el) => el.getAttribute("data-severity")));
    expect(severities).toEqual(["high", "low"]);
    const { reviews } = await gateway.state();
    expect(reviews).toHaveLength(1);
    expect(reviews[0].paths).toEqual(["src/app.ts"]);
    expect(reviews[0].task).toBe("Add the parser");
    // The serious one opens by itself and shows how to fix it.
    await page.waitForSelector(".finding[data-severity=high] .finding-body");
    expect(await page.locator(".finding[data-severity=high] .finding-body").innerText()).toContain("Check the length first.");
    expect(await page.locator(".review-counts").innerText()).toMatch(/1 High/);
  });

  test("a fix goes to the agent as a request and is marked as sent", async () => {
    await openEditedChat({ polls: 0, findings });
    await page.click('[data-action="review"]');
    await page.waitForSelector(".finding");
    await page.click('.finding[data-severity=high] [data-action="fix"]');
    await page.waitForSelector(".finding[data-severity=high] .finding-sent");
    const { prompts } = await gateway.state();
    const sent = prompts.at(-1);
    expect(sent.sid).toBe("s1");
    expect(sent.prompt).toContain("[high] Empty input panics");
    expect(sent.prompt).toContain("src/app.ts:7");
    expect(sent.prompt).toContain("Check the length first.");
    expect(sent.prompt).not.toContain("Unclear name");
  });

  test("fix all hands the open findings over in one request, worst first", async () => {
    await openEditedChat({ polls: 0, findings });
    const before = (await gateway.state()).prompts.length;
    await page.click('[data-action="review"]');
    await page.waitForSelector(".finding");
    await page.click('[data-action="fix-all"]');
    await page.waitForSelector(".finding-sent");
    const sent = (await gateway.state()).prompts.slice(before);
    expect(sent).toHaveLength(1);
    expect(sent[0].prompt.indexOf("Empty input panics")).toBeLessThan(sent[0].prompt.indexOf("Unclear name"));
    expect(await page.locator(".finding-sent").count()).toBe(2);
    expect(await page.locator('[data-action="fix-all"]').count()).toBe(0);
  });

  test("a dismissed finding is hidden, counted, and stays dismissed after a reload", async () => {
    await openEditedChat({ polls: 0, findings });
    await page.click('[data-action="review"]');
    await page.waitForSelector(".finding");
    await page.click('.finding[data-severity=low] [data-action="dismiss"]');
    await page.waitForFunction(() => document.querySelectorAll(".finding").length === 1);
    expect(await page.locator(".review-foot").innerText()).toContain("1 dismissed");
    await page.reload();
    await page.click(".session-main");
    await page.waitForSelector(".edit-card");
    await page.waitForFunction(() => document.querySelectorAll(".finding").length === 1);
    await page.click(".review-foot .link-button");
    await page.waitForFunction(() => document.querySelectorAll(".finding").length === 2);
  });

  test("a review with nothing to report says so", async () => {
    await openEditedChat({ polls: 0, findings: [], summary: "Read the diff and the callers; nothing wrong." });
    await page.click('[data-action="review"]');
    await page.waitForSelector(".review-clean");
    expect(await page.locator(".review-summary").innerText()).toContain("nothing wrong");
  });

  test("a failed review shows why and can be tried again", async () => {
    await openEditedChat({ polls: 0, fail: "provider down" });
    await page.click('[data-action="review"]');
    await page.waitForSelector('.review[data-phase="failed"]');
    expect(await page.locator(".review").innerText()).toContain("provider down");
    await page.click(".review .edit-button");
    await page.waitForFunction(async () => (await fetch("/__test/state").then((r) => r.json())).reviews.length === 2).catch(() => {});
    expect((await gateway.state()).reviews).toHaveLength(2);
  });

  test("a server that cannot review says to restart it", async () => {
    await openEditedChat({ refuse: 501, message: "the running Atlas server is an older version" });
    await page.click('[data-action="review"]');
    await page.waitForSelector('.review[data-phase="failed"]');
    expect(await page.locator(".review").innerText()).toContain("Restart it");
  });

  test("a running review can be stopped", async () => {
    await openEditedChat({ polls: 1000 });
    await page.click('[data-action="review"]');
    await page.waitForSelector('[data-action="review-cancel"]');
    await page.click('[data-action="review-cancel"]');
    await page.waitForSelector('.review[data-phase="canceled"]');
    expect(await page.locator('[data-action="review"]').count()).toBe(1);
  });

  test("an answer that is not in the findings format is shown as written", async () => {
    await openEditedChat({ polls: 0, structured: false, summary: "I think the change is fine." });
    await page.click('[data-action="review"]');
    await page.waitForSelector(".review-note");
    expect(await page.locator(".review").innerText()).toContain("I think the change is fine.");
  });
});

describe("a sub-agent's work", () => {
  const call = (id, name, input) => ({ type: "tool_call", data: { id, name, input: JSON.stringify(input), finished: true } });
  const result = (id, name, content) => ({ type: "tool_result", data: { tool_call_id: id, name, content, metadata: "", is_error: false } });
  const CHILD = "a1$$c1";
  const delegated = [call("c1", "agent", { description: "Check the docs", prompt: "Look in the docs folder" })];

  const row = () => page.locator(".tool-row", { hasText: /Delegat/ });

  test("a finished sub-agent opens onto the steps it took and what it answered", async () => {
    await gateway.seed({
      sessions: [session("s1", "Delegating chat")],
      messages: {
        s1: [userMessage("u1", "s1", "Look it up"), assistantMessage("a1", "s1", [...delegated, result("c1", "agent", "The docs mention it."), ...parts("Done.")])],
        [CHILD]: [
          userMessage("cu1", CHILD, "Look in the docs folder"),
          assistantMessage("ca1", CHILD, [call("v1", "view", { file_path: "docs/intro.md" }), result("v1", "view", "intro text"), ...parts("The docs mention it.")]),
        ],
      },
    });
    await launch();
    await page.click(".session-main");
    await row().first().waitFor();
    // Before it is opened, the row already says how many steps it took.
    await page.waitForFunction(() => document.querySelector(".tool-hint")?.textContent === "1 step");
    expect(await row().first().getAttribute("aria-expanded")).toBe("false");
    await row().first().click();
    await page.waitForSelector(".subagent .tool");
    const inside = await page.locator(".subagent").innerText();
    expect(inside).toContain("intro.md");
    expect(inside).toContain("The docs mention it.");
    // Once its work is read, the row says how much there was.
    await page.waitForFunction(() => document.querySelector(".tool-hint")?.textContent === "1 step");
  });

  test("a finished sub-agent row says how long it took and what it changed", async () => {
    const metadata = JSON.stringify({ run: { duration_ms: 12_000, cost_usd: 0.01, files_changed: ["docs/intro.md"] } });
    await gateway.seed({
      sessions: [session("s1", "Delegating chat")],
      messages: {
        s1: [
          userMessage("u1", "s1", "Look it up"),
          assistantMessage("a1", "s1", [...delegated, { type: "tool_result", data: { tool_call_id: "c1", name: "agent", content: "Done.", metadata, is_error: false } }, ...parts("Done.")]),
        ],
        [CHILD]: [userMessage("cu1", CHILD, "Look in the docs folder"), assistantMessage("ca1", CHILD, [call("v1", "view", { file_path: "docs/intro.md" }), result("v1", "view", "intro text"), ...parts("Done.")])],
      },
    });
    await launch();
    await page.click(".session-main");
    await row().first().waitFor();
    await page.waitForFunction(() => document.querySelector(".tool-hint")?.textContent === "1 step · 12s · 1 file changed");
  });

  test("a running sub-agent is open, says what it is doing, and keeps up with it", async () => {
    await gateway.seed({
      sessions: [session("s1", "Delegating chat")],
      messages: {
        s1: [userMessage("u1", "s1", "Look it up"), assistantMessage("a1", "s1", delegated, false)],
        [CHILD]: [userMessage("cu1", CHILD, "Look in the docs folder"), assistantMessage("ca1", CHILD, [call("v1", "view", { file_path: "docs/intro.md" })], false)],
      },
    });
    await launch();
    await page.click(".session-main");
    await gateway.emit("session_status", { session_id: "s1", busy: true });
    await row().first().waitFor();
    expect(await row().first().getAttribute("aria-expanded")).toBe("true");
    await page.waitForFunction(() => document.querySelector(".tool-hint")?.textContent?.includes("intro.md"));

    await gateway.emit("message", {
      type: "updated",
      payload: assistantMessage("ca1", CHILD, [call("v1", "view", { file_path: "docs/intro.md" }), result("v1", "view", "intro text"), call("g1", "grep", { pattern: "needle" })], false),
    });
    await page.waitForFunction(() => document.querySelector(".tool-hint")?.textContent?.includes("needle"));

    await gateway.emit("message", {
      type: "updated",
      payload: assistantMessage("a1", "s1", [...delegated, result("c1", "agent", "Found.")]),
    });
    await gateway.emit("session_status", { session_id: "s1", busy: false });
    await page.waitForFunction(() => document.querySelector(".tool-hint")?.textContent === "2 steps");
    expect(await row().first().getAttribute("aria-expanded")).toBe("false");
  });

  test("a sub-agent that has just started says it is loading, not that nothing was kept", async () => {
    await gateway.seed({
      sessions: [session("s1", "Delegating chat")],
      messages: { s1: [userMessage("u1", "s1", "Look it up"), assistantMessage("a1", "s1", delegated, false)] },
    });
    await launch();
    await page.click(".session-main");
    await gateway.emit("session_status", { session_id: "s1", busy: true });
    await row().first().waitFor();
    await page.waitForSelector(".subagent-note");
    expect(await page.locator(".subagent-note").innerText()).toContain("Loading");
  });

  test("with nothing kept of its work the answer is what opens", async () => {
    await gateway.seed({
      sessions: [session("s1", "Delegating chat")],
      messages: { s1: [userMessage("u1", "s1", "Look it up"), assistantMessage("a1", "s1", [...delegated, result("c1", "agent", "Nothing to add."), ...parts("Done.")])] },
    });
    await launch();
    await page.click(".session-main");
    await row().first().click();
    await page.waitForFunction(() => document.querySelector(".tool-body")?.textContent?.includes("Nothing to add."));
    expect(await page.locator(".subagent").count()).toBe(0);
  });
});

describe("the design tool", () => {
  const toggle = '[aria-label="Toggle panel"]';

  const sampleDesign = () => ({
    format: "atlas-design",
    version: 1,
    name: "Landing",
    pages: [
      {
        id: "p1",
        name: "Page 1",
        background: "#F5F5F5",
        children: [
          {
            id: "desk",
            type: "frame",
            name: "Desktop",
            x: 0,
            y: 0,
            width: 800,
            height: 500,
            fills: [{ type: "solid", color: "#FFFFFF" }],
            clip: true,
            children: [
              { id: "title", type: "text", name: "Title", x: 40, y: 40, width: 0, height: 0, text: "Hello design", font: { family: "Inter", size: 48, weight: 700 }, auto_resize: "width", fills: [{ type: "solid", color: "#111111" }] },
              { id: "cta", type: "rect", name: "Button", x: 40, y: 160, width: 160, height: 48, radius: 12, fills: [{ type: "solid", color: "#5B5BF7" }] },
            ],
          },
        ],
      },
    ],
  });

  /** Opens the design tool in an expanded panel; with a seeded file it opens that file. */
  async function openDesign(designs = {}) {
    await gateway.seed({ sessions: [session("s1", "Design chat")], messages: { s1: [userMessage("u1", "s1", "Hello"), assistantMessage("a1", "s1", parts("Hi there."))] }, designs });
    await launch();
    await page.click(".session-main");
    await ready();
    await page.click(toggle);
    await page.click('.launch-row[data-tool="design"]');
    await page.waitForSelector(".design");
    await page.click('[aria-label="Expand panel"]');
    await page.waitForSelector(".body[data-expanded]");
    // The panel sweeps open under a clip; clicks land once it has.
    await page.waitForFunction(() => {
      const slot = document.querySelector(".panel-slot");
      return slot && !slot.style.clipPath && slot.getAnimations().length === 0;
    });
  }

  const canvasBox = async () => (await page.locator(".design-canvas").boundingBox());
  const saved = async (name) => (await gateway.design(name)).doc;
  const layerNames = () => page.locator(".design-layer .design-row-name").allInnerTexts();

  test("a new design is made from the start screen, drawn on, undone and saved", async () => {
    await openDesign();
    await page.waitForSelector(".design-start");
    await page.fill('[aria-label="Name of the new design"]', "My Logo");
    await page.click('.design-new button[type="submit"]');
    await page.waitForSelector(".design-canvas[data-ready]");
    expect(await page.locator(".design-file").inputValue()).toBe("my-logo");
    await expect.poll(async () => (await saved("my-logo"))?.name).toBe("My Logo");

    // R draws a rectangle by dragging.
    await page.locator(".design-canvas").click({ position: { x: 5, y: 5 } });
    await page.keyboard.press("r");
    expect(await page.locator('.design-tool[data-tool="rect"]').getAttribute("aria-pressed")).toBe("true");
    const box = await canvasBox();
    await page.mouse.move(box.x + 100, box.y + 100);
    await page.mouse.down();
    await page.mouse.move(box.x + 180, box.y + 150, { steps: 5 });
    await page.mouse.move(box.x + 260, box.y + 200, { steps: 5 });
    await page.mouse.up();
    expect(await layerNames()).toEqual(["Rectangle 1"]);
    expect(await page.locator('.design-tool[data-tool="select"]').getAttribute("aria-pressed")).toBe("true", "a shape tool hands back to Move");
    await page.waitForSelector(".design-selection");
    await expect.poll(async () => (await saved("my-logo")).pages[0].children.map((n) => [n.type, n.width, n.height])).toEqual([["rect", 160, 100]]);

    // The inspector edits the selection.
    const width = page.locator('.design-right input[aria-label="Width"]');
    expect(await width.inputValue()).toBe("160");
    await width.fill("240");
    await width.press("Enter");
    await expect.poll(async () => (await saved("my-logo")).pages[0].children[0].width).toBe(240);

    // Undo goes back one step at a time; redo comes back.
    await page.locator(".design-canvas").click({ position: { x: 5, y: 5 } });
    await page.keyboard.press("Control+z");
    await expect.poll(async () => (await saved("my-logo")).pages[0].children[0].width).toBe(160);
    await page.keyboard.press("Control+z");
    await expect.poll(async () => (await saved("my-logo")).pages[0].children.length).toBe(0);
    await page.keyboard.press("Control+Shift+z");
    await expect.poll(async () => (await saved("my-logo")).pages[0].children.length).toBe(1);
    await expect.poll(() => page.locator(".design-status").innerText()).toBe("Saved");
  });

  test("selection, moves, grouping and layers act on the document", async () => {
    await openDesign({ landing: sampleDesign() });
    await page.waitForSelector('.design-canvas[data-ready] [data-node="cta"]');
    expect(await layerNames()).toEqual(["Desktop", "Button", "Title"]);

    // A click inside a top-level frame selects the layer in it.
    const cta = await page.locator('.design-canvas [data-node="cta"]').boundingBox();
    await page.mouse.click(cta.x + cta.width / 2, cta.y + cta.height / 2);
    expect(await page.locator('.design-layer[aria-selected="true"] .design-row-name').innerText()).toBe("Button");
    await page.keyboard.press("Shift+ArrowRight");
    await page.keyboard.press("ArrowDown");
    await expect.poll(async () => {
      const doc = await saved("landing");
      const btn = doc.pages[0].children[0].children.find((n) => n.id === "cta");
      return [btn.x, btn.y];
    }).toEqual([50, 161]);

    // Dragging moves it, in page units, as one undo step. At 100% the snap reach is small.
    await page.keyboard.press("Control+0");
    await expect.poll(() => page.locator(".design-zoom-value").innerText()).toBe("100%");
    const zoom = 1;
    const at = await page.locator('.design-canvas [data-node="cta"]').boundingBox();
    const cx = at.x + at.width / 2;
    const cy = at.y + at.height / 2;
    await page.mouse.move(cx, cy);
    await page.mouse.down();
    await page.mouse.move(cx + 50 * zoom, cy + 100 * zoom, { steps: 8 });
    await page.mouse.up();
    await expect.poll(async () => {
      const doc = await saved("landing");
      return doc.pages[0].children[0].children.find((n) => n.id === "cta").y;
    }).toBeGreaterThan(240);

    // Shift-click adds to the selection; Ctrl+G groups.
    const title = await page.locator('.design-canvas [data-node="title"]').boundingBox();
    await page.keyboard.down("Shift");
    await page.mouse.click(title.x + 10, title.y + 10);
    await page.keyboard.up("Shift");
    expect(await page.locator('.design-layer[aria-selected="true"]').count()).toBe(2);
    await page.keyboard.press("Control+g");
    await expect.poll(layerNames).toEqual(["Desktop", "Group 1", "Button", "Title"]);
    await expect.poll(async () => (await saved("landing")).pages[0].children[0].children.map((n) => n.type)).toEqual(["group"]);

    // The eye hides a layer; Delete removes the selection.
    await page.click('.design-layer:has-text("Group 1") [aria-label="Hide: Group 1"]');
    await expect.poll(async () => (await saved("landing")).pages[0].children[0].children[0].visible).toBe(false);
    await page.keyboard.press("Delete");
    await expect.poll(async () => (await saved("landing")).pages[0].children[0].children.length).toBe(0);
  });

  test("the agent's edits to the file show up live, and its tool call opens the tool", async () => {
    await openDesign({ landing: sampleDesign() });
    await page.waitForSelector('.design-canvas[data-ready] [data-node="cta"]');
    const next = sampleDesign();
    next.pages[0].children[0].children.push({ id: "badge", type: "ellipse", name: "Badge", x: 400, y: 40, width: 80, height: 80, fills: [{ type: "solid", color: "#FF5C00" }] });
    await gateway.writeDesign("landing", next);
    await page.waitForSelector('.design-canvas [data-node="badge"]');
    expect(await layerNames()).toEqual(["Desktop", "Badge", "Button", "Title"]);
    // What changed is highlighted for a moment.
    await page.waitForSelector(".design-flash");
    // The agent's edit is one undo away.
    await page.locator(".design-canvas").click({ position: { x: 5, y: 5 } });
    await page.keyboard.press("Control+z");
    await expect.poll(layerNames).toEqual(["Desktop", "Button", "Title"]);

    // A design write in a running chat brings the design tool forward on its file.
    await gateway.writeDesign("logo", { ...sampleDesign(), name: "Logo" });
    await page.click('[aria-label="Restore panel size"]');
    await page.click('.side-panel [aria-label="New tab"]');
    await page.waitForSelector(".newtab");
    await gateway.emit("session_status", { session_id: "s1", busy: true });
    await gateway.emit("message", { type: "created", payload: assistantMessage("a2", "s1", [{ type: "tool_call", data: { id: "d1", name: "design_canvas", input: JSON.stringify({ action: "apply", file: "logo", ops: [] }), finished: false } }], false) });
    await page.waitForSelector(".design");
    await expect.poll(() => page.locator(".design-file").inputValue()).toBe("logo");
    await expect.poll(async () => (await page.locator(".thread").innerText()).includes("Designing")).toBe(true);
    await gateway.emit("session_status", { session_id: "s1", busy: false });
  });

  test("exports the selection or the page to PNG, SVG and PDF", async () => {
    const out = fs.mkdtempSync(path.join(os.tmpdir(), "atlas-e2e-export-"));
    process.env.ATLAS_APP_TEST_SAVE_DIR = out;
    try {
      await openDesign({ landing: sampleDesign() });
      await page.waitForSelector('.design-canvas[data-ready] [data-node="cta"]');
      // Nothing selected: the page.
      await page.selectOption('.design-right [aria-label="Export format"]', "svg");
      await page.click('.design-right .design-button:has-text("Export page")');
      await expect.poll(() => fs.existsSync(path.join(out, "Landing - Page 1.svg"))).toBe(true);
      const svg = fs.readFileSync(path.join(out, "Landing - Page 1.svg"), "utf8");
      expect(svg).toContain("Hello design");
      expect(svg).toContain("@font-face");
      expect(svg).toContain('viewBox="0 0 800 500"');

      const cta = await page.locator('.design-canvas [data-node="cta"]').boundingBox();
      await page.mouse.click(cta.x + 10, cta.y + 10);
      await page.selectOption('.design-right [aria-label="Export format"]', "png");
      await page.selectOption('.design-right [aria-label="Export scale"]', "2");
      await page.click('.design-right .design-button:has-text("Export selection")');
      await expect.poll(() => fs.existsSync(path.join(out, "Button@2x.png"))).toBe(true);
      const png = fs.readFileSync(path.join(out, "Button@2x.png"));
      expect(png.subarray(1, 4).toString()).toBe("PNG");
      // 160 x 48 at 2x.
      expect(png.readUInt32BE(16)).toBe(320);
      expect(png.readUInt32BE(20)).toBe(96);

      await page.selectOption('.design-right [aria-label="Export format"]', "pdf");
      await page.click('.design-right .design-button:has-text("Export selection")');
      await expect.poll(() => fs.existsSync(path.join(out, "Button.pdf")), { timeout: 20_000 }).toBe(true);
      expect(fs.readFileSync(path.join(out, "Button.pdf")).subarray(0, 5).toString()).toBe("%PDF-");
    } finally {
      delete process.env.ATLAS_APP_TEST_SAVE_DIR;
      fs.rmSync(out, { recursive: true, force: true });
    }
  });

  test("the design tool passes an accessibility check", async () => {
    await openDesign({ landing: sampleDesign() });
    await page.waitForSelector('.design-canvas[data-ready] [data-node="cta"]');
    await page.evaluate(axeSource);
    const result = await page.evaluate(() => window.axe.run(document.querySelector(".design"), { runOnly: ["wcag2a", "wcag2aa"], rules: { "color-contrast": { enabled: false } } }));
    expect(result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`)).toEqual([]);
  });
  test("auto layout stacks children, and a comment is pinned and saved", async () => {
    await openDesign({ landing: sampleDesign() });
    await page.waitForSelector('.design-canvas[data-ready] [data-node="cta"]');
    await page.locator('.design-layer:has(.design-row-name:text-is("Desktop"))').click();
    await page.click('[aria-label="Add auto layout"]');
    await expect
      .poll(async () => {
        const doc = await saved("landing");
        const kids = doc.pages[0].children[0].children;
        return [doc.pages[0].children[0].layout?.dir, kids.map((k) => k.x), kids[1].y > kids[0].y && kids[1].y < 160];
      })
      .toEqual(["column", [0, 0], true]);

    // A comment: C, click the canvas, write, Enter.
    await page.locator(".design-canvas").click({ position: { x: 5, y: 5 } });
    await page.keyboard.press("c");
    const box = await canvasBox();
    await page.mouse.click(box.x + box.width - 60, box.y + 60);
    await page.fill('[aria-label="Write a comment…"]', "Make the title bigger");
    await page.press('[aria-label="Write a comment…"]', "Enter");
    await expect.poll(async () => (await saved("landing")).comments?.map((c) => [c.text, c.author])).toEqual([["Make the title bigger", "person"]]);
    expect(await page.locator(".design-pin").count()).toBe(1);
    await page.click('button:has-text("Resolve")');
    await expect.poll(async () => (await saved("landing")).comments[0].resolved).toBe(true);
  });

  test("shapes combine into a boolean group", async () => {
    const two = {
      format: "atlas-design", version: 1, name: "Shapes",
      pages: [{ id: "p1", name: "Page 1", background: "#F5F5F5", children: [
        { id: "a", type: "ellipse", name: "A", x: 100, y: 100, width: 120, height: 120, fills: [{ type: "solid", color: "#FF0000" }] },
        { id: "b", type: "ellipse", name: "B", x: 160, y: 100, width: 120, height: 120, fills: [{ type: "solid", color: "#0000FF" }] },
      ] }],
    };
    await openDesign({ shapes: two });
    await page.waitForSelector('.design-canvas[data-ready] [data-node="a"]');
    await page.locator(".design-canvas").click({ position: { x: 5, y: 5 } });
    await page.keyboard.press("Control+a");
    await page.click('.design-right button:has-text("Subtract")');
    await expect
      .poll(async () => {
        const kids = (await saved("shapes")).pages[0].children;
        return [kids.length, kids[0].type, kids[0].bool_op, kids[0].children?.length];
      })
      .toEqual([1, "group", "subtract", 2]);
    expect(await page.locator('.design-canvas mask').count()).toBeGreaterThan(0);
  });

  test("a prototype plays: a click moves to the next frame and Escape leaves", async () => {
    const flow = {
      format: "atlas-design", version: 1, name: "Flow",
      pages: [{ id: "p1", name: "Page 1", background: "#F5F5F5", children: [
        { id: "home", type: "frame", name: "Home", x: 0, y: 0, width: 300, height: 400, flow_start: true, fills: [{ type: "solid", color: "#FFFFFF" }], children: [
          { id: "go", type: "rect", name: "Go", x: 20, y: 20, width: 200, height: 60, fills: [{ type: "solid", color: "#0055FF" }], interactions: [{ trigger: "click", action: "navigate", to: "detail", transition: "dissolve", duration: 50 }] },
        ] },
        { id: "detail", type: "frame", name: "Detail", x: 400, y: 0, width: 300, height: 400, fills: [{ type: "solid", color: "#EEEEEE" }], children: [] },
      ] }],
    };
    await openDesign({ flow });
    await page.waitForSelector('.design-canvas[data-ready] [data-node="go"]');
    await page.click('button[aria-label="Present"]');
    await page.waitForSelector(".design-present[data-ready]");
    expect(await page.locator(".design-present-bar .design-muted").innerText()).toBe("Home");
    await page.locator('.design-present [data-node="go"]').click();
    await expect.poll(() => page.locator(".design-present-bar .design-muted").innerText()).toBe("Detail");
    await page.click('.design-present button[aria-label="Back"]');
    await expect.poll(() => page.locator(".design-present-bar .design-muted").innerText()).toBe("Home");
    await escapeUntilGone(".design-present");
  });

  test("the swarm dialog sends a brief that sets one page and one agent per variant", async () => {
    const flow = { format: "atlas-design", version: 1, name: "Shop", pages: [{ id: "p1", name: "Page 1", background: "#F5F5F5", children: [] }] };
    await openDesign({ flow });
    await page.waitForSelector(".design-canvas[data-ready]");
    const before = (await gateway.state()).prompts.length;
    await page.click('button[aria-label="Swarm"]');
    await page.waitForSelector(".design-swarm");
    expect(await page.locator(".design-swarm button[data-on]").isDisabled()).toBe(true);
    await page.fill(".design-swarm textarea", "A landing page for a coffee shop");
    await page.fill('.design-swarm input[type="number"]', "9");
    expect(await page.inputValue('.design-swarm input[type="number"]')).toBe("4");
    await page.fill('.design-swarm input[type="text"]', "designer-a designer-b");
    await page.click(".design-swarm button[data-on]");
    await page.waitForSelector(".design-swarm", { state: "detached" });
    await expect.poll(async () => (await gateway.state()).prompts.length).toBe(before + 1);
    const sent = JSON.stringify((await gateway.state()).prompts.at(-1));
    expect(sent).toContain("A landing page for a coffee shop");
    expect(sent).toContain("Variant D");
    expect(sent).toContain("run_in_background: true");
    expect(sent).toContain("designer-a, designer-b");
  });

  test("the library switches the theme, places a block where it belongs and adds icons", async () => {
    const flow = { format: "atlas-design", version: 1, name: "Shop", pages: [{ id: "p1", name: "Page 1", background: "#F5F5F5", children: [] }] };
    await openDesign({ flow });
    await page.waitForSelector(".design-canvas[data-ready]");
    await page.click('button[aria-label="Library"]');
    await page.waitForSelector(".design-library");
    // A design without a theme opens on the themes.
    await page.waitForSelector('.design-theme[aria-label="Zinc"]');
    await page.click('.design-theme[aria-label="iOS"]');
    await expect.poll(async () => (await saved("flow"))?.theme?.id).toBe("ios");
    await page.click('.design-accent[aria-label="grass"]');
    await expect.poll(async () => (await saved("flow"))?.theme).toEqual({ id: "ios", accent: "grass" });

    // Blocks open on the theme's platform and land selected.
    await page.click('.design-library-tabs [role="tab"]:has-text("Blocks")');
    await page.waitForSelector('.design-block[aria-label="Grouped list"]');
    expect(await page.locator('.design-block[aria-label="Hero"]').count()).toBe(0);
    await page.click('.design-block[aria-label="Grouped list"]');
    await expect.poll(async () => (await saved("flow"))?.pages[0].children.map((n) => n.id)).toEqual(["list"]);
    await expect.poll(layerNames).toContain("Grouped list");
    expect(await page.locator(".design-layer[aria-selected=true] .design-row-name").allInnerTexts()).toEqual(["Grouped list"]);

    // Icons search and go into the selected frame.
    await page.click('.design-library-tabs [role="tab"]:has-text("Icons")');
    await page.fill(".design-library-search", "hea");
    await page.waitForSelector('.design-icon[aria-label="heart"]');
    expect(await page.locator('.design-icon[aria-label="house"]').count()).toBe(0);
    await page.click('.design-icon[aria-label="heart"]');
    await expect.poll(async () => (await gateway.state()).kitOps.at(-1)).toMatchObject({ op: "add", parent: "list", node: { icon: "heart" } });

    // The icon is saved with the list; Escape closes the library.
    await expect.poll(async () => (await saved("flow"))?.pages[0].children.length).toBe(2);
    await page.locator(".design-library").press("Escape");
    await page.waitForSelector(".design-library", { state: "detached" });
  });


});

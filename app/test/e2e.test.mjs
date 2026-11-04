// End-to-end tests: the real Electron shell and renderer against the fake
// gateway. Run with `npm run test:e2e` after `npm run build`.

import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { _electron as electron } from "playwright-core";
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, test } from "vitest";
import { assistantMessage, session, startFakeGateway, userMessage } from "./fake-gateway.mjs";

const appDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const electronBin = path.join(appDir, "node_modules", "electron", "dist", fs.readFileSync(path.join(appDir, "node_modules", "electron", "path.txt"), "utf8").trim());
const axeSource = fs.readFileSync(path.join(appDir, "node_modules", "axe-core", "axe.min.js"), "utf8");

let gateway;
let app;
let page;
let userData;

async function launch(prefs = { ui: { onboarded: true, lang: "en" } }) {
  userData = fs.mkdtempSync(path.join(os.tmpdir(), "atlas-e2e-"));
  fs.writeFileSync(path.join(userData, "prefs.json"), JSON.stringify(prefs));
  app = await electron.launch({
    executablePath: electronBin,
    args: [appDir, `--project=${path.join(os.tmpdir(), "atlas-e2e-project")}`],
    env: { ...process.env, ATLAS_APP_DEV_GATEWAY: gateway.origin, ATLAS_APP_USER_DATA: userData },
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
    await page.keyboard.press("Escape");
    await page.waitForSelector(".settings", { state: "detached" });
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
    expect(await page.evaluate(() => document.getAnimations().filter((a) => a.playState === "running" && a.effect?.getTiming().iterations === Infinity).length)).toBe(0);
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
    expect(nodes).toBeLessThan(1500);

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
    await page.keyboard.press("Escape");
    await page.waitForSelector(".settings", { state: "detached" });

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
    await page.keyboard.press("Escape");
    await page.waitForSelector("text=Press keys…", { state: "detached" });
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
    await page.keyboard.press("Escape");
    await page.waitForSelector(".user-edit", { state: "detached" });
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

// Screenshots of the side panel, the browser and the thread's summary popover
// in every state, dark and light: `node test/panel-shots.mjs <folder>`, after
// `npm run build`. Runs against the fake gateway and its test pages, with its
// own profile and downloads folder; nothing touches the person's own data.

import { spawnSync } from "node:child_process";
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
const electronBin = electronExecutable();
const gateway = await startFakeGateway({ renderer: path.join(appDir, "renderer", "dist") });

const text = (t) => [{ type: "text", data: { text: t } }];
const call = (id, name, input, meta = "") => [
  { type: "tool_call", data: { id, name, input: JSON.stringify(input), finished: true } },
  { type: "tool_result", data: { tool_call_id: id, name, content: "ok", metadata: meta, is_error: false } },
];
const edit = (id, file, oldText, newText, add, del) => call(id, "edit", { file_path: file }, JSON.stringify({ additions: add, removals: del, old_content: oldText, new_content: newText }));
const png = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAKAAAABkCAIAAACO1KzYAAABB0lEQVR4nO3RQQ2AQAADwV6CGDShDqfgYzPzqIDuuZ9329lnV/zh2hlhAscJHCdwnMBxAscJHCdwnMBxAscJHCdwnMBxAscJHCdwnMBxAscJHCdwnMBxAscJHCdwnMBxAscJHCdwnMBxAscJHCdwnMBxAscJHCdwnMBxAscJHCdwnMBxAscJHCdwnMBxAscJHCdwnMBxAscJHCdwnMBxAscJHCdwnMBxAscJHCdwnMBxAscJHCdwnMBxAscJHCdwnMBxAscJHCdwnMBxAscJHCdwnMBxAscJHCdwnMBxAscJHCdwnMBxAscJHCdwnMBxAscJHCdwnMBxAscJHCdwnMBxAscJvLYfy8tSE8PKYy0AAAAASUVORK5CYII=";

const seed = () =>
  gateway.seed({
    sessions: [session("s1", "GitHub Actions'ı incele")],
    messages: {
      s1: [
        { ...userMessage("u1", "s1", "Dosyalara bak"), parts: [{ type: "text", data: { text: "Dosyalara bak" } }, { type: "image_url", data: { url: png } }] },
        assistantMessage("a1", "s1", [
          ...call("v1", "view", { file_path: "app/main/gateway.cjs" }),
          ...edit("e1", "app/main/launch-queue.cjs", "const a = 1;\nconst b = 2;\n", "const a = 1;\nconst b = 3;\nconst c = 4;\n", 2, 1),
          ...edit("e2", "app/renderer/src/app.tsx", "x\n", "x\ny\nz\n", 2, 0),
          ...call("f1", "fetch", { url: "https://github.com/github/gh-aw/issues/52187" }),
          ...call("f2", "fetch", { url: "https://docs.github.com/en/copilot/reference" }),
          ...call("w1", "web_search", { query: "atlas side panel" }),
          ...call("t1", "agent", { description: "one" }),
          ...call("t2", "agent", { description: "two" }),
          ...text("Düzelttim."),
        ]),
      ],
    },
  });

const downloads = fs.mkdtempSync(path.join(os.tmpdir(), "atlas-shots-dl-"));
const project = path.join(os.tmpdir(), "atlas-e2e-project");
fs.mkdirSync(project, { recursive: true });

async function run(theme) {
  await seed();
  const userData = fs.mkdtempSync(path.join(os.tmpdir(), "atlas-panel-shots-"));
  fs.writeFileSync(path.join(userData, "prefs.json"), JSON.stringify({ theme, ui: { lang: "tr", onboarded: true } }));
  const env = { ...process.env, ATLAS_APP_DEV_GATEWAY: gateway.origin, ATLAS_APP_USER_DATA: userData, ATLAS_APP_DOWNLOADS: downloads, ATLAS_APP_TEST_NO_PROFILES: "1", ATLAS_APP_TEST_EXTENSION_DIR: path.join(appDir, "test", "fixtures", "extension") };
  const app = await electron.launch({ executablePath: electronBin, args: [appDir, `--project=${project}`], env });
  const page = await app.firstWindow();
  await page.waitForSelector(".composer textarea");
  await page.click(".session-main");
  await page.waitForSelector(".turn");
  // Native views are invisible to page screenshots, so the screen is captured.
  await app.evaluate(({ BrowserWindow }) => {
    const win = BrowserWindow.getAllWindows()[0];
    win.setContentSize(1600, 900);
    win.center();
    win.setAlwaysOnTop(true, "screen-saver");
    win.show();
    win.moveTop();
    win.focus();
  });
  await page.waitForTimeout(800);
  const shot = async (name) => {
    await page.waitForTimeout(600);
    const box = await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].getBounds());
    // Take the front back if something else has it; never capture another app's window.
    let front = false;
    for (let i = 0; i < 8 && !front; i++) {
      front = await app.evaluate(({ BrowserWindow }) => {
        const win = BrowserWindow.getAllWindows()[0];
        if (!win.isFocused()) {
          win.setAlwaysOnTop(true, "screen-saver");
          win.show();
          win.moveTop();
          win.focus();
        }
        return win.isFocused();
      });
      if (!front) await page.waitForTimeout(400);
    }
    if (!front) throw new Error("The window is not in front, so the capture would show something else");
    const file = path.join(out, `${theme}-${name}.png`);
    spawnSync("powershell", ["-NoProfile", "-Command", `Add-Type -AssemblyName System.Drawing; $b = New-Object System.Drawing.Bitmap(${box.width}, ${box.height}); $g = [System.Drawing.Graphics]::FromImage($b); $g.CopyFromScreen(${box.x}, ${box.y}, 0, 0, $b.Size); $b.Save('${file}')`]);
  };
  const go = async (p) => {
    await page.fill('input[aria-label="Adres çubuğu"], input[placeholder="Ara veya URL gir"]', `${gateway.origin}${p}`);
    await page.keyboard.press("Enter");
  };
  const menu = async (...names) => {
    await page.waitForSelector(".web-menu", { state: "detached" });
    await page.click('.side-panel [aria-label="Daha fazla"]');
    await page.waitForSelector(".web-menu");
    for (const n of names.slice(0, -1)) {
      await page.locator(".menu-item", { hasText: n }).first().hover();
      await page.waitForTimeout(250);
    }
    // The last name is matched whole: "Parolalar" must not pick the import item that mentions passwords.
    if (names.length) {
      const exact = page.locator(".menu-item").filter({ has: page.getByText(names.at(-1), { exact: true }) });
      await ((await exact.count()) > 0 ? exact : page.locator(".menu-item", { hasText: names.at(-1) })).last().click();
    }
  };

  await shot("00-thread");
  await page.click('.thread-head [aria-label="Yan paneli aç"]');
  await page.waitForSelector(".newtab");
  await shot("01-new-tab");
  await page.click('.launch-row[data-tool="changes"]');
  await page.waitForSelector(".work-tree .tree-row");
  await shot("02-changes");
  await page.click('.side-panel [aria-label="Yeni sekme"]');
  await page.click('.launch-row[data-tool="files"]');
  await page.waitForSelector(".work-tree .tree-row");
  await shot("03-files");
  await page.click('.side-panel [aria-label="Yeni sekme"]');
  await page.click('.launch-row[data-tool="terminal"]');
  await page.waitForSelector(".xterm");
  await page.waitForTimeout(1500);
  await page.keyboard.type("echo atlas-terminal");
  await page.keyboard.press("Enter");
  await shot("04-terminal");
  await page.click('.side-panel [aria-label="Yeni sekme"]');
  await page.click('.launch-row[data-tool="side"]');
  await page.waitForSelector(".side-chat .composer");
  await shot("05-side-chat");
  await page.click('.side-panel [aria-label="Yeni sekme"]');
  await go("/__test/page");
  await page.waitForSelector('.ptab-main:has-text("Atlas test page")');
  await page.waitForTimeout(1000);
  await shot("06-web-page");
  await page.click('button[aria-label="Sohbet özeti"]');
  await page.waitForSelector(".summary-pop");
  await shot("07-popover");
  await page.click('.summary-pop [aria-label="Proje menüsü"]');
  await page.waitForSelector(".menu");
  await shot("08-popover-menu");
  await page.keyboard.press("Escape");
  await page.waitForSelector(".menu", { state: "detached" });
  await page.keyboard.press("Escape");
  await page.waitForSelector(".summary-pop", { state: "detached" });
  await menu();
  await shot("09-browser-menu");
  await page.keyboard.press("Escape");
  await page.waitForSelector(".web-menu", { state: "detached" });
  await go("/__test/find");
  await page.waitForSelector('.ptab-main:has-text("Find page")');
  await menu("Sayfada bul");
  await page.fill(".find-bar input", "needle");
  await shot("10-find");
  await page.keyboard.press("Escape");
  await go("/__test/page");
  await page.waitForSelector('.ptab-main:has-text("Atlas test page")');
  await page.click('[aria-label="Not ekle"]');
  await page.waitForSelector(".note-pop");
  await page.fill(".note-pop .note-input", "Bu başlığı kontrol et");
  await shot("11-note");
  await page.keyboard.press("Escape");
  await go("/__test/file");
  await page.waitForSelector('.download-row[data-state="completed"]');
  await shot("12-downloads");
  await page.keyboard.press("Escape");
  await page.click('button[aria-label="Uzantılar"]');
  await page.waitForSelector(".ext-pop");
  await shot("13-extensions-popup");
  await page.click('.ext-pop .summary-row:has-text("Uzantıları yönet")');
  await page.click('.data-head .button[data-variant="primary"]');
  await page.waitForSelector(".modal .permission-list");
  await shot("14-extension-confirm");
  await page.click('.modal .button[data-variant="primary"]');
  await page.waitForSelector(".ext-card");
  await shot("15-extensions-view");
  await page.click('.side-panel [aria-label="Yeni sekme"]');
  await go("/__test/second");
  await page.waitForSelector('.ptab-main:has-text("Second page")');
  await menu("Geçmiş");
  await page.waitForSelector(".data-row");
  await shot("16-history");
  await page.click('.side-panel [aria-label="Yeni sekme"]');
  await go("/__test/login");
  await page.waitForSelector('.ptab-main:has-text("Login page")');
  await page.waitForSelector(".web-snapshot", { state: "detached" });
  await page.waitForTimeout(1500);
  await app.evaluate(async ({ webContents }) => {
    const view = webContents.getAllWebContents().find((w) => w.getURL().includes("/__test/login"));
    await view.executeJavaScript("document.getElementById('user').focus()");
    await new Promise((r) => setTimeout(r, 200));
    for (const ch of "ada") view.sendInputEvent({ type: "char", keyCode: ch });
    await new Promise((r) => setTimeout(r, 200));
    await view.executeJavaScript("document.getElementById('pass').focus()");
    await new Promise((r) => setTimeout(r, 200));
    for (const ch of "fixture-pw") view.sendInputEvent({ type: "char", keyCode: ch });
    await new Promise((r) => setTimeout(r, 200));
    const b = JSON.parse(await view.executeJavaScript("JSON.stringify(document.getElementById('go').getBoundingClientRect())"));
    view.sendInputEvent({ type: "mouseDown", x: Math.round(b.x + 10), y: Math.round(b.y + 10), button: "left", clickCount: 1 });
    view.sendInputEvent({ type: "mouseUp", x: Math.round(b.x + 10), y: Math.round(b.y + 10), button: "left", clickCount: 1 });
  });
  await page.waitForSelector(".login-bar");
  await shot("17-login-offer");
  await page.click('.login-bar .button[data-variant="primary"]');
  await page.waitForSelector(".login-bar", { state: "detached" });
  await page.waitForTimeout(600);
  await menu("Şifreler ve otomatik doldurma", "Parolalar");
  await page.waitForSelector(".password-row");
  await shot("18-passwords");
  await page.click(`.ptab-main:has-text("Logged in")`);
  await menu("Göz atma verilerini temizle");
  await page.waitForSelector(".modal");
  await shot("19-clear-dialog");
  await page.keyboard.press("Escape");
  await menu("Çerezleri ve parolaları içe aktar");
  await page.waitForSelector(".modal");
  await shot("20-import-dialog");
  await page.keyboard.press("Escape");
  await page.waitForSelector(".modal", { state: "detached" });
  await go("/__test/size");
  await page.waitForSelector('.ptab-main:has-text("Size page")');
  await menu("Cihaz araç çubuğu", "iPhone 15");
  await page.waitForTimeout(800);
  await shot("21-device");
  await menu("Cihaz araç çubuğu", "Cihaz modunu kapat");
  await page.click('[aria-label="Paneli genişlet"]');
  await shot("22-expanded");
  await page.click('[aria-label="Panel boyutunu geri al"]');
  const edge = await page.locator(".panel-handle").boundingBox();
  await page.mouse.move(edge.x + 4, edge.y + 300);
  await page.mouse.down();
  await page.mouse.move(0, edge.y + 300, { steps: 6 });
  await page.mouse.up();
  await shot("23-resized-wide");
  await page.click(".side-panel .ptab-add");
  await menu("Tarayıcı ayarları");
  await page.waitForSelector(".settings-view");
  await shot("24-browser-settings");
  await page.click(".side-panel .ptab-add");
  await menu("Şifreler ve otomatik doldurma", "Adresler");
  await page.waitForSelector(".data-view");
  await shot("25-addresses");
  await app.close();
}

await run("dark");
await run("light");
await gateway.close();
console.log(`Saved to ${out}`);

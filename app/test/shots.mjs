// Takes screenshots of the screens worth a human look, against the fake
// gateway: `node test/shots.mjs <folder>`, after `npm run build`.

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
const turns = Array.from({ length: 14 }, (_, i) => [userMessage(`u${i}`, "s1", `Soru ${i + 1}: bunu nasıl düzeltiriz, bir bakalım`), assistantMessage(`a${i}`, "s1", text(`Cevap ${i + 1}. ${"Sorun şurada ve çözüm de bu şekilde olacak. ".repeat(6)}`))]).flat();
await gateway.seed({ sessions: [session("s1", "Örnek sohbet", { prompt_tokens: 116300, completion_tokens: 2100, cost: 0.008 })], messages: { s1: turns } });
await gateway.modes(["refactor", "research", "review", "security", "template-builder", "test", "visual-qa"].map((name) => ({ name, description: "Reviews changed or specified code for correctness, security and maintainability." })));

async function run(theme, shots) {
  const userData = fs.mkdtempSync(path.join(os.tmpdir(), "atlas-shots-"));
  fs.writeFileSync(path.join(userData, "prefs.json"), JSON.stringify({ theme, ui: { lang: "tr", onboarded: shots !== "onboarding" } }));
  const project = path.join(os.tmpdir(), "atlas-e2e-project");
  fs.mkdirSync(project, { recursive: true });
  const app = await electron.launch({ executablePath: electronBin, args: [appDir, `--project=${project}`], env: { ...process.env, ATLAS_APP_DEV_GATEWAY: gateway.origin, ATLAS_APP_USER_DATA: userData } });
  const page = await app.firstWindow();
  await page.waitForLoadState();
  const shot = async (name) => {
    await page.waitForTimeout(350);
    await page.screenshot({ path: path.join(out, `${theme}-${name}.png`) });
  };
  const settings = async (tab) => {
    await page.keyboard.press("Control+,");
    await page.click(`.settings-tab:has-text("${tab}")`);
  };

  if (shots === "onboarding") {
    await page.waitForSelector(".onboarding");
    await shot("onboarding-1");
    await page.click("text=Başla");
    await page.waitForSelector(".provider-tile");
    await shot("onboarding-2");
    await page.click(".onboarding-foot .button:last-child");
    await shot("onboarding-3");
  } else {
    await page.waitForSelector(".composer textarea");
    await page.click('[title="Ajan modu"]');
    await shot("modes");
    await page.keyboard.press("Escape");
    await page.click(".session-main");
    await page.waitForSelector(".tick");
    await page.locator(".tick").nth(5).hover();
    await shot("rail");
    await page.hover(".context-ring");
    await shot("context-hover");
    await page.click(".context-ring");
    await shot("context-panel");
    await page.keyboard.press("Escape");
    await settings("Modeller");
    await shot("providers");
    await page.click(".picker-button");
    await shot("picker");
    await page.keyboard.press("Escape");
    await page.locator('[data-provider="kimi-code"] .button').click();
    await page.waitForSelector(".login-code");
    await shot("login");
    await page.click('.login .button:has-text("Vazgeç")');
    await page.click('.settings-tab:has-text("Klavye")');
    await shot("shortcuts");
  }
  await Promise.race([app.close(), new Promise((r) => setTimeout(r, 6000))]);
}

await run("dark", "app");
await run("light", "app");
await run("dark", "onboarding");
await gateway.close();
process.exit(0);

// Visual review of the real Electron composer against the deterministic gateway.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { _electron as electron } from "playwright-core";
import { electronExecutable } from "./electron-bin.mjs";
import { assistantMessage, session, startFakeGateway, userMessage } from "./fake-gateway.mjs";

const appDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const out = path.resolve(process.argv[2] ?? "composer-shots");
fs.mkdirSync(out, { recursive: true });
const gateway = await startFakeGateway({ renderer: path.join(appDir, "renderer", "dist") });
await gateway.seed({
  sessions: [session("s1", "Atlas masaüstü deneyimi")],
  messages: { s1: [userMessage("u1", "s1", "Bu projeyi birlikte geliştirelim."), assistantMessage("a1", "s1", [{ type: "text", data: { text: "Hazırım. Bir dosya ekleyebilir, **/** ile bir beceri seçebilir veya sesle yazabilirsin.\n\nÖnce ilgili kodu inceleyip değişikliği test edeceğim." } }])] },
  commands: [
    { id: "skill:review", name: "code-review", kind: "skill", description: "Değişiklikleri doğruluk, güvenlik ve bakım açısından incele" },
    { id: "skill:commit", name: "commit", kind: "skill", description: "Mevcut değişiklikler için açıklayıcı bir commit hazırla" },
    { id: "skill:debug", name: "debug", kind: "skill", description: "Sorunu yeniden üret, kök nedenini bul ve düzelt" },
    { id: "skill:plan", name: "plan", kind: "skill", description: "Uygulanabilir bir geliştirme planı hazırla" },
    { id: "skill:test", name: "test", kind: "skill", description: "İlgili testleri çalıştır ve sonuçları doğrula" },
  ],
  mcp: [{ name: "Proje dokümanları", state: "connected", enabled: true, tools: 8 }, { name: "Tarayıcı araçları", state: "connected", enabled: true, tools: 12 }, { name: "Gmail", state: "needs_auth", enabled: true, error: "Oturum açılması gerekiyor" }],
});
try {
  for (const theme of ["dark", "light"]) {
    const userData = fs.mkdtempSync(path.join(os.tmpdir(), "atlas-composer-shots-"));
    fs.writeFileSync(path.join(userData, "prefs.json"), JSON.stringify({ theme, ui: { lang: "tr", onboarded: true } }));
    const project = path.join(os.tmpdir(), "atlas-e2e-project");
    fs.mkdirSync(project, { recursive: true });
    const app = await electron.launch({ executablePath: electronExecutable(), args: [appDir, `--project=${project}`, "--use-fake-device-for-media-stream", "--use-fake-ui-for-media-stream"], env: { ...process.env, ATLAS_APP_DEV_GATEWAY: gateway.origin, ATLAS_APP_USER_DATA: userData } });
    try {
      const page = await app.firstWindow();
      await page.setViewportSize({ width: 1280, height: 800 });
      await page.waitForSelector(".composer textarea");
      await page.click(".session-main");
      const shot = async (name) => { await page.mouse.move(0, 0); await page.waitForTimeout(350); await page.screenshot({ path: path.join(out, `${theme}-${name}.png`) }); };
      if (process.env.ATLAS_SHOTS_ONLY === "slash") {
        await page.fill(".composer textarea", "/");
        await page.waitForSelector(".suggest-row");
        await shot("slash");
        continue;
      }
      await shot("composer");
      await page.click(".composer-add"); await shot("plus");
      await page.click('.context-menu-row:has-text("MCP")'); await shot("mcp");
      await page.keyboard.press("Escape");
      await page.fill(".composer textarea", "/"); await shot("slash");
      await page.keyboard.press("Escape"); await page.fill(".composer textarea", "");
      await page.click(".model-trigger"); await shot("models"); await page.keyboard.press("Escape");
      await page.click(".voice-trigger"); await page.waitForSelector('.voice-capture[data-phase="recording"]');
      await page.waitForTimeout(1200); await shot("recording"); await page.click('[aria-label="Kaydı iptal et"]');
      await page.setViewportSize({ width: 760, height: 520 }); await shot("small");
      await page.click(".composer-add"); await shot("small-plus");
      await page.keyboard.press("Escape");
      for (const width of [1280, 760]) {
        await page.setViewportSize({ width, height: width === 760 ? 520 : 800 });
        for (const mode of ["plan", "debug", "multi", "ask"]) {
          await page.click(".composer-add");
          await page.click(`.context-menu-row[data-mode="${mode}"]`);
          await shot(`mode-${mode}-${width}`);
          await page.click('[aria-label="Seçili modu kaldır"]');
        }
      }
    } finally { await app.close(); }
  }
} finally { await gateway.close(); }
console.log(`Saved composer screenshots to ${out}`);

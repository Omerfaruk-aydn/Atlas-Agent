const fs = require("node:fs/promises");
const path = require("node:path");
const pw = require("playwright");
const { fixture, themeQuestion } = require("./fixture.cjs");

const out = process.argv[2] || "screens";
const engine = process.argv[3] || "webkit";

const sizes = [
  { name: "390", viewport: { width: 390, height: 844 } },
  { name: "360", viewport: { width: 360, height: 800 } },
  { name: "430", viewport: { width: 430, height: 932 } },
  { name: "320", viewport: { width: 320, height: 640 } },
];

// t mimics the node:test context the fixture expects.
function ctx() {
  const after = [];
  return { after: (fn) => after.push(fn), run: async () => { for (const fn of after.reverse()) await fn(); } };
}

async function shot(browser, name, options, drive) {
  const t = ctx();
  const { page, state, url } = await fixture(browser, t, options);
  await drive(page, url, state);
  await page.waitForTimeout(250);
  const file = path.join(out, name + ".png");
  await page.screenshot({ path: file });
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
  if (overflow) console.log("OVERFLOW", name);
  await t.run();
  console.log("saved", file);
}

const screens = {
  "01-pair": async (page, url) => {
    await page.goto(url + "/#pair=fixture-only");
    await page.locator("#pair-name").fill("Telefonum");
    await page.locator("#pair-go").click();
    await page.locator("#pair-wait").waitFor();
  },
  "01b-pair-name": async (page, url) => { await page.goto(url + "/#pair=fixture-only"); await page.locator("#pair-go").waitFor(); },
  "01c-pair-noqr": async (page, url) => { await page.goto(url); await page.locator("#pair-none").waitFor(); },
  "02-chat": async (page, url) => {
    await page.goto(url);
    await page.locator('.task-row[data-id="s1"]').click();
    await page.locator("#messages .msg").first().waitFor();
  },
  // The on-screen keyboard is emulated by shrinking the viewport by 45%.
  "02k-chat-keyboard": async (page, url) => {
    await page.goto(url);
    await page.locator('.task-row[data-id="s1"]').click();
    await page.locator("#messages .msg").first().waitFor();
    await page.locator("#prompt").fill("Mobil düzeni kontrol et");
    const v = page.viewportSize();
    await page.setViewportSize({ width: v.width, height: Math.round(v.height * 0.55) });
  },
  "03-permission": async (page, url) => {
    await page.goto(url);
    await page.locator('.task-row[data-id="s2"]').click();
    await page.locator("#pending-pane button[data-decision]").first().waitFor();
  },
  "04-list": async (page, url) => { await page.goto(url); await page.locator(".task-row").first().waitFor(); },
  "05-question": async (page, url) => {
    await page.goto(url);
    await page.locator('.task-row[data-id="s1"]').click();
    await page.locator("#pending-pane form").waitFor();
  },
  "06-offline": async (page, url, state) => {
    await page.goto(url);
    await page.locator('.task-row[data-id="s1"]').click();
    await page.locator("#messages .msg").first().waitFor();
    state.unavailable = true;
    await page.evaluate(() => window.testStreams[window.testStreams.length - 1].fail());
    await page.locator("#offline-banner").waitFor();
  },
  "07-settings": async (page, url) => {
    await page.goto(url);
    await page.locator("#settings-btn").click();
    await page.locator("#set-device").waitFor();
  },
};

(async () => {
  await fs.mkdir(out, { recursive: true });
  const browser = engine === "webkit" ? await pw.webkit.launch() : await pw.chromium.launch({ channel: process.env.ATLAS_CHROMIUM_CHANNEL || undefined });
  const only = process.env.ONLY ? process.env.ONLY.split(",") : null;
  const sizeList = process.env.SIZES ? sizes.filter((s) => process.env.SIZES.split(",").includes(s.name)) : sizes.slice(0, 1);
  const schemes = process.env.SCHEMES ? process.env.SCHEMES.split(",") : ["light"];
  try {
    for (const size of sizeList) {
      for (const scheme of schemes) {
        for (const [name, drive] of Object.entries(screens)) {
          if (only && !only.some((o) => name.startsWith(o))) continue;
          const options = { viewport: size.viewport, colorScheme: scheme };
          if (name.startsWith("01")) options.setup = (s) => { s.authenticated = false; };
          if (name === "05-question") options.setup = (s) => { s.details.s1.questions = [themeQuestion()]; };
          await shot(browser, `${name}-${size.name}-${scheme}`, options, drive);
        }
      }
    }
  } finally {
    await browser.close();
  }
})().catch((e) => { console.error(e); process.exit(1); });

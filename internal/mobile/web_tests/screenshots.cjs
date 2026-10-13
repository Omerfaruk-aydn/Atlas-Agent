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

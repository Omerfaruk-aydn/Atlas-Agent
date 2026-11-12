// Measures the same strings Go's TextWidth measures, with Chromium's
// canvas and the same @font-face rules the app loads. Prints a JSON array.
//
//   node measure.mjs samples.json out.json
//
// samples.json is [{id, family, cssFamily, weight, size, letterSpacing, text}].

import fs from "node:fs";
import http from "node:http";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, "../../../..");
const require = createRequire(path.join(root, "app/package.json"));
const { chromium } = require("playwright-core");

const fontsDir = path.join(root, "app/node_modules");
const [samplesPath, outPath] = process.argv.slice(2);

const sheets = [
  "@fontsource-variable/inter/index.css",
  "@fontsource-variable/geist/index.css",
  "@fontsource-variable/figtree/index.css",
  "@fontsource-variable/outfit/index.css",
  "@fontsource-variable/dm-sans/index.css",
  "@fontsource-variable/albert-sans/index.css",
  "@fontsource-variable/fraunces/index.css",
  "@fontsource-variable/fustat/index.css",
  "@fontsource-variable/montserrat/index.css",
  "@fontsource-variable/roboto/index.css",
  "@fontsource-variable/jetbrains-mono/index.css",
  "@fontsource-variable/playwrite-us-trad/index.css",
  "@fontsource/poppins/400.css",
  "@fontsource/poppins/500.css",
  "@fontsource/poppins/600.css",
  "@fontsource/poppins/700.css",
];

const links = sheets.map((s) => `<link rel="stylesheet" href="/${s}">`).join("");
const samples = JSON.parse(fs.readFileSync(samplesPath, "utf8"));

const server = http.createServer((req, res) => {
  const url = decodeURIComponent(req.url.split("?")[0]);
  if (url === "/") {
    res.setHeader("content-type", "text/html; charset=utf-8");
    res.end(`<!doctype html>${links}<canvas id="c"></canvas>`);
    return;
  }
  const file = path.normalize(path.join(fontsDir, url.slice(1)));
  if (!file.startsWith(fontsDir) || !fs.existsSync(file) || fs.statSync(file).isDirectory()) {
    res.writeHead(404).end();
    return;
  }
  res.end(fs.readFileSync(file));
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));

async function launch() {
  let last;
  for (const channel of ["msedge", "chrome"]) {
    try {
      return await chromium.launch({ channel, headless: true });
    } catch (err) {
      last = err;
    }
  }
  throw last;
}

const browser = await launch();
const page = await browser.newPage();
const failed = [];
page.on("response", (r) => {
  if (r.status() >= 400 && r.url().includes("font")) failed.push(r.url());
});
await page.goto(`http://127.0.0.1:${server.address().port}/`);
const widths = await page.evaluate(async (items) => {
  const c = document.getElementById("c").getContext("2d");
  const out = [];
  for (const s of items) {
    const font = `${s.weight} ${s.size}px "${s.cssFamily}"`;
    await document.fonts.load(font);
    c.font = font;
    const spacing = s.letterSpacing || 0;
    out.push({ id: s.id, width: c.measureText(s.text).width + [...s.text].length * spacing, loaded: document.fonts.check(font) });
  }
  return out;
}, samples);
await browser.close();
server.close();
if (failed.length) {
  console.error(failed.slice(0, 5).join("\n"));
  process.exit(1);
}
fs.writeFileSync(outPath, JSON.stringify(widths));
const missed = widths.filter((w) => !w.loaded);
if (missed.length) {
  console.error(`${missed.length} fonts did not load`);
  process.exit(1);
}

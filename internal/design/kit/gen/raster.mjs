// Rasterises one SVG to a PNG with the same fonts the app loads.
//   node raster.mjs <svg> <png> <fonts dir or ""> <playwright-core dir>
// One SVG per page: several SVGs in one HTML collide on ids.

import fs from "node:fs";
import http from "node:http";
import path from "node:path";
import { createRequire } from "node:module";

const [svgPath, pngPath, fontsArg, pwDir] = process.argv.slice(2);
const fontsDir = fontsArg && fontsArg !== "-" ? fontsArg : "";
const require = createRequire(path.join(pwDir, "package.json"));
const { chromium } = require("playwright-core");

const svg = fs.readFileSync(svgPath, "utf8");
const width = Number(/<svg\b[^>]*\bwidth="([\d.]+)"/.exec(svg)?.[1] ?? 0);
const height = Number(/<svg\b[^>]*\bheight="([\d.]+)"/.exec(svg)?.[1] ?? 0);
if (!(width > 0) || !(height > 0)) {
  console.error("svg has no width and height");
  process.exit(1);
}

const sheets = fontsDir
  ? [
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
    ].filter((s) => fs.existsSync(path.join(fontsDir, s)))
  : [];
const links = sheets.map((s) => `<link rel="stylesheet" href="/${s}">`).join("");

const server = http.createServer((req, res) => {
  const url = decodeURIComponent(req.url.split("?")[0]);
  if (url === "/") {
    res.setHeader("content-type", "text/html; charset=utf-8");
    res.end(`<!doctype html><style>html,body{margin:0}svg{display:block}</style>${links}${svg}`);
    return;
  }
  if (!fontsDir) {
    res.writeHead(404).end();
    return;
  }
  const file = path.normalize(path.join(fontsDir, url.slice(1)));
  const root = path.normalize(fontsDir + path.sep);
  if (!file.startsWith(root) || !fs.existsSync(file) || fs.statSync(file).isDirectory()) {
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
const page = await browser.newPage({ viewport: { width: Math.min(1600, Math.ceil(width)), height: Math.min(1200, Math.ceil(height)) }, deviceScaleFactor: 1 });
await page.goto(`http://127.0.0.1:${server.address().port}/`);
await page.evaluate(() => document.fonts.ready);
await page.locator("svg").screenshot({ path: pngPath });
await browser.close();
server.close();

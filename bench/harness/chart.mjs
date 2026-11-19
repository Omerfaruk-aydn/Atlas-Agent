// Turns a harness benchmark result into README material: a light and a dark
// SVG chart and a Markdown table. The numbers come straight from the result
// file, so the picture cannot drift from the data.
//
//   node bench/harness/chart.mjs bench/harness/results/<stamp>.json docs/assets

import fs from "node:fs";
import path from "node:path";

const [file, outDir = "docs/assets"] = process.argv.slice(2);
const data = JSON.parse(fs.readFileSync(file, "utf8"));
fs.mkdirSync(outDir, { recursive: true });

const median = (xs) => {
  const s = [...xs].sort((a, b) => a - b);
  return s.length ? (s.length % 2 ? s[(s.length - 1) / 2] : (s[s.length / 2 - 1] + s[s.length / 2]) / 2) : NaN;
};

const errors = data.runs.filter((r) => r.error);
if (errors.length) console.error(`${errors.length} setup errors left out; fix the harness login or model and rerun them.`);
const rows = Object.entries(Object.groupBy(data.runs.filter((r) => !r.error), (r) => r.harness)).map(([name, runs]) => {
  const h = data.config.harnesses[name];
  const tokens = runs.map((r) => r.tokens).filter((t) => t > 0);
  return {
    name,
    label: h.label,
    model: h.model,
    atlas: name.startsWith("atlas"),
    runs: runs.length,
    passed: runs.filter((r) => r.pass).length,
    rate: runs.filter((r) => r.pass).length / runs.length,
    seconds: median(runs.map((r) => r.seconds)),
    tokens: tokens.length ? median(tokens) : null,
  };
});
// Group by model so harnesses on the same model sit together, best first.
rows.sort((a, b) => a.model.localeCompare(b.model) || b.rate - a.rate);

const allAtlas = rows.every((r) => r.atlas);
const pct = (x) => `${(x * 100).toFixed(1)}%`;
const tasks = new Set(data.runs.map((r) => r.task)).size;
const repeats = Math.max(...data.runs.map((r) => r.repeat)) + 1;

const md = [
  `| ${"Harness"} | Model | Pass rate | Passed | Median time |`,
  `| --- | --- | ---: | ---: | ---: |`,
  ...rows.map((r) => `| ${r.atlas ? `**${r.label}**` : r.label} | ${r.model} | ${r.atlas ? `**${pct(r.rate)}**` : pct(r.rate)} | ${r.passed}/${r.runs} | ${Math.round(r.seconds)}s |`),
  "",
  `${tasks} tasks × ${repeats} run${repeats > 1 ? "s" : ""} each, hidden checks, ${data.started.slice(0, 10)}. Reproduce with \`node bench/harness/run.mjs\`.`,
].join("\n");
fs.writeFileSync(path.join(outDir, "harness-bench.md"), md + "\n");

function svg(theme) {
  const c = theme === "dark"
    ? { bg: "#0d1117", fg: "#e6edf3", dim: "#8b949e", track: "#21262d", bar: "#3d444d", hi: "#f0883e", grid: "#30363d" }
    : { bg: "#ffffff", fg: "#1f2328", dim: "#59636e", track: "#eff2f5", bar: "#c8d1da", hi: "#d4570f", grid: "#d1d9e0" };
  const W = 960, rowH = 46, top = 92, left = 280, barW = 440;
  const H = top + rows.length * rowH + 56;
  let groupY = top;
  const out = [];
  out.push(`<svg xmlns="http://www.w3.org/2000/svg" width="${W}" height="${H}" viewBox="0 0 ${W} ${H}" font-family="Inter, -apple-system, Segoe UI, Helvetica, Arial, sans-serif">`);
  out.push(`<rect width="${W}" height="${H}" rx="14" fill="${c.bg}"/>`);
  out.push(`<text x="32" y="44" font-size="22" font-weight="700" fill="${c.fg}">${allAtlas ? "One harness, any model" : "Same model, same tasks, different harness"}</text>`);
  out.push(`<text x="32" y="70" font-size="14" fill="${c.dim}">Pass rate on ${tasks} bench tasks with hidden checks · ${repeats} run${repeats > 1 ? "s" : ""} each · ${data.started.slice(0, 10)}</text>`);
  for (let g = 0; g <= 4; g++) {
    const x = left + (barW * g) / 4;
    out.push(`<line x1="${x}" y1="${top - 8}" x2="${x}" y2="${top + rows.length * rowH - 6}" stroke="${c.grid}" stroke-dasharray="3 4"/>`);
    out.push(`<text x="${x}" y="${top + rows.length * rowH + 14}" font-size="12" fill="${c.dim}" text-anchor="middle">${g * 25}%</text>`);
  }
  rows.forEach((r, i) => {
    const y = top + i * rowH;
    if (i > 0 && rows[i - 1].model !== r.model) out.push(`<line x1="32" y1="${y - 4}" x2="${W - 32}" y2="${y - 4}" stroke="${c.grid}"/>`);
    out.push(`<text x="32" y="${y + 20}" font-size="15" font-weight="${r.atlas ? 700 : 500}" fill="${c.fg}">${allAtlas ? r.model : r.label}</text>`);
    out.push(`<text x="32" y="${y + 37}" font-size="12" fill="${c.dim}">${allAtlas ? "via Atlas Agent" : r.model}</text>`);
    out.push(`<rect x="${left}" y="${y + 8}" width="${barW}" height="22" rx="6" fill="${c.track}"/>`);
    out.push(`<rect x="${left}" y="${y + 8}" width="${Math.max(4, barW * r.rate)}" height="22" rx="6" fill="${r.atlas ? c.hi : c.bar}"/>`);
    out.push(`<text x="${left + barW + 14}" y="${y + 24}" font-size="15" font-weight="700" fill="${r.atlas ? c.hi : c.fg}">${pct(r.rate)}</text>`);
    out.push(`<text x="${W - 32}" y="${y + 24}" font-size="13" fill="${c.dim}" text-anchor="end">${Math.round(r.seconds)}s median</text>`);
    groupY = y;
  });
  out.push(`<text x="32" y="${H - 18}" font-size="12" fill="${c.dim}">github.com/Omerfaruk-aydn/Atlas-Agent · bench/harness</text>`);
  out.push(`</svg>`);
  return out.join("\n");
}
for (const theme of ["light", "dark"]) fs.writeFileSync(path.join(outDir, `harness-bench-${theme}.svg`), svg(theme));
console.log(md);

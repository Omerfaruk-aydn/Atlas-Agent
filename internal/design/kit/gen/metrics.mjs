// Builds kit/metrics.bin.gz: per-glyph advances and pair adjustments for
// the typefaces the themes use. The app measures with Chromium; Go uses
// these tables so the two agree. Reads the woff2 files already installed
// under app/node_modules (@fontsource). fontkit is a dev tool:
//   npm install fontkit --no-save --prefix app
//
// Pair adjustments come from fontkit's layout of two characters minus the
// two singles, so kerning and the common ligatures are both included.

import fs from "node:fs";
import path from "node:path";
import zlib from "node:zlib";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, "../../../..");
const require = createRequire(path.join(root, "app/package.json"));
const fontkit = require("fontkit");
const wawoff2 = require("wawoff2");

const fontsDir = path.join(root, "app/node_modules");
const outPath = path.join(here, "..", "metrics.bin.gz");

const WEIGHTS = [400, 500, 600, 700];

// Figtree's subset carries an HVAR table Chromium does not apply, so the
// app draws every weight from the default instance (plus a synthetic bold
// Edge adds, under 1%). Measuring the real axis would disagree with the
// canvas. The other variable fonts do vary, and match the axis.
const chromeStatic = new Set(["Figtree"]);

// Family name -> files. "latin" covers U+0000-00FF; "ext" fills the rest.
const FAMILIES = [
  ["Inter", "inter", "@fontsource-variable/inter"],
  ["Geist", "geist", "@fontsource-variable/geist"],
  ["Figtree", "figtree", "@fontsource-variable/figtree"],
  ["Outfit", "outfit", "@fontsource-variable/outfit"],
  ["DM Sans", "dm-sans", "@fontsource-variable/dm-sans"],
  ["Albert Sans", "albert-sans", "@fontsource-variable/albert-sans"],
  ["Fraunces", "fraunces", "@fontsource-variable/fraunces"],
  ["Fustat", "fustat", "@fontsource-variable/fustat"],
  ["Montserrat", "montserrat", "@fontsource-variable/montserrat"],
  ["Roboto", "roboto", "@fontsource-variable/roboto"],
  ["JetBrains Mono", "jetbrains-mono", "@fontsource-variable/jetbrains-mono"],
  ["Playwrite US Trad", "playwrite-us-trad", "@fontsource-variable/playwrite-us-trad"],
  ["Poppins", "poppins", "@fontsource/poppins"],
];

function codepoints() {
  const cps = [];
  for (let cp = 0x20; cp <= 0x17f; cp++) cps.push(cp);
  for (const cp of [0x2013, 0x2014, 0x2018, 0x2019, 0x201c, 0x201d, 0x2022, 0x2026, 0x20ac]) cps.push(cp);
  return cps;
}

const CPS = codepoints();

// Pairs worth storing: letters, digits and the punctuation that kerns.
function isPairChar(cp) {
  if (cp === 0x20 || cp === 0x2c || cp === 0x2e || cp === 0x27 || cp === 0x22 || cp === 0x2d || cp === 0x2f || cp === 0x26) return true;
  if (cp >= 0x30 && cp <= 0x39) return true;
  if (cp >= 0x41 && cp <= 0x5a) return true;
  if (cp >= 0x61 && cp <= 0x7a) return true;
  return cp === 0x2013 || cp === 0x2014 || cp === 0x2019;
}

function pairChars(advances) {
  const out = [];
  for (const cp of CPS) if (advances.has(cp) && isPairChar(cp)) out.push(cp);
  return out;
}

const decoded = new Map();

// fontkit's getVariation re-reads the file bytes, which fails on woff2, so
// decompress to a TrueType buffer first.
async function openFace(file, weight) {
  let ttf = decoded.get(file);
  if (!ttf) {
    ttf = Buffer.from(await wawoff2.decompress(fs.readFileSync(file)));
    decoded.set(file, ttf);
  }
  const font = fontkit.create(ttf);
  if (weight == null || !font.variationAxes || !font.variationAxes.wght) return font;
  const axes = { wght: weight };
  if (font.variationAxes.opsz) axes.opsz = font.variationAxes.opsz.default;
  return font.getVariation(axes);
}

function filesFor(slug, pkg, weight) {
  const dir = path.join(fontsDir, pkg, "files");
  if (slug === "poppins") {
    return [path.join(dir, `poppins-latin-${weight}-normal.woff2`), path.join(dir, `poppins-latin-ext-${weight}-normal.woff2`)];
  }
  if (slug === "playwrite-us-trad") {
    return [path.join(dir, "playwrite-us-trad-fallback-wght-normal.woff2")];
  }
  return [path.join(dir, `${slug}-latin-wght-normal.woff2`), path.join(dir, `${slug}-latin-ext-wght-normal.woff2`)];
}

function layoutWidth(font, text) {
  try {
    return font.layout(text).advanceWidth;
  } catch {
    return 0;
  }
}

async function instance(paths, weight) {
  const faces = [];
  for (const p of paths) if (fs.existsSync(p)) faces.push(await openFace(p, weight));
  if (faces.length === 0) throw new Error(`no font file for weight ${weight}: ${paths[0]}`);
  const advances = new Map();
  for (const cp of CPS) {
    for (const face of faces) {
      const g = face.glyphForCodePoint(cp);
      if (!g || g.id === 0) continue;
      advances.set(cp, Math.round(g.advanceWidth));
      break;
    }
  }
  // Pair adjustments from the primary face (latin). Ligatures land here too.
  const primary = faces[0];
  const chars = pairChars(advances);
  const pairs = [];
  for (const a of chars) {
    for (const b of chars) {
      const single = (advances.get(a) ?? 0) + (advances.get(b) ?? 0);
      const both = Math.round(layoutWidth(primary, String.fromCodePoint(a) + String.fromCodePoint(b)));
      const delta = both - single;
      if (delta !== 0 && delta > -2000 && delta < 2000) pairs.push([a, b, delta]);
    }
  }
  return { upem: primary.unitsPerEm, advances, pairs };
}

function u16(n) {
  const b = Buffer.alloc(2);
  b.writeUInt16LE(n & 0xffff, 0);
  return b;
}
function i16(n) {
  const b = Buffer.alloc(2);
  b.writeInt16LE(n, 0);
  return b;
}

const chunks = [Buffer.from("ATLM"), Buffer.from([1])];
chunks.push(u16(FAMILIES.length));

for (const [name, slug, pkg] of FAMILIES) {
  const nameBuf = Buffer.from(name, "utf8");
  chunks.push(Buffer.from([nameBuf.length]), nameBuf);
  const weights = [];
  for (const w of WEIGHTS) {
    const paths = filesFor(slug, pkg, w);
    process.stderr.write(`${name} ${w}\n`);
    weights.push({ w, ...(await instance(paths, chromeStatic.has(name) ? null : w)) });
  }
  chunks.push(Buffer.from([weights.length]));
  for (const inst of weights) {
    chunks.push(u16(inst.w), u16(inst.upem), u16(inst.advances.size));
    for (const [cp, adv] of inst.advances) chunks.push(u16(cp), u16(Math.max(0, Math.min(65535, adv))));
    if (inst.pairs.length > 65535) throw new Error(`${name} has too many pairs`);
    chunks.push(u16(inst.pairs.length));
    for (const [a, b, d] of inst.pairs) chunks.push(u16(a), u16(b), i16(Math.max(-32768, Math.min(32767, d))));
  }
}

const raw = Buffer.concat(chunks);
const gz = zlib.gzipSync(raw, { level: 9 });
fs.writeFileSync(outPath, gz);
process.stderr.write(`wrote ${outPath} (${gz.length} bytes, raw ${raw.length})\n`);

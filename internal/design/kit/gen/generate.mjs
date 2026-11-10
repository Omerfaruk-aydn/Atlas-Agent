// Builds the design kit's data files (themes.json and icons.json.gz in the
// parent folder) from the open source libraries they come from:
//
//   - Astryx themes (MIT, github.com/meta/astryx): run astryx-extract.cjs on
//     the theme sources first; it writes the tokens as JSON.
//   - Radix Colors (MIT, @radix-ui/colors): the 12-step light and dark scales.
//   - Lucide (ISC, lucide-static): icon-nodes.json and tags.json.
//
// The iOS, Material 3 and zinc themes are written out here, from Apple's
// Human Interface Guidelines system colours, the Material 3 baseline scheme
// and shadcn/ui's zinc palette.
//
// Usage:
//   node generate.mjs --astryx astryx.json --radix <@radix-ui/colors dir> \
//     --lucide <lucide-static dir>

import fs from "node:fs";
import path from "node:path";
import zlib from "node:zlib";
import { fileURLToPath } from "node:url";

const args = Object.fromEntries(process.argv.slice(2).reduce((acc, v, i, all) => (v.startsWith("--") ? [...acc, [v.slice(2), all[i + 1]]] : acc), []));
const out = path.join(path.dirname(fileURLToPath(import.meta.url)), "..");

// ---------------------------------------------------------------- colours

const hex = (v) => {
  if (typeof v !== "string" || !/^#([0-9a-f]{3,4}|[0-9a-f]{6}|[0-9a-f]{8})$/i.test(v)) return null;
  let h = v.slice(1);
  if (h.length <= 4) h = [...h].map((c) => c + c).join("");
  return "#" + h.toUpperCase();
};
const pair = (v, fallback) => {
  if (Array.isArray(v)) {
    const l = hex(v[0]);
    const d = hex(v[1]);
    if (l && d) return [l, d];
  } else if (hex(v)) return [hex(v), hex(v)];
  return fallback;
};

// Radix scales: name -> {light: [12], dark: [12]}.
function radixScales(dir) {
  const scales = {};
  for (const file of fs.readdirSync(dir)) {
    const m = /^([a-z]+)(-dark)?\.css$/.exec(file);
    if (!m) continue;
    const css = fs.readFileSync(path.join(dir, file), "utf8").split("@supports")[0];
    const steps = [...css.matchAll(/--[a-z]+-(\d+):\s*(#[0-9a-fA-F]{6})/g)].map((x) => hex(x[2]));
    if (steps.length !== 12) continue;
    scales[m[1]] ??= {};
    scales[m[1]][m[2] ? "dark" : "light"] = steps;
  }
  return Object.fromEntries(Object.entries(scales).filter(([, s]) => s.light && s.dark));
}

// ------------------------------------------------------------- type scale

const SLOTS = ["display", "h1", "h2", "h3", "h4", "lead", "body", "small", "caption", "label"];

/** A modular type scale on a 16px body that keeps the theme's display size. */
function modular(base, ratio, heading, body, headingWeight = 600, displayWeight = Math.min(900, headingWeight + 100)) {
  const display = base * ratio ** 6;
  const r = (display / 16) ** (1 / 6);
  // Large sizes snap to the 4px grid, small ones to even pixels.
  const snap = (v) => (v >= 24 ? Math.round(v / 4) * 4 : Math.round(v / 2) * 2);
  const size = (k) => snap(16 * r ** k);
  const lh = (s, tight) => Math.round(s * (tight ? 1.15 : 1.5) / 2) * 2;
  const t = (family, s, weight, tight, spacing) => ({ family, size: s, weight, line_height: lh(s, tight), ...(spacing ? { letter_spacing: spacing } : {}) });
  return {
    display: t(heading, Math.max(56, size(6) + 8), displayWeight, true, -Math.round(Math.max(56, size(6) + 8) * 0.025 * 10) / 10),
    h1: t(heading, size(5), headingWeight, true, -Math.round(size(5) * 0.02 * 10) / 10),
    h2: t(heading, size(4), headingWeight, true, -Math.round(size(4) * 0.015 * 10) / 10),
    h3: t(heading, size(3), headingWeight, true),
    h4: t(heading, size(2), headingWeight, true),
    lead: t(body, Math.max(18, size(1)), 400, false),
    body: t(body, 16, 400, false),
    small: t(body, 14, 400, false),
    caption: t(body, 12, 400, false),
    label: t(body, 14, 500, false),
  };
}

// ----------------------------------------------------------------- themes

const px = (v, fallback) => {
  if (typeof v !== "string") return fallback;
  const m = /^([\d.]+)(px|rem)$/.exec(v.trim());
  if (!m) return fallback;
  return Math.round(parseFloat(m[1]) * (m[2] === "rem" ? 16 : 1));
};

const ASTRYX_NOTES = {
  neutral: "Quiet greys and black, Figtree. A safe default for products and dashboards.",
  butter: "Cream surfaces with an electric blue accent, Outfit. Friendly consumer products.",
  chocolate: "Warm browns, Albert Sans with Fraunces serif headings. Food, craft, editorial.",
  gothic: "Dark only, cool steel tones, Fustat. Developer tools, night-first apps.",
  matcha: "Earthy greens with handwritten Playwrite headings, DM Sans. Wellness, lifestyle.",
  stone: "Cool stone greys, Montserrat headings over Figtree. Calm, professional.",
  y2k: "Lavender page, hard black borders and square corners, Poppins. Bold, playful brands.",
};

function astryxTheme(id, src) {
  const t = src.tokens;
  const ty = src.typography;
  const body = ty.body.family;
  const heading = ty.heading.family;
  const text = pair(t["--color-text-primary"]);
  const accent = pair(t["--color-accent"]);
  return {
    id,
    name: "Astryx " + id[0].toUpperCase() + id.slice(1),
    source: "Astryx (Meta, MIT)",
    platform: "web",
    note: ASTRYX_NOTES[id],
    dark_only: !Array.isArray(t["--color-background-body"]),
    fonts: { heading, body, mono: "JetBrains Mono" },
    colors: {
      bg: pair(t["--color-background-body"]),
      surface: pair(t["--color-background-surface"]),
      card: pair(t["--color-background-card"]),
      muted: pair(t["--color-background-muted"]),
      border: pair(t["--color-border"]),
      "border-strong": pair(t["--color-border-emphasized"]),
      text,
      "text-muted": pair(t["--color-text-secondary"]),
      "text-subtle": pair(t["--color-text-disabled"]),
      accent,
      "on-accent": pair(t["--color-on-accent"]),
      "accent-soft": pair(t["--color-accent-muted"]),
      "on-accent-soft": pair(t["--color-text-accent"], accent),
      success: pair(t["--color-text-success"], pair(t["--color-success"])),
      warning: pair(t["--color-text-warning"], pair(t["--color-warning"])),
      error: pair(t["--color-text-error"], pair(t["--color-error"])),
      info: pair(t["--color-text-info"], pair(t["--color-info"])),
      overlay: pair(t["--color-overlay"], ["#00000080", "#000000CC"]),
      shadow: pair(t["--color-shadow"], ["#0000001A", "#0000004D"]),
    },
    radius: { sm: px(t["--radius-inner"], 6), md: px(t["--radius-element"], 8), lg: px(t["--radius-container"], 12), xl: px(t["--radius-page"], 24) },
    // Playwrite, Matcha's script face, only goes up to 400.
    type: id === "matcha" ? modular(ty.scale.base, ty.scale.ratio, heading, body, 400, 400) : modular(ty.scale.base, ty.scale.ratio, heading, body),
    ...(id === "y2k" ? { border_width: 2 } : {}),
  };
}

function radixTheme(scales) {
  const g = scales.slate;
  const a = scales.indigo;
  const s = (scale, i) => [scale.light[i - 1], scale.dark[i - 1]];
  return {
    id: "radix",
    name: "Radix",
    source: "Radix Colors (WorkOS, MIT)",
    platform: "web",
    note: "Slate neutrals with an indigo accent, Inter. Pass accent to use any Radix hue.",
    fonts: { heading: "Inter", body: "Inter", mono: "JetBrains Mono" },
    colors: {
      bg: s(g, 1),
      surface: ["#FFFFFF", g.dark[1]],
      card: ["#FFFFFF", g.dark[1]],
      muted: s(g, 3),
      border: s(g, 6),
      "border-strong": s(g, 8),
      text: s(g, 12),
      "text-muted": s(g, 11),
      "text-subtle": s(g, 9),
      accent: s(a, 9),
      "on-accent": ["#FFFFFF", "#FFFFFF"],
      "accent-soft": s(a, 3),
      "on-accent-soft": s(a, 11),
      success: s(scales.green, 11),
      warning: s(scales.amber, 11),
      error: s(scales.red, 11),
      info: s(scales.blue, 11),
      overlay: ["#0000004D", "#00000099"],
      shadow: ["#0000001A", "#00000066"],
    },
    radius: { sm: 4, md: 6, lg: 12, xl: 16 },
    type: modular(16, 1.25, "Inter", "Inter"),
  };
}

const zinc = {
  id: "zinc",
  name: "Zinc",
  source: "shadcn/ui zinc palette (MIT)",
  platform: "web",
  note: "The shadcn/ui look: white, zinc greys, near-black accent, Geist. SaaS and dashboards.",
  fonts: { heading: "Geist", body: "Geist", mono: "JetBrains Mono" },
  colors: {
    bg: ["#FFFFFF", "#09090B"],
    surface: ["#FFFFFF", "#09090B"],
    card: ["#FFFFFF", "#18181B"],
    muted: ["#F4F4F5", "#27272A"],
    border: ["#E4E4E7", "#27272A"],
    "border-strong": ["#D4D4D8", "#3F3F46"],
    text: ["#09090B", "#FAFAFA"],
    "text-muted": ["#71717A", "#A1A1AA"],
    "text-subtle": ["#A1A1AA", "#52525B"],
    accent: ["#18181B", "#FAFAFA"],
    "on-accent": ["#FAFAFA", "#18181B"],
    "accent-soft": ["#F4F4F5", "#27272A"],
    "on-accent-soft": ["#18181B", "#FAFAFA"],
    success: ["#15803D", "#4ADE80"],
    warning: ["#B45309", "#FBBF24"],
    error: ["#DC2626", "#F87171"],
    info: ["#2563EB", "#60A5FA"],
    overlay: ["#00000080", "#000000B3"],
    shadow: ["#0000001A", "#00000066"],
  },
  radius: { sm: 6, md: 8, lg: 12, xl: 16 },
  type: modular(16, 1.25, "Geist", "Geist"),
};

const iosType = (family) => {
  const t = (size, weight, lh, spacing) => ({ family, size, weight, line_height: lh, ...(spacing ? { letter_spacing: spacing } : {}) });
  return {
    display: t(34, 700, 41, 0.4),
    h1: t(28, 700, 34, 0.4),
    h2: t(22, 700, 28, -0.3),
    h3: t(20, 600, 25, -0.4),
    h4: t(17, 600, 22, -0.4),
    lead: t(17, 400, 22, -0.4),
    body: t(17, 400, 22, -0.4),
    small: t(15, 400, 20, -0.2),
    caption: t(12, 400, 16),
    label: t(13, 400, 18, -0.1),
  };
};

const ios = {
  id: "ios",
  name: "iOS",
  source: "Apple Human Interface Guidelines system colours",
  platform: "ios",
  note: "iOS system colours, grouped backgrounds and the HIG type ramp. Inter stands in for SF Pro, which may not be bundled.",
  fonts: { heading: "Inter", body: "Inter", mono: "JetBrains Mono" },
  colors: {
    bg: ["#F2F2F7", "#000000"],
    surface: ["#FFFFFF", "#1C1C1E"],
    card: ["#FFFFFF", "#1C1C1E"],
    muted: ["#E5E5EA", "#2C2C2E"],
    border: ["#C6C6C8", "#38383A"],
    "border-strong": ["#8E8E93", "#636366"],
    text: ["#000000", "#FFFFFF"],
    "text-muted": ["#3C3C4399", "#EBEBF599"],
    "text-subtle": ["#3C3C434D", "#EBEBF54D"],
    accent: ["#007AFF", "#0A84FF"],
    "on-accent": ["#FFFFFF", "#FFFFFF"],
    "accent-soft": ["#007AFF1F", "#0A84FF33"],
    "on-accent-soft": ["#007AFF", "#0A84FF"],
    success: ["#248A3D", "#30D158"],
    warning: ["#C93400", "#FF9F0A"],
    error: ["#D70015", "#FF453A"],
    info: ["#0071A4", "#64D2FF"],
    overlay: ["#00000066", "#00000099"],
    shadow: ["#0000001F", "#00000066"],
  },
  radius: { sm: 8, md: 10, lg: 12, xl: 20 },
  type: iosType("Inter"),
};

const material = {
  id: "material",
  name: "Material 3",
  source: "Material Design 3 baseline scheme (Google, Apache 2.0)",
  platform: "android",
  note: "The Material 3 baseline purple scheme, tonal surfaces and the M3 type scale in Roboto.",
  fonts: { heading: "Roboto", body: "Roboto", mono: "JetBrains Mono" },
  colors: {
    bg: ["#FEF7FF", "#141218"],
    surface: ["#F7F2FA", "#1D1B20"],
    card: ["#F3EDF7", "#211F26"],
    muted: ["#ECE6F0", "#2B2930"],
    border: ["#CAC4D0", "#49454F"],
    "border-strong": ["#79747E", "#938F99"],
    text: ["#1D1B20", "#E6E0E9"],
    "text-muted": ["#49454F", "#CAC4D0"],
    "text-subtle": ["#1D1B2061", "#E6E0E961"],
    accent: ["#6750A4", "#D0BCFF"],
    "on-accent": ["#FFFFFF", "#381E72"],
    "accent-soft": ["#EADDFF", "#4F378B"],
    "on-accent-soft": ["#21005D", "#EADDFF"],
    success: ["#146C2E", "#6DD58C"],
    warning: ["#7D5700", "#F8BD49"],
    error: ["#B3261E", "#F2B8B5"],
    info: ["#00639B", "#94CCFF"],
    overlay: ["#00000052", "#00000080"],
    shadow: ["#00000026", "#00000066"],
  },
  radius: { sm: 8, md: 12, lg: 16, xl: 28 },
  type: (() => {
    const t = (size, weight, lh, spacing) => ({ family: "Roboto", size, weight, line_height: lh, ...(spacing ? { letter_spacing: spacing } : {}) });
    return {
      display: t(45, 400, 52),
      h1: t(32, 400, 40),
      h2: t(28, 400, 36),
      h3: t(24, 400, 32),
      h4: t(22, 400, 28),
      lead: t(16, 400, 24, 0.5),
      body: t(14, 400, 20, 0.25),
      small: t(14, 400, 20, 0.25),
      caption: t(12, 400, 16, 0.4),
      label: t(14, 500, 20, 0.1),
    };
  })(),
};

// ------------------------------------------------------------------ icons

const num = (v) => Math.round(parseFloat(v) * 1000) / 1000;
function elementPath([tag, a]) {
  switch (tag) {
    case "path":
      // A path's first moveto is absolute even when written "m"; joined
      // after another element it would be read relative to where that one
      // ended. Moving to the origin first keeps it (and the relative lines
      // implied after it) where they belong.
      return /^\s*m/.test(a.d) ? "M0 0" + a.d.trim() : a.d.trim();
    case "circle":
    case "ellipse": {
      const cx = num(a.cx ?? 0), cy = num(a.cy ?? 0);
      const rx = num(a.r ?? a.rx), ry = num(a.r ?? a.ry);
      return `M${cx - rx} ${cy}a${rx} ${ry} 0 1 0 ${2 * rx} 0a${rx} ${ry} 0 1 0 ${-2 * rx} 0`;
    }
    case "rect": {
      const x = num(a.x ?? 0), y = num(a.y ?? 0), w = num(a.width), h = num(a.height);
      const r = Math.min(num(a.rx ?? a.ry ?? 0), w / 2, h / 2);
      if (!r) return `M${x} ${y}h${w}v${h}h${-w}z`;
      return `M${x + r} ${y}h${w - 2 * r}a${r} ${r} 0 0 1 ${r} ${r}v${h - 2 * r}a${r} ${r} 0 0 1 ${-r} ${r}h${-(w - 2 * r)}a${r} ${r} 0 0 1 ${-r} ${-r}v${-(h - 2 * r)}a${r} ${r} 0 0 1 ${r} ${-r}z`;
    }
    case "line":
      return `M${num(a.x1)} ${num(a.y1)}L${num(a.x2)} ${num(a.y2)}`;
    case "polyline":
    case "polygon": {
      const pts = a.points.trim().split(/[\s,]+/).map(num);
      let d = `M${pts[0]} ${pts[1]}`;
      for (let i = 2; i < pts.length; i += 2) d += `L${pts[i]} ${pts[i + 1]}`;
      return tag === "polygon" ? d + "z" : d;
    }
    default:
      throw new Error("unknown icon element " + tag);
  }
}

function icons(dir) {
  const nodes = JSON.parse(fs.readFileSync(path.join(dir, "icon-nodes.json"), "utf8"));
  const tags = JSON.parse(fs.readFileSync(path.join(dir, "tags.json"), "utf8"));
  const out = {};
  for (const [name, els] of Object.entries(nodes)) out[name] = { d: els.map(elementPath).join(""), tags: (tags[name] ?? []).slice(0, 8) };
  return out;
}

// ------------------------------------------------------------------- main

const scales = radixScales(args.radix);
const astryx = JSON.parse(fs.readFileSync(args.astryx, "utf8"));
const themes = [zinc, radixTheme(scales), ...Object.entries(astryx).map(([id, src]) => astryxTheme(id, src)), ios, material];
for (const th of themes) {
  for (const [k, v] of Object.entries(th.colors)) if (!v) throw new Error(`${th.id}: colour ${k} is missing`);
  for (const s of SLOTS) if (!th.type[s]) throw new Error(`${th.id}: type ${s} is missing`);
}
const radix = Object.fromEntries(Object.entries(scales).sort());
fs.writeFileSync(path.join(out, "themes.json"), JSON.stringify({ themes, radix }, null, 1) + "\n");
const ic = icons(args.lucide);
fs.writeFileSync(path.join(out, "icons.json.gz"), zlib.gzipSync(JSON.stringify(ic), { level: 9 }));
console.log(`${themes.length} themes, ${Object.keys(radix).length} radix scales, ${Object.keys(ic).length} icons`);

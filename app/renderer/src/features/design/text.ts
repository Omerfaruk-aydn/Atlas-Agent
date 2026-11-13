// Text layout for text nodes: lines broken at the node's width (or at
// newlines only, for auto-width text), measured with the browser's font
// engine when there is one, and an estimate otherwise (tests, the agent's
// own SVG export uses the same estimate in Go).

import { KIT_FONT_NAMES } from "./kit-fonts";
import type { DesignNode } from "./model";

export interface TextLine {
  text: string;
  width: number;
}

export interface TextLayout {
  lines: TextLine[];
  lineHeight: number;
  /** The box the text needs: auto-width text's width, auto-height text's height. */
  width: number;
  height: number;
  ascent: number;
}

type Measure = (text: string, font: string, letterSpacing: number) => number;

let ctx: CanvasRenderingContext2D | OffscreenCanvasRenderingContext2D | null | undefined;

function context() {
  if (ctx !== undefined) return ctx;
  try {
    if (typeof OffscreenCanvas !== "undefined") ctx = new OffscreenCanvas(1, 1).getContext("2d");
    else if (typeof document !== "undefined" && typeof document.createElement === "function") ctx = document.createElement("canvas").getContext("2d");
    else ctx = null;
  } catch {
    ctx = null;
  }
  return ctx;
}

/** Average glyph width of a sans font, as a share of its size. */
const ESTIMATE = 0.55;

export const estimate: Measure = (text, font, letterSpacing) => {
  const size = Number(/(\d+(?:\.\d+)?)px/.exec(font)?.[1] ?? 16);
  return [...text].length * (size * ESTIMATE + letterSpacing);
};

const browser: Measure = (text, font, letterSpacing) => {
  const c = context();
  if (!c) return estimate(text, font, letterSpacing);
  c.font = font;
  return c.measureText(text).width + [...text].length * letterSpacing;
};

let measure: Measure = browser;
/** For tests: lay text out with a known measure. */
export function setMeasure(m: Measure | null) {
  measure = m ?? browser;
  cache.clear();
}

/** A CSS font shorthand for a node's font. */
export function cssFont(n: DesignNode): string {
  const f = n.font ?? { family: "Inter", size: 16, weight: 400 };
  return `${f.style === "italic" ? "italic " : ""}${f.weight} ${f.size}px ${quoteFamily(f.family)}`;
}

export function quoteFamily(family: string): string {
  const generic = new Set(["serif", "sans-serif", "monospace", "cursive", "fantasy", "system-ui"]);
  const name = family.replace(/["\\]/g, "");
  const first = generic.has(name) ? name : `"${name}"`;
  if (name === "Inter" || name === "Inter Variable") return `"Inter Variable", "Inter", sans-serif`;
  // A kit font is bundled under its own name (often "<name> Variable").
  const kit = KIT_FONT_NAMES[name];
  if (kit) return kit.family === name ? `"${name}", ${kit.generic}` : `"${kit.family}", "${name}", ${kit.generic}`;
  return `${first}, sans-serif`;
}

export function displayText(n: DesignNode): string {
  const raw = n.text ?? "";
  return n.transform === "upper" ? raw.toLocaleUpperCase() : n.transform === "lower" ? raw.toLocaleLowerCase() : raw;
}

export const lineHeightOf = (n: DesignNode): number => (n.line_height && n.line_height > 0 ? n.line_height : Math.round((n.font?.size ?? 16) * 1.2 * 100) / 100);

function wrap(paragraph: string, max: number, font: string, spacing: number): TextLine[] {
  if (paragraph === "") return [{ text: "", width: 0 }];
  const words = paragraph.split(/(\s+)/);
  const lines: TextLine[] = [];
  let line = "";
  const push = (text: string) => lines.push({ text, width: measure(text.trimEnd(), font, spacing) });
  for (const word of words) {
    if (word === "") continue;
    const next = line + word;
    if (line === "" || measure(next.trimEnd(), font, spacing) <= max + 0.01) {
      line = next;
      continue;
    }
    if (/^\s+$/.test(word)) {
      push(line);
      line = "";
      continue;
    }
    push(line);
    line = "";
    // A word longer than the line breaks between letters.
    let chunk = "";
    for (const ch of word) {
      if (chunk && measure(chunk + ch, font, spacing) > max) {
        if (measure(chunk, font, spacing) > 0) {
          push(chunk);
          chunk = "";
        }
      }
      chunk += ch;
    }
    line = chunk;
  }
  push(line);
  return lines;
}

const cache = new Map<string, TextLayout>();

export function layoutText(n: DesignNode): TextLayout {
  const font = cssFont(n);
  const spacing = n.letter_spacing ?? 0;
  const text = displayText(n);
  const mode = n.auto_resize ?? "width";
  const key = `${font}|${spacing}|${mode}|${mode === "width" ? 0 : n.width}|${n.line_height ?? 0}|${text}`;
  const hit = cache.get(key);
  if (hit) return hit;
  const lh = lineHeightOf(n);
  const paragraphs = text.split("\n");
  const lines = mode === "width" ? paragraphs.map((p) => ({ text: p, width: measure(p, font, spacing) })) : paragraphs.flatMap((p) => wrap(p, Math.max(1, n.width), font, spacing));
  const widest = lines.reduce((w, l) => Math.max(w, l.width), 0);
  const size = n.font?.size ?? 16;
  const out: TextLayout = {
    lines,
    lineHeight: lh,
    width: mode === "width" ? Math.ceil(widest) : n.width,
    height: mode === "none" ? n.height : Math.ceil(lines.length * lh),
    // Baseline of the first line: centered in its line box like CSS does.
    ascent: (lh - size) / 2 + size * 0.8,
  };
  if (cache.size > 2000) cache.clear();
  cache.set(key, out);
  return out;
}

/** The size a node is drawn at: auto text measures itself; everything else is its stored box. */
export function sizeOf(n: DesignNode): { width: number; height: number } {
  if (n.type !== "text" || n.auto_resize === "none") return { width: n.width, height: n.height };
  const l = layoutText(n);
  return { width: l.width, height: l.height };
}

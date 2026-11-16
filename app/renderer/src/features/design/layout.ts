import { type SizeOf, storedSize } from "./geometry";
import type { Constraints, DesignDoc, DesignNode } from "./model";

/**
 * Auto layout and constraints, resolved over a whole document.
 *
 * Constraints move and stretch the children of a frame that was resized;
 * auto layout then stacks the children of every frame that has a layout.
 * Both are pure: a document comes back with the same node objects where
 * nothing moved, so editing stays cheap and undo stays exact.
 */

const EPS = 1e-6;
const differs = (a: number, b: number) => Math.abs(a - b) > EPS;

type Box = { width: number; height: number; x: number; y: number };

/** The children a layout places: shown and not set to stay put. */
export const inFlow = (c: DesignNode): boolean => c.visible !== false && !c.absolute;

function constrainChild(c: DesignNode, k: Constraints, from: { width: number; height: number }, to: { width: number; height: number }): DesignNode {
  const dw = to.width - from.width;
  const dh = to.height - from.height;
  let { x, y, width, height } = c;
  switch (k.x) {
    case "right":
      x += dw;
      break;
    case "center":
      x += dw / 2;
      break;
    case "stretch":
      width += dw;
      break;
    case "scale":
      if (from.width > 0) {
        x = (x * to.width) / from.width;
        width = (width * to.width) / from.width;
      }
      break;
  }
  switch (k.y) {
    case "bottom":
      y += dh;
      break;
    case "center":
      y += dh / 2;
      break;
    case "stretch":
      height += dh;
      break;
    case "scale":
      if (from.height > 0) {
        y = (y * to.height) / from.height;
        height = (height * to.height) / from.height;
      }
      break;
  }
  width = Math.max(0, width);
  height = Math.max(0, height);
  return x === c.x && y === c.y && width === c.width && height === c.height ? c : { ...c, x, y, width, height };
}

/** Applies a frame's resize to its children's constraints, and on down into children that were stretched. */
function constrain(n: DesignNode, from: { width: number; height: number }, prev: Map<string, Box>, done: Set<string>): DesignNode {
  done.add(n.id);
  if (!n.children || n.layout || (!differs(from.width, n.width) && !differs(from.height, n.height))) return n;
  const to = { width: n.width, height: n.height };
  let changed = false;
  const kids = n.children.map((c) => {
    const p = prev.get(c.id);
    // A child the edit also touched keeps what the edit says.
    if (!p || differs(p.x, c.x) || differs(p.y, c.y) || differs(p.width, c.width) || differs(p.height, c.height)) return c;
    let out = c.constraints ? constrainChild(c, c.constraints, from, to) : c;
    if (out !== c && out.children) out = constrain(out, { width: p.width, height: p.height }, prev, done);
    if (out !== c) changed = true;
    return out;
  });
  return changed ? { ...n, children: kids } : n;
}

interface Given {
  width?: number;
  height?: number;
}

/** A node at a forced size; text switches to wrapping when its width is set. */
function sized(n: DesignNode, given: Given): DesignNode {
  const width = given.width ?? n.width;
  const height = given.height ?? n.height;
  const wrap = n.type === "text" && given.width !== undefined && n.auto_resize === "width";
  const fixH = n.type === "text" && given.height !== undefined && n.auto_resize !== "none";
  if (!differs(width, n.width) && !differs(height, n.height) && !wrap && !fixH) return n;
  const next: DesignNode = { ...n, width, height };
  if (wrap) next.auto_resize = "height";
  if (fixH) next.auto_resize = "none";
  return next;
}

function layoutNode(n: DesignNode, given: Given, sizeOf: SizeOf): DesignNode {
  const base0 = given.width !== undefined || given.height !== undefined ? sized(n, given) : n;
  if (!n.children || n.children.length === 0) return base0;
  const l = n.layout;
  if (!l) {
    const kids = n.children.map((c) => layoutNode(c, {}, sizeOf));
    return kids.every((k, i) => k === n.children![i]) ? base0 : { ...base0, children: kids };
  }
  const row = l.dir === "row";
  const [pt, pr, pb, pl] = l.pad;
  const padMain = row ? pl + pr : pt + pb;
  const padCross = row ? pt + pb : pl + pr;

  const mainOf = (c: DesignNode) => (row ? sizeOf(c).width : sizeOf(c).height);
  const crossOf = (c: DesignNode) => (row ? sizeOf(c).height : sizeOf(c).width);
  const fillMain = (c: DesignNode) => (row ? c.sizing_x === "fill" : c.sizing_y === "fill");
  const fillCross = (c: DesignNode) => l.cross === "stretch" || (row ? c.sizing_y === "fill" : c.sizing_x === "fill");
  const hugX = given.width === undefined && n.sizing_x === "hug";
  const hugY = given.height === undefined && n.sizing_y === "hug";

  // First pass: each child at its natural size, so a hugging parent knows what it holds.
  // When the cross size is already known, children that fill it get it now: text that
  // wraps at the frame's width is as tall as it will be before the frame hugs it.
  // A fill frame's own size is not known until its parent gives it.
  const fixedX = given.width !== undefined || (n.sizing_x ?? "fixed") === "fixed";
  const fixedY = given.height !== undefined || (n.sizing_y ?? "fixed") === "fixed";
  const crossKnown = row ? fixedY : fixedX;
  const knownCross = Math.max(0, (row ? (given.height ?? n.height) : (given.width ?? n.width)) - padCross);
  const kids = n.children.map((c) => (crossKnown && inFlow(c) && fillCross(c) ? layoutNode(c, row ? { height: knownCross } : { width: knownCross }, sizeOf) : layoutNode(c, {}, sizeOf)));
  const flow = kids.filter(inFlow);
  const gaps = Math.max(0, flow.length - 1) * l.gap;
  const natural = flow.reduce((sum, c) => sum + mainOf(c), 0);
  const crossMax = flow.reduce((m, c) => Math.max(m, crossOf(c)), 0);
  let width = given.width ?? n.width;
  let height = given.height ?? n.height;
  if (hugX) width = (row ? natural + gaps + padMain : crossMax + padCross);
  if (hugY) height = (row ? crossMax + padCross : natural + gaps + padMain);

  const innerMain = Math.max(0, (row ? width : height) - padMain);
  const innerCross = Math.max(0, (row ? height : width) - padCross);
  const fills = flow.filter(fillMain);
  const taken = flow.filter((c) => !fillMain(c)).reduce((sum, c) => sum + mainOf(c), 0);
  const share = fills.length ? Math.max(0, innerMain - taken - gaps) / fills.length : 0;

  // Second pass: children that fill get their share, then everyone is placed.
  const placed = kids.map((c) => {
    if (!inFlow(c)) return c;
    const given: Given = {};
    if (fillMain(c)) given[row ? "width" : "height"] = share;
    if (fillCross(c)) given[row ? "height" : "width"] = innerCross;
    return given.width !== undefined || given.height !== undefined ? layoutNode(c, given, sizeOf) : c;
  });
  const flowPlaced = placed.filter(inFlow);
  const used = flowPlaced.reduce((sum, c) => sum + mainOf(c), 0) + gaps;
  const free = Math.max(0, innerMain - used);
  let cursor = (row ? pl : pt) + (l.main === "center" ? free / 2 : l.main === "end" ? free : 0);
  const spread = l.main === "between" && flowPlaced.length > 1 ? free / (flowPlaced.length - 1) : 0;
  const crossStart = row ? pt : pl;

  const positioned = placed.map((c) => {
    if (!inFlow(c)) return c;
    const size = sizeOf(c);
    const main = row ? size.width : size.height;
    const cross = row ? size.height : size.width;
    const offset = l.cross === "center" ? (innerCross - cross) / 2 : l.cross === "end" ? innerCross - cross : 0;
    const pos = cursor;
    cursor += main + l.gap + spread;
    const x = row ? pos : crossStart + offset;
    const y = row ? crossStart + offset : pos;
    return !differs(x, c.x) && !differs(y, c.y) ? c : { ...c, x, y };
  });

  const base = !differs(width, n.width) && !differs(height, n.height) ? n : { ...n, width, height };
  return positioned.every((k, i) => k === n.children![i]) ? base : { ...base, children: positioned };
}

/**
 * An instance's layers placed again at the instance's size, the way its
 * main's auto layout would place them: a longer label or a wider instance
 * reflows the content instead of stretching or clipping it. layout.go's
 * layoutInstance does the same.
 */
export function layoutInstance(master: DesignNode, kids: DesignNode[], width: number, height: number, sizeOf: SizeOf): DesignNode[] {
  const laid = layoutNode({ ...master, children: kids }, { width, height }, sizeOf);
  return laid.children ?? kids;
}

/**
 * Resolves constraints (against `prev`, the document before the edit) and
 * auto layout in `doc`. Without `prev` only layout runs.
 */
export function resolveLayout(doc: DesignDoc, prev: DesignDoc | null = null, sizeOf: SizeOf = storedSize): DesignDoc {
  const before = new Map<string, Box>();
  if (prev) {
    const index = (nodes: DesignNode[]) => {
      for (const n of nodes) {
        before.set(n.id, { width: n.width, height: n.height, x: n.x, y: n.y });
        if (n.children) index(n.children);
      }
    };
    for (const p of prev.pages) index(p.children);
  }
  const done = new Set<string>();
  const constrained = (n: DesignNode): DesignNode => {
    let out = n;
    const p = before.get(n.id);
    if (p && !done.has(n.id) && out.children && !out.layout) out = constrain(out, p, before, done);
    if (!out.children) return out;
    const kids = out.children.map(constrained);
    return kids.every((k, i) => k === out.children![i]) ? out : { ...out, children: kids };
  };
  let changed = false;
  const pages = doc.pages.map((p) => {
    const kids = p.children.map((n) => layoutNode(prev ? constrained(n) : n, {}, sizeOf));
    if (kids.every((k, i) => k === p.children[i])) return p;
    changed = true;
    return { ...p, children: kids };
  });
  return changed ? { ...doc, pages } : doc;
}

// Geometry of the canvas: affine matrices, where a node is on the page,
// what is under the pointer, snapping and guides, alignment, and how a
// resize or rotate drag turns into new node boxes. Pure functions.

import { type DesignNode, type DesignDoc, type LayoutGrid, type Page, isContainer, locate, updateNodes } from "./model";

/** [a, b, c, d, e, f]: x' = a·x + c·y + e, y' = b·x + d·y + f, as in SVG. */
export type Matrix = [number, number, number, number, number, number];
export interface Point {
  x: number;
  y: number;
}
export interface Rect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export const IDENTITY: Matrix = [1, 0, 0, 1, 0, 0];

export function multiply(m: Matrix, n: Matrix): Matrix {
  return [m[0] * n[0] + m[2] * n[1], m[1] * n[0] + m[3] * n[1], m[0] * n[2] + m[2] * n[3], m[1] * n[2] + m[3] * n[3], m[0] * n[4] + m[2] * n[5] + m[4], m[1] * n[4] + m[3] * n[5] + m[5]];
}

export function invert(m: Matrix): Matrix {
  const det = m[0] * m[3] - m[1] * m[2] || 1e-12;
  return [m[3] / det, -m[1] / det, -m[2] / det, m[0] / det, (m[2] * m[5] - m[3] * m[4]) / det, (m[1] * m[4] - m[0] * m[5]) / det];
}

export const apply = (m: Matrix, p: Point): Point => ({ x: m[0] * p.x + m[2] * p.y + m[4], y: m[1] * p.x + m[3] * p.y + m[5] });
/** A direction, without the translation. */
export const applyVector = (m: Matrix, p: Point): Point => ({ x: m[0] * p.x + m[2] * p.y, y: m[1] * p.x + m[3] * p.y });

export const translate = (x: number, y: number): Matrix => [1, 0, 0, 1, x, y];
export function rotateAbout(deg: number, cx: number, cy: number): Matrix {
  const r = (deg * Math.PI) / 180;
  const cos = Math.cos(r);
  const sin = Math.sin(r);
  return [cos, sin, -sin, cos, cx - cos * cx + sin * cy, cy - sin * cx - cos * cy];
}

/** From the node's own box (0,0 at its top left) to its parent's space. */
export function localMatrix(n: Pick<DesignNode, "x" | "y" | "width" | "height" | "rotation">): Matrix {
  const t = translate(n.x, n.y);
  return n.rotation ? multiply(t, rotateAbout(n.rotation, n.width / 2, n.height / 2)) : t;
}

/** The SVG transform attribute for a node. */
export function transformAttr(n: Pick<DesignNode, "x" | "y" | "width" | "height" | "rotation">): string {
  const r = (v: number) => Math.round(v * 1000) / 1000;
  return n.rotation ? `translate(${r(n.x)} ${r(n.y)}) rotate(${r(n.rotation)} ${r(n.width / 2)} ${r(n.height / 2)})` : `translate(${r(n.x)} ${r(n.y)})`;
}

/** From the node's own box to the page, through every ancestor. */
export function worldMatrix(path: DesignNode[]): Matrix {
  let m = IDENTITY;
  for (const n of path) m = multiply(m, localMatrix(n));
  return m;
}

/** The parent's space to the page: the matrix children of `path`'s last node live in. */
export const parentMatrix = (path: DesignNode[]): Matrix => worldMatrix(path.slice(0, -1));

export function corners(m: Matrix, w: number, h: number): Point[] {
  return [apply(m, { x: 0, y: 0 }), apply(m, { x: w, y: 0 }), apply(m, { x: w, y: h }), apply(m, { x: 0, y: h })];
}

export function boundsOf(points: Point[]): Rect {
  if (points.length === 0) return { x: 0, y: 0, width: 0, height: 0 };
  let x0 = Infinity;
  let y0 = Infinity;
  let x1 = -Infinity;
  let y1 = -Infinity;
  for (const p of points) {
    x0 = Math.min(x0, p.x);
    y0 = Math.min(y0, p.y);
    x1 = Math.max(x1, p.x);
    y1 = Math.max(y1, p.y);
  }
  return { x: x0, y: y0, width: x1 - x0, height: y1 - y0 };
}

export function unionRects(rects: Rect[]): Rect {
  return boundsOf(rects.flatMap((r) => [{ x: r.x, y: r.y }, { x: r.x + r.width, y: r.y + r.height }]));
}

export const intersects = (a: Rect, b: Rect): boolean => a.x <= b.x + b.width && b.x <= a.x + a.width && a.y <= b.y + b.height && b.y <= a.y + a.height;
export const contains = (outer: Rect, inner: Rect): boolean => inner.x >= outer.x && inner.y >= outer.y && inner.x + inner.width <= outer.x + outer.width && inner.y + inner.height <= outer.y + outer.height;

/** The size a node is drawn at: auto-sized text measures itself. */
export type SizeOf = (n: DesignNode) => { width: number; height: number };
export const storedSize: SizeOf = (n) => ({ width: n.width, height: n.height });

/** The axis-aligned box of a node on the page. */
export function worldBounds(path: DesignNode[], sizeOf: SizeOf = storedSize): Rect {
  const node = path[path.length - 1]!;
  const size = sizeOf(node);
  const m = multiply(parentMatrix(path), localMatrix({ ...node, ...size }));
  return boundsOf(corners(m, size.width, size.height));
}

export function nodeBounds(page: Page, id: string, sizeOf: SizeOf = storedSize): Rect | null {
  const at = locate(page, id);
  return at ? worldBounds(at.path, sizeOf) : null;
}

export function selectionBounds(page: Page, ids: string[], sizeOf: SizeOf = storedSize): Rect | null {
  const rects = ids.map((id) => nodeBounds(page, id, sizeOf)).filter((r): r is Rect => r !== null);
  return rects.length ? unionRects(rects) : null;
}

// ---------------------------------------------------------------------------
// Hit testing.

function pointInNode(n: DesignNode, local: Point, size: { width: number; height: number }, slop: number): boolean {
  const { width: w, height: h } = size;
  if (n.type === "line") {
    // Distance to the segment from (0,0) to (w,0) in local space.
    const t = Math.max(0, Math.min(w, local.x));
    return Math.hypot(local.x - t, local.y) <= Math.max(slop, (n.strokes?.[0]?.width ?? 1) / 2 + slop);
  }
  if (n.type === "ellipse") {
    const rx = w / 2 + slop;
    const ry = h / 2 + slop;
    if (rx <= 0 || ry <= 0) return false;
    const dx = local.x - w / 2;
    const dy = local.y - h / 2;
    return (dx * dx) / (rx * rx) + (dy * dy) / (ry * ry) <= 1;
  }
  const inBox = local.x >= -slop && local.y >= -slop && local.x <= w + slop && local.y <= h + slop;
  if (n.type === "slice" && inBox) {
    // A slice lies over what it exports: only its edge takes a click, so the layers beneath stay reachable.
    const band = Math.max(slop * 3, 4);
    return !(local.x > band && local.y > band && local.x < w - band && local.y < h - band);
  }
  return inBox;
}

/** The path (page child first) of the topmost visible, unlocked node under a page point. */
export function hitPath(page: Page, point: Point, sizeOf: SizeOf = storedSize, slop = 0): DesignNode[] | null {
  const visit = (nodes: DesignNode[], m: Matrix, path: DesignNode[], clipOk: boolean): DesignNode[] | null => {
    for (let i = nodes.length - 1; i >= 0; i--) {
      const n = nodes[i]!;
      if (n.visible === false) continue;
      const size = sizeOf(n);
      const world = multiply(m, localMatrix({ ...n, ...size }));
      const local = apply(invert(world), point);
      const inside = pointInNode(n, local, size, slop);
      const here = [...path, n];
      if (n.children && n.children.length) {
        const clipped = n.clip === true && n.type !== "group";
        if (!clipped || inside) {
          const deeper = visit(n.children, world, here, clipOk);
          if (deeper) return deeper;
        }
      }
      if (n.type === "group") continue;
      if (inside && !n.locked) return here;
    }
    return null;
  };
  return visit(page.children, IDENTITY, [], true);
}

/**
 * What a click selects, given what is hit: the child of the container the
 * selection is in, or (from the page) the direct child of a top-level frame,
 * as artboards are transparent to a click. `deep` selects the leaf.
 */
export function pickTarget(path: DesignNode[], selection: string[], deep: boolean): DesignNode {
  if (deep) return path[path.length - 1]!;
  // Inside a container a selected node lives in: pick at that level.
  for (let i = path.length - 1; i >= 0; i--) {
    const holder = path[i - 1];
    const level = path[i]!;
    if (holder && selection.some((id) => id === level.id || isSiblingIn(holder, id))) return level;
  }
  const top = path[0]!;
  if (path.length > 1 && (top.type === "frame" || top.type === "component" || top.type === "section") && top.children?.length) return path[1]!;
  return top;
}

const isSiblingIn = (holder: DesignNode, id: string) => holder.children?.some((c) => c.id === id) ?? false;

/** Nodes at the level of the page (and inside top-level frames) whose box meets a marquee. */
export function marquee(page: Page, rect: Rect, sizeOf: SizeOf = storedSize): string[] {
  const out: string[] = [];
  for (const n of page.children) {
    if (n.visible === false || n.locked) continue;
    const box = worldBounds([n], sizeOf);
    const artboard = (n.type === "frame" || n.type === "component" || n.type === "section") && n.children?.length;
    if (artboard && !contains(rect, box)) {
      // Dragging inside a frame selects what is in the frame.
      for (const c of n.children!) {
        if (c.visible === false || c.locked) continue;
        if (intersects(rect, worldBounds([n, c], sizeOf))) out.push(c.id);
      }
      continue;
    }
    if (intersects(rect, box)) out.push(n.id);
  }
  return out;
}

// ---------------------------------------------------------------------------
// Snapping.

export interface Guide {
  axis: "x" | "y";
  /** The page coordinate the line is on. */
  at: number;
  from: number;
  to: number;
}

export interface SnapResult {
  dx: number;
  dy: number;
  guides: Guide[];
}

const edges = (r: Rect) => ({ x: [r.x, r.x + r.width / 2, r.x + r.width], y: [r.y, r.y + r.height / 2, r.y + r.height] });

/**
 * How far to shift `moving` so one of its edges or its center lines up with
 * a target's, within `threshold` page units, and the guides to draw.
 */
export function snapRect(moving: Rect, targets: Rect[], threshold: number): SnapResult {
  const m = edges(moving);
  let bestX = { d: threshold + 1, shift: 0, at: 0 };
  let bestY = { d: threshold + 1, shift: 0, at: 0 };
  for (const t of targets) {
    const e = edges(t);
    for (const a of m.x)
      for (const b of e.x) {
        const d = Math.abs(b - a);
        if (d < bestX.d) bestX = { d, shift: b - a, at: b };
      }
    for (const a of m.y)
      for (const b of e.y) {
        const d = Math.abs(b - a);
        if (d < bestY.d) bestY = { d, shift: b - a, at: b };
      }
  }
  const dx = bestX.d <= threshold ? bestX.shift : 0;
  const dy = bestY.d <= threshold ? bestY.shift : 0;
  const moved = { ...moving, x: moving.x + dx, y: moving.y + dy };
  const guides: Guide[] = [];
  const near = (a: number, b: number) => Math.abs(a - b) < 0.5;
  if (bestX.d <= threshold) {
    const hits = targets.filter((t) => edges(t).x.some((v) => near(v, bestX.at)));
    const ys = [moved.y, moved.y + moved.height, ...hits.flatMap((t) => [t.y, t.y + t.height])].filter(Number.isFinite);
    guides.push({ axis: "x", at: bestX.at, from: Math.min(...ys), to: Math.max(...ys) });
  }
  if (bestY.d <= threshold) {
    const hits = targets.filter((t) => edges(t).y.some((v) => near(v, bestY.at)));
    const xs = [moved.x, moved.x + moved.width, ...hits.flatMap((t) => [t.x, t.x + t.width])].filter(Number.isFinite);
    guides.push({ axis: "y", at: bestY.at, from: Math.min(...xs), to: Math.max(...xs) });
  }
  return { dx, dy, guides };
}

/** Snaps a single page coordinate pair (a resize corner) to target edges. */
export function snapPoint(p: Point, targets: Rect[], threshold: number, axes: { x: boolean; y: boolean } = { x: true, y: true }): SnapResult {
  const r = snapRect({ x: p.x, y: p.y, width: 0, height: 0 }, targets, threshold);
  return { dx: axes.x ? r.dx : 0, dy: axes.y ? r.dy : 0, guides: r.guides.filter((g) => (g.axis === "x" ? axes.x : axes.y)) };
}

/** The boxes a selection snaps to: its siblings' and their container's, not its own. */
export function snapTargets(page: Page, ids: string[], sizeOf: SizeOf = storedSize): Rect[] {
  const skip = new Set(ids);
  const out: Rect[] = [];
  const first = ids[0] ? locate(page, ids[0]) : null;
  const holderPath = first ? first.path.slice(0, -1) : [];
  const siblings = holderPath.length ? holderPath[holderPath.length - 1]!.children ?? [] : page.children;
  if (holderPath.length) out.push(worldBounds(holderPath, sizeOf));
  for (const s of siblings) {
    if (skip.has(s.id) || s.visible === false) continue;
    out.push(worldBounds([...holderPath, s], sizeOf));
  }
  // Top-level frames are useful targets from anywhere.
  if (holderPath.length) for (const s of page.children) if (!skip.has(s.id) && s.id !== holderPath[0]!.id && s.visible !== false) out.push(worldBounds([s], sizeOf));
  // Ruler guides are lines, not boxes: NaN on the other axis keeps them out of that axis's comparisons.
  for (const g of page.guides ?? []) out.push(g.axis === "x" ? { x: g.at, y: NaN, width: 0, height: NaN } : { x: NaN, y: g.at, width: NaN, height: 0 });
  return out;
}

// ---------------------------------------------------------------------------
// Moves, resizes, rotation.

/** Moves nodes by a page-space delta, each in its own parent's space. */
export function moveBy(doc: DesignDoc, page: Page, ids: string[], dx: number, dy: number): DesignDoc {
  const deltas = new Map<string, Point>();
  for (const id of ids) {
    const at = locate(page, id);
    if (!at) continue;
    deltas.set(id, applyVector(invert(parentMatrix(at.path)), { x: dx, y: dy }));
  }
  return updateNodes(doc, deltas.keys(), (n) => {
    const d = deltas.get(n.id)!;
    return { ...n, x: n.x + d.x, y: n.y + d.y };
  });
}

export type Handle = "nw" | "n" | "ne" | "e" | "se" | "s" | "sw" | "w";

const HANDLE_DIR: Record<Handle, [number, number]> = { nw: [-1, -1], n: [0, -1], ne: [1, -1], e: [1, 0], se: [1, 1], s: [0, 1], sw: [-1, 1], w: [-1, 0] };
export const handleDir = (h: Handle): [number, number] => HANDLE_DIR[h];

export interface ResizeOptions {
  /** Keep the aspect ratio (shift). */
  keepRatio?: boolean;
  /** Resize about the center (alt). */
  fromCenter?: boolean;
}

/**
 * The box a single node gets when the `handle` of its box is dragged to the
 * page point `to`. Works in the node's rotated frame, keeping the opposite
 * side (or the center) fixed on the page. A drag past the opposite edge
 * flips the box; sizes never go below zero.
 */
export function resizeNode(node: DesignNode, parentWorld: Matrix, handle: Handle, to: Point, opts: ResizeOptions = {}): Pick<DesignNode, "x" | "y" | "width" | "height"> {
  const [hx, hy] = HANDLE_DIR[handle];
  const world = multiply(parentWorld, localMatrix(node));
  const local = apply(invert(world), to);
  const w0 = node.width;
  const h0 = node.height;
  // Fixed point in local coordinates.
  const fx = opts.fromCenter ? w0 / 2 : hx === 1 ? 0 : hx === -1 ? w0 : 0;
  const fy = opts.fromCenter ? h0 / 2 : hy === 1 ? 0 : hy === -1 ? h0 : 0;
  let left = 0;
  let right = w0;
  let top = 0;
  let bottom = h0;
  if (hx !== 0) {
    if (opts.fromCenter) {
      const half = Math.abs(local.x - w0 / 2);
      left = w0 / 2 - half;
      right = w0 / 2 + half;
    } else if (hx === 1) right = local.x;
    else left = local.x;
  }
  if (hy !== 0) {
    if (opts.fromCenter) {
      const half = Math.abs(local.y - h0 / 2);
      top = h0 / 2 - half;
      bottom = h0 / 2 + half;
    } else if (hy === 1) bottom = local.y;
    else top = local.y;
  }
  let nl = Math.min(left, right);
  let nr = Math.max(left, right);
  let nt = Math.min(top, bottom);
  let nb = Math.max(top, bottom);
  if (opts.keepRatio && w0 > 0 && h0 > 0) {
    const ratio = w0 / h0;
    let w = nr - nl;
    let h = nb - nt;
    if (hx === 0) w = h * ratio;
    else if (hy === 0) h = w / ratio;
    else if (w / h > ratio) h = w / ratio;
    else w = h * ratio;
    // Grow away from the fixed point; an edge handle grows both ways across.
    nl = hx === 0 || opts.fromCenter ? w0 / 2 - w / 2 : local.x >= fx ? fx : fx - w;
    nt = hy === 0 || opts.fromCenter ? h0 / 2 - h / 2 : local.y >= fy ? fy : fy - h;
    nr = nl + w;
    nb = nt + h;
  }
  const width = nr - nl;
  const height = nb - nt;
  // The new box's top left in the old local frame is (nl, nt). With the
  // rotation about the new center, find x/y so the box lands there.
  const centerLocal = { x: nl + width / 2, y: nt + height / 2 };
  const centerParent = apply(localMatrix(node), centerLocal);
  return { x: centerParent.x - width / 2, y: centerParent.y - height / 2, width, height };
}

/**
 * Scales a group of nodes from one page box to another: positions and sizes
 * follow the box; rotations stay. For multi-selection resizes.
 */
export function scaleInto(doc: DesignDoc, page: Page, ids: string[], from: Rect, to: Rect, sizeOf: SizeOf = storedSize): DesignDoc {
  const sx = from.width > 0 ? to.width / from.width : 1;
  const sy = from.height > 0 ? to.height / from.height : 1;
  const plans = new Map<string, Pick<DesignNode, "x" | "y" | "width" | "height">>();
  for (const id of ids) {
    const at = locate(page, id);
    if (!at) continue;
    const size = sizeOf(at.node);
    const pm = parentMatrix(at.path);
    const center = apply(multiply(pm, localMatrix({ ...at.node, ...size })), { x: size.width / 2, y: size.height / 2 });
    const nc = { x: to.x + (center.x - from.x) * sx, y: to.y + (center.y - from.y) * sy };
    const local = apply(invert(pm), nc);
    const width = size.width * sx;
    const height = size.height * sy;
    plans.set(id, { x: local.x - width / 2, y: local.y - height / 2, width, height });
  }
  return updateNodes(doc, plans.keys(), (n) => scaleChildren({ ...n, ...plans.get(n.id)!, ...(n.type === "text" && n.auto_resize === "width" ? { auto_resize: "none" as const } : {}) }, n));
}

/** Groups scale their children with them, since a group's box is its children's. */
function scaleChildren(next: DesignNode, before: DesignNode): DesignNode {
  if (next.type !== "group" || !next.children || before.width <= 0 || before.height <= 0) return next;
  const sx = next.width / before.width;
  const sy = next.height / before.height;
  if (sx === 1 && sy === 1) return next;
  const kids = next.children.map((c) => scaleChildren({ ...c, x: c.x * sx, y: c.y * sy, width: c.width * sx, height: c.height * sy }, c));
  return { ...next, children: kids };
}

/** Applies a single-node resize, scaling a group's children along. */
export function applyResize(doc: DesignDoc, id: string, box: Pick<DesignNode, "x" | "y" | "width" | "height">): DesignDoc {
  return updateNodes(doc, [id], (n) => {
    const next = { ...n, ...box };
    if (n.type === "text" && n.auto_resize === "width") next.auto_resize = box.height !== n.height ? "none" : "height";
    return scaleChildren(next, n);
  });
}

/** The angle in degrees from a center to a point, 0 pointing right, clockwise. */
export const angleOf = (c: Point, p: Point): number => (Math.atan2(p.y - c.y, p.x - c.x) * 180) / Math.PI;

/** Rotates nodes about a page point by `deg`, each keeping its place on the circle. */
export function rotateAround(doc: DesignDoc, page: Page, ids: string[], center: Point, deg: number, base: DesignDoc): DesignDoc {
  const basePage = base.pages.find((p) => p.id === page.id) ?? page;
  const turn = rotateAbout(deg, center.x, center.y);
  const plans = new Map<string, Partial<DesignNode>>();
  for (const id of ids) {
    const at = locate(basePage, id);
    if (!at) continue;
    const n = at.node;
    const pm = parentMatrix(at.path);
    const c = apply(multiply(pm, localMatrix(n)), { x: n.width / 2, y: n.height / 2 });
    const nc = apply(invert(pm), apply(turn, c));
    const rotation = ((((n.rotation ?? 0) + deg) % 360) + 360) % 360;
    plans.set(id, { x: nc.x - n.width / 2, y: nc.y - n.height / 2, rotation: Math.abs(rotation) < 1e-6 || Math.abs(rotation - 360) < 1e-6 ? 0 : rotation });
  }
  return updateNodes(doc, plans.keys(), (n) => {
    const p = { ...n, ...plans.get(n.id)! };
    if (!p.rotation) delete p.rotation;
    return p;
  });
}

// ---------------------------------------------------------------------------
// Alignment.

export type AlignKind = "left" | "hcenter" | "right" | "top" | "vcenter" | "bottom";

/** Aligns nodes to each other, or to their container when only one is selected. */
export function align(doc: DesignDoc, page: Page, ids: string[], kind: AlignKind, sizeOf: SizeOf = storedSize): DesignDoc {
  if (ids.length === 0) return doc;
  let target: Rect | null;
  if (ids.length === 1) {
    const at = locate(page, ids[0]!);
    if (!at || at.path.length < 2) return doc;
    target = worldBounds(at.path.slice(0, -1), sizeOf);
  } else target = selectionBounds(page, ids, sizeOf);
  if (!target) return doc;
  let out = doc;
  for (const id of ids) {
    const box = nodeBounds(page, id, sizeOf);
    if (!box) continue;
    let dx = 0;
    let dy = 0;
    if (kind === "left") dx = target.x - box.x;
    if (kind === "hcenter") dx = target.x + target.width / 2 - (box.x + box.width / 2);
    if (kind === "right") dx = target.x + target.width - (box.x + box.width);
    if (kind === "top") dy = target.y - box.y;
    if (kind === "vcenter") dy = target.y + target.height / 2 - (box.y + box.height / 2);
    if (kind === "bottom") dy = target.y + target.height - (box.y + box.height);
    if (dx || dy) out = moveBy(out, page, [id], dx, dy);
  }
  return out;
}

/** Spaces three or more nodes evenly between the outermost two. */
export function distribute(doc: DesignDoc, page: Page, ids: string[], axis: "x" | "y", sizeOf: SizeOf = storedSize): DesignDoc {
  if (ids.length < 3) return doc;
  const boxes = ids.map((id) => ({ id, box: nodeBounds(page, id, sizeOf)! })).filter((x) => x.box);
  const pos = (r: Rect) => (axis === "x" ? r.x : r.y);
  const len = (r: Rect) => (axis === "x" ? r.width : r.height);
  boxes.sort((a, b) => pos(a.box) - pos(b.box));
  const first = boxes[0]!.box;
  const last = boxes[boxes.length - 1]!.box;
  const span = pos(last) + len(last) - pos(first);
  const total = boxes.reduce((s, b) => s + len(b.box), 0);
  const gap = (span - total) / (boxes.length - 1);
  let at = pos(first);
  let out = doc;
  for (const { id, box } of boxes) {
    const shift = at - pos(box);
    if (Math.abs(shift) > 1e-9) out = moveBy(out, page, [id], axis === "x" ? shift : 0, axis === "y" ? shift : 0);
    at += len(box) + gap;
  }
  return out;
}

// ---------------------------------------------------------------------------
// Groups.

/**
 * Fits every group to its children, as a group's box is always what it
 * holds: children keep their place on the page while the group's origin
 * moves. Unrotated groups only; a rotated group keeps its box.
 */
export function fitGroups(doc: DesignDoc, sizeOf: SizeOf = storedSize): DesignDoc {
  const fit = (n: DesignNode): DesignNode => {
    if (!n.children) return n;
    const kids = n.children.map(fit);
    let next = kids.some((k, i) => k !== n.children![i]) ? { ...n, children: kids } : n;
    if (next.type !== "group" || next.rotation || kids.length === 0) return next;
    const box = unionRects(kids.map((k) => boundsOf(corners(localMatrix({ ...k, ...sizeOf(k) }), sizeOf(k).width, sizeOf(k).height))));
    const eps = 1e-6;
    if (Math.abs(box.x) < eps && Math.abs(box.y) < eps && Math.abs(box.width - next.width) < eps && Math.abs(box.height - next.height) < eps) return next;
    next = {
      ...next,
      x: next.x + box.x,
      y: next.y + box.y,
      width: box.width,
      height: box.height,
      children: kids.map((k) => ({ ...k, x: k.x - box.x, y: k.y - box.y })),
    };
    return next;
  };
  let changed = false;
  const pages = doc.pages.map((p) => {
    const kids = p.children.map(fit);
    if (kids.every((k, i) => k === p.children[i])) return p;
    changed = true;
    return { ...p, children: kids };
  });
  return changed ? { ...doc, pages } : doc;
}

/** Where a page point is inside a container, for moving a node into it. */
export function toLocal(path: DesignNode[], p: Point): Point {
  return apply(invert(worldMatrix(path)), p);
}

/** The topmost frame or component (not in `exclude`) whose box holds a page point, deepest first. */
export function frameAt(page: Page, p: Point, exclude: Set<string>, sizeOf: SizeOf = storedSize): DesignNode[] | null {
  let best: DesignNode[] | null = null;
  const visit = (nodes: DesignNode[], path: DesignNode[]) => {
    for (let i = nodes.length - 1; i >= 0; i--) {
      const n = nodes[i]!;
      if (exclude.has(n.id) || n.visible === false || !isContainer(n) || n.type === "group") continue;
      const here = [...path, n];
      const size = sizeOf(n);
      const local = apply(invert(worldMatrix(here)), p);
      if (local.x >= 0 && local.y >= 0 && local.x <= size.width && local.y <= size.height) {
        best = here;
        if (n.children) visit(n.children, here);
        return;
      }
    }
  };
  visit(page.children, []);
  return best;
}

/** Where a layout grid's tracks fall along one side of a frame: [start, size] pairs. */
export function gridTracks(g: LayoutGrid, length: number): [number, number][] {
  const n = Math.max(1, g.count ?? 5);
  const gutter = g.gutter ?? 0;
  const margin = g.margin ?? 0;
  const align = g.align ?? "stretch";
  let size = g.size && align !== "stretch" ? g.size : (length - 2 * margin - (n - 1) * gutter) / n;
  if (!(size > 0)) size = 0;
  const total = n * size + (n - 1) * gutter;
  const start = align === "center" ? (length - total) / 2 : align === "end" ? length - margin - total : margin;
  return Array.from({ length: n }, (_, i) => [start + i * (size + gutter), size]);
}

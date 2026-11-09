// Vector paths: SVG path data as editable anchors with handles, and back.

import type { DesignNode } from "./model";

export interface Pt {
  x: number;
  y: number;
}

/** An anchor with optional control handles, all in absolute coordinates. */
export interface Anchor extends Pt {
  /** The handle on the way in (from the previous anchor). */
  in?: Pt;
  /** The handle on the way out (to the next anchor). */
  out?: Pt;
}

export interface Subpath {
  anchors: Anchor[];
  closed: boolean;
}

const r3 = (v: number) => Math.round(v * 1000) / 1000;
const ARGS: Record<string, number> = { m: 2, l: 2, h: 1, v: 1, c: 6, s: 4, q: 4, t: 2, a: 7, z: 0 };

function tokens(d: string): string[] {
  return d.match(/[a-zA-Z]|[-+]?(?:\d+\.?\d*|\.\d+)(?:e[-+]?\d+)?/g) ?? [];
}

/** An elliptical arc as cubic béziers: [c1, c2, end] per piece. */
function arcToCubics(from: Pt, rx: number, ry: number, rot: number, large: boolean, sweep: boolean, to: Pt): [Pt, Pt, Pt][] {
  if (rx === 0 || ry === 0 || (from.x === to.x && from.y === to.y)) return [[from, to, to]];
  rx = Math.abs(rx);
  ry = Math.abs(ry);
  const phi = (rot * Math.PI) / 180;
  const cos = Math.cos(phi);
  const sin = Math.sin(phi);
  const dx = (from.x - to.x) / 2;
  const dy = (from.y - to.y) / 2;
  const x1 = cos * dx + sin * dy;
  const y1 = -sin * dx + cos * dy;
  const lambda = (x1 * x1) / (rx * rx) + (y1 * y1) / (ry * ry);
  if (lambda > 1) {
    const s = Math.sqrt(lambda);
    rx *= s;
    ry *= s;
  }
  const num = rx * rx * ry * ry - rx * rx * y1 * y1 - ry * ry * x1 * x1;
  const den = rx * rx * y1 * y1 + ry * ry * x1 * x1;
  const k = (large === sweep ? -1 : 1) * Math.sqrt(Math.max(0, num / den));
  const cxp = (k * rx * y1) / ry;
  const cyp = (-k * ry * x1) / rx;
  const cx = cos * cxp - sin * cyp + (from.x + to.x) / 2;
  const cy = sin * cxp + cos * cyp + (from.y + to.y) / 2;
  const angle = (ux: number, uy: number, vx: number, vy: number) => {
    const a = Math.atan2(ux * vy - uy * vx, ux * vx + uy * vy);
    return a;
  };
  const th1 = angle(1, 0, (x1 - cxp) / rx, (y1 - cyp) / ry);
  let dth = angle((x1 - cxp) / rx, (y1 - cyp) / ry, (-x1 - cxp) / rx, (-y1 - cyp) / ry);
  if (!sweep && dth > 0) dth -= 2 * Math.PI;
  if (sweep && dth < 0) dth += 2 * Math.PI;
  const pieces = Math.max(1, Math.ceil(Math.abs(dth) / (Math.PI / 2) - 1e-9));
  const step = dth / pieces;
  const t = (4 / 3) * Math.tan(step / 4);
  const out: [Pt, Pt, Pt][] = [];
  const at = (a: number): { p: Pt; d: Pt } => {
    const ca = Math.cos(a);
    const sa = Math.sin(a);
    return {
      p: { x: cx + rx * ca * cos - ry * sa * sin, y: cy + rx * ca * sin + ry * sa * cos },
      d: { x: -rx * sa * cos - ry * ca * sin, y: -rx * sa * sin + ry * ca * cos },
    };
  };
  for (let i = 0; i < pieces; i++) {
    const a = at(th1 + i * step);
    const b = at(th1 + (i + 1) * step);
    out.push([{ x: a.p.x + t * a.d.x, y: a.p.y + t * a.d.y }, { x: b.p.x - t * b.d.x, y: b.p.y - t * b.d.y }, i === pieces - 1 ? to : b.p]);
  }
  return out;
}

/** Parses path data. Lines, curves, quadratics and arcs, absolute or relative, come out as anchors with handles. */
export function parsePath(d: string): Subpath[] {
  const tk = tokens(d);
  const out: Subpath[] = [];
  let sub: Subpath | null = null;
  let cur: Pt = { x: 0, y: 0 };
  let start: Pt = { x: 0, y: 0 };
  let lastCtl: Pt | null = null;
  let lastQuad: Pt | null = null;
  let cmd = "";
  let i = 0;
  const begin = (p: Pt) => {
    sub = { anchors: [{ ...p }], closed: false };
    out.push(sub);
    cur = start = p;
  };
  const curveTo = (c1: Pt, c2: Pt, p: Pt) => {
    if (!sub) begin(cur);
    const s = sub!;
    const prev = s.anchors[s.anchors.length - 1]!;
    prev.out = c1;
    s.anchors.push({ ...p, in: c2 });
    cur = p;
  };
  const lineTo = (p: Pt) => {
    if (!sub) begin(cur);
    sub!.anchors.push({ ...p });
    cur = p;
  };
  while (i < tk.length) {
    const tok = tk[i]!;
    if (/[a-zA-Z]/.test(tok)) {
      cmd = tok;
      i++;
      if (cmd.toLowerCase() === "z") {
        if (sub) {
          const s = sub as Subpath;
          s.closed = true;
          // A closing segment that returns to the start merges into the first anchor.
          const first = s.anchors[0]!;
          const last = s.anchors[s.anchors.length - 1]!;
          if (s.anchors.length > 1 && last.x === first.x && last.y === first.y) {
            if (last.in) first.in = last.in;
            s.anchors.pop();
          }
        }
        cur = start;
        sub = null;
        lastCtl = lastQuad = null;
        continue;
      }
    }
    const lower = cmd.toLowerCase();
    const n = ARGS[lower];
    if (!n) {
      i++;
      continue;
    }
    const a = tk.slice(i, i + n).map(Number);
    if (a.length < n || a.some(Number.isNaN)) break;
    i += n;
    const rel = cmd === lower;
    const ox = rel ? cur.x : 0;
    const oy = rel ? cur.y : 0;
    switch (lower) {
      case "m": {
        const p = { x: a[0]! + ox, y: a[1]! + oy };
        begin(p);
        // Further pairs after a move are lines.
        cmd = rel ? "l" : "L";
        lastCtl = lastQuad = null;
        break;
      }
      case "l":
        lineTo({ x: a[0]! + ox, y: a[1]! + oy });
        lastCtl = lastQuad = null;
        break;
      case "h":
        lineTo({ x: a[0]! + ox, y: cur.y });
        lastCtl = lastQuad = null;
        break;
      case "v":
        lineTo({ x: cur.x, y: a[0]! + oy });
        lastCtl = lastQuad = null;
        break;
      case "c": {
        const c2 = { x: a[2]! + ox, y: a[3]! + oy };
        curveTo({ x: a[0]! + ox, y: a[1]! + oy }, c2, { x: a[4]! + ox, y: a[5]! + oy });
        lastCtl = c2;
        lastQuad = null;
        break;
      }
      case "s": {
        const c1 = lastCtl ? { x: 2 * cur.x - lastCtl.x, y: 2 * cur.y - lastCtl.y } : { ...cur };
        const c2 = { x: a[0]! + ox, y: a[1]! + oy };
        curveTo(c1, c2, { x: a[2]! + ox, y: a[3]! + oy });
        lastCtl = c2;
        lastQuad = null;
        break;
      }
      case "q": {
        const q = { x: a[0]! + ox, y: a[1]! + oy };
        const p = { x: a[2]! + ox, y: a[3]! + oy };
        const from = cur;
        curveTo({ x: from.x + (2 / 3) * (q.x - from.x), y: from.y + (2 / 3) * (q.y - from.y) }, { x: p.x + (2 / 3) * (q.x - p.x), y: p.y + (2 / 3) * (q.y - p.y) }, p);
        lastQuad = q;
        lastCtl = null;
        break;
      }
      case "t": {
        const q: Pt = lastQuad ? { x: 2 * cur.x - lastQuad.x, y: 2 * cur.y - lastQuad.y } : { ...cur };
        const p = { x: a[0]! + ox, y: a[1]! + oy };
        const from = cur;
        curveTo({ x: from.x + (2 / 3) * (q.x - from.x), y: from.y + (2 / 3) * (q.y - from.y) }, { x: p.x + (2 / 3) * (q.x - p.x), y: p.y + (2 / 3) * (q.y - p.y) }, p);
        lastQuad = q;
        lastCtl = null;
        break;
      }
      case "a": {
        const to = { x: a[5]! + ox, y: a[6]! + oy };
        for (const [c1, c2, p] of arcToCubics(cur, a[0]!, a[1]!, a[2]!, a[3] !== 0, a[4] !== 0, to)) curveTo(c1, c2, p);
        lastCtl = lastQuad = null;
        break;
      }
    }
  }
  return out.filter((s) => s.anchors.length > 0);
}

/** Path data for subpaths; curved where either neighbouring handle exists. */
export function pathData(subs: Subpath[]): string {
  const p = (pt: Pt) => `${r3(pt.x)} ${r3(pt.y)}`;
  const parts: string[] = [];
  for (const s of subs) {
    const a = s.anchors;
    if (a.length === 0) continue;
    parts.push(`M${p(a[0]!)}`);
    const seg = (from: Anchor, to: Anchor) => (from.out || to.in ? `C${p(from.out ?? from)} ${p(to.in ?? to)} ${p(to)}` : `L${p(to)}`);
    for (let i = 1; i < a.length; i++) parts.push(seg(a[i - 1]!, a[i]!));
    if (s.closed) {
      if (a.length > 1 && (a[a.length - 1]!.out || a[0]!.in)) parts.push(seg(a[a.length - 1]!, a[0]!));
      parts.push("Z");
    }
  }
  return parts.join("");
}

/** The box around anchors and their handles. */
export function pathBounds(subs: Subpath[]): { x: number; y: number; width: number; height: number } {
  let x0 = Infinity;
  let y0 = Infinity;
  let x1 = -Infinity;
  let y1 = -Infinity;
  for (const s of subs)
    for (const a of s.anchors)
      for (const q of [a, a.in, a.out]) {
        if (!q) continue;
        x0 = Math.min(x0, q.x);
        y0 = Math.min(y0, q.y);
        x1 = Math.max(x1, q.x);
        y1 = Math.max(y1, q.y);
      }
  if (!Number.isFinite(x0)) return { x: 0, y: 0, width: 0, height: 0 };
  return { x: x0, y: y0, width: x1 - x0, height: y1 - y0 };
}

/** Moves every point by (dx, dy) and scales it about the origin. */
export function mapPoints(subs: Subpath[], f: (p: Pt) => Pt): Subpath[] {
  return subs.map((s) => ({ closed: s.closed, anchors: s.anchors.map((a) => ({ ...f(a), ...(a.in ? { in: f(a.in) } : {}), ...(a.out ? { out: f(a.out) } : {}) })) }));
}

/** A path node's anchors in its own box (view_box scaled to the node's size). */
export function nodePath(n: DesignNode): Subpath[] {
  const vb = n.view_box ?? [n.width || 1, n.height || 1];
  const sx = vb[0] ? n.width / vb[0] : 1;
  const sy = vb[1] ? n.height / vb[1] : 1;
  return mapPoints(parsePath(n.d ?? ""), (p) => ({ x: p.x * sx, y: p.y * sy }));
}

/** The fields of a path node holding subpaths given in the node's own box, moving the node when the box changed. */
export function pathFields(n: Pick<DesignNode, "x" | "y">, subs: Subpath[]): Pick<DesignNode, "x" | "y" | "width" | "height" | "d" | "view_box"> {
  const b = pathBounds(subs);
  const moved = mapPoints(subs, (p) => ({ x: p.x - b.x, y: p.y - b.y }));
  return { x: n.x + b.x, y: n.y + b.y, width: b.width, height: b.height, d: pathData(moved), view_box: [Math.max(b.width, 0.0001), Math.max(b.height, 0.0001)] };
}

/** A shape as editable path data in its own box; null for shapes with no outline of their own (text, images, groups). */
export function shapeToPath(n: DesignNode): Subpath[] | null {
  const w = n.width;
  const h = n.height;
  const A = (x: number, y: number): Anchor => ({ x, y });
  const poly = (pts: [number, number][]): Subpath[] => [{ closed: true, anchors: pts.map(([x, y]) => A(x, y)) }];
  switch (n.type) {
    case "rect":
    case "frame":
    case "component":
    case "image": {
      const r = n.radius ?? 0;
      const [tl, tr, br, bl] = (typeof r === "number" ? [r, r, r, r] : r).map((v) => Math.max(0, Math.min(v, Math.min(w, h) / 2)));
      if (!tl && !tr && !br && !bl) return poly([[0, 0], [w, 0], [w, h], [0, h]]);
      const k = 0.5522847498;
      const a: Anchor[] = [];
      const corner = (cx: number, cy: number, rad: number, ax: number, ay: number, bx: number, by: number) => {
        // Anchor `a` (entering) then anchor `b` (leaving), with handles bending towards the corner.
        a.push({ x: ax, y: ay, ...(rad ? { out: { x: ax + (cx - ax) * k, y: ay + (cy - ay) * k } } : {}) });
        a.push({ x: bx, y: by, ...(rad ? { in: { x: bx + (cx - bx) * k, y: by + (cy - by) * k } } : {}) });
      };
      corner(w, 0, tr, w - tr, 0, w, tr);
      corner(w, h, br, w, h - br, w - br, h);
      corner(0, h, bl, bl, h, 0, h - bl);
      corner(0, 0, tl, 0, tl, tl, 0);
      return [{ closed: true, anchors: dedupe(a) }];
    }
    case "ellipse": {
      const k = 0.5522847498;
      const cx = w / 2;
      const cy = h / 2;
      const hx = cx * k;
      const hy = cy * k;
      return [
        {
          closed: true,
          anchors: [
            { x: cx, y: 0, in: { x: cx - hx, y: 0 }, out: { x: cx + hx, y: 0 } },
            { x: w, y: cy, in: { x: w, y: cy - hy }, out: { x: w, y: cy + hy } },
            { x: cx, y: h, in: { x: cx + hx, y: h }, out: { x: cx - hx, y: h } },
            { x: 0, y: cy, in: { x: 0, y: cy + hy }, out: { x: 0, y: cy - hy } },
          ],
        },
      ];
    }
    case "line":
      return [{ closed: false, anchors: [A(0, 0), A(w, 0)] }];
    case "polygon":
    case "star": {
      const sides = n.sides ?? (n.type === "star" ? 5 : 3);
      const inner = n.type === "star" ? (n.inner ?? 0.38) : undefined;
      const count = inner === undefined ? sides : sides * 2;
      const pts: [number, number][] = [];
      for (let i = 0; i < count; i++) {
        const ang = -Math.PI / 2 + (i * 2 * Math.PI) / count;
        const k = inner !== undefined && i % 2 === 1 ? inner : 1;
        pts.push([w / 2 + Math.cos(ang) * (w / 2) * k, h / 2 + Math.sin(ang) * (h / 2) * k]);
      }
      return poly(pts);
    }
    case "path":
      return nodePath(n);
    default:
      return null;
  }
}

/** Removes the repeated anchor a rounded corner of radius 0 leaves behind, merging handles. */
function dedupe(a: Anchor[]): Anchor[] {
  const out: Anchor[] = [];
  for (const p of a) {
    const last = out[out.length - 1];
    if (last && Math.abs(last.x - p.x) < 1e-9 && Math.abs(last.y - p.y) < 1e-9) {
      if (p.out) last.out = p.out;
      if (p.in && !last.in) last.in = p.in;
    } else out.push({ ...p });
  }
  const first = out[0]!;
  const end = out[out.length - 1]!;
  if (out.length > 1 && Math.abs(first.x - end.x) < 1e-9 && Math.abs(first.y - end.y) < 1e-9) {
    if (end.in) first.in = end.in;
    out.pop();
  }
  return out;
}

/** Ramer–Douglas–Peucker simplification of a freehand stroke. */
export function simplify(points: Pt[], epsilon: number): Pt[] {
  if (points.length < 3) return points;
  const keep = new Array<boolean>(points.length).fill(false);
  keep[0] = keep[points.length - 1] = true;
  const stack: [number, number][] = [[0, points.length - 1]];
  while (stack.length) {
    const [lo, hi] = stack.pop()!;
    const a = points[lo]!;
    const b = points[hi]!;
    const len = Math.hypot(b.x - a.x, b.y - a.y) || 1e-9;
    let best = -1;
    let far = 0;
    for (let i = lo + 1; i < hi; i++) {
      const p = points[i]!;
      const dist = Math.abs((b.x - a.x) * (a.y - p.y) - (a.x - p.x) * (b.y - a.y)) / len;
      if (dist > far) {
        far = dist;
        best = i;
      }
    }
    if (best >= 0 && far > epsilon) {
      keep[best] = true;
      stack.push([lo, best], [best, hi]);
    }
  }
  return points.filter((_, i) => keep[i]);
}

/** Smooths a polyline into curves through its points (Catmull-Rom as béziers). */
export function smooth(points: Pt[]): Anchor[] {
  if (points.length < 3) return points.map((p) => ({ ...p }));
  return points.map((p, i) => {
    const prev = points[Math.max(0, i - 1)]!;
    const next = points[Math.min(points.length - 1, i + 1)]!;
    const tx = (next.x - prev.x) / 6;
    const ty = (next.y - prev.y) / 6;
    const a: Anchor = { ...p };
    if (i > 0) a.in = { x: p.x - tx, y: p.y - ty };
    if (i < points.length - 1) a.out = { x: p.x + tx, y: p.y + ty };
    return a;
  });
}

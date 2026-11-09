// Draws design nodes as SVG. The same components draw the live canvas and
// the exported SVG, PNG and PDF, so what you see is what you export.

import { createContext, memo, useContext, type ReactNode } from "react";
import { localMatrix, multiply, type Matrix, transformAttr } from "./geometry";
import { type DesignNode, type Effect, type Paint, type Stroke, splitColor } from "./model";
import { cssFont, displayText, layoutText, sizeOf } from "./text";
import { masterIn } from "./tokens";

export interface RenderEnv {
  /** Prefix for the ids of gradients, clips and filters, unique per drawing. */
  prefix: string;
  /** Components by id, for instances. */
  components: Map<string, DesignNode>;
  /** Scroll offsets of scrolling frames, in a presentation. */
  scroll?: Record<string, { x: number; y: number }>;
  /** An image's src as the drawing can load it. */
  image: (src: string) => string;
}

const defaultEnv: RenderEnv = { prefix: "d", components: new Map(), image: (s) => s };
export const RenderContext = createContext<RenderEnv>(defaultEnv);

const r3 = (v: number) => Math.round(v * 1000) / 1000;

const visibleFills = (n: DesignNode) => (n.fills ?? []).filter((f) => f.visible !== false);
const visibleStrokes = (n: DesignNode) => (n.strokes ?? []).filter((s) => s.visible !== false && s.width > 0);
const visibleEffects = (n: DesignNode) => (n.effects ?? []).filter((e) => e.visible !== false);

function radii(n: DesignNode, w: number, h: number): [number, number, number, number] {
  const max = Math.min(w, h) / 2;
  const r = n.radius ?? 0;
  const all = typeof r === "number" ? [r, r, r, r] : r;
  return all.map((v) => Math.max(0, Math.min(max, v))) as [number, number, number, number];
}

/** A rectangle with four corner radii as a path. */
export function roundedRectPath(x: number, y: number, w: number, h: number, [tl, tr, br, bl]: [number, number, number, number]): string {
  return [
    `M${r3(x + tl)} ${r3(y)}`,
    `H${r3(x + w - tr)}`,
    tr ? `A${r3(tr)} ${r3(tr)} 0 0 1 ${r3(x + w)} ${r3(y + tr)}` : "",
    `V${r3(y + h - br)}`,
    br ? `A${r3(br)} ${r3(br)} 0 0 1 ${r3(x + w - br)} ${r3(y + h)}` : "",
    `H${r3(x + bl)}`,
    bl ? `A${r3(bl)} ${r3(bl)} 0 0 1 ${r3(x)} ${r3(y + h - bl)}` : "",
    `V${r3(y + tl)}`,
    tl ? `A${r3(tl)} ${r3(tl)} 0 0 1 ${r3(x + tl)} ${r3(y)}` : "",
    "Z",
  ].join("");
}

export function polygonPoints(sides: number, w: number, h: number, inner?: number): string {
  const pts: string[] = [];
  const count = inner === undefined ? sides : sides * 2;
  for (let i = 0; i < count; i++) {
    const a = -Math.PI / 2 + (i * 2 * Math.PI) / count;
    const k = inner !== undefined && i % 2 === 1 ? inner : 1;
    pts.push(`${r3(w / 2 + Math.cos(a) * (w / 2) * k)},${r3(h / 2 + Math.sin(a) * (h / 2) * k)}`);
  }
  return pts.join(" ");
}

/** Scales SVG path data by sx, sy, so strokes keep their width. */
export function scalePath(d: string, sx: number, sy: number): string {
  if (sx === 1 && sy === 1) return d;
  const tokens = d.match(/[a-zA-Z]|-?(?:\d+\.?\d*|\.\d+)(?:e[-+]?\d+)?/g) ?? [];
  const out: string[] = [];
  let cmd = "";
  let i = 0;
  const args: Record<string, number> = { m: 2, l: 2, h: 1, v: 1, c: 6, s: 4, q: 4, t: 2, a: 7, z: 0 };
  while (i < tokens.length) {
    const tok = tokens[i]!;
    if (/[a-zA-Z]/.test(tok)) {
      cmd = tok;
      out.push(tok);
      i++;
      if (cmd.toLowerCase() === "z") continue;
    }
    const lower = cmd.toLowerCase();
    const n = args[lower] ?? 0;
    if (n === 0) {
      i++;
      continue;
    }
    const nums = tokens.slice(i, i + n).map(Number);
    if (nums.length < n || nums.some((x) => Number.isNaN(x))) break;
    i += n;
    let scaled: number[];
    if (lower === "h") scaled = [nums[0]! * sx];
    else if (lower === "v") scaled = [nums[0]! * sy];
    else if (lower === "a") scaled = [nums[0]! * sx, nums[1]! * sy, nums[2]!, nums[3]!, nums[4]!, nums[5]! * sx, nums[6]! * sy];
    else scaled = nums.map((v, k) => v * (k % 2 === 0 ? sx : sy));
    out.push(scaled.map(r3).join(" "));
  }
  return out.join(" ");
}

function paintAttr(paint: Paint, id: string): { fill: string; fillOpacity?: number } {
  if (paint.type === "image") return paint.opacity !== undefined && paint.opacity < 1 ? { fill: `url(#${id})`, fillOpacity: paint.opacity } : { fill: `url(#${id})` };
  if (paint.type === "solid") {
    const { rgb, alpha } = splitColor(paint.color);
    const o = alpha * (paint.opacity ?? 1);
    return o < 1 ? { fill: rgb, fillOpacity: r3(o) } : { fill: rgb };
  }
  return paint.opacity !== undefined && paint.opacity < 1 ? { fill: `url(#${id})`, fillOpacity: paint.opacity } : { fill: `url(#${id})` };
}

function Gradient({ paint, id, w, h }: { paint: Paint; id: string; w: number; h: number }) {
  const env = useContext(RenderContext);
  if (paint.type === "solid") return null;
  if (paint.type === "image") {
    const ratio = paint.fit === "stretch" ? "none" : paint.fit === "contain" ? "xMidYMid meet" : "xMidYMid slice";
    return (
      <pattern id={id} patternUnits="userSpaceOnUse" x={0} y={0} width={r3(w)} height={r3(h)}>
        <image href={env.image(paint.src)} width={r3(w)} height={r3(h)} preserveAspectRatio={ratio} />
      </pattern>
    );
  }
  const stops = paint.stops.map((s, i) => {
    const { rgb, alpha } = splitColor(s.color);
    return <stop key={i} offset={r3(s.offset)} stopColor={rgb} stopOpacity={alpha < 1 ? r3(alpha) : undefined} />;
  });
  if (paint.type === "radial") return <radialGradient id={id} cx="0.5" cy="0.5" r="0.5">{stops}</radialGradient>;
  // Angle 0 runs left to right, 90 top to bottom, in the box's own space.
  const a = ((paint.angle ?? 90) * Math.PI) / 180;
  const dx = Math.cos(a) / 2;
  const dy = Math.sin(a) / 2;
  return (
    <linearGradient id={id} x1={r3(0.5 - dx)} y1={r3(0.5 - dy)} x2={r3(0.5 + dx)} y2={r3(0.5 + dy)}>
      {stops}
    </linearGradient>
  );
}

function EffectFilter({ effects, id }: { effects: Effect[]; id: string }) {
  const parts: ReactNode[] = [];
  const merge: string[] = [];
  let blur = 0;
  effects.forEach((e, i) => {
    if (e.type === "blur") {
      blur = Math.max(blur, e.blur);
      return;
    }
    const { rgb, alpha } = splitColor(e.color ?? "#00000040");
    const k = `${id}-${i}`;
    if (e.type === "drop-shadow") {
      const spread = e.spread ?? 0;
      parts.push(
        spread > 0 ? <feMorphology key={`${k}m`} in="SourceAlpha" operator="dilate" radius={spread} result={`${k}a`} /> : spread < 0 ? <feMorphology key={`${k}m`} in="SourceAlpha" operator="erode" radius={-spread} result={`${k}a`} /> : null,
        <feGaussianBlur key={`${k}b`} in={spread ? `${k}a` : "SourceAlpha"} stdDeviation={r3(e.blur / 2)} />,
        <feOffset key={`${k}o`} dx={e.x ?? 0} dy={e.y ?? 0} result={`${k}off`} />,
        <feFlood key={`${k}f`} floodColor={rgb} floodOpacity={r3(alpha)} />,
        <feComposite key={`${k}c`} in2={`${k}off`} operator="in" result={k} />,
      );
      merge.push(k);
    } else {
      parts.push(
        <feComponentTransfer key={`${k}i`} in="SourceAlpha" result={`${k}inv`}>
          <feFuncA type="table" tableValues="1 0" />
        </feComponentTransfer>,
        <feGaussianBlur key={`${k}b`} in={`${k}inv`} stdDeviation={r3(e.blur / 2)} />,
        <feOffset key={`${k}o`} dx={e.x ?? 0} dy={e.y ?? 0} result={`${k}off`} />,
        <feFlood key={`${k}f`} floodColor={rgb} floodOpacity={r3(alpha)} />,
        <feComposite key={`${k}c`} in2={`${k}off`} operator="in" result={`${k}col`} />,
        <feComposite key={`${k}c2`} in={`${k}col`} in2="SourceAlpha" operator="in" result={k} />,
      );
    }
  });
  const inner = effects.map((e, i) => (e.type === "inner-shadow" ? `${id}-${i}` : "")).filter(Boolean);
  const source = blur > 0 ? `${id}-blurred` : "SourceGraphic";
  return (
    <filter id={id} x="-50%" y="-50%" width="200%" height="200%" colorInterpolationFilters="sRGB">
      {parts}
      {blur > 0 && <feGaussianBlur in="SourceGraphic" stdDeviation={r3(blur / 2)} result={`${id}-blurred`} />}
      <feMerge>
        {merge.map((m) => (
          <feMergeNode key={m} in={m} />
        ))}
        <feMergeNode in={source} />
        {inner.map((m) => (
          <feMergeNode key={m} in={m} />
        ))}
      </feMerge>
    </filter>
  );
}

/** The outline of a shape, inset or outset by `by`, as an SVG element maker. */
function shapeOf(n: DesignNode, w: number, h: number, by = 0): ((props: Record<string, unknown>) => ReactNode) | null {
  switch (n.type) {
    case "rect":
    case "frame":
    case "component":
    case "instance":
    case "image": {
      const x = -by;
      const y = -by;
      const ww = Math.max(0, w + 2 * by);
      const hh = Math.max(0, h + 2 * by);
      const rr = radii(n, w, h).map((v) => Math.max(0, v + (v > 0 ? by : 0))) as [number, number, number, number];
      if (rr.every((v) => v === rr[0])) return (p) => <rect x={r3(x)} y={r3(y)} width={r3(ww)} height={r3(hh)} rx={rr[0] ? r3(rr[0]) : undefined} {...p} />;
      return (p) => <path d={roundedRectPath(x, y, ww, hh, rr)} {...p} />;
    }
    case "ellipse":
      return (p) => <ellipse cx={r3(w / 2)} cy={r3(h / 2)} rx={r3(Math.max(0, w / 2 + by))} ry={r3(Math.max(0, h / 2 + by))} {...p} />;
    case "line":
      return (p) => <line x1={0} y1={0} x2={r3(w)} y2={0} {...p} />;
    case "polygon":
      return (p) => <polygon points={polygonPoints(n.sides ?? 3, w, h)} {...p} />;
    case "star":
      return (p) => <polygon points={polygonPoints(n.sides ?? 5, w, h, n.inner ?? 0.38)} {...p} />;
    case "path": {
      const vb = n.view_box ?? [w || 1, h || 1];
      const d = scalePath(n.d ?? "", vb[0] ? w / vb[0] : 1, vb[1] ? h / vb[1] : 1);
      return (p) => <path d={d} {...p} />;
    }
    default:
      return null;
  }
}

function strokeProps(s: Stroke) {
  const { rgb, alpha } = splitColor(s.color);
  const o = alpha * (s.opacity ?? 1);
  return {
    fill: "none",
    stroke: rgb,
    strokeWidth: r3(s.width),
    ...(o < 1 ? { strokeOpacity: r3(o) } : {}),
    ...(s.dash?.length ? { strokeDasharray: s.dash.join(" ") } : {}),
  };
}

/** Where a stroke sits: inside and outside strokes move the outline by half the width. */
const strokeOffset = (n: DesignNode, s: Stroke) => {
  const closed = n.type === "rect" || n.type === "frame" || n.type === "component" || n.type === "instance" || n.type === "image" || n.type === "ellipse";
  if (!closed) return 0;
  return s.align === "inside" ? -s.width / 2 : s.align === "outside" ? s.width / 2 : 0;
};

function TextBody({ n, fill }: { n: DesignNode; fill: Record<string, unknown> }) {
  const layout = layoutText(n);
  const size = sizeOf(n);
  const anchor = n.align === "center" ? "middle" : n.align === "right" ? "end" : "start";
  const x = n.align === "center" ? size.width / 2 : n.align === "right" ? size.width : 0;
  const total = layout.lines.length * layout.lineHeight;
  const top = n.valign === "middle" ? (size.height - total) / 2 : n.valign === "bottom" ? size.height - total : 0;
  const f = n.font!;
  return (
    <text
      x={r3(x)}
      textAnchor={anchor}
      fontFamily={cssFont(n).replace(/^.*?px /, "")}
      fontSize={f.size}
      fontWeight={f.weight}
      fontStyle={f.style === "italic" ? "italic" : undefined}
      letterSpacing={n.letter_spacing || undefined}
      textDecoration={n.decoration === "underline" ? "underline" : n.decoration === "strike" ? "line-through" : undefined}
      style={{ whiteSpace: "pre" }}
      {...fill}
    >
      {layout.lines.map((line, i) => (
        <tspan key={i} x={r3(x)} y={r3(top + layout.ascent + i * layout.lineHeight)}>
          {line.text || " "}
        </tspan>
      ))}
    </text>
  );
}

interface ViewProps {
  node: DesignNode;
  /** Text being typed into is drawn by the editor, not here. */
  hidden?: string;
}

function NodeBody({ node: n, hidden }: ViewProps) {
  const env = useContext(RenderContext);
  const { width: w, height: h } = sizeOf(n);
  const id = `${env.prefix}-${n.id}`;
  const fills = visibleFills(n);
  const strokes = visibleStrokes(n);
  const effects = visibleEffects(n);
  const defs: ReactNode[] = [];
  fills.forEach((f, i) => f.type !== "solid" && defs.push(<Gradient key={`g${i}`} paint={f} id={`${id}-f${i}`} w={w} h={h} />));
  if (effects.length) defs.push(<EffectFilter key="fx" effects={effects} id={`${id}-fx`} />);
  const shape = shapeOf(n, w, h);
  const body: ReactNode[] = [];

  if (n.type === "text") {
    if (hidden !== n.id) fills.forEach((f, i) => body.push(<TextBody key={`t${i}`} n={n} fill={paintAttr(f, `${id}-f${i}`)} />));
  } else if (n.type === "image") {
    const clip = `${id}-ic`;
    defs.push(<clipPath key="ic" id={clip}>{shape?.({}) ?? null}</clipPath>);
    body.push(<g key="ibg">{fills.map((f, i) => shape?.({ key: i, ...paintAttr(f, `${id}-f${i}`) }))}</g>);
    const ratio = n.fit === "stretch" ? "none" : n.fit === "contain" ? "xMidYMid meet" : "xMidYMid slice";
    if (n.src) body.push(<image key="img" href={env.image(n.src)} width={r3(w)} height={r3(h)} preserveAspectRatio={ratio} clipPath={`url(#${clip})`} />);
  } else if (shape && n.type !== "group" && n.mask !== true) {
    fills.forEach((f, i) => body.push(shape({ key: `f${i}`, ...paintAttr(f, `${id}-f${i}`) })));
  }

  let kids: ReactNode = null;
  if (n.type === "group" && n.bool_op && n.children?.length) {
    const op = n.bool_op;
    const parts = flatShapes(n.children);
    const shapes = (fill: string, only?: (i: number) => boolean) => parts.map((p, i) => (only && !only(i) ? null : p.shape({ key: i, fill, transform: matrixAttr(p.matrix) })));
    const mid = `${id}-bm`;
    const region = { x: -1, y: -1, width: r3(w + 2), height: r3(h + 2) };
    const mask = (key: string, content: ReactNode) => (
      <mask key={key} id={key} maskUnits="userSpaceOnUse" {...region}>
        {content}
      </mask>
    );
    let paint: (fill: Record<string, unknown>, i: number) => ReactNode;
    if (op === "union") {
      defs.push(mask(mid, <g>{shapes("#fff")}</g>));
      paint = (fill, i) => <rect key={i} width={r3(w)} height={r3(h)} mask={`url(#${mid})`} {...fill} />;
    } else if (op === "subtract") {
      defs.push(mask(mid, <g>{shapes("#fff", (i) => i === 0)}{shapes("#000", (i) => i > 0)}</g>));
      paint = (fill, i) => <rect key={i} width={r3(w)} height={r3(h)} mask={`url(#${mid})`} {...fill} />;
    } else if (op === "exclude") {
      defs.push(
        mask(
          mid,
          <>
            <rect {...region} fill="#000" />
            <g style={{ isolation: "isolate" }}>{parts.map((p, i) => p.shape({ key: i, fill: "#fff", transform: matrixAttr(p.matrix), style: { mixBlendMode: "difference" } }))}</g>
          </>,
        ),
      );
      paint = (fill, i) => <rect key={i} width={r3(w)} height={r3(h)} mask={`url(#${mid})`} {...fill} />;
    } else {
      // Intersect: the fill passes through one mask per shape.
      parts.forEach((p, i) => defs.push(mask(`${mid}${i}`, <g>{p.shape({ fill: "#fff", transform: matrixAttr(p.matrix) })}</g>)));
      paint = (fill, i) =>
        parts.reduceRight<ReactNode>((inner, _p, k) => <g key={k === 0 ? i : undefined} mask={`url(#${mid}${k})`}>{inner}</g>, <rect width={r3(w)} height={r3(h)} {...fill} />);
    }
    fills.forEach((f, i) => body.push(paint(paintAttr(f, `${id}-f${i}`), i)));
  } else if (n.type === "instance") {
    const master = masterIn(n, env.components);
    const shown = n.children ?? master?.children;
    if (master && shown) {
      const sx = master.width > 0 ? w / master.width : 1;
      const sy = master.height > 0 ? h / master.height : 1;
      const inner = (
        <g transform={sx !== 1 || sy !== 1 ? `scale(${r3(sx)} ${r3(sy)})` : undefined}>
          {shown.map((c) => (
            <NodeView key={c.id} node={c} />
          ))}
        </g>
      );
      if (master.clip !== false) {
        defs.push(<clipPath key="cl" id={`${id}-cl`}>{shape?.({})}</clipPath>);
        kids = <g clipPath={`url(#${id}-cl)`}>{inner}</g>;
      } else kids = inner;
    }
  } else if (n.type === "group" && n.bool_op) {
    // An empty boolean group draws nothing.
  } else if (n.children?.length) {
    const off = env.scroll?.[n.id];
    const drawn = maskedKids(n.children, id, defs, hidden);
    const inner = off ? [<g key="scroll" transform={`translate(${-off.x} ${-off.y})`}>{drawn}</g>] : drawn;
    if (n.clip && n.type !== "group" && shape) {
      defs.push(<clipPath key="cl" id={`${id}-cl`}>{shape({})}</clipPath>);
      kids = <g clipPath={`url(#${id}-cl)`}>{inner}</g>;
    } else kids = inner;
  }

  const strokeEls =
    shape && n.type !== "text" && n.type !== "group"
      ? strokes.map((s, i) => {
          const off = strokeOffset(n, s);
          const outline = off ? shapeOf(n, w, h, off) : shape;
          return outline?.({ key: `s${i}`, ...strokeProps(s) });
        })
      : [];

  return (
    <>
      {defs.length > 0 && <defs>{defs}</defs>}
      <g filter={effects.length ? `url(#${id}-fx)` : undefined}>
        {body}
        {kids}
        {strokeEls}
      </g>
    </>
  );
}

const matrixAttr = (m: Matrix) => `matrix(${m.map(r3).join(" ")})`;

interface FlatShape {
  shape: (props: Record<string, unknown>) => ReactNode;
  matrix: Matrix;
}

/** The outlines of every shape in a list, nested groups and frames included, each with its matrix to the list's space. */
function flatShapes(nodes: DesignNode[], parent: Matrix = [1, 0, 0, 1, 0, 0]): FlatShape[] {
  const out: FlatShape[] = [];
  for (const c of nodes) {
    if (c.visible === false) continue;
    const size = sizeOf(c);
    const matrix = multiply(parent, localMatrix({ ...c, ...size }));
    const shape = shapeOf(c, size.width, size.height);
    if (shape && c.type !== "instance") out.push({ shape, matrix });
    else if (c.children) out.push(...flatShapes(c.children, matrix));
  }
  return out;
}

/** Children in order; a shape marked as a mask clips the siblings above it and is not drawn. */
function maskedKids(children: DesignNode[], id: string, defs: ReactNode[], hidden: string | undefined): ReactNode[] {
  const draw = (list: DesignNode[]) => list.map((c) => <NodeView key={c.id} node={c} hidden={hidden} />);
  const at = children.findIndex((c) => c.mask === true && c.visible !== false);
  if (at < 0) return draw(children);
  const mask = children[at]!;
  const clip = `${id}-mk${at}`;
  const parts = flatShapes([{ ...mask, mask: undefined }]);
  defs.push(
    <clipPath key={clip} id={clip}>
      {parts.map((p, i) => p.shape({ key: i, transform: matrixAttr(p.matrix) }))}
    </clipPath>,
  );
  const rest = maskedKids(children.slice(at + 1), `${id}-${at}`, defs, hidden);
  return [...draw(children.slice(0, at)), <g key={clip} clipPath={`url(#${clip})`}>{rest}</g>];
}

/** One node and everything in it. Memoized on the node object: an edit replaces only the nodes on its path. */
export const NodeView = memo(function NodeView({ node, hidden }: ViewProps) {
  if (node.visible === false) return null;
  const size = sizeOf(node);
  return (
    <g data-node={node.id} transform={transformAttr({ ...node, ...size })} opacity={node.opacity !== undefined && node.opacity < 1 ? node.opacity : undefined} style={node.blend ? { mixBlendMode: node.blend as never } : undefined}>
      <NodeBody node={node} hidden={hidden} />
    </g>
  );
});

export function displayTextOf(n: DesignNode): string {
  return displayText(n);
}

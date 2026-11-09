// The design document: pages of nodes (frames, shapes, text, images, groups,
// components and their instances), stored as JSON in the project under
// .atlas/design/files/<name>.json. The Go side (internal/design) reads and
// writes the same format, so the agent and the app edit one file. Every
// function here is pure: it returns a new document and never changes the one
// it was given, which is what makes undo, React's memo and the live merge
// with the agent's edits cheap.

export const FORMAT = "atlas-design";
export const VERSION = 1;

export type NodeType = "frame" | "group" | "rect" | "ellipse" | "line" | "polygon" | "star" | "path" | "text" | "image" | "component" | "instance";

export interface GradientStop {
  offset: number;
  color: string;
}

export type Paint =
  | { type: "solid"; color: string; /** A colour variable that sets `color` for the document's active mode. */ variable?: string; opacity?: number; visible?: boolean }
  | { type: "linear" | "radial"; stops: GradientStop[]; angle?: number; opacity?: number; visible?: boolean }
  /** A picture fills the shape: cropped to cover, fitted whole, or stretched. */
  | { type: "image"; src: string; fit?: "cover" | "contain" | "stretch"; opacity?: number; visible?: boolean };

export interface Stroke {
  color: string;
  variable?: string;
  width: number;
  align?: "center" | "inside" | "outside";
  dash?: number[];
  opacity?: number;
  visible?: boolean;
}

export interface Effect {
  type: "drop-shadow" | "inner-shadow" | "blur";
  x?: number;
  y?: number;
  blur: number;
  spread?: number;
  color?: string;
  visible?: boolean;
}

export interface Font {
  family: string;
  size: number;
  weight: number;
  style?: "normal" | "italic";
}

export type Radius = number | [number, number, number, number];

export type Sizing = "fixed" | "hug" | "fill";

/** Auto layout: a frame stacks its children in a row or a column. */
export interface Layout {
  dir: "row" | "column";
  gap: number;
  /** Top, right, bottom, left. */
  pad: [number, number, number, number];
  /** Along the direction. */
  main: "start" | "center" | "end" | "between";
  /** Across the direction. */
  cross: "start" | "center" | "end" | "stretch";
}

/** How a child follows its parent's resizing when the parent has no layout. */
export interface Constraints {
  x: "left" | "right" | "center" | "stretch" | "scale";
  y: "top" | "bottom" | "center" | "stretch" | "scale";
}

export interface DesignNode {
  id: string;
  type: NodeType;
  name: string;
  /** Relative to the parent's top left corner, before rotation. */
  x: number;
  y: number;
  width: number;
  height: number;
  /** Degrees, clockwise, about the node's center. */
  rotation?: number;
  opacity?: number;
  visible?: boolean;
  locked?: boolean;
  blend?: string;
  fills?: Paint[];
  strokes?: Stroke[];
  effects?: Effect[];
  radius?: Radius;
  children?: DesignNode[];
  /** Frames and components: children outside the box are hidden. */
  clip?: boolean;
  /** Frames and components: stack the children. */
  layout?: Layout;
  /** Width and height rules: "hug" fits the content, "fill" takes the share the parent's layout leaves. */
  sizing_x?: Sizing;
  sizing_y?: Sizing;
  /** In a layout parent: stay where x and y say, outside the flow. */
  absolute?: boolean;
  constraints?: Constraints;
  /** Groups: combine the shapes inside into one (union, subtract the rest from the first, intersect, exclude overlaps); the group's fills paint the result. */
  bool_op?: "union" | "subtract" | "intersect" | "exclude";
  /** A shape that clips the siblings above it in the same parent, and is not drawn itself. */
  mask?: boolean;
  /** Frames: scrolls in the presentation when its content is larger than its box. */
  scroll?: "vertical" | "horizontal" | "both";
  /** Top-level frames: where a presentation starts. */
  flow_start?: boolean;
  interactions?: Interaction[];
  // Shared styles, by id in the document's `styles`.
  fill_style?: string;
  text_style?: string;
  effect_style?: string;
  // Components and instances: variants. A component names its set and its property values; an instance asks for values.
  set?: string;
  variant?: Record<string, string>;
  overrides?: Overrides;
  // Text.
  text?: string;
  font?: Font;
  /** Pixels; 0 or absent means 1.2 × the font size. */
  line_height?: number;
  letter_spacing?: number;
  align?: "left" | "center" | "right";
  valign?: "top" | "middle" | "bottom";
  /** "width": one line per paragraph, the box follows the text; "height": wraps at the width, the box grows down; "none": fixed. */
  auto_resize?: "width" | "height" | "none";
  decoration?: "none" | "underline" | "strike";
  transform?: "none" | "upper" | "lower";
  // Images.
  src?: string;
  fit?: "cover" | "contain" | "stretch";
  // Paths: SVG path data drawn in a box of view_box[0] × view_box[1], scaled to the node.
  d?: string;
  view_box?: [number, number];
  // Polygons and stars.
  sides?: number;
  /** A star's inner radius as a share of its outer radius. */
  inner?: number;
  // Instances.
  component_id?: string;
}

/** Shared looks a node points at by id: change the style, every node using it follows. */
export interface Styles {
  fills?: Record<string, { name: string; fills: Paint[] }>;
  text?: Record<string, TextStyle>;
  effects?: Record<string, { name: string; effects: Effect[] }>;
}

export interface TextStyle {
  name: string;
  font: Font;
  line_height?: number;
  letter_spacing?: number;
  align?: "left" | "center" | "right";
  decoration?: "none" | "underline" | "strike";
  transform?: "none" | "upper" | "lower";
}

/** Colour variables with a value per mode (light, dark…); `mode` is the active one. */
export interface Variables {
  modes: string[];
  mode: string;
  items: Record<string, { name: string; values: Record<string, string> }>;
}

export type Trigger = "click" | "hover" | "press" | "drag" | "after" | "key";
export type Transition = "instant" | "dissolve" | "smart" | "slide" | "push" | "move";

/** What happens when a person uses a layer in the presentation. */
export interface Interaction {
  trigger: Trigger;
  action: "navigate" | "overlay" | "close" | "back" | "url" | "scroll_to";
  /** Navigate and overlay: the top-level frame to go to. Scroll to: the layer to scroll into view. */
  to?: string;
  url?: string;
  transition?: Transition;
  direction?: "left" | "right" | "up" | "down";
  /** Milliseconds. */
  duration?: number;
  easing?: "linear" | "ease" | "ease_in" | "ease_out" | "ease_in_out";
  /** After: milliseconds to wait. */
  delay?: number;
  /** Key: the key name, such as "ArrowRight" or "Enter". */
  key?: string;
  /** Overlay: where it opens. */
  position?: "center" | "top" | "bottom" | "left" | "right";
}

/** What an instance changes in its component's layers, by layer id. */
export type Overrides = Record<string, Partial<Pick<DesignNode, "visible" | "opacity" | "fills" | "strokes" | "effects" | "text" | "font" | "radius" | "fill_style" | "text_style" | "effect_style">>>;

export interface Reply {
  author: string;
  text: string;
  created: string;
}

/** A note pinned to a place on a page. The person leaves them; the agent reads and answers them. */
export interface Comment {
  id: string;
  page: string;
  x: number;
  y: number;
  node?: string;
  author: string;
  text: string;
  created: string;
  resolved?: boolean;
  replies?: Reply[];
}

export interface Page {
  id: string;
  name: string;
  background: string;
  children: DesignNode[];
}

export interface DesignDoc {
  format: typeof FORMAT;
  version: number;
  name: string;
  pages: Page[];
  styles?: Styles;
  variables?: Variables;
  comments?: Comment[];
}

export const CONTAINERS: ReadonlySet<NodeType> = new Set(["frame", "group", "component"]);
const TYPES: ReadonlySet<string> = new Set(["frame", "group", "rect", "ellipse", "line", "polygon", "star", "path", "text", "image", "component", "instance"]);

export const isContainer = (n: DesignNode): boolean => CONTAINERS.has(n.type);

const MAX_DEPTH = 64;
const MAX_NODES = 20000;
const LIMIT = 1_000_000;

let counter = 0;
/** A short id, unique enough within a file. */
export function newId(): string {
  counter = (counter + 1) % 1296;
  const rand = Math.floor(Math.random() * 36 ** 6).toString(36).padStart(6, "0");
  return rand + counter.toString(36).padStart(2, "0");
}

const HEX = /^#(?:[0-9a-f]{3}|[0-9a-f]{4}|[0-9a-f]{6}|[0-9a-f]{8})$/i;
export const isColor = (v: unknown): v is string => typeof v === "string" && HEX.test(v);

/** #RGB / #RGBA / #RRGGBB / #RRGGBBAA as [r, g, b, a] with a in 0-1. */
export function parseColor(hex: string): [number, number, number, number] {
  if (!isColor(hex)) return [0, 0, 0, 1];
  let h = hex.slice(1);
  if (h.length <= 4) h = [...h].map((c) => c + c).join("");
  const n = (i: number) => parseInt(h.slice(i, i + 2), 16);
  return [n(0), n(2), n(4), h.length === 8 ? n(6) / 255 : 1];
}

/** The six hex digits of a colour and its alpha, for SVG's fill and fill-opacity. */
export function splitColor(hex: string): { rgb: string; alpha: number } {
  const [r, g, b, a] = parseColor(hex);
  const to = (v: number) => v.toString(16).padStart(2, "0");
  return { rgb: `#${to(r)}${to(g)}${to(b)}`.toUpperCase(), alpha: a };
}

const num = (v: unknown, fallback: number, lo = -LIMIT, hi = LIMIT): number => (typeof v === "number" && Number.isFinite(v) ? Math.min(hi, Math.max(lo, v)) : fallback);
const str = (v: unknown, fallback: string, max = 200): string => (typeof v === "string" ? v.slice(0, max) : fallback);
const color = (v: unknown, fallback: string): string => (isColor(v) ? v.toUpperCase() : fallback);

const refId = (v: unknown): string | null => (typeof v === "string" && /^[\w:-]{1,64}$/.test(v) ? v : null);

function sanitizePaint(raw: unknown): Paint | null {
  if (typeof raw !== "object" || raw === null) return null;
  const r = raw as Record<string, unknown>;
  const common = {
    ...(r.opacity !== undefined ? { opacity: num(r.opacity, 1, 0, 1) } : {}),
    ...(r.visible === false ? { visible: false } : {}),
  };
  if (r.type === "linear" || r.type === "radial") {
    const stops = Array.isArray(r.stops)
      ? r.stops
          .filter((s): s is Record<string, unknown> => typeof s === "object" && s !== null)
          .slice(0, 16)
          .map((s) => ({ offset: num(s.offset, 0, 0, 1), color: color(s.color, "#000000") }))
      : [];
    if (stops.length < 2) return null;
    return { type: r.type, stops, ...(r.angle !== undefined ? { angle: num(r.angle, 0, -360, 360) } : {}), ...common };
  }
  if (r.type === "image" && typeof r.src === "string" && r.src) return { type: "image", src: r.src.slice(0, 16 << 20), ...(r.fit === "contain" || r.fit === "stretch" ? { fit: r.fit } : {}), ...common };
  if (r.type === "solid" || (r.type === undefined && isColor(r.color))) return { type: "solid", color: color(r.color, "#000000"), ...(refId(r.variable) ? { variable: refId(r.variable)! } : {}), ...common };
  return null;
}

function sanitizeStroke(raw: unknown): Stroke | null {
  if (typeof raw !== "object" || raw === null) return null;
  const r = raw as Record<string, unknown>;
  const s: Stroke = { color: color(r.color, "#000000"), width: num(r.width, 1, 0, 1000) };
  if (r.align === "inside" || r.align === "outside" || r.align === "center") s.align = r.align;
  if (Array.isArray(r.dash)) s.dash = r.dash.filter((x): x is number => typeof x === "number" && Number.isFinite(x) && x >= 0).slice(0, 8);
  if (r.opacity !== undefined) s.opacity = num(r.opacity, 1, 0, 1);
  if (r.visible === false) s.visible = false;
  if (refId(r.variable)) s.variable = refId(r.variable)!;
  return s;
}

function sanitizeEffect(raw: unknown): Effect | null {
  if (typeof raw !== "object" || raw === null) return null;
  const r = raw as Record<string, unknown>;
  if (r.type !== "drop-shadow" && r.type !== "inner-shadow" && r.type !== "blur") return null;
  const e: Effect = { type: r.type, blur: num(r.blur, 0, 0, 1000) };
  if (r.type !== "blur") {
    e.x = num(r.x, 0, -10000, 10000);
    e.y = num(r.y, 0, -10000, 10000);
    e.spread = num(r.spread, 0, -1000, 1000);
    e.color = color(r.color, "#00000040");
  }
  if (r.visible === false) e.visible = false;
  return e;
}

const list = <T,>(raw: unknown, each: (x: unknown) => T | null, max = 32): T[] | undefined => {
  if (!Array.isArray(raw)) return undefined;
  return raw.slice(0, max).map(each).filter((x): x is T => x !== null);
};

const TRIGGERS: ReadonlySet<string> = new Set(["click", "hover", "press", "drag", "after", "key"]);
const ACTIONS: ReadonlySet<string> = new Set(["navigate", "overlay", "close", "back", "url", "scroll_to"]);
const TRANSITIONS: ReadonlySet<string> = new Set(["instant", "dissolve", "smart", "slide", "push", "move"]);

function sanitizeInteraction(raw: unknown): Interaction | null {
  if (typeof raw !== "object" || raw === null) return null;
  const r = raw as Record<string, unknown>;
  if (typeof r.trigger !== "string" || !TRIGGERS.has(r.trigger) || typeof r.action !== "string" || !ACTIONS.has(r.action)) return null;
  const out: Interaction = { trigger: r.trigger as Trigger, action: r.action as Interaction["action"] };
  if (refId(r.to)) out.to = refId(r.to)!;
  if (typeof r.url === "string" && /^https?:\/\//i.test(r.url)) out.url = r.url.slice(0, 2000);
  if ((out.action === "navigate" || out.action === "overlay" || out.action === "scroll_to") && !out.to) return null;
  if (out.action === "url" && !out.url) return null;
  if (typeof r.transition === "string" && TRANSITIONS.has(r.transition)) out.transition = r.transition as Transition;
  if (r.direction === "left" || r.direction === "right" || r.direction === "up" || r.direction === "down") out.direction = r.direction;
  if (r.duration !== undefined) out.duration = num(r.duration, 300, 0, 10000);
  if (r.easing === "linear" || r.easing === "ease" || r.easing === "ease_in" || r.easing === "ease_out" || r.easing === "ease_in_out") out.easing = r.easing;
  if (r.delay !== undefined) out.delay = num(r.delay, 0, 0, 60000);
  if (typeof r.key === "string" && r.key) out.key = r.key.slice(0, 20);
  if (r.position === "center" || r.position === "top" || r.position === "bottom" || r.position === "left" || r.position === "right") out.position = r.position;
  return out;
}

function sanitizeVariant(raw: object): Record<string, string> | null {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(raw).slice(0, 16)) if (typeof v === "string") out[k.slice(0, 40)] = v.slice(0, 80);
  return Object.keys(out).length ? out : null;
}

function sanitizeOverrides(raw: object): Overrides | null {
  const out: Overrides = {};
  for (const [id, v] of Object.entries(raw).slice(0, 500)) {
    if (!refId(id) || typeof v !== "object" || v === null) continue;
    const r = v as Record<string, unknown>;
    const o: Overrides[string] = {};
    if (r.visible === false) o.visible = false;
    if (r.visible === true) o.visible = true;
    if (r.opacity !== undefined) o.opacity = num(r.opacity, 1, 0, 1);
    const fills = list(r.fills, sanitizePaint, 16);
    if (fills) o.fills = fills;
    const strokes = list(r.strokes, sanitizeStroke, 8);
    if (strokes) o.strokes = strokes;
    const effects = list(r.effects, sanitizeEffect, 8);
    if (effects) o.effects = effects;
    if (typeof r.text === "string") o.text = r.text.slice(0, 100_000);
    if (typeof r.font === "object" && r.font !== null) {
      const f = r.font as Record<string, unknown>;
      const font: Partial<Font> = {};
      if (typeof f.family === "string") font.family = f.family.slice(0, 120);
      if (f.size !== undefined) font.size = num(f.size, 16, 1, 2000);
      if (f.weight !== undefined) font.weight = Math.round(num(f.weight, 400, 100, 900) / 100) * 100;
      if (f.style === "italic" || f.style === "normal") font.style = f.style;
      o.font = font as Font;
    }
    if (typeof r.radius === "number") o.radius = num(r.radius, 0, 0, LIMIT);
    if (refId(r.fill_style)) o.fill_style = refId(r.fill_style)!;
    if (refId(r.text_style)) o.text_style = refId(r.text_style)!;
    if (refId(r.effect_style)) o.effect_style = refId(r.effect_style)!;
    if (Object.keys(o).length) out[id] = o;
  }
  return Object.keys(out).length ? out : null;
}

function sanitizeStyles(raw: unknown): Styles | undefined {
  if (typeof raw !== "object" || raw === null) return undefined;
  const r = raw as Record<string, unknown>;
  const out: Styles = {};
  const each = (v: unknown, fn: (id: string, x: Record<string, unknown>) => void) => {
    if (typeof v !== "object" || v === null) return;
    for (const [id, x] of Object.entries(v).slice(0, 500)) if (refId(id) && typeof x === "object" && x !== null) fn(id, x as Record<string, unknown>);
  };
  each(r.fills, (id, x) => {
    const fills = list(x.fills, sanitizePaint, 16);
    if (fills?.length) (out.fills ??= {})[id] = { name: str(x.name, "Fill", 80), fills };
  });
  each(r.effects, (id, x) => {
    const effects = list(x.effects, sanitizeEffect, 8);
    if (effects?.length) (out.effects ??= {})[id] = { name: str(x.name, "Effect", 80), effects };
  });
  each(r.text, (id, x) => {
    const f = (typeof x.font === "object" && x.font !== null ? x.font : {}) as Record<string, unknown>;
    const s: TextStyle = { name: str(x.name, "Text", 80), font: { family: str(f.family, "Inter", 120) || "Inter", size: num(f.size, 16, 1, 2000), weight: Math.round(num(f.weight, 400, 100, 900) / 100) * 100 } };
    if (f.style === "italic") s.font.style = "italic";
    if (x.line_height !== undefined) s.line_height = num(x.line_height, 0, 0, 10000);
    if (x.letter_spacing !== undefined) s.letter_spacing = num(x.letter_spacing, 0, -1000, 1000);
    if (x.align === "center" || x.align === "right") s.align = x.align;
    if (x.decoration === "underline" || x.decoration === "strike") s.decoration = x.decoration;
    if (x.transform === "upper" || x.transform === "lower") s.transform = x.transform;
    (out.text ??= {})[id] = s;
  });
  return out.fills || out.text || out.effects ? out : undefined;
}

function sanitizeComments(raw: unknown, pages: Set<string>): Comment[] {
  if (!Array.isArray(raw)) return [];
  const seen = new Set<string>();
  const out: Comment[] = [];
  for (const v of raw.slice(0, 500)) {
    if (typeof v !== "object" || v === null) continue;
    const r = v as Record<string, unknown>;
    const id = refId(r.id);
    if (!id || seen.has(id) || typeof r.page !== "string" || !pages.has(r.page)) continue;
    seen.add(id);
    const c: Comment = { id, page: r.page, x: num(r.x, 0), y: num(r.y, 0), author: str(r.author, "person", 80).trim() || "person", text: str(r.text, "", 4000), created: str(r.created, "", 40) };
    if (refId(r.node)) c.node = refId(r.node)!;
    if (r.resolved === true) c.resolved = true;
    if (Array.isArray(r.replies)) {
      const replies = r.replies
        .slice(0, 200)
        .filter((x): x is Record<string, unknown> => typeof x === "object" && x !== null && typeof x.text === "string" && x.text.length > 0)
        .map((x) => ({ author: str(x.author, "person", 80).trim() || "person", text: str(x.text, "", 4000), created: str(x.created, "", 40) }));
      if (replies.length) c.replies = replies;
    }
    out.push(c);
  }
  return out;
}

function sanitizeVariables(raw: unknown): Variables | undefined {
  if (typeof raw !== "object" || raw === null) return undefined;
  const r = raw as Record<string, unknown>;
  const modes = Array.isArray(r.modes) ? [...new Set(r.modes.filter((m): m is string => typeof m === "string" && m.length > 0).map((m) => m.slice(0, 40)))].slice(0, 8) : [];
  if (modes.length === 0) modes.push("Default");
  const items: Variables["items"] = {};
  if (typeof r.items === "object" && r.items !== null) {
    for (const [id, v] of Object.entries(r.items).slice(0, 500)) {
      if (!refId(id) || typeof v !== "object" || v === null) continue;
      const x = v as Record<string, unknown>;
      const values: Record<string, string> = {};
      const given = (typeof x.values === "object" && x.values !== null ? x.values : {}) as Record<string, unknown>;
      for (const m of modes) values[m] = color(given[m], "#000000");
      items[id] = { name: str(x.name, id, 80), values };
    }
  }
  return { modes, mode: typeof r.mode === "string" && modes.includes(r.mode) ? r.mode : modes[0]!, items };
}

/** A pad given as one number, two (vertical, horizontal) or four becomes four. */
export function padOf(raw: unknown): [number, number, number, number] {
  const v = (x: unknown) => num(x, 0, 0, LIMIT);
  if (typeof raw === "number") return [v(raw), v(raw), v(raw), v(raw)];
  if (Array.isArray(raw) && raw.length === 2) return [v(raw[0]), v(raw[1]), v(raw[0]), v(raw[1])];
  if (Array.isArray(raw) && raw.length === 4) return [v(raw[0]), v(raw[1]), v(raw[2]), v(raw[3])];
  return [0, 0, 0, 0];
}

function sanitizeLayout(raw: unknown): Layout | null {
  if (typeof raw !== "object" || raw === null) return null;
  const r = raw as Record<string, unknown>;
  return {
    dir: r.dir === "row" ? "row" : "column",
    gap: num(r.gap, 0, 0, LIMIT),
    pad: padOf(r.pad),
    main: r.main === "center" || r.main === "end" || r.main === "between" ? r.main : "start",
    cross: r.cross === "center" || r.cross === "end" || r.cross === "stretch" ? r.cross : "start",
  };
}

interface SanitizeState {
  seen: Set<string>;
  count: number;
}

function sanitizeNode(raw: unknown, depth: number, state: SanitizeState): DesignNode | null {
  if (typeof raw !== "object" || raw === null || depth > MAX_DEPTH || state.count >= MAX_NODES) return null;
  const r = raw as Record<string, unknown>;
  const type = typeof r.type === "string" && TYPES.has(r.type) ? (r.type as NodeType) : null;
  if (!type) return null;
  state.count++;
  let id = typeof r.id === "string" && /^[\w:-]{1,64}$/.test(r.id) ? r.id : newId();
  if (state.seen.has(id)) id = newId();
  state.seen.add(id);
  const n: DesignNode = {
    id,
    type,
    name: str(r.name, defaultName(type)),
    x: num(r.x, 0),
    y: num(r.y, 0),
    width: num(r.width, type === "text" ? 0 : 100, 0),
    height: num(r.height, type === "line" ? 0 : type === "text" ? 0 : 100, 0),
  };
  if (r.rotation !== undefined && num(r.rotation, 0) !== 0) n.rotation = ((num(r.rotation, 0, -3600, 3600) % 360) + 360) % 360;
  if (r.opacity !== undefined) n.opacity = num(r.opacity, 1, 0, 1);
  if (r.visible === false) n.visible = false;
  if (r.locked === true) n.locked = true;
  if (typeof r.blend === "string" && /^[a-z-]{1,20}$/.test(r.blend) && r.blend !== "normal") n.blend = r.blend;
  const fills = list(r.fills, sanitizePaint, 16);
  if (fills) n.fills = fills;
  const strokes = list(r.strokes, sanitizeStroke, 8);
  if (strokes) n.strokes = strokes;
  const effects = list(r.effects, sanitizeEffect, 8);
  if (effects) n.effects = effects;
  if (typeof r.radius === "number") n.radius = num(r.radius, 0, 0, LIMIT);
  else if (Array.isArray(r.radius) && r.radius.length === 4) n.radius = r.radius.map((v) => num(v, 0, 0, LIMIT)) as [number, number, number, number];
  if (CONTAINERS.has(type)) {
    n.children = Array.isArray(r.children) ? r.children.map((c) => sanitizeNode(c, depth + 1, state)).filter((c): c is DesignNode => c !== null) : [];
    if (type !== "group") n.clip = r.clip !== false;
  }
  if (type === "frame" || type === "component") {
    const l = sanitizeLayout(r.layout);
    if (l) n.layout = l;
  }
  if (r.sizing_x === "hug" || r.sizing_x === "fill") n.sizing_x = r.sizing_x;
  if (r.sizing_y === "hug" || r.sizing_y === "fill") n.sizing_y = r.sizing_y;
  if (r.absolute === true) n.absolute = true;
  if (type === "group" && (r.bool_op === "union" || r.bool_op === "subtract" || r.bool_op === "intersect" || r.bool_op === "exclude")) n.bool_op = r.bool_op;
  if (r.mask === true && type !== "text") n.mask = true;
  if ((type === "frame" || type === "component") && (r.scroll === "vertical" || r.scroll === "horizontal" || r.scroll === "both")) n.scroll = r.scroll;
  if (r.flow_start === true && type === "frame") n.flow_start = true;
  if (Array.isArray(r.interactions)) {
    const list = r.interactions.slice(0, 16).map(sanitizeInteraction).filter((x): x is Interaction => x !== null);
    if (list.length) n.interactions = list;
  }
  if (typeof r.constraints === "object" && r.constraints !== null) {
    const c = r.constraints as Record<string, unknown>;
    const x = c.x === "right" || c.x === "center" || c.x === "stretch" || c.x === "scale" ? c.x : "left";
    const y = c.y === "bottom" || c.y === "center" || c.y === "stretch" || c.y === "scale" ? c.y : "top";
    if (x !== "left" || y !== "top") n.constraints = { x, y };
  }
  if (type === "text") {
    n.text = str(r.text, "", 100_000);
    const f = (typeof r.font === "object" && r.font !== null ? r.font : {}) as Record<string, unknown>;
    n.font = { family: str(f.family, "Inter", 120) || "Inter", size: num(f.size, 16, 1, 2000), weight: Math.round(num(f.weight, 400, 100, 900) / 100) * 100 };
    if (f.style === "italic") n.font.style = "italic";
    if (r.line_height !== undefined) n.line_height = num(r.line_height, 0, 0, 10000);
    if (r.letter_spacing !== undefined) n.letter_spacing = num(r.letter_spacing, 0, -1000, 1000);
    if (r.align === "center" || r.align === "right") n.align = r.align;
    if (r.valign === "middle" || r.valign === "bottom") n.valign = r.valign;
    n.auto_resize = r.auto_resize === "height" || r.auto_resize === "none" ? r.auto_resize : "width";
    if (r.decoration === "underline" || r.decoration === "strike") n.decoration = r.decoration;
    if (r.transform === "upper" || r.transform === "lower") n.transform = r.transform;
    if (!n.fills) n.fills = [{ type: "solid", color: "#000000" }];
  }
  if (type === "image") {
    n.src = str(r.src, "", 16 << 20);
    n.fit = r.fit === "contain" || r.fit === "stretch" ? r.fit : "cover";
  }
  if (type === "path") {
    n.d = str(r.d, "", 1 << 20);
    const vb = Array.isArray(r.view_box) && r.view_box.length === 2 ? r.view_box : [n.width || 1, n.height || 1];
    n.view_box = [num(vb[0], 1, 0.0001), num(vb[1], 1, 0.0001)];
  }
  if (type === "polygon" || type === "star") {
    n.sides = Math.round(num(r.sides, type === "star" ? 5 : 3, 3, 100));
    if (type === "star") n.inner = num(r.inner, 0.38, 0.01, 1);
  }
  if (type === "instance") n.component_id = str(r.component_id, "", 64);
  if (refId(r.fill_style)) n.fill_style = refId(r.fill_style)!;
  if (refId(r.text_style) && type === "text") n.text_style = refId(r.text_style)!;
  if (refId(r.effect_style)) n.effect_style = refId(r.effect_style)!;
  if ((type === "component" || type === "instance") && typeof r.variant === "object" && r.variant !== null) {
    const v = sanitizeVariant(r.variant);
    if (v) n.variant = v;
  }
  if (type === "component" && typeof r.set === "string" && r.set) n.set = r.set.slice(0, 80);
  if (type === "instance" && typeof r.overrides === "object" && r.overrides !== null) {
    const o = sanitizeOverrides(r.overrides);
    if (o) n.overrides = o;
  }
  return n;
}

export function defaultName(type: NodeType): string {
  const names: Record<NodeType, string> = {
    frame: "Frame",
    group: "Group",
    rect: "Rectangle",
    ellipse: "Ellipse",
    line: "Line",
    polygon: "Polygon",
    star: "Star",
    path: "Vector",
    text: "Text",
    image: "Image",
    component: "Component",
    instance: "Instance",
  };
  return names[type];
}

export function emptyDoc(name = "Untitled"): DesignDoc {
  return { format: FORMAT, version: VERSION, name, pages: [{ id: newId(), name: "Page 1", background: "#F5F5F5", children: [] }] };
}

/** A document as read from disk or the network, with anything unexpected dropped or repaired. */
export function sanitizeDoc(raw: unknown): DesignDoc {
  if (typeof raw !== "object" || raw === null) return emptyDoc();
  const r = raw as Record<string, unknown>;
  const state: SanitizeState = { seen: new Set(), count: 0 };
  const pages: Page[] = [];
  if (Array.isArray(r.pages)) {
    for (const p of r.pages.slice(0, 200)) {
      if (typeof p !== "object" || p === null) continue;
      const pr = p as Record<string, unknown>;
      let id = typeof pr.id === "string" && /^[\w:-]{1,64}$/.test(pr.id) ? pr.id : newId();
      if (state.seen.has(id)) id = newId();
      state.seen.add(id);
      pages.push({
        id,
        name: str(pr.name, `Page ${pages.length + 1}`),
        background: color(pr.background, "#F5F5F5"),
        children: Array.isArray(pr.children) ? pr.children.map((c) => sanitizeNode(c, 0, state)).filter((c): c is DesignNode => c !== null) : [],
      });
    }
  }
  if (pages.length === 0) pages.push(emptyDoc().pages[0]!);
  const out: DesignDoc = { format: FORMAT, version: VERSION, name: str(r.name, "Untitled"), pages };
  const styles = sanitizeStyles(r.styles);
  if (styles) out.styles = styles;
  const variables = sanitizeVariables(r.variables);
  if (variables) out.variables = variables;
  const comments = sanitizeComments(r.comments, new Set(pages.map((p) => p.id)));
  if (comments.length) out.comments = comments;
  return out;
}

/** A node made fresh for one of the drawing tools, at a place on its parent. */
export function createNode(type: NodeType, props: Partial<DesignNode> = {}): DesignNode {
  const base: Record<string, unknown> = { id: newId(), type, name: defaultName(type), x: 0, y: 0, width: 100, height: 100 };
  switch (type) {
    case "frame":
    case "component":
      Object.assign(base, { fills: [{ type: "solid", color: "#FFFFFF" }], children: [] });
      break;
    case "group":
      base.children = [];
      break;
    case "rect":
    case "ellipse":
    case "polygon":
    case "star":
    case "path":
      base.fills = [{ type: "solid", color: "#D9D9D9" }];
      break;
    case "line":
      Object.assign(base, { height: 0, strokes: [{ color: "#000000", width: 1 }] });
      break;
    case "text":
      Object.assign(base, { text: "", font: { family: "Inter", size: 16, weight: 400 }, auto_resize: "width", fills: [{ type: "solid", color: "#000000" }], width: 0, height: 0 });
      break;
  }
  const merged = { ...base, ...props };
  return sanitizeNode(merged, 0, { seen: new Set(), count: 0 })!;
}

// ---------------------------------------------------------------------------
// Tree access and edits.

export interface Located {
  node: DesignNode;
  /** The parent node, or null when the node sits on the page. */
  parent: DesignNode | null;
  index: number;
  /** From the page's child down to the node itself. */
  path: DesignNode[];
}

export function pageOf(doc: DesignDoc, pageId: string): Page {
  return doc.pages.find((p) => p.id === pageId) ?? doc.pages[0]!;
}

export function walk(nodes: DesignNode[], visit: (node: DesignNode, parent: DesignNode | null, path: DesignNode[]) => void | false, parent: DesignNode | null = null, path: DesignNode[] = []): void {
  for (const node of nodes) {
    const here = [...path, node];
    if (visit(node, parent, here) === false) continue;
    if (node.children) walk(node.children, visit, node, here);
  }
}

export function locate(page: Page, id: string): Located | null {
  let found: Located | null = null;
  const search = (nodes: DesignNode[], parent: DesignNode | null, path: DesignNode[]): boolean => {
    for (let i = 0; i < nodes.length; i++) {
      const node = nodes[i]!;
      const here = [...path, node];
      if (node.id === id) {
        found = { node, parent, index: i, path: here };
        return true;
      }
      if (node.children && search(node.children, node, here)) return true;
    }
    return false;
  };
  search(page.children, null, []);
  return found;
}

export function findNode(doc: DesignDoc, id: string): { page: Page; at: Located } | null {
  for (const page of doc.pages) {
    const at = locate(page, id);
    if (at) return { page, at };
  }
  return null;
}

/** Applies `change` to the children list that holds `parentId` (or the page's list for null). */
function editChildren(doc: DesignDoc, pageId: string, parentId: string | null, change: (children: DesignNode[]) => DesignNode[]): DesignDoc {
  const rewrite = (nodes: DesignNode[]): DesignNode[] => {
    let changed = false;
    const out = nodes.map((n) => {
      if (n.id === parentId && n.children) {
        changed = true;
        return { ...n, children: change(n.children) };
      }
      if (n.children) {
        const kids = rewrite(n.children);
        if (kids !== n.children) {
          changed = true;
          return { ...n, children: kids };
        }
      }
      return n;
    });
    return changed ? out : nodes;
  };
  return {
    ...doc,
    pages: doc.pages.map((p) => (p.id !== pageId ? p : { ...p, children: parentId === null ? change(p.children) : rewrite(p.children) })),
  };
}

/** Replaces nodes by id with what `change` returns for them, anywhere in the document. */
export function updateNodes(doc: DesignDoc, ids: Iterable<string>, change: (node: DesignNode) => DesignNode): DesignDoc {
  const want = new Set(ids);
  if (want.size === 0) return doc;
  const rewrite = (nodes: DesignNode[]): DesignNode[] => {
    let changed = false;
    const out = nodes.map((n) => {
      let next = n;
      if (want.has(n.id)) next = change(n);
      if (next.children) {
        const kids = rewrite(next.children);
        if (kids !== next.children) next = { ...next, children: kids };
      }
      if (next !== n) changed = true;
      return next;
    });
    return changed ? out : nodes;
  };
  let touched = false;
  const pages = doc.pages.map((p) => {
    const kids = rewrite(p.children);
    if (kids === p.children) return p;
    touched = true;
    return { ...p, children: kids };
  });
  return touched ? { ...doc, pages } : doc;
}

/** Sets fields on nodes; a node whose fields already hold those values stays the same object. */
export function patchNodes(doc: DesignDoc, ids: Iterable<string>, patch: Partial<DesignNode> | ((n: DesignNode) => Partial<DesignNode>)): DesignDoc {
  return updateNodes(doc, ids, (n) => {
    const p = typeof patch === "function" ? patch(n) : patch;
    const same = (Object.keys(p) as (keyof DesignNode)[]).every((k) => JSON.stringify(n[k]) === JSON.stringify(p[k]));
    return same ? n : { ...n, ...p };
  });
}

export function insertNodes(doc: DesignDoc, pageId: string, parentId: string | null, nodes: DesignNode[], index?: number): DesignDoc {
  return editChildren(doc, pageId, parentId, (children) => {
    const at = index === undefined ? children.length : Math.max(0, Math.min(children.length, index));
    return [...children.slice(0, at), ...nodes, ...children.slice(at)];
  });
}

export function removeNodes(doc: DesignDoc, ids: Iterable<string>): DesignDoc {
  const gone = new Set(ids);
  if (gone.size === 0) return doc;
  const prune = (nodes: DesignNode[]): DesignNode[] => {
    let changed = false;
    const out: DesignNode[] = [];
    for (const n of nodes) {
      if (gone.has(n.id)) {
        changed = true;
        continue;
      }
      if (n.children) {
        const kids = prune(n.children);
        if (kids !== n.children) {
          changed = true;
          out.push({ ...n, children: kids });
          continue;
        }
      }
      out.push(n);
    }
    return changed ? out : nodes;
  };
  return { ...doc, pages: doc.pages.map((p) => ({ ...p, children: prune(p.children) })) };
}

/** A deep copy with fresh ids, for duplicate and paste. */
export function cloneFresh(node: DesignNode, map?: Map<string, string>): DesignNode {
  const id = newId();
  map?.set(node.id, id);
  return { ...node, id, ...(node.children ? { children: node.children.map((c) => cloneFresh(c, map)) } : {}) };
}

/** Ids of the selection without those whose ancestor is selected too. */
export function topmost(page: Page, ids: string[]): string[] {
  const set = new Set(ids);
  return ids.filter((id) => {
    const at = locate(page, id);
    return at !== null && !at.path.slice(0, -1).some((a) => set.has(a.id));
  });
}

/** Every component on every page, for the assets list and instances. */
export function components(doc: DesignDoc): DesignNode[] {
  const out: DesignNode[] = [];
  for (const page of doc.pages) walk(page.children, (n) => void (n.type === "component" && out.push(n)));
  return out;
}

/** The next free name like "Frame 3" among the page's nodes. */
export function nextName(page: Page, base: string): string {
  let most = 0;
  const re = new RegExp(`^${base.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")} (\\d+)$`);
  walk(page.children, (n) => {
    const m = re.exec(n.name);
    if (m) most = Math.max(most, Number(m[1]));
  });
  return `${base} ${most + 1}`;
}

export function countNodes(doc: DesignDoc): number {
  let total = 0;
  for (const p of doc.pages) walk(p.children, () => void total++);
  return total;
}

/** A name that is safe as a file name in .atlas/design/files. */
export function fileSlug(name: string): string {
  const slug = name
    .trim()
    .toLowerCase()
    .normalize("NFKD")
    .replace(/[̀-ͯ]/g, "")
    .replace(/ı/g, "i")
    .replace(/[^a-z0-9._-]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 64);
  return slug || "untitled";
}

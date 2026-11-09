import { type DesignDoc, type DesignNode, type Effect, type Overrides, type Paint, type Stroke, type Styles, type Variables, components } from "./model";

/**
 * Turns a document's shared looks into plain values for drawing: colour
 * variables take the active mode's colour, styles copy their fills, text
 * and effects onto the nodes that use them, and instances get their
 * component's layers with the instance's overrides and variant applied.
 *
 * The stored document never changes; drawing and export use the resolved
 * copy. Nodes that need nothing keep their identity, and resolved nodes are
 * cached, so the canvas redraws only what an edit touched.
 */

const MAX_NESTING = 8;

interface Context {
  styles: Styles | undefined;
  variables: Variables | undefined;
  masters: DesignNode[];
}

let lastContext: Context | null = null;
let cache = new WeakMap<DesignNode, DesignNode>();

const sameContext = (a: Context, b: Context) => a.styles === b.styles && a.variables === b.variables && a.masters.length === b.masters.length && a.masters.every((m, i) => m === b.masters[i]);

/** A colour with its variable resolved to the active mode. */
function variableColor(v: Variables | undefined, id: string | undefined, fallback: string): string {
  const item = id && v ? v.items[id] : undefined;
  return item?.values[v!.mode] ?? fallback;
}

function paints(list: Paint[] | undefined, v: Variables | undefined): Paint[] | undefined {
  if (!list || !v) return list;
  let changed = false;
  const out = list.map((p) => {
    if (p.type !== "solid" || !p.variable) return p;
    const color = variableColor(v, p.variable, p.color);
    if (color === p.color) return p;
    changed = true;
    return { ...p, color };
  });
  return changed ? out : list;
}

function strokes(list: Stroke[] | undefined, v: Variables | undefined): Stroke[] | undefined {
  if (!list || !v) return list;
  let changed = false;
  const out = list.map((s) => {
    if (!s.variable) return s;
    const color = variableColor(v, s.variable, s.color);
    if (color === s.color) return s;
    changed = true;
    return { ...s, color };
  });
  return changed ? out : list;
}

/** The component an instance shows: its own, or the one in the same set that has the requested variant. */
export function masterFor(instance: DesignNode, masters: DesignNode[]): DesignNode | undefined {
  const own = masters.find((m) => m.id === instance.component_id);
  if (!own || !own.set || !instance.variant) return own;
  const want = Object.entries(instance.variant);
  return masters.find((m) => m.set === own.set && want.every(([k, val]) => m.variant?.[k] === val)) ?? own;
}

/** A node with an instance's override for it laid over. */
function overridden(n: DesignNode, over: Overrides | undefined): DesignNode {
  const o = over?.[n.id];
  if (!o) return n;
  const { font, ...rest } = o;
  const next: DesignNode = { ...n, ...rest };
  if (font && n.font) next.font = { ...n.font, ...font };
  return next;
}

function resolveNode(n: DesignNode, ctx: Context, over: Overrides | undefined, depth: number): DesignNode {
  const cached = over ? undefined : cache.get(n);
  if (cached) return cached;
  let out = overridden(n, over);
  const { styles, variables } = ctx;
  if (styles) {
    if (out.fill_style && styles.fills?.[out.fill_style]) out = { ...out, fills: styles.fills[out.fill_style]!.fills };
    if (out.effect_style && styles.effects?.[out.effect_style]) out = { ...out, effects: styles.effects[out.effect_style]!.effects };
    const ts = out.text_style ? styles.text?.[out.text_style] : undefined;
    if (ts && out.type === "text") {
      const { name: _name, font, ...rest } = ts;
      out = { ...out, font, ...rest };
    }
  }
  const fills = paints(out.fills, variables);
  const lines = strokes(out.strokes, variables);
  if (fills !== out.fills || lines !== out.strokes) out = { ...out, fills, strokes: lines };

  if (out.type === "instance" && depth < MAX_NESTING) {
    const master = masterFor(out, ctx.masters);
    if (master?.children) {
      const merged = out.overrides;
      const kids = master.children.map((c) => resolveNode(c, ctx, merged, depth + 1));
      out = { ...out, children: kids };
    }
  } else if (out.children && out.type !== "instance") {
    const kids = out.children.map((c) => resolveNode(c, ctx, over, depth));
    if (kids.some((k, i) => k !== out.children![i])) out = { ...out, children: kids };
  }
  if (!over) cache.set(n, out);
  return out;
}

/** True when a document uses anything this module resolves. */
function usesTokens(doc: DesignDoc, masters: DesignNode[]): boolean {
  return !!doc.styles || !!doc.variables || masters.length > 0;
}

/** The document as it is drawn. Returns `doc` itself when there is nothing to resolve. */
export function resolveTokens(doc: DesignDoc): DesignDoc {
  const masters = components(doc);
  if (!usesTokens(doc, masters)) return doc;
  const ctx: Context = { styles: doc.styles, variables: doc.variables, masters };
  if (!lastContext || !sameContext(lastContext, ctx)) {
    cache = new WeakMap();
    lastContext = ctx;
  }
  let changed = false;
  const pages = doc.pages.map((p) => {
    const kids = p.children.map((n) => resolveNode(n, ctx, undefined, 0));
    if (kids.every((k, i) => k === p.children[i])) return p;
    changed = true;
    return { ...p, children: kids };
  });
  return changed ? { ...doc, pages } : doc;
}

/** Effects and fills as written, for a style created from a node. */
export function styleFrom(n: DesignNode): { fills?: Paint[]; effects?: Effect[] } {
  return { fills: n.fills, effects: n.effects };
}

/** `masterFor` over a map of components by id, as the renderer holds them. */
export function masterIn(instance: DesignNode, byId: Map<string, DesignNode>): DesignNode | undefined {
  const own = instance.component_id ? byId.get(instance.component_id) : undefined;
  if (!own || !own.set || !instance.variant) return own;
  return masterFor(instance, [...byId.values()]);
}

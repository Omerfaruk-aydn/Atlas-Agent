// Editing commands on a document and a selection: group, ungroup, duplicate,
// paste, reorder, components and instances. Each returns the new document
// and what is selected afterwards. Pure functions.

import { apply, applyVector, fitGroups, invert, localMatrix, multiply, parentMatrix, selectionBounds, worldMatrix } from "./geometry";
import { cloneFresh, createNode, type DesignDoc, type DesignNode, insertNodes, locate, nextName, type Page, pageOf, removeNodes, topmost, updateNodes, walk } from "./model";
import { sizeOf } from "./text";

export interface Result {
  doc: DesignDoc;
  selection: string[];
}

/** A node as it would sit on the page: its world placement folded into x, y and rotation. */
function detach(node: DesignNode, path: DesignNode[]): DesignNode {
  const m = multiply(parentMatrix(path), localMatrix(node));
  const center = apply(m, { x: node.width / 2, y: node.height / 2 });
  const angle = (Math.atan2(m[1], m[0]) * 180) / Math.PI;
  const rotation = ((Math.round(angle * 1000) / 1000) % 360 + 360) % 360;
  const out = { ...node, x: center.x - node.width / 2, y: center.y - node.height / 2 };
  if (rotation) out.rotation = rotation;
  else delete out.rotation;
  return out;
}

/** Re-expresses a page-placed node inside a container whose path is given. */
function attach(node: DesignNode, holderPath: DesignNode[]): DesignNode {
  if (holderPath.length === 0) return node;
  const inv = invert(worldMatrix(holderPath));
  const center = apply(inv, { x: node.x + node.width / 2, y: node.y + node.height / 2 });
  const holderAngle = (Math.atan2(worldMatrix(holderPath)[1], worldMatrix(holderPath)[0]) * 180) / Math.PI;
  const rotation = ((((node.rotation ?? 0) - holderAngle) % 360) + 360) % 360;
  const out = { ...node, x: center.x - node.width / 2, y: center.y - node.height / 2 };
  if (Math.abs(rotation) > 1e-6 && Math.abs(rotation - 360) > 1e-6) out.rotation = rotation;
  else delete out.rotation;
  return out;
}

/** Moves nodes into another container (or the page), keeping where they are on the page. */
export function reparent(doc: DesignDoc, pageId: string, ids: string[], parentId: string | null, index?: number): DesignDoc {
  const page = pageOf(doc, pageId);
  const ordered = topmost(page, ids).filter((id) => {
    if (id === parentId) return false;
    if (!parentId) return true;
    const target = locate(page, parentId);
    return !target?.path.some((p) => p.id === id);
  });
  if (ordered.length === 0) return doc;
  const moving = ordered.map((id) => {
    const at = locate(page, id)!;
    return detach(at.node, at.path);
  });
  let out = removeNodes(doc, ordered);
  const holder = parentId ? locate(pageOf(out, pageId), parentId) : null;
  if (parentId && !holder) return doc;
  const placed = moving.map((n) => attach(n, holder?.path ?? []));
  out = insertNodes(out, pageId, parentId, placed, index);
  return fitGroups(out, sizeOf);
}

/** Wraps the selection in a group (or a frame) at the place of its topmost member. */
export function groupSelection(doc: DesignDoc, pageId: string, ids: string[], as: "group" | "frame" = "group"): Result | null {
  const page = pageOf(doc, pageId);
  const top = topmost(page, ids);
  if (top.length === 0) return null;
  const firstAt = locate(page, top[0]!)!;
  const parentPath = firstAt.path.slice(0, -1);
  const parentId = firstAt.parent?.id ?? null;
  // Only siblings of the first can share its group; others come along into it.
  const siblings = parentId ? firstAt.parent!.children! : page.children;
  const indices = top.map((id) => siblings.findIndex((s) => s.id === id)).filter((i) => i >= 0);
  const insertAt = indices.length ? Math.max(...indices) - (indices.length - 1) : siblings.length;
  const bounds = selectionBounds(page, top, sizeOf)!;
  const box = attach({ ...createNode(as), x: bounds.x, y: bounds.y, width: bounds.width, height: bounds.height }, parentPath);
  const container: DesignNode = {
    ...box,
    name: nextName(page, as === "group" ? "Group" : "Frame"),
    ...(as === "frame" ? { fills: [], clip: false } : {}),
    children: [],
  };
  let out = removeNodes(doc, top);
  out = insertNodes(out, pageId, parentId, [container], Math.max(0, insertAt));
  // Children in order of the page's stacking.
  const order = new Map<string, number>();
  let k = 0;
  walk(page.children, (n) => void order.set(n.id, k++));
  const kids = top
    .slice()
    .sort((a, b) => order.get(a)! - order.get(b)!)
    .map((id) => {
      const at = locate(page, id)!;
      return attach(detach(at.node, at.path), [...parentPath, container]);
    });
  out = updateNodes(out, [container.id], (c) => ({ ...c, children: kids }));
  return { doc: fitGroups(out, sizeOf), selection: [container.id] };
}

/** Puts a group's (or frame's) children where it was, and removes it. */
export function ungroup(doc: DesignDoc, pageId: string, ids: string[]): Result | null {
  let out = doc;
  const selection: string[] = [];
  for (const id of ids) {
    const page = pageOf(out, pageId);
    const at = locate(page, id);
    if (!at || !at.node.children || at.node.type === "component") continue;
    const parentPath = at.path.slice(0, -1);
    const kids = at.node.children.map((c) => attach(detach(c, [...at.path, c]), parentPath));
    out = removeNodes(out, [id]);
    out = insertNodes(out, pageId, at.parent?.id ?? null, kids, at.index);
    selection.push(...kids.map((k) => k.id));
  }
  if (out === doc) return null;
  return { doc: fitGroups(out, sizeOf), selection };
}

/** Copies the selection in place (offset by `offset` page units), right above each original. */
export function duplicate(doc: DesignDoc, pageId: string, ids: string[], offset = 0): Result | null {
  const page = pageOf(doc, pageId);
  const top = topmost(page, ids);
  if (top.length === 0) return null;
  let out = doc;
  const selection: string[] = [];
  for (const id of top) {
    const at = locate(pageOf(out, pageId), id);
    if (!at) continue;
    const copy = cloneFresh(at.node);
    const d = applyVector(invert(parentMatrix(at.path)), { x: offset, y: offset });
    copy.x += d.x;
    copy.y += d.y;
    out = insertNodes(out, pageId, at.parent?.id ?? null, [copy], at.index + 1);
    selection.push(copy.id);
  }
  return { doc: fitGroups(out, sizeOf), selection };
}

/** Page-placed copies of the selection, for the clipboard. */
export function copyNodes(page: Page, ids: string[]): DesignNode[] {
  return topmost(page, ids)
    .map((id) => {
      const at = locate(page, id);
      return at ? detach(at.node, at.path) : null;
    })
    .filter((n): n is DesignNode => n !== null);
}

/**
 * Pastes clipboard nodes: into the selected frame when one is selected,
 * else next to the selection's parent, else onto the page. Fresh ids.
 */
export function paste(doc: DesignDoc, pageId: string, clip: DesignNode[], selection: string[], at?: { x: number; y: number }): Result | null {
  if (clip.length === 0) return null;
  const page = pageOf(doc, pageId);
  const sel = selection.length === 1 ? locate(page, selection[0]!) : null;
  let holderPath: DesignNode[] = [];
  if (sel && (sel.node.type === "frame" || sel.node.type === "component")) holderPath = sel.path;
  else if (sel?.parent) holderPath = sel.path.slice(0, -1);
  const fresh = clip.map((n) => cloneFresh(n));
  if (at) {
    const box = fresh.reduce(
      (b, n) => ({ x0: Math.min(b.x0, n.x), y0: Math.min(b.y0, n.y), x1: Math.max(b.x1, n.x + n.width), y1: Math.max(b.y1, n.y + n.height) }),
      { x0: Infinity, y0: Infinity, x1: -Infinity, y1: -Infinity },
    );
    const dx = at.x - (box.x0 + box.x1) / 2;
    const dy = at.y - (box.y0 + box.y1) / 2;
    for (const n of fresh) {
      n.x += dx;
      n.y += dy;
    }
  }
  const placed = fresh.map((n) => attach(n, holderPath));
  const holderId = holderPath.length ? holderPath[holderPath.length - 1]!.id : null;
  const out = insertNodes(doc, pageId, holderId, placed);
  return { doc: fitGroups(out, sizeOf), selection: placed.map((n) => n.id) };
}

export type Reorder = "front" | "back" | "forward" | "backward";

/** Changes where nodes sit among their siblings. */
export function reorder(doc: DesignDoc, pageId: string, ids: string[], how: Reorder): DesignDoc {
  const want = new Set(ids);
  const move = (list: DesignNode[]): DesignNode[] => {
    if (!list.some((n) => want.has(n.id))) return list;
    const picked = list.filter((n) => want.has(n.id));
    const rest = list.filter((n) => !want.has(n.id));
    if (how === "front") return [...rest, ...picked];
    if (how === "back") return [...picked, ...rest];
    const out = list.slice();
    if (how === "forward") {
      for (let i = out.length - 2; i >= 0; i--) if (want.has(out[i]!.id) && !want.has(out[i + 1]!.id)) [out[i], out[i + 1]] = [out[i + 1]!, out[i]!];
    } else {
      for (let i = 1; i < out.length; i++) if (want.has(out[i]!.id) && !want.has(out[i - 1]!.id)) [out[i], out[i - 1]] = [out[i - 1]!, out[i]!];
    }
    return out;
  };
  const deep = (nodes: DesignNode[]): DesignNode[] => {
    const here = move(nodes);
    let changed = here !== nodes;
    const out = here.map((n) => {
      if (!n.children) return n;
      const kids = deep(n.children);
      if (kids === n.children) return n;
      changed = true;
      return { ...n, children: kids };
    });
    return changed ? out : nodes;
  };
  return { ...doc, pages: doc.pages.map((p) => (p.id === pageId ? { ...p, children: deep(p.children) } : p)) };
}

/** Turns one frame or group into a component, or wraps several nodes in a new one. */
export function makeComponent(doc: DesignDoc, pageId: string, ids: string[]): Result | null {
  const page = pageOf(doc, pageId);
  const top = topmost(page, ids);
  if (top.length === 0) return null;
  if (top.length === 1) {
    const at = locate(page, top[0]!)!;
    if (at.node.type === "frame" || at.node.type === "group") {
      const out = updateNodes(doc, [at.node.id], (n) => ({ ...n, type: "component", clip: n.type === "frame" ? n.clip !== false : false, fills: n.fills ?? [] }));
      return { doc: out, selection: [at.node.id] };
    }
    if (at.node.type === "component" || at.node.type === "instance") return null;
  }
  const grouped = groupSelection(doc, pageId, top, "frame");
  if (!grouped) return null;
  const id = grouped.selection[0]!;
  const name = nextName(pageOf(grouped.doc, pageId), "Component");
  const out = updateNodes(grouped.doc, [id], (n) => ({ ...n, type: "component", name, clip: false }));
  return { doc: out, selection: [id] };
}

/** A new instance of a component, placed at a page point (its center), or to the right of the master. */
export function instantiate(doc: DesignDoc, pageId: string, componentId: string, at?: { x: number; y: number }): Result | null {
  let master: DesignNode | null = null;
  let masterPath: DesignNode[] = [];
  for (const p of doc.pages) {
    const found = locate(p, componentId);
    if (found) {
      master = found.node;
      masterPath = found.path;
      break;
    }
  }
  if (!master || master.type !== "component") return null;
  const placed = detach(master, masterPath);
  const x = at ? at.x - master.width / 2 : placed.x + master.width + 40;
  const y = at ? at.y - master.height / 2 : placed.y;
  const inst = createNode("instance", {
    name: master.name,
    component_id: componentId,
    x,
    y,
    width: master.width,
    height: master.height,
    fills: master.fills,
    strokes: master.strokes,
    effects: master.effects,
    radius: master.radius,
  });
  return { doc: insertNodes(doc, pageId, null, [inst]), selection: [inst.id] };
}

/** Turns instances back into plain frames holding a copy of their component's content. */
export function detachInstance(doc: DesignDoc, ids: string[]): DesignDoc {
  const masters = new Map<string, DesignNode>();
  for (const p of doc.pages) walk(p.children, (n) => void (n.type === "component" && masters.set(n.id, n)));
  return updateNodes(doc, ids, (n) => {
    if (n.type !== "instance") return n;
    const master = n.component_id ? masters.get(n.component_id) : undefined;
    const sx = master && master.width > 0 ? n.width / master.width : 1;
    const sy = master && master.height > 0 ? n.height / master.height : 1;
    const kids = (master?.children ?? []).map((c) => {
      const copy = cloneFresh(c);
      return { ...copy, x: c.x * sx, y: c.y * sy, width: c.width * sx, height: c.height * sy };
    });
    const out: DesignNode = { ...n, type: "frame", children: kids, clip: master?.clip !== false };
    delete out.component_id;
    return out;
  });
}

/** Stores the measured size of auto-sized text, so the file says where text really is. */
export function bakeText(doc: DesignDoc): DesignDoc {
  const ids: string[] = [];
  for (const p of doc.pages)
    walk(p.children, (n) => {
      if (n.type !== "text" || n.auto_resize === "none") return;
      const s = sizeOf(n);
      if (Math.abs(s.width - n.width) > 0.01 || Math.abs(s.height - n.height) > 0.01) ids.push(n.id);
    });
  return ids.length ? updateNodes(doc, ids, (n) => ({ ...n, ...sizeOf(n) })) : doc;
}

/** Ids of nodes that are new or differ between two documents, for highlighting the agent's edits. */
export function changedIds(before: DesignDoc, after: DesignDoc): string[] {
  const sig = new Map<string, string>();
  const shallow = (n: DesignNode) => JSON.stringify({ ...n, children: n.children?.map((c) => c.id) });
  for (const p of before.pages) walk(p.children, (n) => void sig.set(n.id, shallow(n)));
  const out: string[] = [];
  for (const p of after.pages) walk(p.children, (n) => void (sig.get(n.id) !== shallow(n) && out.push(n.id)));
  return out;
}

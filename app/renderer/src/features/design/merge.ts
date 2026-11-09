// Three-way merge of two edits of one design: the person's unsaved work and
// whatever the agent saved meanwhile. Each layer is merged field by field, so
// two people (or a person and the agent) can work on different parts of the
// same file at once. Where both changed the same field, `theirs` wins.

import type { Comment, DesignDoc, DesignNode, Page } from "./model";

const same = (a: unknown, b: unknown): boolean => a === b || JSON.stringify(a) === JSON.stringify(b);

/** Takes whichever side moved away from the base; theirs when both did. */
function pick<T>(base: T, mine: T, theirs: T): T {
  if (same(mine, base)) return theirs;
  if (same(theirs, base)) return mine;
  return theirs;
}

function mergeNode(base: DesignNode | undefined, mine: DesignNode, theirs: DesignNode): DesignNode {
  if (same(mine, theirs)) return theirs;
  const out: Record<string, unknown> = {};
  const keys = new Set([...Object.keys(mine), ...Object.keys(theirs), ...(base ? Object.keys(base) : [])]);
  keys.delete("children");
  const b = (base ?? {}) as unknown as Record<string, unknown>;
  const m = mine as unknown as Record<string, unknown>;
  const t = theirs as unknown as Record<string, unknown>;
  for (const k of keys) {
    const v = pick(b[k], m[k], t[k]);
    if (v !== undefined) out[k] = v;
  }
  if (mine.children || theirs.children) out.children = mergeList(base?.children ?? [], mine.children ?? [], theirs.children ?? []);
  return out as unknown as DesignNode;
}

function idsOf(nodes: DesignNode[], into = new Set<string>()): Set<string> {
  for (const n of nodes) {
    into.add(n.id);
    if (n.children) idsOf(n.children, into);
  }
  return into;
}

function mergeList(base: DesignNode[], mine: DesignNode[], theirs: DesignNode[]): DesignNode[] {
  const b = new Map(base.map((n) => [n.id, n]));
  const m = new Map(mine.map((n) => [n.id, n]));
  const known = idsOf(theirs);
  const baseIds = idsOf(base);
  const out: DesignNode[] = [];
  for (const t of theirs) {
    const mn = m.get(t.id);
    const bn = b.get(t.id);
    if (!mn) {
      // Mine removed it: gone, unless theirs changed it meanwhile.
      if (bn && same(bn, t)) continue;
      out.push(t);
      continue;
    }
    out.push(mergeNode(bn, mn, t));
  }
  // Layers mine added, next to the sibling they followed.
  mine.forEach((n, i) => {
    if (known.has(n.id) || baseIds.has(n.id) || out.some((o) => o.id === n.id)) return;
    let at = out.length;
    for (let k = i - 1; k >= 0; k--) {
      const prev = out.findIndex((o) => o.id === mine[k]!.id);
      if (prev >= 0) {
        at = prev + 1;
        break;
      }
    }
    if (i === 0) at = 0;
    out.splice(at, 0, n);
  });
  // Theirs dropped it but mine kept it unchanged: gone. (Handled above by iterating theirs.)
  return out;
}

function mergeComments(base: Comment[] = [], mine: Comment[] = [], theirs: Comment[] = []): Comment[] {
  const m = new Map(mine.map((c) => [c.id, c]));
  const b = new Map(base.map((c) => [c.id, c]));
  const out = theirs.map((t) => {
    const mc = m.get(t.id);
    if (!mc) return b.has(t.id) && same(b.get(t.id), t) ? null : t;
    const merged = pick(b.get(t.id), mc, t);
    // Replies from both sides are kept.
    const replies = [...(t.replies ?? [])];
    for (const r of mc.replies ?? []) if (!replies.some((x) => same(x, r))) replies.push(r);
    return { ...merged, ...(replies.length ? { replies } : {}) };
  });
  const have = new Set(theirs.map((t) => t.id));
  return [...out.filter((c): c is Comment => c !== null), ...mine.filter((c) => !have.has(c.id) && !b.has(c.id))];
}

function mergePage(base: Page | undefined, mine: Page, theirs: Page): Page {
  return { id: theirs.id, name: pick(base?.name, mine.name, theirs.name) as string, background: pick(base?.background, mine.background, theirs.background) as string, children: mergeList(base?.children ?? [], mine.children, theirs.children) };
}

/** `base` is the document both edits started from. */
export function mergeDocs(base: DesignDoc, mine: DesignDoc, theirs: DesignDoc): DesignDoc {
  const bp = new Map(base.pages.map((p) => [p.id, p]));
  const mp = new Map(mine.pages.map((p) => [p.id, p]));
  const pages: Page[] = [];
  for (const t of theirs.pages) {
    const mpage = mp.get(t.id);
    if (!mpage) {
      // Mine deleted the page: gone unless theirs changed it.
      if (bp.has(t.id) && same(bp.get(t.id), t)) continue;
      pages.push(t);
      continue;
    }
    pages.push(mergePage(bp.get(t.id), mpage, t));
  }
  for (const p of mine.pages) if (!theirs.pages.some((t) => t.id === p.id) && !bp.has(p.id)) pages.push(p);
  const out: DesignDoc = { ...theirs, name: pick(base.name, mine.name, theirs.name), pages: pages.length ? pages : theirs.pages };
  const styles = pick(base.styles, mine.styles, theirs.styles);
  const variables = pick(base.variables, mine.variables, theirs.variables);
  if (styles) out.styles = styles;
  else delete out.styles;
  if (variables) out.variables = variables;
  else delete out.variables;
  const comments = mergeComments(base.comments, mine.comments, theirs.comments);
  if (comments.length) out.comments = comments;
  else delete out.comments;
  return out;
}

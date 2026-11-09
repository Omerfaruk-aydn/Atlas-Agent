// The pen, the pencil and point editing: drawing and reshaping vector paths on the canvas.

import { useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import { useDesign } from "../../stores/design";
import { apply, frameAt, invert, localMatrix, multiply, parentMatrix, type Point, toLocal } from "./geometry";
import { locate, pageOf, patchNodes } from "./model";
import { type Anchor, nodePath, pathData, pathFields, simplify, smooth, type Subpath } from "./path";
import { sizeOf } from "./text";

const HIT = 8;
const CLOSE = 9;

const dist = (a: Point, b: Point) => Math.hypot(a.x - b.x, a.y - b.y);

/** Splits the segment between two anchors at t, returning the anchors that replace them (the new one in the middle). */
export function splitSegment(a: Anchor, b: Anchor, t: number): [Anchor, Anchor, Anchor] {
  const lerp = (p: Point, q: Point): Point => ({ x: p.x + (q.x - p.x) * t, y: p.y + (q.y - p.y) * t });
  const p0: Point = a;
  const p1 = a.out ?? a;
  const p2 = b.in ?? b;
  const p3: Point = b;
  if (!a.out && !b.in) {
    const m = lerp(p0, p3);
    return [{ ...a }, { x: m.x, y: m.y }, { ...b }];
  }
  const a1 = lerp(p0, p1);
  const a2 = lerp(p1, p2);
  const a3 = lerp(p2, p3);
  const b1 = lerp(a1, a2);
  const b2 = lerp(a2, a3);
  const c = lerp(b1, b2);
  return [{ ...a, out: a1 }, { x: c.x, y: c.y, in: b1, out: b2 }, { ...b, in: a3 }];
}

/** The point on a segment closest to p, with its position t along it. */
function nearestOnSegment(a: Anchor, b: Anchor, p: Point): { t: number; d: number } {
  let best = { t: 0, d: Infinity };
  for (let i = 0; i <= 40; i++) {
    const t = i / 40;
    const [, mid] = splitSegment(a, b, t);
    const d = dist(mid, p);
    if (d < best.d) best = { t, d };
  }
  return best;
}

type Drag = { kind: "anchor" | "in" | "out"; sub: number; at: number } | null;

export function PathTools() {
  const tool = useDesign((s) => s.tool);
  const editId = useDesign((s) => s.editPath);
  const camera = useDesign((s) => s.camera);
  const doc = useDesign((s) => s.doc);
  const pageId = useDesign((s) => s.pageId);
  const box = useRef<SVGSVGElement>(null);
  const [draft, setDraft] = useState<Anchor[]>([]);
  const [rubber, setRubber] = useState<Point | null>(null);
  const [stroke, setStroke] = useState<Point[]>([]);
  const dragging = useRef<{ index: number } | null>(null);
  const drawing = useRef(false);

  const page = doc ? pageOf(doc, pageId) : null;
  const editing = editId && page ? locate(page, editId) : null;
  const editable = editing && editing.node.type === "path" && !editing.node.rotation ? editing : null;
  const [subs, setSubs] = useState<Subpath[]>([]);
  const [picked, setPicked] = useState<{ sub: number; at: number } | null>(null);
  const drag = useRef<Drag>(null);
  const base = useRef<typeof doc>(null);

  // The path being edited, in its own box.
  useEffect(() => {
    setSubs(editable ? nodePath(editable.node) : []);
    // Re-read when the node changes, from here or from outside.
  }, [editable?.node.id, editable?.node.d, editable?.node.width, editable?.node.height]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => setPicked(null), [editable?.node.id]);

  const matrix = useMemo(() => (editable ? multiply(parentMatrix(editable.path), localMatrix({ ...editable.node, ...sizeOf(editable.node) })) : null), [editable]);
  const toScreen = (p: Point): Point => ({ x: p.x * camera.zoom + camera.x, y: p.y * camera.zoom + camera.y });
  const world = (e: { clientX: number; clientY: number }): Point => {
    const r = box.current!.getBoundingClientRect();
    const cam = useDesign.getState().camera;
    return { x: (e.clientX - r.left - cam.x) / cam.zoom, y: (e.clientY - r.top - cam.y) / cam.zoom };
  };
  const screenOf = (p: Point) => toScreen(p);

  /** Places a new path where it was drawn: in the frame under its first point. */
  const finish = (anchors: Anchor[], closed: boolean) => {
    const s = useDesign.getState();
    const p = s.doc ? pageOf(s.doc, s.pageId) : null;
    if (!p || anchors.length < 2) return;
    const holder = frameAt(p, anchors[0]!, new Set(), sizeOf) ?? [];
    const local = (pt: Point): Point => (holder.length ? toLocal(holder, pt) : pt);
    const mapped = anchors.map((a) => ({ ...local(a), ...(a.in ? { in: local(a.in) } : {}), ...(a.out ? { out: local(a.out) } : {}) }));
    const fields = pathFields({ x: 0, y: 0 }, [{ anchors: mapped, closed }]);
    s.addNode("path", { ...fields, fills: closed ? [{ type: "solid", color: "#D9D9D9" }] : [], strokes: [{ color: "#000000", width: 2, align: "center" }] }, holder.length ? holder[holder.length - 1]!.id : null);
    s.setTool("select");
  };

  // Keys: Enter ends an open path or point editing, Escape cancels, Delete removes the picked point.
  useEffect(() => {
    if (tool !== "pen" && !editable) return;
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement | null;
      if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.tagName === "SELECT" || t.isContentEditable)) return;
      const s = useDesign.getState();
      if (tool === "pen") {
        if (e.key === "Enter") {
          e.preventDefault();
          e.stopPropagation();
          finish(draft, false);
          setDraft([]);
        } else if (e.key === "Escape") {
          e.preventDefault();
          e.stopPropagation();
          if (draft.length) setDraft([]);
          else s.setTool("select");
        }
      } else if (editable) {
        if (e.key === "Enter" || e.key === "Escape") {
          e.preventDefault();
          e.stopPropagation();
          s.setEditPath(null);
        } else if ((e.key === "Delete" || e.key === "Backspace") && picked) {
          e.preventDefault();
          const next = subs.map((sp, i) => (i === picked.sub ? { ...sp, anchors: sp.anchors.filter((_, k) => k !== picked.at) } : sp)).filter((sp) => sp.anchors.length > 1);
          setPicked(null);
          if (next.length === 0) {
            s.setEditPath(null);
            return;
          }
          setSubs(next);
          apply2(next, true);
        }
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  });

  /** Writes edited subpaths (in the node's box) back to the node, as a preview or a commit. */
  const apply2 = (next: Subpath[], commit: boolean) => {
    const s = useDesign.getState();
    if (!editable || !s.doc) return;
    const fields = pathFields(editable.node, next);
    const source = base.current ?? s.doc;
    const out = patchNodes(source, [editable.node.id], fields);
    if (commit) s.commit(out);
    else s.preview(out);
  };

  const down = (e: ReactPointerEvent<SVGSVGElement>) => {
    if (e.button !== 0) return;
    e.stopPropagation();
    e.currentTarget.setPointerCapture(e.pointerId);
    const w = world(e);
    if (tool === "pen") {
      const first = draft[0];
      if (first && draft.length >= 2 && dist(screenOf(first), screenOf(w)) < CLOSE) {
        finish(draft, true);
        setDraft([]);
        return;
      }
      setDraft([...draft, { x: w.x, y: w.y }]);
      dragging.current = { index: draft.length };
      return;
    }
    if (tool === "pencil") {
      drawing.current = true;
      setStroke([w]);
      return;
    }
    if (!editable || !matrix) return;
    const s = useDesign.getState();
    // A handle or an anchor under the pointer starts a drag.
    const sp = { x: e.clientX - box.current!.getBoundingClientRect().left, y: e.clientY - box.current!.getBoundingClientRect().top };
    for (let si = 0; si < subs.length; si++) {
      const a = subs[si]!.anchors;
      for (let ai = 0; ai < a.length; ai++) {
        const pts: [Drag, Point | undefined][] = [
          [{ kind: "in", sub: si, at: ai }, a[ai]!.in],
          [{ kind: "out", sub: si, at: ai }, a[ai]!.out],
          [{ kind: "anchor", sub: si, at: ai }, a[ai]!],
        ];
        for (const [d, p] of pts) {
          if (!p || !d) continue;
          if (dist(toScreen(apply(matrix, p)), sp) <= HIT) {
            if (d.kind === "anchor" && e.altKey) {
              // Alt toggles a corner into a smooth point and back.
              const prev = a[(ai - 1 + a.length) % a.length]!;
              const next = a[(ai + 1) % a.length]!;
              const cur = a[ai]!;
              const dir = { x: (next.x - prev.x) / 6, y: (next.y - prev.y) / 6 };
              const smoothed: Anchor = cur.in || cur.out ? { x: cur.x, y: cur.y } : { x: cur.x, y: cur.y, in: { x: cur.x - dir.x, y: cur.y - dir.y }, out: { x: cur.x + dir.x, y: cur.y + dir.y } };
              const ns = subs.map((sp2, i) => (i === si ? { ...sp2, anchors: sp2.anchors.map((x, k) => (k === ai ? smoothed : x)) } : sp2));
              setSubs(ns);
              apply2(ns, true);
              setPicked({ sub: si, at: ai });
              return;
            }
            drag.current = d;
            setPicked({ sub: si, at: ai });
            base.current = s.doc;
            s.beginGesture();
            return;
          }
        }
      }
    }
    setPicked(null);
  };

  const move = (e: ReactPointerEvent<SVGSVGElement>) => {
    const w = world(e);
    if (tool === "pen") {
      setRubber(w);
      const d = dragging.current;
      if (d) {
        setDraft((list) => {
          const a = list[d.index];
          if (!a) return list;
          const px = dist(screenOf(a), screenOf(w));
          if (px < 3) return list;
          const next = list.slice();
          next[d.index] = { x: a.x, y: a.y, out: { x: w.x, y: w.y }, in: { x: 2 * a.x - w.x, y: 2 * a.y - w.y } };
          return next;
        });
      }
      return;
    }
    if (tool === "pencil") {
      if (drawing.current) setStroke((list) => (dist(screenOf(list[list.length - 1]!), screenOf(w)) > 2 ? [...list, w] : list));
      return;
    }
    const d = drag.current;
    if (!d || !matrix) return;
    const local = apply(invert(matrix), w);
    const next = subs.map((sp, si) =>
      si !== d.sub
        ? sp
        : {
            ...sp,
            anchors: sp.anchors.map((a, ai) => {
              if (ai !== d.at) return a;
              if (d.kind === "anchor") {
                const dx = local.x - a.x;
                const dy = local.y - a.y;
                return { x: local.x, y: local.y, ...(a.in ? { in: { x: a.in.x + dx, y: a.in.y + dy } } : {}), ...(a.out ? { out: { x: a.out.x + dx, y: a.out.y + dy } } : {}) };
              }
              const mirrored = !e.altKey;
              const other = d.kind === "in" ? "out" : "in";
              const patched: Anchor = { ...a, [d.kind]: { x: local.x, y: local.y } };
              if (mirrored && a[other]) patched[other] = { x: 2 * a.x - local.x, y: 2 * a.y - local.y };
              return patched;
            }),
          },
    );
    setSubs(next);
    apply2(next, false);
  };

  const up = (e: ReactPointerEvent<SVGSVGElement>) => {
    e.stopPropagation();
    dragging.current = null;
    if (tool === "pencil" && drawing.current) {
      drawing.current = false;
      const zoom = useDesign.getState().camera.zoom;
      const pts = simplify(stroke, 2 / zoom);
      setStroke([]);
      if (pts.length >= 2) finish(smooth(pts), false);
      return;
    }
    if (drag.current) {
      drag.current = null;
      const s = useDesign.getState();
      s.endGesture();
      base.current = null;
      void s;
    }
  };

  const dbl = (e: ReactPointerEvent<SVGSVGElement> | React.MouseEvent<SVGSVGElement>) => {
    e.stopPropagation();
    if (tool === "pen") {
      // The double click's second press already added a point on top of the first.
      const list = draft.length >= 2 && dist(draft[draft.length - 1]!, draft[draft.length - 2]!) < 1e-6 ? draft.slice(0, -1) : draft;
      finish(list, false);
      setDraft([]);
      return;
    }
    if (!editable || !matrix) return;
    // A double click on a segment adds a point there.
    const local = apply(invert(matrix), world(e));
    let best: { sub: number; at: number; t: number; d: number } | null = null;
    subs.forEach((sp, si) => {
      const n = sp.anchors.length;
      for (let ai = 0; ai < (sp.closed ? n : n - 1); ai++) {
        const hit = nearestOnSegment(sp.anchors[ai]!, sp.anchors[(ai + 1) % n]!, local);
        if (!best || hit.d < best.d) best = { sub: si, at: ai, ...hit };
      }
    });
    const b = best as { sub: number; at: number; t: number; d: number } | null;
    if (!b || b.d * camera.zoom > HIT * 2) return;
    const ns = subs.map((sp, si) => {
      if (si !== b.sub) return sp;
      const n = sp.anchors.length;
      const j = (b.at + 1) % n;
      const [a0, mid, a1] = splitSegment(sp.anchors[b.at]!, sp.anchors[j]!, b.t);
      const anchors = sp.anchors.slice();
      anchors[b.at] = a0;
      anchors[j] = a1;
      anchors.splice(b.at + 1, 0, mid);
      return { ...sp, anchors };
    });
    setSubs(ns);
    apply2(ns, true);
  };

  if (tool !== "pen" && tool !== "pencil" && !editable) return null;

  const penPath = draft.length ? pathData([{ anchors: draft.map((a) => mapAnchor(a, toScreen)), closed: false }]) : "";
  const rubberPath =
    draft.length && rubber
      ? pathData([{ anchors: [mapAnchor({ ...draft[draft.length - 1]!, in: undefined }, toScreen), { ...toScreen(rubber) }], closed: false }])
      : "";

  return (
    <svg ref={box} className="design-pathtools" data-mode={tool === "pen" || tool === "pencil" ? tool : "edit"} onPointerDown={down} onPointerMove={move} onPointerUp={up} onDoubleClick={dbl}>
      {penPath && <path className="design-pen-line" d={penPath} />}
      {rubberPath && <path className="design-pen-rubber" d={rubberPath} />}
      {draft.map((a, i) => {
        const s = toScreen(a);
        return <rect key={i} className="design-anchor" data-first={i === 0 || undefined} x={s.x - 3.5} y={s.y - 3.5} width={7} height={7} />;
      })}
      {stroke.length > 1 && <polyline className="design-pen-line" points={stroke.map((p) => `${toScreen(p).x},${toScreen(p).y}`).join(" ")} />}
      {editable && matrix && (
        <g>
          <path className="design-pen-line" d={pathData(subs.map((s) => ({ closed: s.closed, anchors: s.anchors.map((a) => mapAnchor(a, (p) => toScreen(apply(matrix, p)))) })))} />
          {subs.map((sp, si) =>
            sp.anchors.map((a, ai) => {
              const s = toScreen(apply(matrix, a));
              const on = picked?.sub === si && picked.at === ai;
              return (
                <g key={`${si}-${ai}`}>
                  {on &&
                    (["in", "out"] as const).map((k) => {
                      const h = a[k];
                      if (!h) return null;
                      const hs = toScreen(apply(matrix, h));
                      return (
                        <g key={k}>
                          <line className="design-handle-line" x1={s.x} y1={s.y} x2={hs.x} y2={hs.y} />
                          <circle className="design-handle-dot" cx={hs.x} cy={hs.y} r={4} />
                        </g>
                      );
                    })}
                  <rect className="design-anchor" data-on={on || undefined} x={s.x - 3.5} y={s.y - 3.5} width={7} height={7} />
                </g>
              );
            }),
          )}
        </g>
      )}
    </svg>
  );
}

function mapAnchor(a: Anchor, f: (p: Point) => Point): Anchor {
  return { ...f(a), ...(a.in ? { in: f(a.in) } : {}), ...(a.out ? { out: f(a.out) } : {}) };
}

// The infinite canvas: draws the page, and turns pointer and wheel input
// into selection, moves, resizes, rotation, drawing and panning. Gestures
// preview against the document as it was when they began, so a drag never
// accumulates rounding, and Escape puts everything back.

import { useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import { useApp } from "../../stores/app";
import { type Tool, useDesign } from "../../stores/design";
import {
  angleOf,
  apply,
  applyResize,
  corners,
  frameAt,
  type Guide,
  type Handle,
  handleDir,
  hitPath,
  gridTracks,
  localMatrix,
  marquee,
  type Matrix,
  moveBy,
  multiply,
  type Point,
  parentMatrix,
  pickTarget,
  type Rect,
  resizeNode,
  rotateAround,
  scaleInto,
  selectionBounds,
  snapPoint,
  snapRect,
  snapTargets,
  toLocal,
  nodeBounds,
  worldBounds,
  worldMatrix,
} from "./geometry";
import { components, createNode, defaultName, type DesignDoc, type DesignNode, insertNodes, type LayoutGrid, locate, nextName, type NodeType, type Page, pageOf, topmost } from "./model";
import { reparent } from "./ops";
import { NodeView, RenderContext, type RenderEnv } from "./render";
import { type MenuEntry, RightClick } from "../../primitives/ui";
import { t } from "../../lib/i18n";
import { resolveTokens } from "./tokens";
import { PathTools } from "./PathTools";
import { CommentPins, newCommentAt } from "./Review";
import { cssFont, lineHeightOf, sizeOf } from "./text";

const HANDLES: Handle[] = ["nw", "n", "ne", "e", "se", "s", "sw", "w"];
const DRAG_START = 3;
const SNAP_PX = 6;

const DRAW_TYPES: Partial<Record<Tool, NodeType>> = { frame: "frame", section: "section", slice: "slice", rect: "rect", ellipse: "ellipse", line: "line", polygon: "polygon", star: "star" };

export function imageUrl(src: string): string {
  if (/^(data|blob):/i.test(src)) return src;
  return `/api/workspaces/${encodeURIComponent(useApp.getState().workspace?.id ?? "")}/design/asset?path=${encodeURIComponent(src)}`;
}

type Gesture =
  | { kind: "pan"; start: Point; cam: { x: number; y: number } }
  | { kind: "marquee"; start: Point; add: string[] }
  | { kind: "pending"; start: Point; world: Point; ids: string[]; duplicate: boolean }
  | { kind: "move"; start: Point; world: Point; ids: string[]; base: DesignDoc }
  | { kind: "resize"; handle: Handle; ids: string[]; base: DesignDoc; from: Rect }
  | { kind: "rotate"; ids: string[]; base: DesignDoc; center: Point; startAngle: number }
  | { kind: "draw"; type: NodeType; start: Point; id: string | null; base: DesignDoc; parent: DesignNode[] }
  | { kind: "crop"; id: string; start: Point; from: { zoom: number; x: number; y: number } };

/** Width of the rulers, in screen pixels. */
const RULER = 20;

/** The colour a layer shows: its first visible solid fill, else its first stroke. */
function sampleColor(n: DesignNode, stroke: boolean): string | null {
  const fill = n.fills?.find((f) => f.visible !== false && f.type === "solid");
  const line = n.strokes?.find((s) => s.visible !== false && s.width > 0);
  if (stroke) return line?.color ?? null;
  return (fill && fill.type === "solid" ? fill.color : null) ?? line?.color ?? null;
}

/**
 * Pulls a guide out of a ruler (index -1) or moves one that exists. Dropped
 * back on a ruler, it is removed. The whole drag is one undo step.
 */
function dragGuide(host: HTMLElement, axis: "x" | "y", index: number) {
  const s0 = useDesign.getState();
  const page = s0.doc ? pageOf(s0.doc, s0.pageId) : null;
  if (!page) return;
  const at = index < 0 ? (page.guides?.length ?? 0) : index;
  s0.beginGesture();
  const place = (ev: PointerEvent) => {
    const rect = host.getBoundingClientRect();
    const cam = useDesign.getState().camera;
    const sx = ev.clientX - rect.left;
    const sy = ev.clientY - rect.top;
    const over = axis === "x" ? sx < RULER : sy < RULER;
    const pos = axis === "x" ? (sx - cam.x) / cam.zoom : (sy - cam.y) / cam.zoom;
    return { over, pos: Math.round(pos) };
  };
  const move = (ev: PointerEvent) => {
    const { over, pos } = place(ev);
    // Over the ruler a guide is on its way out: it is not drawn there.
    useDesign.getState().setGuide(at, over ? null : { axis, at: pos }, true);
    if (over) return;
  };
  const up = (ev: PointerEvent) => {
    window.removeEventListener("pointermove", move);
    window.removeEventListener("pointerup", up);
    const { over, pos } = place(ev);
    const st = useDesign.getState();
    const now = st.doc ? pageOf(st.doc, st.pageId).guides?.length ?? 0 : 0;
    if (over) {
      if (now > at) st.setGuide(at, null, true);
    } else st.setGuide(at, { axis, at: pos }, true);
    st.endGesture();
  };
  window.addEventListener("pointermove", move);
  window.addEventListener("pointerup", up);
}

function cursorFor(handle: Handle, rotation: number): string {
  const order = ["n", "ne", "e", "se", "s", "sw", "w", "nw"];
  const names = ["ns-resize", "nesw-resize", "ew-resize", "nwse-resize"];
  const i = (order.indexOf(handle) + Math.round(rotation / 45) + 8) % 8;
  return names[i % 4]!;
}

/** The corners of what is selected, on screen: a rotated node keeps its angle. */
function selectionFrame(page: Page, ids: string[]): { pts: Point[]; rotation: number; box: Rect } | null {
  if (ids.length === 1) {
    const at = locate(page, ids[0]!);
    if (!at) return null;
    const size = sizeOf(at.node);
    const m = multiply(parentMatrix(at.path), localMatrix({ ...at.node, ...size }));
    const pts = corners(m, size.width, size.height);
    const rotation = (Math.atan2(m[1], m[0]) * 180) / Math.PI;
    return { pts, rotation, box: worldBounds(at.path, sizeOf) };
  }
  const box = selectionBounds(page, ids, sizeOf);
  if (!box) return null;
  return { pts: [{ x: box.x, y: box.y }, { x: box.x + box.width, y: box.y }, { x: box.x + box.width, y: box.y + box.height }, { x: box.x, y: box.y + box.height }], rotation: 0, box };
}

export function Canvas() {
  const doc = useDesign((s) => s.doc);
  const pageId = useDesign((s) => s.pageId);
  const selection = useDesign((s) => s.selection);
  const hover = useDesign((s) => s.hover);
  const tool = useDesign((s) => s.tool);
  const camera = useDesign((s) => s.camera);
  const editing = useDesign((s) => s.editing);
  const showGrids = useDesign((s) => s.showGrids);
  const showRulers = useDesign((s) => s.showRulers);
  const cropping = useDesign((s) => s.cropping);
  // Alt held: distances from the selection to the layer under the pointer.
  const [alt, setAlt] = useState(false);
  useEffect(() => {
    const on = (e: KeyboardEvent) => setAlt(e.altKey && !e.ctrlKey && !e.metaKey);
    const off = () => setAlt(false);
    window.addEventListener("keydown", on);
    window.addEventListener("keyup", on);
    window.addEventListener("blur", off);
    return () => {
      window.removeEventListener("keydown", on);
      window.removeEventListener("keyup", on);
      window.removeEventListener("blur", off);
    };
  }, []);
  const flash = useDesign((s) => s.flash);
  const host = useRef<HTMLDivElement>(null);
  const gesture = useRef<Gesture | null>(null);
  const [marqueeRect, setMarquee] = useState<Rect | null>(null);
  const [guides, setGuides] = useState<Guide[]>([]);
  const [size, setSize] = useState({ width: 0, height: 0 });
  const [space, setSpace] = useState(false);
  const [panning, setPanning] = useState(false);
  const fitted = useRef("");
  /** The camera as the last automatic fit left it. */
  const autoCam = useRef<{ x: number; y: number; zoom: number } | null>(null);
  const [ready, setReady] = useState(false);

  const page = doc ? pageOf(doc, pageId) : null;
  const drawn = useMemo(() => (doc ? resolveTokens(doc) : null), [doc]);
  const drawnPage = drawn ? pageOf(drawn, pageId) : page;
  const env = useMemo<RenderEnv>(() => ({ prefix: "c", components: new Map(doc ? components(doc).map((c) => [c.id, c]) : []), image: imageUrl }), [doc]);

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    let last = { width: el.clientWidth, height: el.clientHeight };
    // When the view grows or shrinks (the panel expands), what was in the middle stays there.
    const ro = new ResizeObserver(() => {
      const next = { width: el.clientWidth, height: el.clientHeight };
      if (next.width === last.width && next.height === last.height) return;
      const s = useDesign.getState();
      const cam = s.camera;
      // A view nobody has moved since it was fitted is fitted again; otherwise the middle stays put.
      if (autoCam.current === cam && s.doc) {
        zoomToFit(pageOf(s.doc, s.pageId), next, false);
        autoCam.current = useDesign.getState().camera;
      } else if (last.width && last.height) s.setCamera({ ...cam, x: cam.x + (next.width - last.width) / 2, y: cam.y + (next.height - last.height) / 2 });
      last = next;
      setSize(next);
    });
    ro.observe(el);
    setSize(last);
    return () => ro.disconnect();
  }, []);

  // A page opens fitted to what is on it.
  const file = useDesign((s) => s.file);
  useEffect(() => {
    if (!page || !size.width || fitted.current === `${file}:${page.id}`) return;
    fitted.current = `${file}:${page.id}`;
    zoomToFit(page, size, false);
    autoCam.current = useDesign.getState().camera;
    setReady(true);
  }, [page, size, file]);

  // The space bar pans while held, as in every design tool.
  useEffect(() => {
    const down = (e: KeyboardEvent) => {
      if (e.code === "Space" && !e.repeat && !isTyping(e.target) && host.current?.closest(".design")?.contains(document.activeElement)) {
        e.preventDefault();
        setSpace(true);
      }
    };
    const up = (e: KeyboardEvent) => e.code === "Space" && setSpace(false);
    window.addEventListener("keydown", down);
    window.addEventListener("keyup", up);
    return () => {
      window.removeEventListener("keydown", down);
      window.removeEventListener("keyup", up);
    };
  }, []);

  // Wheel: pan, or zoom about the pointer with ctrl (and pinch). Not passive, so the page never scrolls.
  useEffect(() => {
    const el = host.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const st = useDesign.getState();
      const cam = st.camera;
      const box = el.getBoundingClientRect();
      if (e.altKey && st.cropping && st.doc) {
        // Alt + wheel in crop mode zooms the picture inside its box.
        const at = locate(pageOf(st.doc, st.pageId), st.cropping);
        if (at) {
          const from = { zoom: 1, x: 0, y: 0, ...at.node.crop };
          const zoom = Math.max(1, Math.min(20, from.zoom * Math.exp(-e.deltaY * 0.002)));
          const limit = (zoom - 1) / 2;
          const crop = { zoom, x: Math.max(-limit, Math.min(limit, from.x)), y: Math.max(-limit, Math.min(limit, from.y)) };
          st.patch([st.cropping], () => ({ crop: zoom === 1 ? undefined : crop }));
        }
        return;
      }
      if (e.ctrlKey || e.metaKey) {
        const p = { x: e.clientX - box.left, y: e.clientY - box.top };
        const factor = Math.exp(-e.deltaY * (e.deltaMode === 1 ? 0.05 : 0.0025));
        zoomAbout(p, cam.zoom * factor);
      } else {
        const dx = e.shiftKey && !e.deltaX ? e.deltaY : e.deltaX;
        const dy = e.shiftKey && !e.deltaX ? 0 : e.deltaY;
        useDesign.getState().setCamera({ ...cam, x: cam.x - dx, y: cam.y - dy });
      }
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, []);

  const screenPoint = (e: { clientX: number; clientY: number }): Point => {
    const box = host.current!.getBoundingClientRect();
    return { x: e.clientX - box.left, y: e.clientY - box.top };
  };
  const toWorld = (p: Point): Point => {
    const cam = useDesign.getState().camera;
    return { x: (p.x - cam.x) / cam.zoom, y: (p.y - cam.y) / cam.zoom };
  };
  const toScreen = (p: Point): Point => ({ x: p.x * camera.zoom + camera.x, y: p.y * camera.zoom + camera.y });

  const currentPage = () => {
    const s = useDesign.getState();
    return s.doc ? pageOf(s.doc, s.pageId) : null;
  };

  /** What a pointer on the selection's frame is over: a handle, the rotation ring, or nothing. */
  const handleAt = (sp: Point): { handle: Handle } | { rotate: true } | null => {
    const p = currentPage();
    const s = useDesign.getState();
    if (!p || s.selection.length === 0) return null;
    const frame = selectionFrame(p, s.selection);
    if (!frame) return null;
    const pts = frame.pts.map((q) => ({ x: q.x * s.camera.zoom + s.camera.x, y: q.y * s.camera.zoom + s.camera.y }));
    const spots = handleSpots(pts);
    const line = s.selection.length === 1 && locate(p, s.selection[0]!)?.node.type === "line";
    for (const h of HANDLES) {
      if (line && h !== "w" && h !== "e") continue;
      const q = spots[h];
      if (Math.abs(q.x - sp.x) <= 5 && Math.abs(q.y - sp.y) <= 5) return { handle: h };
    }
    // Just outside a corner rotates; inside the box is a move.
    if (!insidePolygon(sp, pts)) for (const q of pts) if (Math.hypot(q.x - sp.x, q.y - sp.y) <= 18) return { rotate: true };
    return null;
  };

  const onPointerDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    const s = useDesign.getState();
    const p = currentPage();
    if (!p || !s.doc) return;
    host.current?.focus({ preventScroll: true });
    if (s.editing) {
      // A click elsewhere ends text editing (the editor commits on blur).
      s.setEditing(null);
    }
    e.currentTarget.setPointerCapture(e.pointerId);
    const sp = screenPoint(e);
    const world = toWorld(sp);
    if (e.button === 1 || s.tool === "hand" || space) {
      gesture.current = { kind: "pan", start: sp, cam: { x: s.camera.x, y: s.camera.y } };
      setPanning(true);
      return;
    }
    if (e.button !== 0) return;
    if (s.tool === "comment") {
      const id = newCommentAt(p.id, world, hitPath(p, world, sizeOf, 2 / s.camera.zoom)?.at(-1)?.id);
      s.setTool("select");
      if (id) s.setActiveComment(id);
      return;
    }
    if (s.tool === "eyedropper") {
      // Takes the colour of the layer under the pointer: onto the selection's fill (Alt: its stroke), or onto the clipboard.
      const hit = hitPath(p, world, sizeOf, 2 / s.camera.zoom);
      const picked = hit ? sampleColor(hit[hit.length - 1]!, e.altKey) : pageOf(s.doc, s.pageId).background;
      s.setTool("select");
      if (!picked) return;
      if (s.selection.length) {
        s.patch(s.selection, (n) =>
          e.altKey || n.type === "line"
            ? { strokes: [{ ...(n.strokes?.[0] ?? { width: 1 }), color: picked }, ...(n.strokes?.slice(1) ?? [])] as DesignNode["strokes"] }
            : { fills: [{ type: "solid", color: picked }] },
        );
      } else void navigator.clipboard?.writeText(picked).catch(() => undefined);
      useApp.getState().toast(picked);
      return;
    }
    if (s.cropping) {
      const at = locate(p, s.cropping);
      if (at && at.node.type === "image") {
        const hitCrop = hitPath(p, world, sizeOf, 2 / s.camera.zoom);
        if (hitCrop?.some((n) => n.id === s.cropping)) {
          gesture.current = { kind: "crop", id: s.cropping, start: world, from: { zoom: 1, x: 0, y: 0, ...at.node.crop } };
          s.beginGesture();
          return;
        }
      }
      s.setCropping(null);
    }
    if (s.showRulers && s.tool === "select") {
      const near = (p.guides ?? []).findIndex((g) => Math.abs((g.axis === "x" ? sp.x - s.camera.x : sp.y - s.camera.y) - g.at * s.camera.zoom) <= 4);
      if (near >= 0) {
        e.preventDefault();
        dragGuide(host.current!, p.guides![near]!.axis, near);
        return;
      }
    }
    const drawType = DRAW_TYPES[s.tool];
    if (drawType || s.tool === "text") {
      if (s.tool === "text") {
        const holder = frameAt(p, world, new Set(), sizeOf) ?? [];
        const local = holder.length ? toLocal(holder, world) : world;
        const id = s.addNode("text", { x: Math.round(local.x), y: Math.round(local.y), text: "" }, holder.length ? holder[holder.length - 1]!.id : null);
        if (id) {
          s.setTool("select");
          // The press still moves focus to the canvas after this handler;
          // an editor opened now would lose it at once and drop the empty
          // text. Open it once the press has settled.
          e.preventDefault();
          requestAnimationFrame(() => useDesign.getState().setEditing(id));
        }
        return;
      }
      // Sections and slices lie on the page itself, not inside a frame.
      const holder = drawType === "section" || drawType === "slice" ? [] : frameAt(p, world, new Set(), sizeOf) ?? [];
      gesture.current = { kind: "draw", type: drawType!, start: world, id: null, base: s.doc, parent: holder };
      s.beginGesture();
      return;
    }
    const near = handleAt(sp);
    if (near && "handle" in near) {
      const from = selectionFrame(p, s.selection)!.box;
      gesture.current = { kind: "resize", handle: near.handle, ids: topmost(p, s.selection), base: s.doc, from };
      s.beginGesture();
      return;
    }
    const hit = hitPath(p, world, sizeOf, 2 / s.camera.zoom);
    if (near && "rotate" in near) {
      const box = selectionFrame(p, s.selection)!;
      const center = { x: (box.pts[0]!.x + box.pts[2]!.x) / 2, y: (box.pts[0]!.y + box.pts[2]!.y) / 2 };
      gesture.current = { kind: "rotate", ids: topmost(p, s.selection), base: s.doc, center, startAngle: angleOf(center, world) };
      s.beginGesture();
      return;
    }
    if (hit) {
      const target = pickTarget(hit, s.selection, e.ctrlKey || e.metaKey);
      let ids = s.selection;
      if (e.shiftKey) ids = s.selection.includes(target.id) ? s.selection.filter((id) => id !== target.id) : [...s.selection, target.id];
      else if (!s.selection.includes(target.id)) ids = [target.id];
      s.select(ids);
      if (!e.shiftKey || ids.includes(target.id)) gesture.current = { kind: "pending", start: sp, world, ids: topmost(p, ids), duplicate: e.altKey };
      return;
    }
    // Empty canvas: a marquee.
    if (!e.shiftKey) s.select([]);
    gesture.current = { kind: "marquee", start: world, add: e.shiftKey ? s.selection : [] };
  };

  const onPointerMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    const s = useDesign.getState();
    const p = currentPage();
    if (!p) return;
    const sp = screenPoint(e);
    const world = toWorld(sp);
    const g = gesture.current;
    const threshold = SNAP_PX / s.camera.zoom;
    if (!g) {
      if (s.tool === "select" && !space) {
        const near = handleAt(sp);
        const el = host.current!;
        const onGuide = s.showRulers ? (p.guides ?? []).find((q) => Math.abs((q.axis === "x" ? sp.x - s.camera.x : sp.y - s.camera.y) - q.at * s.camera.zoom) <= 4) : undefined;
        if (onGuide) el.style.cursor = onGuide.axis === "x" ? "col-resize" : "row-resize";
        else if (near && "handle" in near) el.style.cursor = cursorFor(near.handle, selectionFrame(p, s.selection)?.rotation ?? 0);
        else if (near) el.style.cursor = "alias";
        else el.style.cursor = "";
        const hit = hitPath(p, world, sizeOf, 2 / s.camera.zoom);
        s.setHover(hit ? pickTarget(hit, s.selection, e.ctrlKey || e.metaKey).id : null);
      }
      return;
    }
    switch (g.kind) {
      case "crop": {
        // Dragging moves the picture inside its box; it never leaves a gap.
        const at = locate(pageOf(useDesign.getState().doc!, s.pageId), g.id);
        if (!at) return;
        const size = sizeOf(at.node);
        const limit = Math.max(0, (g.from.zoom - 1) / 2);
        const x = Math.max(-limit, Math.min(limit, g.from.x + (world.x - g.start.x) / Math.max(1, size.width)));
        const y = Math.max(-limit, Math.min(limit, g.from.y + (world.y - g.start.y) / Math.max(1, size.height)));
        const doc = useDesign.getState().doc!;
        s.preview(patchOne(doc, g.id, { crop: g.from.zoom === 1 && x === 0 && y === 0 ? undefined : { zoom: g.from.zoom, x, y } }));
        return;
      }
      case "pan":
        s.setCamera({ ...s.camera, x: g.cam.x + sp.x - g.start.x, y: g.cam.y + sp.y - g.start.y });
        return;
      case "marquee": {
        const r = { x: Math.min(g.start.x, world.x), y: Math.min(g.start.y, world.y), width: Math.abs(world.x - g.start.x), height: Math.abs(world.y - g.start.y) };
        setMarquee(r);
        const hits = marquee(p, r, sizeOf);
        s.select([...new Set([...g.add, ...hits])]);
        return;
      }
      case "pending": {
        if (Math.hypot(sp.x - g.start.x, sp.y - g.start.y) < DRAG_START) return;
        const ids = g.ids.filter((id) => !locate(p, id)?.node.locked);
        if (ids.length === 0) {
          gesture.current = null;
          return;
        }
        s.beginGesture();
        let base = s.doc!;
        let moving = ids;
        if (g.duplicate) {
          s.duplicate();
          base = useDesign.getState().doc!;
          moving = useDesign.getState().selection;
          s.beginGesture();
          // The duplicate is one undo step; the move is another.
        }
        gesture.current = { kind: "move", start: g.start, world: g.world, ids: moving, base };
        return onPointerMove(e);
      }
      case "move": {
        let dx = world.x - g.world.x;
        let dy = world.y - g.world.y;
        if (e.shiftKey) {
          if (Math.abs(dx) > Math.abs(dy)) dy = 0;
          else dx = 0;
        }
        const basePage = pageOf(g.base, s.pageId);
        const box = selectionBounds(basePage, g.ids, sizeOf);
        let found: Guide[] = [];
        if (box && !(e.ctrlKey || e.metaKey)) {
          const snap = snapRect({ ...box, x: box.x + dx, y: box.y + dy }, snapTargets(basePage, g.ids, sizeOf), threshold);
          dx += snap.dx;
          dy += snap.dy;
          found = snap.guides;
        }
        if (box) {
          // Whole pixels on the page, as the box lands.
          dx = Math.round(box.x + dx) - box.x;
          dy = Math.round(box.y + dy) - box.y;
        }
        setGuides(found);
        s.preview(moveBy(g.base, basePage, g.ids, dx, dy));
        return;
      }
      case "resize": {
        const basePage = pageOf(g.base, s.pageId);
        let target = world;
        let found: Guide[] = [];
        const [hx, hy] = handleDir(g.handle);
        if (!(e.ctrlKey || e.metaKey)) {
          const snap = snapPoint(world, snapTargets(basePage, g.ids, sizeOf), threshold, { x: hx !== 0, y: hy !== 0 });
          target = { x: world.x + snap.dx, y: world.y + snap.dy };
          found = snap.guides;
        }
        setGuides(found);
        if (g.ids.length === 1) {
          const at = locate(basePage, g.ids[0]!);
          if (!at) return;
          const node = { ...at.node, ...sizeOf(at.node) };
          const box = resizeNode(node, parentMatrix(at.path), g.handle, target, { keepRatio: e.shiftKey, fromCenter: e.altKey });
          const rounded = { x: Math.round(box.x * 100) / 100, y: Math.round(box.y * 100) / 100, width: Math.max(at.node.type === "line" ? 0 : 1, Math.round(box.width)), height: at.node.type === "line" ? 0 : Math.max(1, Math.round(box.height)) };
          s.preview(applyResize(g.base, at.node.id, rounded));
          return;
        }
        const from = g.from;
        let x0 = from.x;
        let y0 = from.y;
        let x1 = from.x + from.width;
        let y1 = from.y + from.height;
        if (hx === -1) x0 = target.x;
        if (hx === 1) x1 = target.x;
        if (hy === -1) y0 = target.y;
        if (hy === 1) y1 = target.y;
        if (e.shiftKey && from.width > 0 && from.height > 0) {
          const ratio = from.width / from.height;
          const w = Math.abs(x1 - x0);
          const h = Math.abs(y1 - y0);
          if (hx === 0 || (hy !== 0 && w / ratio < h)) {
            const nw = h * ratio;
            if (hx === -1) x0 = x1 - nw;
            else x1 = x0 + nw;
          } else {
            const nh = w / ratio;
            if (hy === -1) y0 = y1 - nh;
            else y1 = y0 + nh;
          }
        }
        const to = { x: Math.min(x0, x1), y: Math.min(y0, y1), width: Math.max(1, Math.abs(x1 - x0)), height: Math.max(1, Math.abs(y1 - y0)) };
        s.preview(scaleInto(g.base, basePage, g.ids, from, to, sizeOf));
        return;
      }
      case "rotate": {
        let deg = angleOf(g.center, world) - g.startAngle;
        if (e.shiftKey) {
          const at = g.ids.length === 1 ? locate(pageOf(g.base, s.pageId), g.ids[0]!) : null;
          const now = (at?.node.rotation ?? 0) + deg;
          deg = Math.round(now / 15) * 15 - (at?.node.rotation ?? 0);
        }
        s.preview(rotateAround(g.base, pageOf(g.base, s.pageId), g.ids, g.center, deg, g.base));
        return;
      }
      case "draw": {
        let x0 = g.start.x;
        let y0 = g.start.y;
        let x1 = world.x;
        let y1 = world.y;
        const basePage = pageOf(g.base, s.pageId);
        if (!(e.ctrlKey || e.metaKey)) {
          const snap = snapPoint(world, snapTargets(basePage, [], sizeOf).concat(g.parent.length ? [worldBounds(g.parent, sizeOf)] : basePage.children.map((c) => worldBounds([c], sizeOf))), threshold);
          x1 += snap.dx;
          y1 += snap.dy;
          setGuides(snap.guides);
        }
        if (Math.hypot(x1 - x0, y1 - y0) * s.camera.zoom < DRAG_START && !g.id) return;
        if (g.type === "line") {
          let len = Math.hypot(x1 - x0, y1 - y0);
          let ang = (Math.atan2(y1 - y0, x1 - x0) * 180) / Math.PI;
          if (e.shiftKey) ang = Math.round(ang / 45) * 45;
          len = Math.round(len);
          const mid = { x: x0 + (Math.cos((ang * Math.PI) / 180) * len) / 2, y: y0 + (Math.sin((ang * Math.PI) / 180) * len) / 2 };
          const local = g.parent.length ? toLocal(g.parent, mid) : mid;
          const parentAngle = g.parent.length ? (Math.atan2(worldMatrix(g.parent)[1], worldMatrix(g.parent)[0]) * 180) / Math.PI : 0;
          const props = { x: local.x - len / 2, y: local.y, width: len, height: 0, rotation: (((ang - parentAngle) % 360) + 360) % 360 };
          draft(g, props);
          return;
        }
        if (e.shiftKey) {
          const side = Math.max(Math.abs(x1 - x0), Math.abs(y1 - y0));
          x1 = x0 + Math.sign(x1 - x0 || 1) * side;
          y1 = y0 + Math.sign(y1 - y0 || 1) * side;
        }
        if (e.altKey) {
          x0 = g.start.x - (x1 - g.start.x);
          y0 = g.start.y - (y1 - g.start.y);
        }
        const r = { x: Math.round(Math.min(x0, x1)), y: Math.round(Math.min(y0, y1)), width: Math.max(1, Math.round(Math.abs(x1 - x0))), height: Math.max(1, Math.round(Math.abs(y1 - y0))) };
        const local = g.parent.length ? toLocal(g.parent, { x: r.x, y: r.y }) : { x: r.x, y: r.y };
        draft(g, { x: local.x, y: local.y, width: r.width, height: r.height });
        return;
      }
    }
  };

  /** Creates the shape being drawn on the first move, then resizes it. */
  const draft = (g: Extract<Gesture, { kind: "draw" }>, props: Partial<DesignNode>) => {
    const s = useDesign.getState();
    if (!g.id) {
      const parentId = g.parent.length ? g.parent[g.parent.length - 1]!.id : null;
      const node = createNode(g.type, { ...props, name: nextName(pageOf(g.base, s.pageId), defaultName(g.type)) });
      g.id = node.id;
      s.preview(insertNodes(g.base, s.pageId, parentId, [node]));
      s.select([node.id]);
      return;
    }
    if (s.doc) s.preview(patchOne(s.doc, g.id, props));
  };

  const onPointerUp = (e: ReactPointerEvent<HTMLDivElement>) => {
    const s = useDesign.getState();
    const g = gesture.current;
    gesture.current = null;
    setMarquee(null);
    setGuides([]);
    setPanning(false);
    if (!g) return;
    if (g.kind === "pending") {
      // A click on something already selected, among others, selects just it.
      const p = currentPage();
      if (p && !e.shiftKey && s.selection.length > 1) {
        const hit = hitPath(p, g.world, sizeOf, 2 / s.camera.zoom);
        if (hit) s.select([pickTarget(hit, s.selection, e.ctrlKey || e.metaKey).id]);
      }
      return;
    }
    if (g.kind === "move") {
      // Dropped over a frame (or off its own): it moves into that frame, in the same undo step.
      const after = useDesign.getState();
      const p = currentPage();
      if (!p || !after.doc) return s.endGesture();
      const world = toWorld(screenPoint(e));
      const exclude = new Set(g.ids);
      const target = frameAt(p, world, exclude, sizeOf);
      const targetId = target ? target[target.length - 1]!.id : null;
      const parents = new Set(g.ids.map((id) => locate(p, id)?.parent?.id ?? null));
      const allTopLevelFrames = g.ids.every((id) => {
        const at = locate(p, id);
        return at && !at.parent && (at.node.type === "frame" || at.node.type === "component");
      });
      // Frames on the page stay there unless they are dropped into a section (and leave it the same way).
      const intoSection = !!target && target[target.length - 1]!.type === "section" && g.ids.every((id) => locate(p, id)?.node.type !== "section");
      if ((!allTopLevelFrames || intoSection) && (parents.size !== 1 || !parents.has(targetId))) {
        const fromGroup = [...parents].some((id) => id && locate(p, id)?.node.type === "group");
        if (!fromGroup) {
          const moved = reparent(after.doc, after.pageId, g.ids, targetId);
          if (moved !== after.doc) s.preview(moved);
        }
      }
      s.endGesture();
      return;
    }
    if (g.kind === "resize" || g.kind === "rotate" || g.kind === "crop") {
      s.endGesture();
      return;
    }
    if (g.kind === "draw") {
      if (!g.id) {
        s.cancelGesture();
        // A click with a shape tool: a default-sized shape at the point.
        const sizes: Partial<Record<NodeType, [number, number]>> = { frame: [100, 100], section: [480, 320], slice: [200, 200], line: [100, 0] };
        const [w, h] = sizes[g.type] ?? [100, 100];
        const local = g.parent.length ? toLocal(g.parent, g.start) : g.start;
        const id = s.addNode(g.type, { x: Math.round(local.x - w / 2), y: Math.round(local.y - h / 2), width: w, height: h }, g.parent.length ? g.parent[g.parent.length - 1]!.id : null);
        if (id) s.select([id]);
      } else {
        // A section drawn around top-level frames takes them in, as in Figma.
        if (g.type === "section") {
          const after = useDesign.getState();
          const p = currentPage();
          const box = p ? nodeBounds(p, g.id, sizeOf) : null;
          if (p && after.doc && box) {
            const inside = p.children
              .filter((n) => n.id !== g.id && n.visible !== false && (n.type === "frame" || n.type === "component"))
              .filter((n) => {
                const b = worldBounds([n], sizeOf);
                return b.x >= box.x && b.y >= box.y && b.x + b.width <= box.x + box.width && b.y + b.height <= box.y + box.height;
              })
              .map((n) => n.id);
            if (inside.length) {
              const moved = reparent(after.doc, after.pageId, inside, g.id);
              if (moved !== after.doc) s.preview(moved);
            }
          }
        }
        s.endGesture();
        s.select([g.id]);
      }
      s.setTool("select");
    }
  };

  const onDoubleClick = (e: React.MouseEvent<HTMLDivElement>) => {
    const s = useDesign.getState();
    const p = currentPage();
    if (!p) return;
    const world = toWorld(screenPoint(e));
    const hit = hitPath(p, world, sizeOf, 2 / s.camera.zoom);
    if (!hit) return;
    const current = s.selection.length === 1 ? s.selection[0] : null;
    const at = current ? hit.findIndex((n) => n.id === current) : -1;
    const target = at >= 0 && at < hit.length - 1 ? hit[at + 1]! : pickTarget(hit, s.selection, false);
    if (target.type === "text" && !target.locked) {
      s.select([target.id]);
      s.setEditing(target.id);
      return;
    }
    s.select([target.id]);
    if (target.type === "image" && !target.locked) s.setCropping(target.id);
  };

  const onPointerLeave = () => useDesign.getState().setHover(null);

  if (!page) return <div className="design-canvas" ref={host} />;

  const frame = selectionFrame(page, selection);
  const hoverAt = hover && !selection.includes(hover) ? locate(page, hover) : null;
  const editingAt = editing ? locate(page, editing) : null;
  const cursor = panning ? "grabbing" : space || tool === "hand" ? "grab" : tool === "text" ? "text" : tool !== "select" && tool !== "pen" && tool !== "pencil" ? "crosshair" : undefined;

  const menu = contextItems(selection.length);
  return (
    <RightClick items={menu}>
    <div
      ref={host}
      className="design-canvas"
      data-tool={tool}
      data-ready={ready || undefined}
      tabIndex={-1}
      style={{ background: page.background, cursor }}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={() => {
        gesture.current = null;
        useDesign.getState().cancelGesture();
        setMarquee(null);
        setGuides([]);
      }}
      onPointerLeave={onPointerLeave}
      onDoubleClick={onDoubleClick}
      onContextMenu={(e) => {
        // Like Figma, a right-click on a layer outside the selection selects it first.
        const s = useDesign.getState();
        const p = currentPage();
        if (!p || s.editing) return;
        const hit = hitPath(p, toWorld(screenPoint(e)), sizeOf, 2 / s.camera.zoom);
        if (hit) {
          const target = pickTarget(hit, s.selection, e.ctrlKey || e.metaKey);
          if (!s.selection.includes(target.id)) s.select([target.id]);
        }
      }}
    >
      <svg className="design-page" width={size.width} height={size.height} aria-label={page.name} role="img">
        <RenderContext.Provider value={env}>
          <g transform={`translate(${camera.x} ${camera.y}) scale(${camera.zoom})`}>
            {(drawnPage ?? page).children.map((n) => (
              <NodeView key={n.id} node={n} hidden={editing ?? undefined} />
            ))}
          </g>
        </RenderContext.Provider>
      </svg>
      <svg className="design-overlay" width={size.width} height={size.height} aria-hidden="true">
        {showGrids && <LayoutGrids page={drawnPage ?? page} camera={camera} />}
        <FrameLabels page={page} toScreen={toScreen} selection={selection} />
        <Slices page={page} toScreen={toScreen} zoom={camera.zoom} selection={selection} />
        {showRulers &&
          (page.guides ?? []).map((g, i) => (
            <line key={`g${i}`} className="design-ruler-guide" x1={g.axis === "x" ? camera.x + g.at * camera.zoom : 0} x2={g.axis === "x" ? camera.x + g.at * camera.zoom : size.width} y1={g.axis === "x" ? 0 : camera.y + g.at * camera.zoom} y2={g.axis === "x" ? size.height : camera.y + g.at * camera.zoom} />
          ))}
        {cropping && locate(page, cropping) && <Outline path={locate(page, cropping)!.path} toScreen={toScreen} className="design-crop" />}
        {flash.map((id) => {
          const at = locate(page, id);
          if (!at || at.node.visible === false) return null;
          return <Outline key={`f${id}`} path={at.path} toScreen={toScreen} className="design-flash" />;
        })}
        {hoverAt && <Outline path={hoverAt.path} toScreen={toScreen} className="design-hover" />}
        {alt && selection.length > 0 && !gesture.current && <Measure page={page} selection={selection} target={hoverAt?.path ?? null} toScreen={toScreen} />}
        {selection.length > 1 &&
          selection.map((id) => {
            const at = locate(page, id);
            return at ? <Outline key={id} path={at.path} toScreen={toScreen} className="design-member" /> : null;
          })}
        {frame && !editingAt && <SelectionBox pts={frame.pts.map(toScreen)} box={frame.box} line={selection.length === 1 && locate(page, selection[0]!)?.node.type === "line"} />}
        {marqueeRect && <rect className="design-marquee" x={toScreen(marqueeRect).x} y={toScreen(marqueeRect).y} width={marqueeRect.width * camera.zoom} height={marqueeRect.height * camera.zoom} />}
        {guides.map((g, i) => {
          const a = toScreen(g.axis === "x" ? { x: g.at, y: g.from } : { x: g.from, y: g.at });
          const b = toScreen(g.axis === "x" ? { x: g.at, y: g.to } : { x: g.to, y: g.at });
          return <line key={i} className="design-guide" x1={a.x} y1={a.y} x2={b.x} y2={b.y} />;
        })}
      </svg>
      {editingAt && editingAt.node.type === "text" && <TextEditor node={editingAt.node} path={editingAt.path} camera={camera} />}
      {showRulers && <Rulers host={host} size={size} camera={camera} selection={selection} page={page} />}
      <PathTools />
      <CommentPins />
    </div>
    </RightClick>
  );
}

/** The canvas's right-click menu: Figma's common commands, with their keys. */
function contextItems(selected: number): MenuEntry[] {
  const s = () => useDesign.getState();
  const view = () => document.querySelector<HTMLElement>(".design-canvas")?.getBoundingClientRect() ?? { width: 0, height: 0 };
  const page = () => pageOf(s().doc!, s().pageId);
  const items: MenuEntry[] = [];
  if (selected) {
    items.push(
      { label: t("design.menu.copy"), keys: "Ctrl+C", onSelect: () => s().copy() },
      { label: t("design.menu.cut"), keys: "Ctrl+X", onSelect: () => s().cut() },
    );
  }
  items.push({ label: t("design.menu.paste"), keys: "Ctrl+V", onSelect: () => s().paste() });
  if (selected) {
    items.push(
      { label: t("design.menu.duplicate"), keys: "Ctrl+D", onSelect: () => s().duplicate() },
      { label: t("design.menu.delete"), keys: "Del", danger: true, onSelect: () => s().remove() },
      { label: t("design.menu.group"), keys: "Ctrl+G", onSelect: () => s().group() },
      { label: t("design.menu.ungroup"), keys: "Ctrl+Shift+G", onSelect: () => s().ungroup() },
      { label: t("design.menu.frame"), keys: "Ctrl+Alt+G", onSelect: () => s().frameSelection() },
      { label: t("design.menu.autoLayout"), keys: "Shift+A", onSelect: () => s().addAutoLayout() },
      { label: t("design.menu.component"), keys: "Ctrl+Alt+K", onSelect: () => s().makeComponent() },
      { label: t("design.menu.detach"), keys: "Ctrl+Alt+B", onSelect: () => s().detach() },
      { label: t("design.menu.front"), keys: "Ctrl+Shift+]", onSelect: () => s().reorder("front") },
      { label: t("design.menu.back"), keys: "Ctrl+Shift+[", onSelect: () => s().reorder("back") },
      { label: t("design.menu.flatten"), keys: "Ctrl+E", onSelect: () => s().flatten() },
      { label: t("design.menu.hide"), keys: "Shift+H", onSelect: () => s().patch(s().selection, (n) => ({ visible: n.visible === false })) },
      { label: t("design.menu.lock"), keys: "Ctrl+Shift+L", onSelect: () => s().patch(s().selection, (n) => ({ locked: !n.locked })) },
      { label: t("design.menu.zoomSelection"), keys: "Shift+2", onSelect: () => zoomToFit(page(), view(), true) },
    );
  }
  items.push(
    { label: t("design.menu.selectAll"), keys: "Ctrl+A", onSelect: () => s().selectAll() },
    { label: t("design.menu.grids"), keys: "Ctrl+Shift+4", checked: s().showGrids, onSelect: () => s().toggleGrids() },
    { label: t("design.menu.rulers"), keys: "Shift+R", checked: s().showRulers, onSelect: () => s().toggleRulers() },
  );
  return items;
}

function patchOne(doc: DesignDoc, id: string, props: Partial<DesignNode>): DesignDoc {
  const at = doc.pages.map((p) => locate(p, id)).find(Boolean);
  if (!at) return doc;
  const rewrite = (nodes: DesignNode[]): DesignNode[] => nodes.map((n) => (n.id === id ? { ...n, ...props } : n.children ? { ...n, children: rewrite(n.children) } : n));
  return { ...doc, pages: doc.pages.map((p) => ({ ...p, children: rewrite(p.children) })) };
}

function isTyping(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null;
  return !!el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.tagName === "SELECT" || el.isContentEditable);
}

function handleSpots(pts: Point[]): Record<Handle, Point> {
  const mid = (a: Point, b: Point) => ({ x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 });
  const [nw, ne, se, sw] = pts as [Point, Point, Point, Point];
  return { nw, n: mid(nw, ne), ne, e: mid(ne, se), se, s: mid(se, sw), sw, w: mid(sw, nw) };
}

function Outline({ path, toScreen, className }: { path: DesignNode[]; toScreen: (p: Point) => Point; className: string }) {
  const node = path[path.length - 1]!;
  const size = sizeOf(node);
  const m = multiply(parentMatrix(path), localMatrix({ ...node, ...size }));
  if (node.type === "line") {
    const a = toScreen(apply(m, { x: 0, y: 0 }));
    const b = toScreen(apply(m, { x: size.width, y: 0 }));
    return <line className={className} x1={a.x} y1={a.y} x2={b.x} y2={b.y} />;
  }
  const pts = corners(m, size.width, size.height).map(toScreen);
  return <polygon className={className} points={pts.map((p) => `${p.x},${p.y}`).join(" ")} />;
}

function SelectionBox({ pts, box, line }: { pts: Point[]; box: Rect; line: boolean }) {
  const spots = handleSpots(pts);
  // On a small box the side handles would cover it; the corners do the job.
  const across = Math.hypot(pts[1]!.x - pts[0]!.x, pts[1]!.y - pts[0]!.y);
  const down = Math.hypot(pts[3]!.x - pts[0]!.x, pts[3]!.y - pts[0]!.y);
  const roomy = (h: Handle) => (h === "n" || h === "s" ? across >= 40 : down >= 40);
  const label = `${Math.round(box.width * 100) / 100} × ${Math.round(box.height * 100) / 100}`;
  const bottom = pts.reduce((a, b) => (b.y > a.y ? b : a));
  const cx = (pts[0]!.x + pts[2]!.x) / 2;
  return (
    <g className="design-selection">
      {line ? <line x1={spots.w.x} y1={spots.w.y} x2={spots.e.x} y2={spots.e.y} /> : <polygon points={pts.map((p) => `${p.x},${p.y}`).join(" ")} />}
      {HANDLES.filter((h) => (line ? h === "w" || h === "e" : h.length === 2 || roomy(h))).map((h) => (
        <rect key={h} className="design-handle" data-handle={h} x={spots[h].x - 4} y={spots[h].y - 4} width={8} height={8} rx={1.5} />
      ))}
      <g transform={`translate(${cx} ${bottom.y + 10})`} className="design-size">
        <rect x={-label.length * 3.4 - 6} y={0} width={label.length * 6.8 + 12} height={18} rx={4} />
        <text x={0} y={12.5} textAnchor="middle">
          {label}
        </text>
      </g>
    </g>
  );
}

/** The red distance lines Figma shows with Alt held: from the selection to
 * the hovered layer, or to the inside of the selection's parent frame. */
function Measure({ page, selection, target, toScreen }: { page: Page; selection: string[]; target: DesignNode[] | null; toScreen: (p: Point) => Point }) {
  const a = selectionBounds(page, selection, sizeOf);
  if (!a) return null;
  let path = target;
  if (!path) {
    const at = locate(page, selection[0]!);
    if (!at || at.path.length < 2) return null;
    path = at.path.slice(0, -1);
  }
  const b = worldBounds(path, sizeOf);
  const lines: { x1: number; y1: number; x2: number; y2: number }[] = [];
  const ax2 = a.x + a.width;
  const ay2 = a.y + a.height;
  const bx2 = b.x + b.width;
  const by2 = b.y + b.height;
  const inside = a.x >= b.x && ax2 <= bx2 && a.y >= b.y && ay2 <= by2;
  const midY = (Math.max(a.y, b.y) + Math.min(ay2, by2)) / 2;
  const midX = (Math.max(a.x, b.x) + Math.min(ax2, bx2)) / 2;
  const cy = a.y + a.height / 2;
  const cx = a.x + a.width / 2;
  if (inside) {
    lines.push({ x1: b.x, y1: cy, x2: a.x, y2: cy }, { x1: ax2, y1: cy, x2: bx2, y2: cy }, { x1: cx, y1: b.y, x2: cx, y2: a.y }, { x1: cx, y1: ay2, x2: cx, y2: by2 });
  } else {
    const yOverlap = Math.min(ay2, by2) > Math.max(a.y, b.y);
    const xOverlap = Math.min(ax2, bx2) > Math.max(a.x, b.x);
    const y = yOverlap ? midY : cy;
    const x = xOverlap ? midX : cx;
    if (bx2 <= a.x) lines.push({ x1: bx2, y1: y, x2: a.x, y2: y });
    else if (b.x >= ax2) lines.push({ x1: ax2, y1: y, x2: b.x, y2: y });
    if (by2 <= a.y) lines.push({ x1: x, y1: by2, x2: x, y2: a.y });
    else if (b.y >= ay2) lines.push({ x1: x, y1: ay2, x2: x, y2: b.y });
  }
  const shown = lines.filter((l) => Math.hypot(l.x2 - l.x1, l.y2 - l.y1) >= 0.5);
  const box = { x: toScreen(b).x, y: toScreen(b).y, x2: toScreen({ x: bx2, y: by2 }).x, y2: toScreen({ x: bx2, y: by2 }).y };
  return (
    <g className="design-measure" aria-hidden="true">
      <rect x={box.x} y={box.y} width={box.x2 - box.x} height={box.y2 - box.y} />
      {shown.map((l, i) => {
        const p = toScreen({ x: l.x1, y: l.y1 });
        const q = toScreen({ x: l.x2, y: l.y2 });
        const label = String(Math.round(Math.hypot(l.x2 - l.x1, l.y2 - l.y1) * 100) / 100);
        const mx = (p.x + q.x) / 2;
        const my = (p.y + q.y) / 2;
        const w = label.length * 6.8 + 10;
        return (
          <g key={i}>
            <line x1={p.x} y1={p.y} x2={q.x} y2={q.y} />
            <rect className="design-measure-tag" x={mx - w / 2} y={my - 9} width={w} height={18} rx={3} />
            <text x={mx} y={my + 4} textAnchor="middle">
              {label}
            </text>
          </g>
        );
      })}
    </g>
  );
}

const GRID_COLOR: Record<LayoutGrid["pattern"], string> = { columns: "#FF00001A", rows: "#FF00001A", grid: "#FF000033" };

/** Frames' layout grids over their content, as Figma draws them; never exported. */
function LayoutGrids({ page, camera }: { page: Page; camera: { x: number; y: number; zoom: number } }) {
  const out: ReactNode[] = [];
  const visit = (nodes: DesignNode[], parent: Matrix) => {
    for (const n of nodes) {
      if (n.visible === false) continue;
      const m = multiply(parent, localMatrix(n));
      if (n.grids?.length && (n.type === "frame" || n.type === "component")) {
        const parts: ReactNode[] = [];
        n.grids.forEach((g, gi) => {
          if (g.hidden) return;
          const fill = g.color ?? GRID_COLOR[g.pattern];
          if (g.pattern === "grid") {
            const step = Math.max(1, g.size ?? 8);
            // Lines closer than 4 screen pixels would be a solid wash.
            if (step * camera.zoom < 4) return;
            const d: string[] = [];
            for (let x = step; x < n.width; x += step) d.push(`M${x} 0V${n.height}`);
            for (let y = step; y < n.height; y += step) d.push(`M0 ${y}H${n.width}`);
            parts.push(<path key={gi} d={d.join("")} stroke={fill} strokeWidth={1 / camera.zoom} fill="none" />);
            return;
          }
          for (const [at, size] of gridTracks(g, g.pattern === "columns" ? n.width : n.height)) {
            parts.push(g.pattern === "columns" ? <rect key={`${gi}-${at}`} x={at} y={0} width={size} height={n.height} fill={fill} /> : <rect key={`${gi}-${at}`} x={0} y={at} width={n.width} height={size} fill={fill} />);
          }
        });
        if (parts.length) {
          out.push(
            <g key={n.id} transform={`matrix(${m.join(" ")})`}>
              {parts}
            </g>,
          );
        }
      }
      if (n.children) visit(n.children, m);
    }
  };
  visit(page.children, [1, 0, 0, 1, 0, 0]);
  if (!out.length) return null;
  return (
    <g className="design-grids" transform={`translate(${camera.x} ${camera.y}) scale(${camera.zoom})`}>
      {out}
    </g>
  );
}

function FrameLabels({ page, toScreen, selection }: { page: Page; toScreen: (p: Point) => Point; selection: string[] }) {
  // Top-level frames and sections are labelled, and so are the frames a section holds.
  const labelled: { node: DesignNode; origin: Point }[] = [];
  for (const n of page.children) {
    if (n.visible === false) continue;
    if (n.type === "frame" || n.type === "component" || n.type === "section") labelled.push({ node: n, origin: { x: n.x, y: n.y } });
    if (n.type === "section")
      for (const c of n.children ?? []) if (c.visible !== false && (c.type === "frame" || c.type === "component")) labelled.push({ node: c, origin: { x: n.x + c.x, y: n.y + c.y } });
  }
  return (
    <g className="design-frame-labels">
      {labelled.map(({ node: n, origin }) => {
        const p = toScreen(origin);
        return (
          <text key={n.id} x={p.x} y={p.y - 6} data-on={selection.includes(n.id) || undefined} data-component={n.type === "component" || undefined} data-section={n.type === "section" || undefined}>
            {n.type === "component" ? `◇ ${n.name}` : n.name}
          </text>
        );
      })}
    </g>
  );
}

/** Slices: a dashed green box with the name and the size, never exported. */
function Slices({ page, toScreen, zoom, selection }: { page: Page; toScreen: (p: Point) => Point; zoom: number; selection: string[] }) {
  const slices: DesignNode[] = [];
  const visit = (nodes: DesignNode[]) => {
    for (const n of nodes) {
      if (n.visible === false) continue;
      if (n.type === "slice") slices.push(n);
      if (n.children) visit(n.children);
    }
  };
  visit(page.children);
  if (!slices.length) return null;
  return (
    <g className="design-slices">
      {slices.map((n) => {
        const at = locate(page, n.id);
        if (!at) return null;
        const box = worldBounds(at.path, sizeOf);
        const a = toScreen({ x: box.x, y: box.y });
        return (
          <g key={n.id} data-on={selection.includes(n.id) || undefined}>
            <rect x={a.x} y={a.y} width={box.width * zoom} height={box.height * zoom} />
            <text x={a.x + 4} y={a.y + 12}>{`${n.name} · ${Math.round(box.width)}×${Math.round(box.height)}`}</text>
          </g>
        );
      })}
    </g>
  );
}

/** A step for ruler labels that keeps them about 60 screen pixels apart. */
function rulerStep(zoom: number): number {
  const target = 60 / zoom;
  const pow = Math.pow(10, Math.floor(Math.log10(target)));
  for (const m of [1, 2, 5, 10]) if (pow * m >= target) return pow * m;
  return pow * 10;
}

/** Rulers along the top and left edge. Dragging out of one pulls a guide. */
function Rulers({ host, size, camera, selection, page }: { host: React.RefObject<HTMLDivElement | null>; size: { width: number; height: number }; camera: { x: number; y: number; zoom: number }; selection: string[]; page: Page }) {
  const step = rulerStep(camera.zoom);
  const minor = step / (step >= 5 ? 5 : 1);
  const ticks = (len: number, offset: number): { at: number; major: boolean; screen: number }[] => {
    const out: { at: number; major: boolean; screen: number }[] = [];
    const from = Math.floor((RULER - offset) / camera.zoom / minor) * minor;
    const to = Math.ceil((len - offset) / camera.zoom / minor) * minor;
    for (let v = from; v <= to; v += minor) {
      const screen = offset + v * camera.zoom;
      if (screen < RULER || screen > len) continue;
      out.push({ at: Math.round(v * 1000) / 1000, major: Math.abs(v / step - Math.round(v / step)) < 1e-6, screen });
    }
    return out;
  };
  const box = selectionBounds(page, selection, sizeOf);
  const start = (axis: "x" | "y") => (e: React.PointerEvent) => {
    if (e.button !== 0 || !host.current) return;
    e.preventDefault();
    e.stopPropagation();
    dragGuide(host.current, axis, -1);
  };
  const w = size.width;
  const h = size.height;
  return (
    <svg className="design-rulers" width={w} height={h} aria-hidden="true">
      <rect className="design-ruler-bg" x={0} y={0} width={w} height={RULER} onPointerDown={start("y")} style={{ cursor: "row-resize" }} />
      <rect className="design-ruler-bg" x={0} y={0} width={RULER} height={h} onPointerDown={start("x")} style={{ cursor: "col-resize" }} />
      {box && <rect className="design-ruler-sel" x={camera.x + box.x * camera.zoom} y={0} width={box.width * camera.zoom} height={RULER} />}
      {box && <rect className="design-ruler-sel" x={0} y={camera.y + box.y * camera.zoom} width={RULER} height={box.height * camera.zoom} />}
      {ticks(w, camera.x).map((k) => (
        <g key={`x${k.at}`}>
          <line className="design-ruler-tick" x1={k.screen} x2={k.screen} y1={k.major ? 8 : 15} y2={RULER} />
          {k.major && <text className="design-ruler-label" x={k.screen + 3} y={9}>{k.at}</text>}
        </g>
      ))}
      {ticks(h, camera.y).map((k) => (
        <g key={`y${k.at}`}>
          <line className="design-ruler-tick" y1={k.screen} y2={k.screen} x1={k.major ? 8 : 15} x2={RULER} />
          {k.major && (
            <text className="design-ruler-label" transform={`translate(9 ${k.screen - 3}) rotate(-90)`}>
              {k.at}
            </text>
          )}
        </g>
      ))}
      <rect className="design-ruler-corner" x={0} y={0} width={RULER} height={RULER} />
    </svg>
  );
}

/** Edits a text node in place: a textarea laid over it, in the node's font at the canvas's zoom. */
function TextEditor({ node, path, camera }: { node: DesignNode; path: DesignNode[]; camera: { x: number; y: number; zoom: number } }) {
  const ref = useRef<HTMLTextAreaElement>(null);
  const [value, setValue] = useState(node.text ?? "");
  const done = useRef(false);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.focus();
    el.select();
  }, []);
  const finish = () => {
    if (done.current) return;
    done.current = true;
    const s = useDesign.getState();
    if (value.trim() === "" && !(node.text ?? "").trim()) {
      // An empty new text box is not kept, nor its step in the history.
      const prev = s.past[s.past.length - 1];
      if (prev && !prev.pages.some((p) => locate(p, node.id))) s.discard();
      else if (s.doc) s.commit(removeOne(s.doc, node.id), []);
    } else if (value !== node.text) {
      s.patch([node.id], { text: value, name: node.name === "Text" || node.name === (node.text ?? "").slice(0, 40) || /^Text \d+$/.test(node.name) ? value.split("\n")[0]!.slice(0, 40) || node.name : node.name });
    }
    s.setEditing(null);
  };
  const preview = { ...node, text: value };
  const size = sizeOf(preview);
  const m = multiply(parentMatrix(path), localMatrix({ ...node, ...size }));
  const origin = apply(m, { x: 0, y: 0 });
  const angle = (Math.atan2(m[1], m[0]) * 180) / Math.PI;
  const f = node.font!;
  const fill = node.fills?.find((x) => x.type === "solid" && x.visible !== false);
  return (
    <textarea
      ref={ref}
      className="design-text-editor"
      aria-label="Text"
      value={value}
      spellCheck={false}
      onChange={(e) => setValue(e.target.value)}
      onBlur={finish}
      onPointerDown={(e) => e.stopPropagation()}
      onKeyDown={(e) => {
        e.stopPropagation();
        if (e.key === "Escape" || (e.key === "Enter" && (e.ctrlKey || e.metaKey))) {
          e.preventDefault();
          finish();
        }
      }}
      style={{
        left: origin.x * camera.zoom + camera.x,
        top: origin.y * camera.zoom + camera.y,
        width: Math.max(8, (node.auto_resize === "width" ? size.width + f.size : size.width)) * camera.zoom,
        height: Math.max(lineHeightOf(node), size.height) * camera.zoom,
        transform: `rotate(${angle}deg) scale(${camera.zoom})`,
        transformOrigin: "0 0",
        font: cssFont(node),
        lineHeight: `${lineHeightOf(node)}px`,
        letterSpacing: node.letter_spacing ? `${node.letter_spacing}px` : undefined,
        textAlign: node.align ?? "left",
        color: fill && fill.type === "solid" ? fill.color : "#000",
        whiteSpace: node.auto_resize === "width" ? "pre" : "pre-wrap",
        ["--zoom" as string]: camera.zoom,
      }}
    />
  );
}

function removeOne(doc: DesignDoc, id: string): DesignDoc {
  const prune = (nodes: DesignNode[]): DesignNode[] => nodes.filter((n) => n.id !== id).map((n) => (n.children ? { ...n, children: prune(n.children) } : n));
  return { ...doc, pages: doc.pages.map((p) => ({ ...p, children: prune(p.children) })) };
}

/** Zooms so the page's content fits the view. */
export function zoomToFit(page: Page, view: { width: number; height: number }, selected: boolean) {
  const s = useDesign.getState();
  const ids = selected && s.selection.length ? s.selection : page.children.filter((n) => n.visible !== false).map((n) => n.id);
  const box = selectionBounds(page, ids, sizeOf);
  if (!box || !view.width || !view.height) {
    s.setCamera({ x: view.width / 2, y: view.height / 2, zoom: 1 });
    return;
  }
  const pad = 64;
  const zoom = Math.min(selected ? 8 : 1, (view.width - pad * 2) / Math.max(1, box.width), (view.height - pad * 2) / Math.max(1, box.height));
  const z = Math.max(0.02, zoom);
  s.setCamera({ zoom: z, x: view.width / 2 - (box.x + box.width / 2) * z, y: view.height / 2 - (box.y + box.height / 2) * z });
}

/** Zooms to `zoom` keeping the page point under screen point `p` where it is. */
export function zoomAbout(p: Point, zoom: number) {
  const s = useDesign.getState();
  const cam = s.camera;
  const z = Math.min(256, Math.max(0.02, zoom));
  const wx = (p.x - cam.x) / cam.zoom;
  const wy = (p.y - cam.y) / cam.zoom;
  s.setCamera({ zoom: z, x: p.x - wx * z, y: p.y - wy * z });
}

function insidePolygon(p: Point, pts: Point[]): boolean {
  let inside = false;
  for (let i = 0, j = pts.length - 1; i < pts.length; j = i++) {
    const a = pts[i]!;
    const b = pts[j]!;
    if (a.y > p.y !== b.y > p.y && p.x < ((b.x - a.x) * (p.y - a.y)) / (b.y - a.y) + a.x) inside = !inside;
  }
  return inside;
}

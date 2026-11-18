// The design tool's state: which design file is open, its document, the
// selection, the tool, the camera and the undo history. The file lives in
// the project (.atlas/design/files); the agent edits the same file, so the
// store watches it and takes the agent's changes in live.

import { create } from "zustand";
import { ApiError, http } from "../api/http";
import { t } from "../lib/i18n";
import { align, type AlignKind, distribute, fitGroups, moveBy } from "../features/design/geometry";
import { resolveLayout } from "../features/design/layout";
import { mergeDocs } from "../features/design/merge";
import { createNode, type DesignDoc, type DesignNode, emptyDoc, fileSlug, findNode, insertNodes, locate, nextName, type NodeType, type Page, pageOf, patchNodes, type RulerGuide, removeNodes, sanitizeDoc, topmost, updateNodes, walk } from "../features/design/model";
import { nodePath, outlineStroke, pathFields, shapeToPath } from "../features/design/path";
import { bakeText, changedIds, copyNodes, detachInstance, duplicate, groupSelection, instantiate, makeComponent, paste, type Reorder, reorder, reparent, ungroup } from "../features/design/ops";
import { sizeOf } from "../features/design/text";
import { useApp } from "./app";

/** A design kit op, as internal/design/ops.go reads it. */
export interface KitOp {
  op: "set_theme" | "insert" | "add";
  name?: string;
  accent?: string;
  mode?: string;
  block?: string;
  id?: string;
  parent?: string;
  index?: number;
  x?: number;
  y?: number;
  props?: Record<string, unknown>;
  node?: Record<string, unknown>;
}

export type Tool = "select" | "hand" | "frame" | "section" | "slice" | "eyedropper" | "rect" | "ellipse" | "line" | "polygon" | "star" | "text" | "pen" | "pencil" | "comment";
export type BoolOp = "union" | "subtract" | "intersect" | "exclude";

export interface DesignFile {
  name: string;
  rev: string;
  size: number;
  modified_at: number;
}

export interface Camera {
  x: number;
  y: number;
  zoom: number;
}

const HISTORY = 200;
const SAVE_DELAY = 300;
const WATCH_EVERY = 700;
export const ZOOM_MIN = 0.02;
export const ZOOM_MAX = 256;

interface DesignState {
  files: DesignFile[];
  file: string | null;
  doc: DesignDoc | null;
  rev: string;
  pageId: string;
  selection: string[];
  hover: string | null;
  tool: Tool;
  camera: Camera;
  past: DesignDoc[];
  future: DesignDoc[];
  dirty: boolean;
  saving: boolean;
  loading: boolean;
  error: string;
  editing: string | null;
  /** Nodes the agent just changed, highlighted for a moment. */
  flash: string[];
  /** Before a drag: the document to undo back to. */
  gesture: DesignDoc | null;
  /** A field is being scrubbed: changes preview until it is let go. */
  holding: boolean;
  /** A file the design tab should open when it shows (the agent's). */
  target: string;
  /** The project the open file belongs to (its id and path); saves go there even after a switch. */
  wsId: string;
  wsPath: string;

  loadFiles(): Promise<void>;
  open(name: string): Promise<void>;
  create(name: string): Promise<string | null>;
  /** Replaces the document as one undoable step and saves it. */
  commit(doc: DesignDoc, selection?: string[]): void;
  /** Shows a document mid-gesture: not saved, not in history until endGesture. */
  preview(doc: DesignDoc): void;
  beginGesture(): void;
  endGesture(): void;
  cancelGesture(): void;
  /** Holds (true) or releases (false) a run of changes as one undo step. */
  hold(on: boolean): void;
  undo(): void;
  redo(): void;
  /** Drops the last step as if it never happened (an empty new text box). */
  discard(): void;
  select(ids: string[]): void;
  setHover(id: string | null): void;
  setTool(tool: Tool): void;
  setCamera(camera: Camera): void;
  setPage(id: string): void;
  setEditing(id: string | null): void;
  /** The path whose points are being edited. */
  editPath: string | null;
  setEditPath(id: string | null): void;
  /** Shift+A: auto layout on the selected frames, or the selection wrapped in a new auto-layout frame. */
  addAutoLayout(): void;
  /** Frames' layout grids are drawn (Ctrl+Shift+4). */
  showGrids: boolean;
  toggleGrids(): void;
  /** Rulers along the top and left of the canvas (Shift+R); guides are pulled out of them. */
  showRulers: boolean;
  toggleRulers(): void;
  /** Adds (index -1), moves or, with `at` null, removes a ruler guide of the open page, as one undo step unless `live`. */
  setGuide(index: number, guide: RulerGuide | null, live?: boolean): void;
  /** The image whose crop is being adjusted on the canvas (Enter or double-click on an image). */
  cropping: string | null;
  setCropping(id: string | null): void;
  /** The presentation is open over the editor. */
  presenting: boolean;
  setPresenting(on: boolean): void;
  /** The swarm dialog is open. */
  swarming: boolean;
  setSwarming(on: boolean): void;
  /** The design kit library is open. */
  library: boolean;
  setLibrary(on: boolean): void;
  /**
   * Runs design kit ops (set_theme, insert, icon adds: internal/design/kit.go)
   * on the open document through the server, as one undoable step that
   * selects what they made. Resolves to the made nodes' ids.
   */
  applyKit(ops: KitOp[]): Promise<string[]>;
  /** The comment thread open in the side panel. */
  activeComment: string | null;
  setActiveComment(id: string | null): void;
  /** Combines the selected shapes into a boolean group. */
  boolean(op: BoolOp): void;
  /** Turns the selected shapes into editable paths. */
  flatten(): void;
  outlineStroke(): void;

  addNode(type: NodeType, props: Partial<DesignNode>, parentId?: string | null): string | null;
  patch(ids: string[], patch: Partial<DesignNode> | ((n: DesignNode) => Partial<DesignNode>)): void;
  remove(): void;
  duplicate(): void;
  copy(): void;
  cut(): void;
  paste(at?: { x: number; y: number }): void;
  group(): void;
  ungroup(): void;
  frameSelection(): void;
  makeComponent(): void;
  instance(componentId: string, at?: { x: number; y: number }): void;
  detach(): void;
  reorder(how: Reorder): void;
  moveInto(ids: string[], parentId: string | null, index?: number): void;
  nudge(dx: number, dy: number): void;
  align(kind: AlignKind): void;
  distribute(axis: "x" | "y"): void;
  selectAll(): void;
  addPage(): void;
  renamePage(id: string, name: string): void;
  removePage(id: string): void;
  renameDoc(name: string): void;
  insertImage(src: string, width: number, height: number, at?: { x: number; y: number }): void;

  save(): Promise<void>;
  /** Looks for changes made to the file elsewhere (by the agent). */
  poll(): Promise<void>;
}

const api = (suffix: string, ws = useApp.getState().workspace?.id ?? "") => `/api/workspaces/${encodeURIComponent(ws)}/design${suffix}`;
const message = (err: unknown) => (err instanceof Error ? err.message : String(err));

/** The id of the open file's project: the current one while it is open (its id can change on a reconnect), else the one it was opened in. */
function ownWorkspace(): string | undefined {
  const { wsId, wsPath } = useDesign.getState();
  const w = useApp.getState().workspace;
  if (!wsId) return undefined;
  return w && w.path === wsPath ? w.id : wsId;
}

/** The document as last in step with the file: what an unsaved edit started from. */
let synced: DesignDoc | null = null;
let saveTimer: ReturnType<typeof setTimeout> | undefined;
let flashTimer: ReturnType<typeof setTimeout> | undefined;
let clipboard: DesignNode[] = [];
/** A save is running; another is wanted when it ends. */
let saveAgain = false;

export const useDesign = create<DesignState>((set, get) => {
  const scheduleSave = () => {
    clearTimeout(saveTimer);
    saveTimer = setTimeout(() => void get().save(), SAVE_DELAY);
  };
  const page = () => {
    const s = get();
    return s.doc ? pageOf(s.doc, s.pageId) : null;
  };
  /** Runs a command on the open document and its selection, committing what it returns. */
  const run = (fn: (doc: DesignDoc, pageId: string, selection: string[]) => { doc: DesignDoc; selection: string[] } | DesignDoc | null) => {
    const s = get();
    if (!s.doc) return;
    const out = fn(s.doc, s.pageId, s.selection);
    if (!out) return;
    if ("pages" in out) get().commit(out);
    else get().commit(out.doc, out.selection);
  };
  const adopt = (doc: DesignDoc, rev: string, name: string) => {
    synced = doc;
    const s = get();
    const pageId = doc.pages.some((p) => p.id === s.pageId) ? s.pageId : doc.pages[0]!.id;
    const alive = (id: string) => findNode(doc, id) !== null;
    set({ doc, rev, file: name, pageId, selection: s.file === name ? s.selection.filter(alive) : [], dirty: false, error: "" });
  };

  return {
    files: [],
    file: null,
    doc: null,
    rev: "",
    pageId: "",
    selection: [],
    hover: null,
    tool: "select",
    camera: { x: 0, y: 0, zoom: 1 },
    past: [],
    future: [],
    dirty: false,
    saving: false,
    loading: false,
    error: "",
    editing: null,
    editPath: null,
    presenting: false,
    showGrids: true,
    showRulers: false,
    cropping: null,
    swarming: false,
    library: false,
    activeComment: null,
    flash: [],
    gesture: null,
    holding: false,
    target: "",
    wsId: "",
    wsPath: "",

    async loadFiles() {
      try {
        const res = await http.get<{ files: DesignFile[] | null }>(api("/files"));
        set({ files: res.files ?? [] });
      } catch (err) {
        set({ error: message(err) });
      }
    },

    async open(name) {
      if (get().dirty) await get().save();
      set({ loading: true });
      try {
        const res = await http.get<{ name: string; rev: string; doc: unknown }>(api(`/file?name=${encodeURIComponent(name)}`));
        const doc = sanitizeDoc(res.doc);
        set({ past: [], future: [], selection: [], editing: null, camera: { x: 0, y: 0, zoom: 1 }, pageId: doc.pages[0]!.id, wsId: useApp.getState().workspace?.id ?? "", wsPath: useApp.getState().workspace?.path ?? "" });
        adopt(doc, res.rev, res.name);
        try {
          localStorage.setItem(`atlas.design.last.${useApp.getState().workspace?.id ?? ""}`, res.name);
        } catch {
          // Only a convenience.
        }
      } catch (err) {
        set({ error: message(err) });
        useApp.getState().toast(`${t("design.openFailed")}: ${message(err)}`, "danger");
      } finally {
        set({ loading: false });
      }
    },

    async create(name) {
      const slug = fileSlug(name);
      const taken = new Set(get().files.map((f) => f.name));
      let unique = slug;
      for (let i = 2; taken.has(unique); i++) unique = `${slug}-${i}`;
      const doc = emptyDoc(name.trim() || "Untitled");
      try {
        await http.post(api("/file"), { name: unique, doc, create: true });
        await get().loadFiles();
        await get().open(unique);
        return unique;
      } catch (err) {
        useApp.getState().toast(`${t("design.createFailed")}: ${message(err)}`, "danger");
        return null;
      }
    },

    commit(doc, selection) {
      const s = get();
      if (!s.doc) return;
      const next = bakeText(fitGroups(resolveLayout(doc, s.gesture ?? s.doc, sizeOf), sizeOf));
      // A gesture's last preview is already shown; what counts is whether it differs from where it began.
      const base = s.gesture ?? s.doc;
      if (next === base && selection === undefined) {
        if (s.gesture) set({ gesture: null });
        return;
      }
      set({
        doc: next,
        past: next === base ? s.past : [...s.past, base].slice(-HISTORY),
        future: next === base ? s.future : [],
        gesture: null,
        ...(selection ? { selection } : {}),
        dirty: s.dirty || next !== base,
      });
      if (next !== base) scheduleSave();
    },

    preview(doc) {
      if (!get().gesture) set({ gesture: get().doc });
      set({ doc });
    },

    beginGesture() {
      set({ gesture: get().doc });
    },

    endGesture() {
      const s = get();
      if (!s.gesture || !s.doc) return set({ gesture: null });
      if (s.doc === s.gesture) return set({ gesture: null });
      get().commit(s.doc);
    },

    hold(on) {
      if (on) set({ holding: true, gesture: get().doc });
      else {
        set({ holding: false });
        get().endGesture();
      }
    },

    cancelGesture() {
      const s = get();
      if (s.gesture) set({ doc: s.gesture, gesture: null });
    },

    undo() {
      const s = get();
      const prev = s.past[s.past.length - 1];
      if (!prev || !s.doc) return;
      set({ doc: prev, past: s.past.slice(0, -1), future: [s.doc, ...s.future].slice(0, HISTORY), dirty: true, editing: null, selection: s.selection.filter((id) => findNode(prev, id)) });
      scheduleSave();
    },

    discard() {
      const s = get();
      const prev = s.past[s.past.length - 1];
      if (!prev) return;
      set({ doc: prev, past: s.past.slice(0, -1), dirty: true, editing: null, selection: [] });
      scheduleSave();
    },

    redo() {
      const s = get();
      const next = s.future[0];
      if (!next || !s.doc) return;
      set({ doc: next, future: s.future.slice(1), past: [...s.past, s.doc].slice(-HISTORY), dirty: true, editing: null, selection: s.selection.filter((id) => findNode(next, id)) });
      scheduleSave();
    },

    select(ids) {
      const s = get();
      if (ids.length === s.selection.length && ids.every((id, i) => s.selection[i] === id)) return;
      set({ selection: ids, editing: s.editing && ids.includes(s.editing) ? s.editing : null, editPath: s.editPath && ids.includes(s.editPath) ? s.editPath : null });
    },
    setHover(id) {
      if (get().hover !== id) set({ hover: id });
    },
    setTool(tool) {
      set({ tool, editing: null, editPath: null });
    },
    setActiveComment(id) {
      set({ activeComment: id });
    },
    setSwarming(on) {
      set({ swarming: on });
    },
    addAutoLayout() {
      const s = get();
      if (!s.doc || !s.selection.length) return;
      const page = pageOf(s.doc, s.pageId);
      const nodes = s.selection.map((id) => locate(page, id)?.node).filter((n): n is DesignNode => !!n);
      const frames = nodes.length > 0 && nodes.every((n) => (n.type === "frame" || n.type === "component") && !n.layout);
      // The stack runs the way the content already does: wider than tall spreads as a row.
      const direction = (kids: DesignNode[]): "row" | "column" => {
        if (kids.length < 2) return "column";
        const xs = kids.map((k) => k.x + k.width / 2);
        const ys = kids.map((k) => k.y + k.height / 2);
        return Math.max(...xs) - Math.min(...xs) > Math.max(...ys) - Math.min(...ys) ? "row" : "column";
      };
      const gapOf = (kids: DesignNode[], dir: "row" | "column"): number => {
        const sorted = [...kids].sort((a, b) => (dir === "row" ? a.x - b.x : a.y - b.y));
        const gaps = sorted.slice(1).map((k, i) => (dir === "row" ? k.x - (sorted[i]!.x + sorted[i]!.width) : k.y - (sorted[i]!.y + sorted[i]!.height)));
        const avg = gaps.length ? gaps.reduce((a, b) => a + b, 0) / gaps.length : 8;
        return Math.max(0, Math.round(avg));
      };
      if (frames) {
        s.patch(
          nodes.map((n) => n.id),
          (n) => {
            const kids = n.children ?? [];
            const dir = direction(kids);
            const minX = kids.length ? Math.min(...kids.map((k) => k.x)) : 0;
            const minY = kids.length ? Math.min(...kids.map((k) => k.y)) : 0;
            const maxX = kids.length ? Math.max(...kids.map((k) => k.x + k.width)) : n.width;
            const maxY = kids.length ? Math.max(...kids.map((k) => k.y + k.height)) : n.height;
            const pad: [number, number, number, number] = kids.length ? [Math.max(0, Math.round(minY)), Math.max(0, Math.round(n.width - maxX)), Math.max(0, Math.round(n.height - maxY)), Math.max(0, Math.round(minX))] : [0, 0, 0, 0];
            return { layout: { dir, gap: gapOf(kids, dir), pad, main: "start", cross: "start" } as NonNullable<DesignNode["layout"]> };
          },
        );
        return;
      }
      const dir = direction(nodes);
      const gap = gapOf(nodes, dir);
      s.frameSelection();
      const after = get();
      if (after.selection.length === 1) after.patch(after.selection, () => ({ layout: { dir, gap, pad: [0, 0, 0, 0], main: "start", cross: "start" } as NonNullable<DesignNode["layout"]>, sizing_x: "hug", sizing_y: "hug" }));
    },
    toggleGrids() {
      set((s) => ({ showGrids: !s.showGrids }));
    },
    toggleRulers() {
      set((s) => ({ showRulers: !s.showRulers }));
    },
    setCropping(id) {
      set({ cropping: id });
    },
    setGuide(index, guide, live) {
      const s = get();
      if (!s.doc) return;
      const rewrite = (p: Page): Page => {
        const list = [...(p.guides ?? [])];
        if (guide === null) list.splice(index, 1);
        else if (index < 0 || index >= list.length) list.push(guide);
        else list[index] = guide;
        const next: Page = { ...p, guides: list };
        if (!list.length) delete next.guides;
        return next;
      };
      const doc = { ...s.doc, pages: s.doc.pages.map((p) => (p.id === s.pageId ? rewrite(p) : p)) };
      if (live) get().preview(doc);
      else get().commit(doc, s.selection);
    },
    setPresenting(on) {
      set({ presenting: on, editing: null, editPath: null });
    },
    setEditPath(id) {
      const s = get();
      if (!id) return set({ editPath: null });
      if (!s.doc || !findNode(s.doc, id) || findNode(s.doc, id)!.at.node.type !== "path") return;
      set({ editPath: id, tool: "select", selection: [id] });
    },
    boolean(op) {
      run((doc, pageId, sel) => {
        const page = pageOf(doc, pageId);
        const nodes = sel.map((id) => locate(page, id)?.node).filter((n): n is DesignNode => !!n);
        // A boolean group takes the shapes as they are; an existing one just changes its operation.
        if (nodes.length === 1 && nodes[0]!.type === "group") return { doc: patchNodes(doc, [nodes[0]!.id], { bool_op: op, name: t(`design.${op}` as never) }), selection: sel };
        if (nodes.length < 2) return null;
        const out = groupSelection(doc, pageId, sel, "group");
        if (!out) return null;
        const first = nodes[0]!;
        return { doc: patchNodes(out.doc, out.selection, { bool_op: op, name: t(`design.${op}` as never), fills: first.fills?.length ? first.fills : [{ type: "solid", color: "#D9D9D9" }] }), selection: out.selection };
      });
    },
    flatten() {
      run((doc, pageId, sel) => {
        const page = pageOf(doc, pageId);
        let next = doc;
        let changed = false;
        for (const id of topmost(page, sel)) {
          const at = locate(page, id);
          if (!at || at.node.children?.length || at.node.type === "path" || at.node.type === "text" || at.node.type === "group" || at.node.type === "instance") continue;
          const subs = shapeToPath(at.node);
          if (!subs) continue;
          const fields = pathFields({ x: 0, y: 0 }, subs);
          next = updateNodes(next, [id], (n) => {
            const { radius: _r, sides: _s, inner: _i, clip: _c, children: _k, layout: _l, src: _src, fit: _f, ...rest } = n;
            return { ...rest, type: "path", d: fields.d, view_box: fields.view_box, width: fields.width, height: fields.height, x: n.x + fields.x, y: n.y + fields.y } as DesignNode;
          });
          changed = true;
        }
        return changed ? { doc: next, selection: sel } : null;
      });
    },
    outlineStroke() {
      run((doc, pageId, sel) => {
        const page = pageOf(doc, pageId);
        let next = doc;
        let changed = false;
        for (const id of topmost(page, sel)) {
          const at = locate(page, id);
          const n = at?.node;
          if (!n || n.children?.length || n.type === "text" || n.type === "group" || n.type === "instance") continue;
          const stroke = n.strokes?.find((s) => s.visible !== false && s.width > 0);
          if (!stroke) continue;
          const subs = n.type === "path" ? nodePath(n) : shapeToPath(n);
          if (!subs) continue;
          const outline = outlineStroke(subs, stroke.width);
          if (!outline.length) continue;
          const f = pathFields({ x: 0, y: 0 }, outline);
          next = updateNodes(next, [id], (m) => {
            const { radius: _r, sides: _s, inner: _i, clip: _c, children: _k, layout: _l, src: _src, fit: _f, strokes: _st, ...rest } = m;
            return { ...rest, type: "path", d: f.d, view_box: f.view_box, width: f.width, height: f.height, x: m.x + f.x, y: m.y + f.y, fills: [{ type: "solid", color: stroke.color, ...(stroke.variable ? { variable: stroke.variable } : {}), ...(stroke.opacity !== undefined ? { opacity: stroke.opacity } : {}) }] } as DesignNode;
          });
          changed = true;
        }
        return changed ? { doc: next, selection: sel } : null;
      });
    },
    setCamera(camera) {
      set({ camera: { x: camera.x, y: camera.y, zoom: Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, camera.zoom)) } });
    },
    setPage(id) {
      set({ pageId: id, selection: [], editing: null });
    },
    setEditing(id) {
      set({ editing: id });
    },

    addNode(type, props, parentId = null) {
      const s = get();
      const p = page();
      if (!s.doc || !p) return null;
      const name = props.name ?? nextName(p, createNode(type).name);
      const node = createNode(type, { ...props, name });
      get().commit(insertNodes(s.doc, p.id, parentId, [node]), [node.id]);
      return node.id;
    },

    patch(ids, patchValue) {
      const s = get();
      if (!s.doc || ids.length === 0) return;
      const next = patchNodes(s.doc, ids, patchValue);
      if (s.holding) get().preview(fitGroups(resolveLayout(next, s.gesture ?? s.doc, sizeOf), sizeOf));
      else get().commit(next);
    },

    remove() {
      run((doc, _p, sel) => (sel.length ? { doc: removeNodes(doc, sel), selection: [] } : null));
    },

    duplicate() {
      run((doc, pageId, sel) => duplicate(doc, pageId, sel, 10));
    },

    copy() {
      const p = page();
      if (!p) return;
      clipboard = copyNodes(p, get().selection);
      const text = JSON.stringify({ atlasDesign: 1, nodes: clipboard });
      void navigator.clipboard?.writeText(text).catch(() => undefined);
    },

    cut() {
      get().copy();
      get().remove();
    },

    paste(at) {
      run((doc, pageId, sel) => paste(doc, pageId, clipboard, sel, at));
    },

    group() {
      run((doc, pageId, sel) => groupSelection(doc, pageId, sel, "group"));
    },
    ungroup() {
      run((doc, pageId, sel) => ungroup(doc, pageId, sel));
    },
    frameSelection() {
      run((doc, pageId, sel) => {
        const out = groupSelection(doc, pageId, sel, "frame");
        if (!out) return null;
        return { doc: patchNodes(out.doc, out.selection, { fills: [{ type: "solid", color: "#FFFFFF" }], clip: true }), selection: out.selection };
      });
    },
    makeComponent() {
      run((doc, pageId, sel) => makeComponent(doc, pageId, sel));
    },
    instance(componentId, at) {
      run((doc, pageId) => instantiate(doc, pageId, componentId, at));
    },
    detach() {
      run((doc, _p, sel) => detachInstance(doc, sel));
    },
    reorder(how) {
      run((doc, pageId, sel) => (sel.length ? reorder(doc, pageId, sel, how) : null));
    },
    moveInto(ids, parentId, index) {
      run((doc, pageId) => reparent(doc, pageId, ids, parentId, index));
    },
    nudge(dx, dy) {
      run((doc, pageId, sel) => {
        const p = pageOf(doc, pageId);
        const ids = topmost(p, sel).filter((id) => !locate(p, id)?.node.locked);
        return ids.length ? moveBy(doc, p, ids, dx, dy) : null;
      });
    },
    align(kind) {
      run((doc, pageId, sel) => align(doc, pageOf(doc, pageId), topmost(pageOf(doc, pageId), sel), kind, sizeOf));
    },
    distribute(axis) {
      run((doc, pageId, sel) => distribute(doc, pageOf(doc, pageId), topmost(pageOf(doc, pageId), sel), axis, sizeOf));
    },
    selectAll() {
      const s = get();
      const p = page();
      if (!p) return;
      const first = s.selection[0] ? locate(p, s.selection[0]) : null;
      const level = first?.parent?.children ?? p.children;
      set({ selection: level.filter((n) => n.visible !== false && !n.locked).map((n) => n.id) });
    },

    addPage() {
      const s = get();
      if (!s.doc) return;
      const base = emptyDoc().pages[0]!;
      const pageNew = { ...base, name: `Page ${s.doc.pages.length + 1}` };
      get().commit({ ...s.doc, pages: [...s.doc.pages, pageNew] }, []);
      set({ pageId: pageNew.id });
    },
    renamePage(id, name) {
      const s = get();
      if (!s.doc || !name.trim()) return;
      get().commit({ ...s.doc, pages: s.doc.pages.map((p) => (p.id === id ? { ...p, name: name.trim().slice(0, 200) } : p)) });
    },
    removePage(id) {
      const s = get();
      if (!s.doc || s.doc.pages.length < 2) return;
      const pages = s.doc.pages.filter((p) => p.id !== id);
      get().commit({ ...s.doc, pages }, []);
      if (s.pageId === id) set({ pageId: pages[0]!.id });
    },
    renameDoc(name) {
      const s = get();
      if (!s.doc || !name.trim()) return;
      get().commit({ ...s.doc, name: name.trim().slice(0, 200) });
    },

    insertImage(src, width, height, at) {
      const p = page();
      if (!p) return;
      const scale = Math.min(1, 800 / Math.max(width, height, 1));
      const w = Math.round(width * scale);
      const h = Math.round(height * scale);
      get().addNode("image", { src, width: w, height: h, x: Math.round((at?.x ?? 0) - w / 2), y: Math.round((at?.y ?? 0) - h / 2), fit: "cover" });
      set({ tool: "select" });
    },

    async save() {
      const s = get();
      if (!s.doc || !s.file || !s.dirty) return;
      if (s.saving) {
        saveAgain = true;
        return;
      }
      clearTimeout(saveTimer);
      const doc = s.doc;
      const name = s.file;
      set({ saving: true });
      try {
        const res = await http.post<{ name: string; rev: string }>(api("/file", ownWorkspace()), { name, doc, base_rev: s.rev });
        if (get().file === name) {
          synced = doc;
          set({ rev: res.rev, dirty: get().doc !== doc, error: "" });
        }
      } catch (err) {
        if (err instanceof ApiError && err.status === 409 && get().file === name) {
          // Changed elsewhere (the agent) since this window last read it:
          // the two edits are merged layer by layer, and the result is one undo step.
          const mine = get().doc;
          try {
            const res = await http.get<{ rev: string; doc: unknown }>(api(`/file?name=${encodeURIComponent(name)}`, ownWorkspace()));
            const theirs = sanitizeDoc(res.doc);
            if (mine && get().file === name) {
              const merged = synced ? mergeDocs(synced, mine, theirs) : theirs;
              synced = theirs;
              set({ doc: merged, rev: res.rev, dirty: merged !== theirs, past: [...get().past, mine].slice(-HISTORY), future: [], flash: changedIds(mine, merged) });
              clearTimeout(flashTimer);
              flashTimer = setTimeout(() => set({ flash: [] }), 1600);
              if (get().dirty) scheduleSave();
            }
          } catch {
            set({ dirty: false });
            await get().poll();
          }
          useApp.getState().toast(t("design.conflict"));
        } else {
          set({ error: message(err) });
          useApp.getState().toast(`${t("design.saveFailed")}: ${message(err)}`, "danger");
        }
      } finally {
        set({ saving: false });
        if (saveAgain) {
          saveAgain = false;
          if (get().dirty) scheduleSave();
        }
      }
    },

    setLibrary(on) {
      set({ library: on });
    },

    async applyKit(ops) {
      const s = get();
      if (!s.doc || ops.length === 0) return [];
      try {
        const res = await http.post<{ doc: unknown; created: string[] | null; notes: string[] | null }>(api("/kit/apply", ownWorkspace()), { doc: s.doc, page: s.pageId, ops });
        // An edit that landed while the server worked wins; the person can click again.
        if (get().doc !== s.doc) return [];
        const created = res.created ?? [];
        get().commit(sanitizeDoc(res.doc), created.length ? created : undefined);
        return created;
      } catch (err) {
        useApp.getState().toast(`${t("design.kitFailed")}: ${message(err)}`, "danger");
        return [];
      }
    },

    async poll() {
      const s = get();
      if (!s.file || s.saving || s.dirty || s.gesture || s.loading) return;
      const name = s.file;
      try {
        const res = await http.get<{ name: string; rev: string; doc?: unknown; unchanged?: boolean }>(api(`/file?name=${encodeURIComponent(name)}&rev=${encodeURIComponent(s.rev)}`, ownWorkspace()));
        const now = get();
        if (res.unchanged || res.rev === now.rev || now.file !== name || now.dirty || now.saving || now.gesture) return;
        const doc = sanitizeDoc(res.doc);
        const before = now.doc;
        const flash = before ? changedIds(before, doc) : [];
        adopt(doc, res.rev, name);
        // The agent's edit is one step back in undo, like any other.
        if (before) set({ past: [...now.past, before].slice(-HISTORY), future: [] });
        if (flash.length) {
          set({ flash });
          clearTimeout(flashTimer);
          flashTimer = setTimeout(() => set({ flash: [] }), 1600);
        }
      } catch (err) {
        if (err instanceof ApiError && err.status === 404) set({ error: t("design.gone") });
      }
    },
  };
});

let watchers = 0;
let watchTimer: ReturnType<typeof setInterval> | undefined;
/** Starts looking for outside changes while a design view is on screen. Returns the stop. */
export function watchDesign(): () => void {
  watchers++;
  if (watchers === 1) watchTimer = setInterval(() => void useDesign.getState().poll(), WATCH_EVERY);
  return () => {
    watchers--;
    if (watchers === 0) clearInterval(watchTimer);
  };
}

/** The name opened last in this project, to open again. */
export function lastDesign(): string {
  try {
    return localStorage.getItem(`atlas.design.last.${useApp.getState().workspace?.id ?? ""}`) ?? "";
  } catch {
    return "";
  }
}

/** For a paste from another app: nodes copied from a design, if the text is one. */
export function readClipboardText(text: string): DesignNode[] | null {
  try {
    const value = JSON.parse(text) as { atlasDesign?: number; nodes?: unknown[] };
    if (value.atlasDesign !== 1 || !Array.isArray(value.nodes)) return null;
    const doc = sanitizeDoc({ pages: [{ id: "clip", children: value.nodes }] });
    return doc.pages[0]!.children;
  } catch {
    return null;
  }
}

export function setClipboard(nodes: DesignNode[]) {
  clipboard = nodes;
}

/** Every node id on a page, in drawing order, for tests and the layers list. */
export function pageIds(doc: DesignDoc, pageId: string): string[] {
  const ids: string[] = [];
  walk(pageOf(doc, pageId).children, (n) => void ids.push(n.id));
  return ids;
}

// The design tool as a tab of the side panel: a toolbar, the layers on the
// left, the canvas, and the properties on the right. Its files live in the
// project under .atlas/design/files, where the agent edits them too.

import { useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from "react";
import { t } from "../../lib/i18n";
import { useApp } from "../../stores/app";
import { lastDesign, readClipboardText, setClipboard, type Tool, useDesign, watchDesign } from "../../stores/design";
import { Canvas, zoomAbout, zoomToFit } from "./Canvas";
import { IconEllipse, IconFrame, IconHand, IconImage, IconLine, IconMove, IconSection, IconSlice, IconEyedropper, IconComment, IconPenTool, IconPencil, IconPolygon, IconRect, IconRedo, IconStar, IconText, IconUndo } from "./icons";
import { Inspector } from "./Inspector";
import { LeftPanel } from "./Layers";
import { pageOf } from "./model";
import { Present } from "./Present";
import { loadKitFonts } from "./fonts";
import { Library } from "./Library";
import { Swarm } from "./Swarm";
import { setMeasure } from "./text";
import "./design.css";

const TOOLS: { tool: Tool; key: string; icon: (p: object) => React.ReactNode; label: string }[] = [
  { tool: "select", key: "V", icon: IconMove, label: "design.tool.select" },
  { tool: "hand", key: "H", icon: IconHand, label: "design.tool.hand" },
  { tool: "frame", key: "F", icon: IconFrame, label: "design.tool.frame" },
  { tool: "section", key: "⇧S", icon: IconSection, label: "design.tool.section" },
  { tool: "slice", key: "S", icon: IconSlice, label: "design.tool.slice" },
  { tool: "rect", key: "R", icon: IconRect, label: "design.tool.rect" },
  { tool: "ellipse", key: "O", icon: IconEllipse, label: "design.tool.ellipse" },
  { tool: "line", key: "L", icon: IconLine, label: "design.tool.line" },
  { tool: "polygon", key: "", icon: IconPolygon, label: "design.tool.polygon" },
  { tool: "star", key: "", icon: IconStar, label: "design.tool.star" },
  { tool: "pen", key: "P", icon: IconPenTool, label: "design.tool.pen" },
  { tool: "pencil", key: "⇧P", icon: IconPencil, label: "design.tool.pencil" },
  { tool: "text", key: "T", icon: IconText, label: "design.tool.text" },
  { tool: "eyedropper", key: "I", icon: IconEyedropper, label: "design.tool.eyedropper" },
  { tool: "comment", key: "C", icon: IconComment, label: "design.tool.comment" },
];
const KEY_TOOL: Record<string, Tool> = { v: "select", h: "hand", f: "frame", a: "frame", s: "slice", i: "eyedropper", r: "rect", o: "ellipse", l: "line", t: "text", p: "pen", c: "comment" };

const MAX_IMAGE_BYTES = 12 * 1024 * 1024;

function readImage(file: File): Promise<{ src: string; width: number; height: number }> {
  return new Promise((resolve, reject) => {
    if (!/^image\/(png|jpeg|gif|webp|svg\+xml)$/.test(file.type)) return reject(new Error(t("design.notImage")));
    if (file.size > MAX_IMAGE_BYTES) return reject(new Error(t("design.imageTooLarge")));
    const reader = new FileReader();
    reader.onload = () => {
      const src = String(reader.result);
      const img = new Image();
      img.onload = () => resolve({ src, width: img.naturalWidth || 200, height: img.naturalHeight || 200 });
      img.onerror = () => reject(new Error(t("design.notImage")));
      img.src = src;
    };
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(file);
  });
}

/** The page point at the middle of the canvas. */
function viewCenter(): { x: number; y: number } {
  const el = document.querySelector<HTMLElement>(".design-canvas");
  const cam = useDesign.getState().camera;
  const w = el?.clientWidth ?? 800;
  const h = el?.clientHeight ?? 600;
  return { x: (w / 2 - cam.x) / cam.zoom, y: (h / 2 - cam.y) / cam.zoom };
}

function canvasSize() {
  const el = document.querySelector<HTMLElement>(".design-canvas");
  return { width: el?.clientWidth ?? 800, height: el?.clientHeight ?? 600 };
}

async function insertFiles(files: File[], at?: { x: number; y: number }) {
  for (const file of files) {
    try {
      const img = await readImage(file);
      useDesign.getState().insertImage(img.src, img.width, img.height, at ?? viewCenter());
    } catch (err) {
      useApp.getState().toast(err instanceof Error ? err.message : String(err), "danger");
    }
  }
}

function FilePicker() {
  const files = useDesign((s) => s.files);
  const file = useDesign((s) => s.file);
  const open = useDesign((s) => s.open);
  const create = useDesign((s) => s.create);
  return (
    <select
      className="design-file"
      aria-label={t("design.file")}
      value={file ?? ""}
      onChange={(e) => {
        const v = e.target.value;
        if (v === "\u0000new") void create(t("design.untitled"));
        else if (v) void open(v);
      }}
    >
      {files.map((f) => (
        <option key={f.name} value={f.name}>
          {f.name}
        </option>
      ))}
      {file && !files.some((f) => f.name === file) && <option value={file}>{file}</option>}
      <option value={"\u0000new"}>{t("design.newFile")}</option>
    </select>
  );
}

function Toolbar() {
  const tool = useDesign((s) => s.tool);
  const setTool = useDesign((s) => s.setTool);
  const camera = useDesign((s) => s.camera);
  const past = useDesign((s) => s.past.length);
  const future = useDesign((s) => s.future.length);
  const swarmOpen = useDesign((s) => s.swarming);
  const libraryOpen = useDesign((s) => s.library);
  const saving = useDesign((s) => s.saving);
  const dirty = useDesign((s) => s.dirty);
  const imageInput = useRef<HTMLInputElement>(null);
  return (
    <header className="design-bar" role="toolbar" aria-label={t("design.tools")}>
      <FilePicker />
      <div className="design-toolgroup">
        {TOOLS.map(({ tool: id, key, icon: I, label }) => (
          <button key={id} type="button" className="icon-button design-tool" data-tool={id} data-on={tool === id || undefined} aria-pressed={tool === id} aria-label={t(label as never)} title={key ? `${t(label as never)} (${key})` : t(label as never)} onClick={() => setTool(id)}>
            <I />
          </button>
        ))}
        <button type="button" className="icon-button design-tool" aria-label={t("design.tool.image")} title={`${t("design.tool.image")} (Shift+Ctrl+K)`} onClick={() => imageInput.current?.click()}>
          <IconImage />
        </button>
        <input
          ref={imageInput}
          type="file"
          accept="image/png,image/jpeg,image/gif,image/webp,image/svg+xml"
          multiple
          hidden
          data-testid="design-image-input"
          onChange={(e) => {
            const list = [...(e.target.files ?? [])];
            e.target.value = "";
            void insertFiles(list);
          }}
        />
      </div>
      <div className="design-toolgroup">
        <button type="button" className="design-chip" aria-label={t("design.library")} aria-expanded={libraryOpen} title={`${t("design.libraryHint")} (Shift+I)`} onClick={() => useDesign.getState().setLibrary(!useDesign.getState().library)}>
          ◇ {t("design.library")}
        </button>
        <button type="button" className="design-chip" aria-label={t("design.swarm")} aria-expanded={swarmOpen} title={t("design.swarmHint")} onClick={() => useDesign.getState().setSwarming(!useDesign.getState().swarming)}>
          ✦ {t("design.swarm")}
        </button>
        <button type="button" className="design-chip" aria-label={t("design.present")} title={`${t("design.present")} (Ctrl+Alt+Enter)`} onClick={() => useDesign.getState().setPresenting(true)}>
          ▶ {t("design.present")}
        </button>
        <button type="button" className="icon-button" aria-label={t("design.undo")} title={`${t("design.undo")} (Ctrl+Z)`} disabled={past === 0} onClick={() => useDesign.getState().undo()}>
          <IconUndo />
        </button>
        <button type="button" className="icon-button" aria-label={t("design.redo")} title={`${t("design.redo")} (Ctrl+Shift+Z)`} disabled={future === 0} onClick={() => useDesign.getState().redo()}>
          <IconRedo />
        </button>
      </div>
      <span className="design-status" role="status" aria-live="polite">
        {saving ? t("design.saving") : dirty ? t("design.unsaved") : t("design.saved")}
      </span>
      <div className="design-zoom">
        <button type="button" className="icon-button" aria-label={t("design.zoomOut")} onClick={() => zoomAbout({ x: canvasSize().width / 2, y: canvasSize().height / 2 }, camera.zoom / 1.25)}>
          −
        </button>
        <button
          type="button"
          className="design-zoom-value"
          aria-label={t("design.zoomFit")}
          title={`${t("design.zoomFit")} (Shift+1)`}
          onClick={() => {
            const s = useDesign.getState();
            if (s.doc) zoomToFit(pageOf(s.doc, s.pageId), canvasSize(), false);
          }}
        >
          {Math.round(camera.zoom * 100)}%
        </button>
        <button type="button" className="icon-button" aria-label={t("design.zoomIn")} onClick={() => zoomAbout({ x: canvasSize().width / 2, y: canvasSize().height / 2 }, camera.zoom * 1.25)}>
          +
        </button>
      </div>
    </header>
  );
}

function Start() {
  const files = useDesign((s) => s.files);
  const open = useDesign((s) => s.open);
  const create = useDesign((s) => s.create);
  const [name, setName] = useState("");
  return (
    <div className="design-start">
      <h2>{t("design.title")}</h2>
      <p className="design-muted">{t("design.intro")}</p>
      <form
        className="design-new"
        onSubmit={(e) => {
          e.preventDefault();
          void create(name || t("design.untitled"));
        }}
      >
        <input aria-label={t("design.newName")} placeholder={t("design.newName")} value={name} onChange={(e) => setName(e.target.value)} />
        <button type="submit" className="design-button">
          {t("design.newFile")}
        </button>
      </form>
      {files.length > 0 && (
        <ul className="design-files" aria-label={t("design.files")}>
          {files.map((f) => (
            <li key={f.name}>
              <button type="button" className="design-file-row" onClick={() => void open(f.name)}>
                <span>{f.name}</span>
                <span className="design-muted">{new Date(f.modified_at * 1000).toLocaleString()}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function onKey(e: ReactKeyboardEvent<HTMLDivElement>) {
  const target = e.target as HTMLElement;
  if (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.tagName === "SELECT" || target.isContentEditable) return;
  const s = useDesign.getState();
  if (!s.doc) return;
  const mod = e.ctrlKey || e.metaKey;
  const key = e.key.toLowerCase();
  let handled = true;
  if (mod && key === "z" && !e.shiftKey) s.undo();
  else if ((mod && key === "z" && e.shiftKey) || (mod && key === "y")) s.redo();
  else if (mod && key === "a") s.selectAll();
  else if (mod && key === "d") s.duplicate();
  else if (mod && key === "c") s.copy();
  else if (mod && key === "x") s.cut();
  else if (mod && key === "v") return; // The paste event handles it, with the system clipboard.
  else if (mod && e.altKey && key === "k") s.makeComponent();
  else if (mod && e.altKey && key === "b") s.detach();
  else if (mod && e.shiftKey && key === "k") document.querySelector<HTMLInputElement>('[data-testid="design-image-input"]')?.click();
  else if (mod && e.altKey && key === "g") s.frameSelection();
  else if (mod && e.shiftKey && key === "g") s.ungroup();
  else if (mod && key === "g") s.group();
  else if (mod && e.shiftKey && (e.code === "Digit4" || e.key === "$")) s.toggleGrids();
  else if (mod && e.key === "]") s.reorder(e.shiftKey || e.altKey ? "front" : "forward");
  else if (mod && e.key === "[") s.reorder(e.shiftKey || e.altKey ? "back" : "backward");
  else if (mod && (e.key === "=" || e.key === "+")) zoomAbout({ x: canvasSize().width / 2, y: canvasSize().height / 2 }, s.camera.zoom * 1.25);
  else if (mod && e.key === "-") zoomAbout({ x: canvasSize().width / 2, y: canvasSize().height / 2 }, s.camera.zoom / 1.25);
  else if (mod && e.key === "0") zoomAbout({ x: canvasSize().width / 2, y: canvasSize().height / 2 }, 1);
  else if (e.shiftKey && (e.code === "Digit1" || e.key === "!")) zoomToFit(pageOf(s.doc, s.pageId), canvasSize(), false);
  else if (e.shiftKey && (e.code === "Digit2" || e.key === "@")) zoomToFit(pageOf(s.doc, s.pageId), canvasSize(), true);
  else if (e.key === "Delete" || e.key === "Backspace") s.remove();
  else if (e.key === "Escape") {
    if (s.gesture) s.cancelGesture();
    else if (s.cropping) s.setCropping(null);
    else if (s.tool !== "select") s.setTool("select");
    else if (s.selection.length) {
      // Up one level: the selection's parent, then nothing.
      const page = pageOf(s.doc, s.pageId);
      const first = s.selection[0]!;
      let parent: string | null = null;
      for (const n of page.children) {
        const stack: { node: typeof n; parent: string | null }[] = [{ node: n, parent: null }];
        while (stack.length) {
          const cur = stack.pop()!;
          if (cur.node.id === first) parent = cur.parent;
          for (const c of cur.node.children ?? []) stack.push({ node: c, parent: cur.node.id });
        }
      }
      s.select(parent ? [parent] : []);
    } else handled = false;
  } else if (e.key === "Enter" && !mod && !e.altKey && s.selection.length === 1) {
    const page = pageOf(s.doc, s.pageId);
    const stack = [...page.children];
    while (stack.length) {
      const n = stack.pop()!;
      if (n.id === s.selection[0]) {
        if (n.type === "text") s.setEditing(n.id);
        else if (n.type === "path") s.setEditPath(n.id);
        else if (n.type === "image") s.setCropping(s.cropping === n.id ? null : n.id);
        else if (n.children?.length) s.select(n.children.map((c) => c.id));
        break;
      }
      stack.push(...(n.children ?? []));
    }
  } else if (e.key.startsWith("Arrow")) {
    const step = e.shiftKey ? 10 : 1;
    const dx = e.key === "ArrowLeft" ? -step : e.key === "ArrowRight" ? step : 0;
    const dy = e.key === "ArrowUp" ? -step : e.key === "ArrowDown" ? step : 0;
    if (s.selection.length) s.nudge(dx, dy);
  } else if (mod && e.altKey && e.key === "Enter") s.setPresenting(true);
  else if (!mod && !e.altKey && e.shiftKey && key === "p") s.setTool("pencil");
  else if (!mod && !e.altKey && e.shiftKey && key === "i") s.setLibrary(!s.library);
  else if (!mod && !e.altKey && e.shiftKey && key === "s") s.setTool("section");
  else if (!mod && !e.altKey && e.shiftKey && key === "r") s.toggleRulers();
  else if (mod && e.shiftKey && key === "r") s.toggleRulers();
  else if (!mod && !e.altKey && e.shiftKey && key === "a" && s.selection.length) s.addAutoLayout();
  else if (mod && key === "e" && !e.shiftKey && !e.altKey && s.selection.length === 1) s.flatten();
  else if (!mod && !e.altKey && KEY_TOOL[key] && !e.shiftKey) s.setTool(KEY_TOOL[key]!);
  else if (e.shiftKey && key === "h" && s.selection.length) s.patch(s.selection, (n) => ({ visible: n.visible === false }));
  else if (mod && e.shiftKey && key === "l" && s.selection.length) s.patch(s.selection, (n) => ({ locked: !n.locked }));
  else handled = false;
  if (handled) {
    e.preventDefault();
    e.stopPropagation();
  }
}

/** The project the design store holds files of. */
let shownFor = "";

export function DesignTab() {
  const file = useDesign((s) => s.file);
  const presenting = useDesign((s) => s.presenting);
  const swarming = useDesign((s) => s.swarming);
  const library = useDesign((s) => s.library);
  const doc = useDesign((s) => s.doc);
  const loading = useDesign((s) => s.loading);
  const workspace = useApp((s) => s.workspace?.id);
  const project = useApp((s) => s.workspace?.path ?? "");
  const target = useDesign((s) => s.target);
  const root = useRef<HTMLDivElement>(null);
  const [dragging, setDragging] = useState(false);

  useEffect(() => watchDesign(), []);
  useEffect(() => {
    // Text measured before the theme fonts arrived used a fallback face: measure again, then redraw.
    let live = true;
    void loadKitFonts().then(() => {
      if (!live) return;
      setMeasure(null);
      useDesign.setState((s) => ({ camera: { ...s.camera } }));
    });
    return () => {
      live = false;
    };
  }, []);
  useEffect(() => {
    if (!workspace) return;
    // Another project: its own designs, nothing of the last one's.
    const switched = shownFor !== "" && shownFor !== project;
    shownFor = project;
    void (async () => {
      if (switched) {
        // Unsaved work goes to its own project first.
        await useDesign.getState().save();
        useDesign.setState({ file: null, doc: null, rev: "", files: [], past: [], future: [], selection: [], editing: null, dirty: false, wsId: "", wsPath: "" });
      }
      const s = useDesign.getState();
      await s.loadFiles();
      const want = useDesign.getState().target || (s.file ? "" : lastDesign());
      const files = useDesign.getState().files;
      if (want && want !== useDesign.getState().file && files.some((f) => f.name === want)) await useDesign.getState().open(want);
      else if (!useDesign.getState().file && files.length === 1) await useDesign.getState().open(files[0]!.name);
      useDesign.setState({ target: "" });
    })();
  }, [workspace, project, target]);

  // Paste: design nodes copied here or in another window, or an image.
  useEffect(() => {
    const onPaste = (e: ClipboardEvent) => {
      if (!root.current?.contains(document.activeElement) && document.activeElement !== document.body) return;
      const active = document.activeElement as HTMLElement | null;
      if (active && (active.tagName === "INPUT" || active.tagName === "TEXTAREA")) return;
      const s = useDesign.getState();
      if (!s.doc) return;
      const files = [...(e.clipboardData?.files ?? [])].filter((f) => f.type.startsWith("image/"));
      if (files.length) {
        e.preventDefault();
        void insertFiles(files);
        return;
      }
      const text = e.clipboardData?.getData("text/plain") ?? "";
      const nodes = text ? readClipboardText(text) : null;
      if (nodes) setClipboard(nodes);
      e.preventDefault();
      s.paste();
    };
    document.addEventListener("paste", onPaste);
    return () => document.removeEventListener("paste", onPaste);
  }, []);

  return (
    <div
      ref={root}
      className="design"
      data-dragging={dragging || undefined}
      onKeyDown={onKey}
      onDragOver={(e) => {
        if (e.dataTransfer.types.includes("Files")) {
          e.preventDefault();
          setDragging(true);
        }
      }}
      onDragLeave={(e) => {
        if (e.currentTarget === e.target) setDragging(false);
      }}
      onDrop={(e) => {
        setDragging(false);
        const files = [...e.dataTransfer.files].filter((f) => f.type.startsWith("image/"));
        if (!files.length || !useDesign.getState().doc) return;
        e.preventDefault();
        const canvas = document.querySelector<HTMLElement>(".design-canvas")?.getBoundingClientRect();
        const cam = useDesign.getState().camera;
        const at = canvas ? { x: (e.clientX - canvas.left - cam.x) / cam.zoom, y: (e.clientY - canvas.top - cam.y) / cam.zoom } : undefined;
        void insertFiles(files, at);
      }}
    >
      {file && doc ? (
        <>
          <Toolbar />
          <div className="design-main">
            <LeftPanel />
            <Canvas />
            <Inspector />
          </div>
          {swarming && <Swarm onClose={() => useDesign.getState().setSwarming(false)} />}
          {library && <Library onClose={() => useDesign.getState().setLibrary(false)} />}
          {presenting && <Present onClose={() => useDesign.getState().setPresenting(false)} />}
        </>
      ) : loading ? (
        <div className="loading" role="status" aria-label={t("app.loading")} />
      ) : (
        <Start />
      )}
    </div>
  );
}

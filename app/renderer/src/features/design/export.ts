// Export of a page, a frame or a selection to SVG, PNG and PDF. The SVG is
// drawn by the same components as the canvas; PNG rasterizes that SVG; PDF
// is printed from it by the main process, so it stays vector.

import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { multiply, parentMatrix, selectionBounds, type Rect } from "./geometry";
import { components, type DesignDoc, type DesignNode, locate, type Page, pageOf, walk } from "./model";
import { resolveTokens } from "./tokens";
import { NodeView, RenderContext, type RenderEnv } from "./render";
import { sizeOf } from "./text";

export type ExportFormat = "png" | "svg" | "pdf";

export interface ExportPlan {
  /** Nodes to draw; empty means the whole page. */
  ids: string[];
  bounds: Rect;
  /** The page's colour behind everything, for page exports. */
  background?: string;
}

const PAD = 0;

/** What an export covers: the selection, or the whole page with its background. */
export function exportPlan(page: Page, ids: string[]): ExportPlan | null {
  const slices = ids.map((id) => locate(page, id)?.node).filter((n): n is DesignNode => !!n && n.type === "slice");
  if (slices.length && slices.length === ids.length) {
    // A slice exports the page as it looks inside the slice's box.
    const bounds = selectionBounds(page, ids, sizeOf);
    const all = page.children.filter((n) => n.visible !== false).map((n) => n.id);
    return bounds ? { ids: all, bounds } : null;
  }
  if (ids.length) {
    const bounds = selectionBounds(page, ids, sizeOf);
    return bounds ? { ids, bounds } : null;
  }
  const all = page.children.filter((n) => n.visible !== false).map((n) => n.id);
  const bounds = selectionBounds(page, all, sizeOf);
  if (!bounds) return null;
  return { ids: all, bounds: { x: bounds.x - PAD, y: bounds.y - PAD, width: bounds.width + 2 * PAD, height: bounds.height + 2 * PAD }, background: page.background };
}

const fmt = (m: number[]) => `matrix(${m.map((v) => Math.round(v * 10000) / 10000).join(" ")})`;

/** An image source as a data URL, so the exported file stands on its own. */
export type Inline = (src: string) => string;

export function fontFaces(families: Set<string>, faces: Record<string, string>): string {
  return [...families]
    .map((f) => faces[f])
    .filter(Boolean)
    .join("\n");
}

export function familiesOf(source: DesignDoc, sourcePage: Page, ids: string[]): Set<string> {
  const out = new Set<string>();
  const page = pageOf(resolveTokens(source), sourcePage.id);
  // Instances come resolved: their layers are the node's own children.
  const add = (n: DesignNode) => void (n.type === "text" && n.font && out.add(n.font.family));
  for (const id of ids) {
    const at = locate(page, id);
    if (at) walk([at.node], add);
  }
  return out;
}

/** The SVG text of an export. `style` holds @font-face rules to embed. */
export function buildSvg(doc: DesignDoc, page: Page, plan: ExportPlan, opts: { image?: Inline; style?: string } = {}): string {
  const { bounds } = plan;
  const env: RenderEnv = { prefix: "x", components: new Map(components(doc).map((c) => [c.id, c])), image: opts.image ?? ((s) => s) };
  const groups = plan.ids.map((id) => {
    const at = locate(page, id);
    if (!at) return null;
    const m = parentMatrix(at.path);
    return createElement("g", { key: id, transform: fmt(multiply([1, 0, 0, 1, -bounds.x, -bounds.y], m)) }, createElement(NodeView, { node: at.node }));
  });
  const w = Math.max(1, Math.round(bounds.width * 100) / 100);
  const h = Math.max(1, Math.round(bounds.height * 100) / 100);
  const inner = renderToStaticMarkup(
    createElement(
      RenderContext.Provider,
      { value: env },
      createElement(
        "svg",
        { xmlns: "http://www.w3.org/2000/svg", xmlnsXlink: "http://www.w3.org/1999/xlink", width: w, height: h, viewBox: `0 0 ${w} ${h}` },
        opts.style ? createElement("style", null, opts.style) : null,
        plan.background ? createElement("rect", { width: w, height: h, fill: plan.background }) : null,
        groups,
      ),
    ),
  );
  return `<?xml version="1.0" encoding="UTF-8"?>\n${inner}`;
}

/** Rasterizes SVG text at a scale; resolves to PNG bytes. */
export async function svgToPng(svg: string, width: number, height: number, scale: number): Promise<Blob> {
  const url = URL.createObjectURL(new Blob([svg], { type: "image/svg+xml" }));
  try {
    const img = new Image();
    img.decoding = "async";
    img.src = url;
    await img.decode();
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(width * scale));
    canvas.height = Math.max(1, Math.round(height * scale));
    const c = canvas.getContext("2d");
    if (!c) throw new Error("no 2d canvas");
    c.drawImage(img, 0, 0, canvas.width, canvas.height);
    return await new Promise<Blob>((resolve, reject) => canvas.toBlob((b) => (b ? resolve(b) : reject(new Error("PNG encoding failed"))), "image/png"));
  } finally {
    URL.revokeObjectURL(url);
  }
}

export function blobToBase64(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result).replace(/^data:[^,]*,/, ""));
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(blob);
  });
}

export function exportName(doc: DesignDoc, page: Page, ids: string[], format: ExportFormat, scale: number): string {
  const first = ids.length === 1 ? locate(page, ids[0]!)?.node.name : undefined;
  const base = (first ?? `${doc.name} - ${page.name}`).replace(/[<>:"/\\|?*\u0000-\u001f]/g, "_").slice(0, 100) || "design";
  return `${base}${format === "png" && scale !== 1 ? `@${scale}x` : ""}.${format}`;
}


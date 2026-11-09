// Runs an export from the app: inlines images and the bundled fonts the
// design uses, draws the SVG, rasterizes or prints it, and hands the bytes
// to the main process to save.

import interLatin from "@fontsource-variable/inter/files/inter-latin-wght-normal.woff2?url";
import interLatinExt from "@fontsource-variable/inter/files/inter-latin-ext-wght-normal.woff2?url";
import monoLatin from "@fontsource-variable/jetbrains-mono/files/jetbrains-mono-latin-wght-normal.woff2?url";
import monoLatinExt from "@fontsource-variable/jetbrains-mono/files/jetbrains-mono-latin-ext-wght-normal.woff2?url";
import { t } from "../../lib/i18n";
import { useApp } from "../../stores/app";
import { useDesign } from "../../stores/design";
import { imageUrl } from "./Canvas";
import { blobToBase64, buildSvg, type ExportFormat, exportName, exportPlan, familiesOf, svgToPng } from "./export";
import { pageOf, walk } from "./model";

const FONT_FILES: Record<string, { family: string; files: string[] }> = {
  Inter: { family: "Inter Variable", files: [interLatin, interLatinExt] },
  "Inter Variable": { family: "Inter Variable", files: [interLatin, interLatinExt] },
  "JetBrains Mono": { family: "JetBrains Mono", files: [monoLatin, monoLatinExt] },
};

async function dataUrl(url: string): Promise<string> {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`${res.status} ${url}`);
  const blob = await res.blob();
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(blob);
  });
}

async function fontStyle(families: Set<string>): Promise<string> {
  const rules: string[] = [];
  const done = new Set<string>();
  for (const name of families) {
    const font = FONT_FILES[name];
    if (!font || done.has(font.family)) continue;
    done.add(font.family);
    for (const file of font.files) {
      try {
        rules.push(`@font-face{font-family:"${font.family}";font-weight:100 900;src:url(${await dataUrl(file)}) format("woff2");}`);
      } catch {
        // The export falls back to an installed font.
      }
    }
  }
  return rules.join("\n");
}

/** Exports the selection (or the page) and saves it. Resolves to the saved path, or "". */
export async function runExport(format: ExportFormat, scale: number): Promise<string> {
  const s = useDesign.getState();
  if (!s.doc) return "";
  const page = pageOf(s.doc, s.pageId);
  const plan = exportPlan(page, s.selection);
  if (!plan) {
    useApp.getState().toast(t("design.nothingToExport"));
    return "";
  }
  try {
    const images = new Map<string, string>();
    const srcs = new Set<string>();
    for (const p of s.doc.pages) walk(p.children, (n) => void (n.type === "image" && n.src && srcs.add(n.src)));
    for (const src of srcs) {
      try {
        images.set(src, /^data:/i.test(src) ? src : await dataUrl(imageUrl(src)));
      } catch {
        // A missing image exports as its background.
      }
    }
    const style = await fontStyle(familiesOf(s.doc, page, plan.ids));
    const svg = buildSvg(s.doc, page, plan, { image: (src) => images.get(src) ?? "", style });
    const name = exportName(s.doc, page, s.selection, format, scale);
    const bridge = window.atlas?.designExport;
    if (!bridge) throw new Error("export is not available here");
    let saved = "";
    if (format === "svg") saved = await bridge(name, "svg", { data: svg });
    else if (format === "pdf") saved = await bridge(name, "pdf", { data: svg, width: plan.bounds.width, height: plan.bounds.height });
    else saved = await bridge(name, "png", { data: await blobToBase64(await svgToPng(svg, plan.bounds.width, plan.bounds.height, scale)) });
    if (saved) useApp.getState().toast(`${t("design.exported")}: ${saved}`);
    return saved;
  } catch (err) {
    useApp.getState().toast(`${t("design.exportFailed")}: ${err instanceof Error ? err.message : String(err)}`, "danger");
    return "";
  }
}

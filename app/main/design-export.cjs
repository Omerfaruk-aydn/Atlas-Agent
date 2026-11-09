// Saves exports of the design tool. The renderer draws the export (SVG text
// or PNG bytes); a PDF is printed here from the SVG, in a hidden window of
// its own that runs no script, so the PDF stays vector with fonts embedded.

const { app, BrowserWindow, dialog, session } = require("electron");
const crypto = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const MAX_EXPORT_CHARS = 96 * 1024 * 1024;
const PDF_PARTITION = "atlas-design-pdf";
const PDF_TIMEOUT_MS = 30_000;

const FILTERS = {
  png: [{ name: "PNG", extensions: ["png"] }],
  svg: [{ name: "SVG", extensions: ["svg"] }],
  pdf: [{ name: "PDF", extensions: ["pdf"] }],
};

/** Prints SVG text to PDF bytes at its own size. */
async function svgToPdf(svg, width, height) {
  const ses = session.fromPartition(PDF_PARTITION, { cache: false });
  // The page is our own SVG; it may load its embedded data: fonts and images, nothing else.
  ses.webRequest.onBeforeRequest((details, callback) => callback({ cancel: !/^(file|data):/i.test(details.url) }));
  const file = path.join(os.tmpdir(), `atlas-design-${crypto.randomUUID()}.html`);
  const w = Math.max(1, Number(width) || 1);
  const h = Math.max(1, Number(height) || 1);
  const body = String(svg).replace(/^<\?xml[^>]*>\s*/, "");
  const html = `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; font-src data:; style-src 'unsafe-inline'"><style>@page{size:${w}px ${h}px;margin:0}html,body{margin:0;padding:0}svg{display:block}</style></head><body>${body}</body></html>`;
  await fs.promises.writeFile(file, html, "utf8");
  const win = new BrowserWindow({ show: false, width: Math.min(4000, Math.ceil(w)), height: Math.min(4000, Math.ceil(h)), webPreferences: { partition: PDF_PARTITION, javascript: false, sandbox: true, contextIsolation: true, nodeIntegration: false } });
  try {
    await Promise.race([win.loadFile(file), new Promise((_, reject) => setTimeout(() => reject(new Error("PDF page did not load")), PDF_TIMEOUT_MS))]);
    return await win.webContents.printToPDF({ printBackground: true, preferCSSPageSize: true, margins: { marginType: "none" } });
  } finally {
    win.destroy();
    fs.promises.rm(file, { force: true }).catch(() => undefined);
  }
}

function safeName(name, format) {
  const base = path.basename(String(name || `design.${format}`)).replace(/[<>:"/\\|?*\u0000-\u001f]/g, "_").slice(0, 120) || `design.${format}`;
  return base.toLowerCase().endsWith(`.${format}`) ? base : `${base}.${format}`;
}

/** Where an export goes: a save dialog, or (end-to-end tests) a folder named by the environment. */
async function target(win, file, format) {
  const testDir = process.env.ATLAS_APP_TEST_SAVE_DIR;
  if (testDir && path.isAbsolute(testDir)) {
    await fs.promises.mkdir(testDir, { recursive: true });
    return path.join(testDir, file);
  }
  const result = await dialog.showSaveDialog(win, { defaultPath: path.join(app.getPath("downloads"), file), filters: FILTERS[format] });
  return result.canceled || !result.filePath ? "" : result.filePath;
}

/**
 * @param {import("electron").BrowserWindow | null} win
 * @param {unknown} name
 * @param {unknown} format
 * @param {unknown} payload
 */
async function exportDesign(win, name, format, payload) {
  if (format !== "png" && format !== "svg" && format !== "pdf") return "";
  if (typeof payload !== "object" || payload === null) return "";
  const data = /** @type {{ data?: unknown; width?: unknown; height?: unknown }} */ (payload).data;
  if (typeof data !== "string" || data.length > MAX_EXPORT_CHARS) return "";
  const file = safeName(name, format);
  // The PDF is made before asking, so a failure never leaves an empty file.
  let bytes;
  if (format === "png") bytes = Buffer.from(data, "base64");
  else if (format === "svg") bytes = Buffer.from(data, "utf8");
  else bytes = await svgToPdf(data, payload.width, payload.height);
  const where = await target(win, file, format);
  if (!where) return "";
  await fs.promises.writeFile(where, bytes);
  return where;
}

module.exports = { exportDesign };

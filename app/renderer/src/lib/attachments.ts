// Files going along with a message. The limits are the gateway's
// (internal/webui/api.go): checking here gives a clear message before upload.

import type { Attachment } from "../api/types";
import { t } from "./i18n";

export const MAX_ATTACHMENTS = 10;
export const MAX_FILE_BYTES = 10 << 20;
export const MAX_TOTAL_BYTES = 25 << 20;

const TEXT_EXTENSIONS = new Set(
  "txt md markdown json jsonc yaml yml toml ini cfg conf env log csv tsv xml html htm css scss less js jsx mjs cjs ts tsx mts cts go py rb rs java kt c h cpp cc hpp cs php sh bash zsh ps1 bat sql lua swift dart vue svelte gradle make mk".split(" "),
);

export interface Pending {
  id: string;
  file: File;
  mime: string;
  preview?: string;
}

/** A MIME type the gateway accepts, or "" when the file cannot be attached. */
export function attachmentMime(file: Pick<File, "name" | "type">): string {
  const type = file.type.toLowerCase();
  if (type.startsWith("image/")) return type === "image/svg+xml" ? "" : type;
  if (type.startsWith("text/") || type === "application/json") return type;
  const ext = file.name.split(".").pop()?.toLowerCase() ?? "";
  return TEXT_EXTENSIONS.has(ext) ? "text/plain" : "";
}

/** Adds what fits. The returned message says what did not, if anything. */
export function addFiles(current: Pending[], files: File[]): { next: Pending[]; error: string } {
  const next = current.slice();
  let total = next.reduce((sum, p) => sum + p.file.size, 0);
  let error = "";
  for (const file of files) {
    const mime = attachmentMime(file);
    if (next.length >= MAX_ATTACHMENTS) {
      error = t("attach.tooMany");
      break;
    }
    if (!mime) {
      error = `${file.name}: ${t("attach.badType")}`;
    } else if (file.size > MAX_FILE_BYTES) {
      error = `${file.name}: ${t("attach.tooBig")}`;
    } else if (total + file.size > MAX_TOTAL_BYTES) {
      error = t("attach.totalTooBig");
    } else {
      total += file.size;
      next.push({ id: crypto.randomUUID(), file, mime, preview: mime.startsWith("image/") ? URL.createObjectURL(file) : undefined });
    }
  }
  return { next, error };
}

function base64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error ?? new Error("read failed"));
    reader.onload = () => resolve(String(reader.result).split(",", 2)[1] ?? "");
    reader.readAsDataURL(file);
  });
}

export async function encode(pending: Pending[]): Promise<Attachment[]> {
  return Promise.all(pending.map(async (p) => ({ file_name: p.file.name || "image.png", mime_type: p.mime, content: await base64(p.file) })));
}

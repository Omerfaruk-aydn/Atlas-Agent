// A small badge for the kind of a file, by its extension.

import { IconDriveFile } from "../../primitives/icons";

const KINDS: Record<string, { label: string; hue: number }> = {
  js: { label: "JS", hue: 52 },
  cjs: { label: "JS", hue: 52 },
  mjs: { label: "JS", hue: 52 },
  jsx: { label: "JSX", hue: 195 },
  ts: { label: "TS", hue: 215 },
  tsx: { label: "TSX", hue: 195 },
  json: { label: "{}", hue: 150 },
  css: { label: "CSS", hue: 280 },
  scss: { label: "CSS", hue: 320 },
  html: { label: "HTM", hue: 18 },
  md: { label: "MD", hue: 210 },
  go: { label: "GO", hue: 190 },
  py: { label: "PY", hue: 215 },
  rs: { label: "RS", hue: 25 },
  yml: { label: "YML", hue: 0 },
  yaml: { label: "YML", hue: 0 },
  toml: { label: "TML", hue: 30 },
  sh: { label: "SH", hue: 120 },
  ps1: { label: "PS", hue: 220 },
  sql: { label: "SQL", hue: 200 },
  svg: { label: "SVG", hue: 35 },
  png: { label: "IMG", hue: 160 },
  jpg: { label: "IMG", hue: 160 },
};

export function FileIcon({ name }: { name: string }) {
  const ext = name.includes(".") ? (name.split(".").pop() ?? "").toLowerCase() : "";
  const kind = KINDS[ext];
  if (!kind) return <IconDriveFile className="file-icon file-icon-plain" />;
  return (
    <span className="file-icon" style={{ ["--hue" as string]: kind.hue }} aria-hidden="true">
      {kind.label}
    </span>
  );
}

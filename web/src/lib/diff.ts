// Line diffs between file versions recorded by the server's history
// service. Only real content is diffed; nothing is inferred.

import { structuredPatch } from "diff"
import type { HistoryFile } from "../api/types"

export interface DiffLine {
  kind: "add" | "del" | "ctx"
  oldLine?: number
  newLine?: number
  text: string
}

export interface DiffHunk {
  header: string
  lines: DiffLine[]
}

export interface FileDiff {
  path: string
  before: string
  after: string
  additions: number
  deletions: number
  hunks: DiffHunk[]
  created: boolean
}

export function diffText(path: string, before: string, after: string, context = 3): FileDiff {
  const patch = structuredPatch(path, path, before, after, "", "", { context })
  let additions = 0
  let deletions = 0
  const hunks: DiffHunk[] = patch.hunks.map((h) => {
    let oldLine = h.oldStart
    let newLine = h.newStart
    const lines: DiffLine[] = []
    for (const raw of h.lines) {
      if (raw.startsWith("\\")) continue
      const text = raw.slice(1)
      if (raw[0] === "+") {
        additions++
        lines.push({ kind: "add", newLine: newLine++, text })
      } else if (raw[0] === "-") {
        deletions++
        lines.push({ kind: "del", oldLine: oldLine++, text })
      } else {
        lines.push({ kind: "ctx", oldLine: oldLine++, newLine: newLine++, text })
      }
    }
    return { header: `@@ -${h.oldStart},${h.oldLines} +${h.newStart},${h.newLines} @@`, lines }
  })
  return { path, before, after, additions, deletions, hunks, created: before === "" }
}

/** sessionChanges turns a session's file history into one diff per path,
 * from the content before the session's first edit to the latest version. */
export function sessionChanges(history: HistoryFile[], root: string): FileDiff[] {
  const byPath = new Map<string, HistoryFile[]>()
  for (const f of history) {
    const list = byPath.get(f.path) ?? []
    list.push(f)
    byPath.set(f.path, list)
  }
  const out: FileDiff[] = []
  for (const [path, versions] of byPath) {
    versions.sort((a, b) => a.version - b.version || a.created_at - b.created_at)
    const first = versions[0]
    const last = versions[versions.length - 1]
    if (first.content === last.content && versions.length > 1) continue
    // A single recorded version is the file as first seen; with nothing
    // after it there is no change to show.
    if (versions.length === 1) continue
    out.push(diffText(relativePath(path, root), first.content, last.content))
  }
  return out.sort((a, b) => a.path.localeCompare(b.path))
}

export function relativePath(path: string, root: string): string {
  const p = path.replaceAll("\\", "/")
  const r = root.replaceAll("\\", "/").replace(/\/+$/, "")
  if (r && p.toLowerCase().startsWith(r.toLowerCase() + "/")) return p.slice(r.length + 1)
  return p
}

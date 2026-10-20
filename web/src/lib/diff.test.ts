import { describe, expect, test } from "vitest"
import type { HistoryFile } from "../api/types"
import { diffText, relativePath, sessionChanges } from "./diff"

const file = (path: string, version: number, content: string): HistoryFile => ({
  id: `${path}-${version}`,
  session_id: "s",
  path,
  content,
  version,
  created_at: version,
  updated_at: version,
})

describe("diffText", () => {
  test("counts and numbers changed lines", () => {
    const d = diffText("a.txt", "one\ntwo\nthree\n", "one\n2\nthree\nfour\n")
    expect(d.additions).toBe(2)
    expect(d.deletions).toBe(1)
    const lines = d.hunks.flatMap((h) => h.lines)
    expect(lines.find((l) => l.kind === "del")).toMatchObject({ text: "two", oldLine: 2 })
    expect(lines.find((l) => l.kind === "add")).toMatchObject({ text: "2", newLine: 2 })
  })

  test("a new file is all additions", () => {
    const d = diffText("n.txt", "", "a\nb\n")
    expect(d.created).toBe(true)
    expect(d.additions).toBe(2)
    expect(d.deletions).toBe(0)
  })
})

describe("sessionChanges", () => {
  test("diffs the first recorded version against the latest", () => {
    const changes = sessionChanges(
      [file("C:/p/a.go", 0, "x\n"), file("C:/p/a.go", 1, "y\n"), file("C:/p/a.go", 2, "z\n"), file("C:/p/b.go", 0, "same\n")],
      "C:\\p",
    )
    expect(changes).toHaveLength(1)
    expect(changes[0].path).toBe("a.go")
    expect(changes[0].before).toBe("x\n")
    expect(changes[0].after).toBe("z\n")
  })

  test("a file edited back to its original content shows no change", () => {
    expect(sessionChanges([file("a", 0, "x"), file("a", 1, "y"), file("a", 2, "x")], "")).toHaveLength(0)
  })

  test("relativePath strips the project root, ignoring case and separators", () => {
    expect(relativePath("C:\\Proj\\src\\a.ts", "c:/proj")).toBe("src/a.ts")
    expect(relativePath("/other/a.ts", "/proj")).toBe("/other/a.ts")
  })
})

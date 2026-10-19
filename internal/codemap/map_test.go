package codemap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// writeProject writes files under a new temporary directory, which is not
// a git work tree, so the builder walks it.
func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
	return root
}

var polyglot = map[string]string{
	"src/orbit.ts": `export function solveKepler(m: number, e: number): number {
  return m + e;
}

export class Orbit {
  step(dt: number) {
    return solveKepler(dt, 0.1);
  }
}
`,
	"src/app.ts": `import { Orbit } from "./orbit";
import * as THREE from "three";

export function main() {
  const o = new Orbit();
  o.step(1);
  THREE.Vector3(0, 0, 0);
  helper();
}

function helper() {}
`,
	"src/util.ts": `export function helper() {}
export function step() {}
`,
	"py/sim.py": `from .physics import integrate

def run():
    integrate()
    print("done")
`,
	"py/physics.py": `def integrate():
    pass
`,
	"go.mod": "module example.com/m\n",
	"pkg/a/a.go": `package a

func A() { B() }
`,
	"pkg/a/b.go": `package a

func B() {}

func append() {}
`,
	"cmd/main.go": `package main

import (
	"fmt"

	"example.com/m/pkg/a"
)

func main() {
	a.A()
	fmt.Println(append([]int{}, 1))
}
`,
	"node_modules/three/index.js": "export function Vector3() {}\n",
	"dist/bundle.js":              "function solveKepler() {}\n",
}

func buildPolyglot(t *testing.T) *Map {
	t.Helper()
	m, err := NewBuilder(t.TempDir()).Map(t.Context(), writeProject(t, polyglot))
	require.NoError(t, err)
	return m
}

func TestMapResolvesThroughImportsAndScope(t *testing.T) {
	t.Parallel()
	m := buildPolyglot(t)
	var paths []string
	for _, f := range m.Files {
		paths = append(paths, f.Path)
	}
	require.Equal(t, []string{"cmd/main.go", "pkg/a/a.go", "pkg/a/b.go", "py/physics.py", "py/sim.py", "src/app.ts", "src/orbit.ts", "src/util.ts"}, paths, "dependencies and build output are not mapped")

	solve := m.Explain(t.Context(), "solveKepler")
	require.Contains(t, solve, "function solveKepler: src/orbit.ts:1-3")
	require.Contains(t, solve, "Used by (1: 1 resolved)")
	require.Contains(t, solve, "src/orbit.ts:7 in method Orbit.step [call, resolved]")

	orbit := m.Explain(t.Context(), "Orbit")
	require.Contains(t, orbit, "src/app.ts:5 in function main [new, resolved]")

	// A call through a value of unknown type is never more than a guess.
	step := m.Explain(t.Context(), "Orbit.step")
	require.Contains(t, step, "src/app.ts:6 in function main [call, ambiguous, receiver type unknown]")

	// helper is defined twice; the call resolves to the file's own.
	helper := m.Explain(t.Context(), "src/app.ts:helper")
	require.Contains(t, helper, "src/app.ts:8 in function main [call, resolved]")
	require.Contains(t, m.Explain(t.Context(), "src/util.ts:helper"), "Used by (0)")

	// A module from a dependency is not linked to anything in the project.
	require.NotContains(t, m.Explain(t.Context(), "src/app.ts"), "Vector3")
	require.Contains(t, m.Explain(t.Context(), "src/app.ts"), "three (outside the project)")

	integrate := m.Explain(t.Context(), "integrate")
	require.Contains(t, integrate, "py/sim.py:4 in function run [call, resolved]")

	a := m.Explain(t.Context(), "A")
	require.Contains(t, a, "cmd/main.go:10 in function main [call, resolved]")
	b := m.Explain(t.Context(), "B")
	require.Contains(t, b, "pkg/a/a.go:3 in function A [call, resolved]", "same package, no import needed")
	// The builtin append is not the package's function of that name.
	require.Contains(t, m.Explain(t.Context(), "pkg/a/b.go:append"), "Used by (0)")
}

func TestMapCallersAndPath(t *testing.T) {
	t.Parallel()
	m := buildPolyglot(t)
	callers := m.Callers("solveKepler", 2)
	require.Contains(t, callers, "1. src/orbit.ts:7 in method Orbit.step -> solveKepler [call, resolved]")
	require.Contains(t, callers, "  2. src/app.ts:6 in function main -> Orbit.step [call, ambiguous, receiver type unknown]")
	require.Contains(t, callers, "inferred or ambiguous")

	path := m.Path("src/app.ts:main", "solveKepler")
	require.Contains(t, path, "reaches solveKepler in 2 steps")
	require.Less(t, strings.Index(path, "-> step"), strings.Index(path, "-> solveKepler"))

	require.Contains(t, m.Path("cmd/main.go", "pkg/a/b.go"), "cmd/main.go\n  -> pkg/a/a.go\n  -> pkg/a/b.go")
	require.Contains(t, m.Path("solveKepler", "integrate"), "No chain")
}

func TestMapOverviewAndFind(t *testing.T) {
	t.Parallel()
	m := buildPolyglot(t)
	overview := m.Overview(0)
	require.Contains(t, overview, "8 files (go 3, typescript 3, python 2)")
	require.Contains(t, overview, "Entry points: ")
	require.Contains(t, overview, "cmd/main.go")
	require.Contains(t, overview, "src/orbit.ts (typescript, 10 lines, used by 1 file)")
	require.Contains(t, overview, "export function solveKepler(m: number, e: number): number {")

	found := m.Find("step")
	require.Contains(t, found, "method Orbit.step  src/orbit.ts:6")
	require.Contains(t, found, "function step  src/util.ts:2")
	require.Contains(t, m.Find("nothing_like_this"), "Nothing named like")
	require.Contains(t, m.Explain(t.Context(), "missing"), "nothing named missing")
	both := m.Explain(t.Context(), "helper")
	require.Contains(t, both, "function helper: src/app.ts:11-11")
	require.Contains(t, both, "function helper: src/util.ts:1-1")
}

// A second call reuses the map; an edit re-reads only that file and a new
// builder starts from the saved cache.
func TestBuilderRefreshesChangedFiles(t *testing.T) {
	t.Parallel()
	root := writeProject(t, map[string]string{"a.py": "def one():\n    pass\n"})
	cache := t.TempDir()
	b := NewBuilder(cache)
	first, err := b.Map(t.Context(), root)
	require.NoError(t, err)
	again, err := b.Map(t.Context(), root)
	require.NoError(t, err)
	require.Same(t, first, again)

	file := filepath.Join(root, "a.py")
	require.NoError(t, os.WriteFile(file, []byte("def one():\n    pass\n\ndef two():\n    one()\n"), 0o644))
	later := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(file, later, later))
	updated, err := b.Map(t.Context(), root)
	require.NoError(t, err)
	require.NotSame(t, first, updated)
	require.Contains(t, updated.Explain(t.Context(), "one"), "a.py:5 in function two [call, resolved]")

	entries, err := os.ReadDir(cache)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	fresh, err := NewBuilder(cache).Map(t.Context(), root)
	require.NoError(t, err)
	require.Len(t, fresh.Files[0].Symbols, 2)
}


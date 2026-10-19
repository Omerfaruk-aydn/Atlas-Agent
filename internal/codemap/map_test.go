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


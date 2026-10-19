package codemap

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// symbolsOf lists a file's symbols as "kind container.name line-end".
func symbolsOf(f File) []string {
	var out []string
	for _, s := range f.Symbols {
		name := s.Name
		if s.Container != "" {
			name = s.Container + "." + name
		}
		out = append(out, fmt.Sprintf("%s %s %d-%d", s.Kind, name, s.Line, s.EndLine))
	}
	return out
}

func importsOf(f File) []string {
	var out []string
	for _, imp := range f.Imports {
		line := imp.Spec
		if imp.Module != "" {
			line += " as " + imp.Module
		}
		for _, b := range imp.Bindings {
			if b.Local == b.Remote {
				line += " " + b.Local
			} else {
				line += " " + b.Remote + "=>" + b.Local
			}
		}
		out = append(out, line)
	}
	return out
}

func refsOf(f File) []string {
	var out []string
	for _, r := range f.Refs {
		name := r.Name
		if r.Qualifier != "" {
			name = r.Qualifier + "." + name
		}
		out = append(out, r.Kind+" "+name)
	}
	return out
}

func TestExtractTypeScript(t *testing.T) {
	t.Parallel()
	src := `import * as THREE from "three";
import { orbit, Body as Planet } from './physics/orbit';
import type { Config } from "../config";
import Default from "./default";
const helpers = require("./helpers");

// function notAFunction() {}
const label = ` + "`orbit ${name()} done`" + `;

export class SolarSystem extends Group implements Updatable {
  private bodies: Planet[] = [];

  constructor(private scene: THREE.Scene) {
    super();
  }

  async update(dt: number): Promise<void> {
    for (const b of this.bodies) {
      b.position.copy(orbit(b, dt));
    }
    this.render();
  }

  handleClick = (event: MouseEvent) => {
    helpers.log(event);
  };
}

export function createScene(canvas: HTMLCanvasElement) {
  const system = new SolarSystem(new THREE.Scene());
  if (canvas) {
    return system;
  }
}

export const formatDistance = (km: number): string => ` + "`${km} km`" + `;

export type Body = { mass: number };
export interface Updatable { update(dt: number): void }

function App() {
  return <Canvas><Planet name="earth" /></Canvas>;
}
`
	f := Extract("src/scene.ts", src)
	require.Equal(t, "typescript", f.Language)
	require.Equal(t, []string{
		"three as THREE",
		"./physics/orbit orbit Body=>Planet",
		"../config Config",
		"./default default=>Default",
		"./helpers as helpers",
	}, importsOf(f))
	syms := symbolsOf(f)
	for _, want := range []string{
		"class SolarSystem 10-27",
		"method SolarSystem.constructor 13-15",
		"method SolarSystem.update 17-22",
		"method SolarSystem.handleClick 24-26",
		"function createScene 29-34",
		"function formatDistance 36-36",
		"type Body 38-38",
		"interface Updatable 39-39",
		"function App 41-43",
	} {
		require.Contains(t, syms, want)
	}
	require.NotContains(t, strings.Join(syms, "\n"), "notAFunction")
	require.NotContains(t, strings.Join(syms, "\n"), "function if")
	refs := refsOf(f)
	for _, want := range []string{"inherit Group", "inherit Updatable", "call orbit", "call this.render", "call helpers.log", "new SolarSystem", "new THREE.Scene", "use Canvas", "use Planet"} {
		require.Contains(t, refs, want)
	}
	require.NotContains(t, refs, "call name", "a call inside a template string is not read")
	require.True(t, f.Symbols[slices.IndexFunc(f.Symbols, func(s Symbol) bool { return s.Name == "createScene" })].Exported)
}


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

func TestExtractPython(t *testing.T) {
	t.Parallel()
	src := `"""Module docs: def fake(): pass"""
import numpy as np
import os.path
from .models import Planet, Star as Sun
from ..util import (
    clamp,
    lerp,
)

class Orbit(Base, metaclass=ABCMeta):
    def __init__(self, body):
        self.body = body

    def step(self, dt):
        x = np.sin(dt)
        return clamp(self.advance(x), 0, 1)

def main():
    o = Orbit(Planet("earth"))
    o.step(0.1)


if __name__ == "__main__":
    main()
`
	f := Extract("sim/orbit.py", src)
	require.Equal(t, []string{
		"numpy as np",
		"os.path as path",
		".models Planet Star=>Sun",
		"..util clamp lerp",
	}, importsOf(f))
	require.Equal(t, []string{
		"class Orbit 10-16",
		"method Orbit.__init__ 11-12",
		"method Orbit.step 14-16",
		"function main 18-20",
	}, symbolsOf(f))
	refs := refsOf(f)
	for _, want := range []string{"inherit Base", "inherit ABCMeta", "call np.sin", "call clamp", "call self.advance", "call Orbit", "call Planet", "call o.step", "call main"} {
		require.Contains(t, refs, want)
	}
}

func TestExtractBraceLanguages(t *testing.T) {
	t.Parallel()
	cases := []struct {
		file, src string
		symbols   []string
		imports   []string
		refs      []string
	}{
		{
			file: "src/main/java/app/Orbit.java",
			src: `package app;
import java.util.List;
import app.physics.*;

public class Orbit extends Body implements Stepper {
    private final List<Body> bodies;

    public Orbit(List<Body> bodies) {
        this.bodies = bodies;
    }

    @Override
    public void step(double dt) {
        if (dt > 0) {
            Physics.integrate(bodies, dt);
        }
    }
}
`,
			symbols: []string{"class Orbit 5-18", "method Orbit.Orbit 8-10", "method Orbit.step 13-17"},
			imports: []string{"java.util.List as List List", "app.physics.*"},
			refs:    []string{"inherit Body", "inherit Stepper", "call Physics.integrate"},
		},
		{
			file: "src/orbit.c",
			src: `#include "orbit.h"
#include <math.h>

struct body { double x; };
struct body *current;

static double *kepler(double e, double m) {
    return solve(e, m);
}

int main(void)
{
    kepler(0.1, 2.0);
    return 0;
}
`,
			symbols: []string{"struct body 4-4", "function kepler 7-9", "function main 11-15"},
			imports: []string{"orbit.h", "<math.h>"},
			refs:    []string{"call solve", "call kepler"},
		},
		{
			file: "src/orbit.rs",
			src: `use crate::physics::{integrate, Body as B};
use std::fmt;
mod helpers;

pub struct Orbit<'a> { bodies: &'a [B] }

impl<'a> fmt::Display for Orbit<'a> {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "orbit")
    }
}

impl<'a> Orbit<'a> {
    pub fn step(&mut self, dt: f64) {
        let s = r#"fn fake() {}"#;
        integrate(self.bodies, dt);
        helpers::clamp(dt);
    }
}
`,
			symbols: []string{"struct Orbit 5-5", "impl Orbit 7-11", "method Orbit.fmt 8-10", "impl Orbit 13-19", "method Orbit.step 14-18"},
			imports: []string{"crate::physics integrate Body=>B", "std fmt", "mod:helpers"},
			refs:    []string{"inherit Display", "call integrate", "call helpers.clamp"},
		},
		{
			file: "app/src/Orbit.kt",
			src: `import com.example.physics.Body
import com.example.util.clamp as limit

class Orbit(val body: Body) : Stepper {
    fun step(dt: Double) {
        limit(body.advance(dt))
    }
}

fun Body.describe(): String = "body"
`,
			symbols: []string{"class Orbit 4-8", "method Orbit.step 5-7", "method Body.describe 10-10"},
			imports: []string{"com.example.physics.Body as Body Body", "com.example.util.clamp as limit clamp=>limit"},
			refs:    []string{"inherit Stepper", "call limit", "call body.advance"},
		},
		{
			file: "Orbit.cs",
			src: `using System;
using App.Physics;

namespace App.Sim
{
    public class Orbit : Body, IStepper
    {
        public void Step(double dt)
        {
            Physics.Integrate(this, dt);
        }

        public double Energy() => Kinetic() + Potential();
    }
}
`,
			symbols: []string{"namespace App.Sim 4-15", "class App.Sim.Orbit 6-14", "method Orbit.Step 8-11", "method Orbit.Energy 13-13"},
			imports: []string{"System", "App.Physics"},
			refs:    []string{"inherit Body", "inherit IStepper", "call Physics.Integrate", "call Kinetic"},
		},
		{
			file: "lib/orbit.php",
			src: `<?php
use App\Physics\Body as B;
require_once 'helpers.php';

class Orbit extends B {
    public function step($dt) {
        return integrate($this, $dt);
    }
}
`,
			symbols: []string{"class Orbit 5-9", "method Orbit.step 6-8"},
			imports: []string{"App\\Physics\\Body Body=>B", "helpers.php"},
			refs:    []string{"inherit B", "call integrate"},
		},
		{
			file: "Sources/Orbit.swift",
			src: `import SceneKit

class Orbit: Body {
    func step(dt: Double) {
        integrate(self, dt)
    }
}

extension Orbit {
    func reset() {}
}
`,
			symbols: []string{"class Orbit 3-7", "method Orbit.step 4-6", "impl Orbit 9-11", "method Orbit.reset 10-10"},
			imports: []string{"SceneKit"},
			refs:    []string{"inherit Body", "call integrate"},
		},
		{
			file: "scripts/build.sh",
			src: `#!/bin/bash
source ./lib/common.sh
echo "args: $#"

build() {
  compile "$1"
}

function deploy {
  build release
}
`,
			symbols: []string{"function deploy 9-11"},
			imports: []string{"./lib/common.sh"},
			refs:    []string{"call build"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			f := Extract(tc.file, tc.src)
			syms := symbolsOf(f)
			for _, want := range tc.symbols {
				require.Contains(t, syms, want, "symbols: %v", syms)
			}
			require.Equal(t, tc.imports, importsOf(f))
			refs := refsOf(f)
			for _, want := range tc.refs {
				require.Contains(t, refs, want, "refs: %v", refs)
			}
		})
	}
}


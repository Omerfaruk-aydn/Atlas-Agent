package codemap

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// langSamples are small, idiomatic files in each language the map reads
// beyond the mainstream ones.
var langSamples = map[string]string{
	"src/Orbit/Kepler.hs": `module Orbit.Kepler (solve, Body(..)) where

import qualified Data.Map as M
import Orbit.Units (au)

data Body = Body { mass :: Double }

solve :: Double -> Double -> Double
solve m e = go m 0
  where go x n = x + e * sin x

class Shape a where
  area :: a -> Double
`,
	"lib/orbit.ml": `open Units
module P = Physics

type body = { mass : float }

let solve m e =
  Physics.kepler m e

let rec loop n = if n = 0 then () else loop (n - 1)

module Planet = struct
  let make () = { mass = 1.0 }
end
`,
	"src/Orbit.fs": `module Orbit

open System
open Physics.Units

type Body = { Mass: float }

let solve m e =
    Kepler.iterate m e

let private helper x = x + 1
`,
	"src/orbit/core.clj": `(ns orbit.core
  (:require [orbit.units :as u]
            [clojure.string :as str]))

(defn solve [m e]
  (u/kepler m e))

(defn- helper [x] (inc x))

(defrecord Body [mass])
`,
	"src/orbit.erl": `-module(orbit).
-export([solve/2]).
-include("units.hrl").

solve(M, E) ->
    kepler:iterate(M, E),
    helper(M).

helper(X) -> X + 1.
`,
	"src/orbit.nim": `import physics/units, strutils

type
  Body* = object
    mass*: float

proc solve*(m, e: float): float =
  result = kepler(m, e)

func helper(x: int): int = x + 1
`,
	"src/orbit.cr": `require "./units"

module Orbit
  class Body
    def initialize(@mass : Float64)
    end

    def solve(e)
      Kepler.iterate(@mass, e)
    end
  end
end
`,
	"scripts/Deploy.ps1": `. ./common.ps1
Import-Module ./Helpers.psm1

function Invoke-Deploy {
    param([string]$Target)
    Get-Config $Target
    Publish-Site -Path $Target
}

function Get-Config($name) { return $name }
`,
	"src/Orbit.vb": `Imports System.Math
Namespace Physics
    Public Class Orbit
        Public Function Solve(m As Double) As Double
            Return Kepler(m)
        End Function
        Private Sub Reset()
        End Sub
    End Class
End Namespace
`,
	"src/orbit.pas": `unit Orbit;

interface

uses SysUtils, Units;

type
  TBody = class
    procedure Step;
  end;

function Solve(M, E: Double): Double;

implementation

function Solve(M, E: Double): Double;
begin
  Result := Kepler(M, E);
end;

procedure TBody.Step;
begin
  Solve(1, 2);
end;

end.
`,
	"src/orbit.f90": `module orbit
  use units
  implicit none
contains
  function solve(m, e) result(x)
    real :: m, e, x
    x = kepler(m, e)
  end function solve

  subroutine step(b)
    real :: b
    call integrate(b)
  end subroutine step
end module orbit
`,
	"player.gd": `extends CharacterBody2D
class_name Player

const Bullet = preload("res://bullet.tscn")
signal died

var speed := 200

func _physics_process(delta):
    move(delta)

func move(delta):
    velocity = Vector2.ZERO
`,
	"contracts/Token.sol": `pragma solidity ^0.8.0;
import "./IERC20.sol";

contract Token is IERC20 {
    event Transfer(address from, address to);
    function transfer(address to) public returns (bool) {
        _move(msg.sender, to);
        return true;
    }
    function _move(address a, address b) internal {}
}
`,
	"rtl/alu.v": "`include \"defs.vh\"\nmodule alu(input a, input b, output y);\n  adder u_add(.a(a), .b(b), .s(y));\nendmodule\n",
	"rtl/alu.vhd": `library ieee;
use ieee.std_logic_1164.all;
use work.defs.all;

entity alu is
  port (a, b : in std_logic; y : out std_logic);
end entity;

architecture rtl of alu is
begin
  u1: entity work.adder port map (a => a, b => b, s => y);
end architecture;
`,
	"src/orbit.gleam": `import gleam/io
import orbit/units.{au}

pub type Body {
  Body(mass: Float)
}

pub fn solve(m: Float) -> Float {
  units.kepler(m)
}
`,
	"src/Counter.svelte": `<script lang="ts">
  import { format } from "./format";
  export let count = 0;
  function increment() { count += 1; }
</script>

<button class="btn" on:click={increment}>{format(count)}</button>

<style>
  .btn { color: red; }
</style>
`,
	"db/schema.sql": `CREATE TABLE users (id INT PRIMARY KEY);
CREATE VIEW active_users AS SELECT * FROM users WHERE active;
CREATE INDEX users_id ON users (id);
CREATE FUNCTION count_users() RETURNS int AS $$ SELECT count(*) FROM users $$ LANGUAGE sql;
INSERT INTO audit SELECT id FROM users;
`,
	"api/schema.graphql": `type Query {
  user(id: ID!): User
}

type User implements Node {
  id: ID!
  posts: [Post]
}

enum Role { ADMIN USER }
`,
	"proto/user.proto": `syntax = "proto3";
import "common.proto";

message User {
  string id = 1;
  Address address = 2;
}

service Users {
  rpc Get(GetRequest) returns (User);
}
`,
	"infra/main.tf": `module "network" {
  source = "./modules/network"
}

resource "aws_instance" "web" {
  subnet_id = module.network.subnet_id
  ami       = var.ami
}

variable "ami" {}
`,
	"styles/app.scss": `@use "./theme";
$primary: blue;

@mixin card { padding: 1rem; }

.button {
  @include card;
  color: $primary;
  animation: spin 1s;
}

@keyframes spin { to { transform: rotate(360deg); } }
`,
	"Dockerfile": `FROM golang:1.25 AS build
COPY . .
RUN go build -o /app ./cmd/server

FROM alpine
COPY --from=build /app /app
ENTRYPOINT ["/app"]
`,
	"compose.yaml": `services:
  web:
    build: .
    depends_on:
      - db
  db:
    image: postgres
`,
	".github/workflows/ci.yml": `name: ci
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: make test
  deploy:
    needs: test
    runs-on: ubuntu-latest
    steps:
      - run: ./scripts/deploy.sh
`,
	"Makefile": `.PHONY: test build

build: gen
	go build ./...

test: build
	go test ./...

gen:
	go generate ./...
`,
	"docs/guide.md":  "# Guide\n\n## Setup\n\nRead [the API](api.md#auth) and see ![arch](img/arch.svg).\n\n```go\nserver.Start()\n```\n\nCall `orbit.Solve` to compute.\n",
	"docs/intro.rst": "Intro\n=====\n\nSee :doc:`guide` and `Python <https://python.org>`_.\n\n.. image:: img/logo.png\n\nUsage\n-----\n\nText.\n",
	"api/openapi.yaml": `openapi: 3.0.0
paths:
  /users/{id}:
    get:
      operationId: getUser
      responses:
        "200":
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/User"
components:
  schemas:
    User:
      type: object
`,
	"prisma/schema.prisma": `model User {
  id    Int    @id
  posts Post[]
}

model Post {
  id     Int  @id
  author User @relation(fields: [authorId], references: [id])
}
`,
	"scenes/main.tscn": `[gd_scene load_steps=3 format=3]

[ext_resource type="Script" path="res://player.gd" id="1"]
[ext_resource type="Texture2D" path="res://art/ship.png" id="2"]

[node name="Main" type="Node2D"]

[node name="Player" type="CharacterBody2D" parent="."]
script = ExtResource("1")
`,
}

// langWant is what extracting a sample must find; each list must be a
// subset of what is found, and none of notRefs may be.
type langWant struct {
	lang                          string
	symbols, imports, refs, links []string
	notRefs                       []string
}

var langWants = map[string]langWant{
	"src/Orbit/Kepler.hs": {
		lang: "haskell", symbols: []string{"type Body 6-6", "function solve 8-10", "interface Shape 12-13"},
		imports: []string{"Data.Map as M", "Orbit.Units as Units au"}, refs: []string{"call sin", "use Double"},
		notRefs: []string{"call mass", "call x", "call e"},
	},
	"lib/orbit.ml": {
		lang: "ocaml", symbols: []string{"type body 4-4", "function solve 6-7", "function loop 9-9", "namespace Planet 11-13"},
		imports: []string{"Units"}, refs: []string{"call Physics.kepler", "call loop"},
		notRefs: []string{"call mass", "call float", "call m", "call e"},
	},
	"src/Orbit.fs": {
		lang: "fsharp", symbols: []string{"namespace Orbit 1-1", "type Body 6-6", "function solve 8-9", "function helper 11-11"},
		imports: []string{"System", "Physics.Units"}, refs: []string{"call Kepler.iterate"},
		notRefs: []string{"use Mass", "call float"},
	},
	"src/orbit/core.clj": {
		lang: "clojure", symbols: []string{"function solve 5-6", "function helper 8-8", "struct Body 10-10"},
		imports: []string{"orbit.units as u", "clojure.string as str"}, refs: []string{"call u.kepler", "call inc"},
	},
	"src/orbit.erl": {
		lang: "erlang", symbols: []string{"function orbit.solve 5-7", "function orbit.helper 9-9"},
		imports: []string{"units.hrl"}, refs: []string{"call kepler.iterate", "call helper"},
	},
	"src/orbit.nim": {
		lang: "nim", symbols: []string{"struct Body 4-5", "function solve 7-8", "function helper 10-10"},
		imports: []string{"physics/units", "physics/units as units", "strutils"}, refs: []string{"call kepler"},
	},
	"src/orbit.cr": {
		lang: "crystal", symbols: []string{"namespace Orbit 3-12", "class Orbit.Body 4-11", "method Body.solve 8-10"},
		imports: []string{"./units"}, refs: []string{"call Kepler.iterate"},
	},
	"scripts/Deploy.ps1": {
		lang: "powershell", symbols: []string{"function Invoke-Deploy 4-8", "function Get-Config 10-10"},
		imports: []string{"./common.ps1", "./Helpers.psm1"}, refs: []string{"call get-config", "call publish-site"},
	},
	"src/Orbit.vb": {
		lang: "vb", symbols: []string{"namespace Physics 2-10", "class Physics.Orbit 3-9", "method Orbit.Solve 4-6", "method Orbit.Reset 7-8"},
		imports: []string{"System.Math"}, refs: []string{"call kepler"},
	},
	"src/orbit.pas": {
		lang: "pascal", symbols: []string{"class TBody 8-10", "function Solve 16-19", "method TBody.Step 21-24"},
		imports: []string{"SysUtils", "Units"}, refs: []string{"call kepler", "call solve"},
	},
	"src/orbit.f90": {
		lang: "fortran", symbols: []string{"namespace orbit 1-14", "method orbit.solve 5-8", "method orbit.step 10-13"},
		imports: []string{"units"}, refs: []string{"call kepler", "call integrate"}, notRefs: []string{"call result"},
	},
	"player.gd": {
		lang: "gdscript", symbols: []string{"class Player 2-14", "method Player._physics_process 9-10", "method Player.move 12-13"},
		refs: []string{"inherit CharacterBody2D", "call move"}, links: []string{"names res://bullet.tscn"},
		notRefs: []string{"call preload"},
	},
	"contracts/Token.sol": {
		lang: "solidity", symbols: []string{"class Token 4-11", "method Token.transfer 6-9", "method Token._move 10-10"},
		imports: []string{"./IERC20.sol"}, refs: []string{"inherit IERC20", "call _move"}, notRefs: []string{"call returns"},
	},
	"rtl/alu.v": {
		lang: "verilog", symbols: []string{"module alu 2-4"}, imports: []string{"defs.vh"}, refs: []string{"new adder"},
		notRefs: []string{"call a", "call b", "call s"},
	},
	"rtl/alu.vhd": {
		lang: "vhdl", symbols: []string{"module alu 5-7", "impl alu.rtl 9-12"}, imports: []string{"defs"}, refs: []string{"new adder"},
	},
	"src/orbit.gleam": {
		lang: "gleam", symbols: []string{"type Body 4-6", "function solve 8-10"},
		imports: []string{"gleam/io as io", "orbit/units as units au"}, refs: []string{"call units.kepler"},
	},
	"src/Counter.svelte": {
		lang: "typescript", symbols: []string{"function increment 4-4", "css class btn 10-10", "component Counter 1-12"},
		imports: []string{"./format format"}, refs: []string{"css btn", "call increment", "call format"},
	},
	"db/schema.sql": {
		lang: "sql", symbols: []string{"table users 1-1", "view active_users 2-2", "index users_id 3-3", "function count_users 4-4"},
		refs: []string{"table users", "table audit"}, notRefs: []string{"table count"},
	},
	"api/schema.graphql": {
		lang: "graphql", symbols: []string{"type Query 1-3", "field Query.user 2-2", "type User 5-8", "enum Role 10-10"},
		refs: []string{"use User", "use Node", "use Post"},
	},
	"proto/user.proto": {
		lang: "protobuf", symbols: []string{"struct User 4-7", "interface Users 9-11", "method Users.Get 10-10"},
		imports: []string{"common.proto"}, refs: []string{"use Address", "use GetRequest", "use User"},
	},
	"infra/main.tf": {
		lang: "terraform", symbols: []string{"module module.network 1-3", "resource aws_instance.web 5-8", "variable var.ami 10-10"},
		imports: []string{"./modules/network"}, refs: []string{"infra module.network", "infra var.ami"},
	},
	"styles/app.scss": {
		lang: "scss", symbols: []string{"scss variable $primary 2-2", "mixin card 4-4", "css class button 6-10", "keyframes spin 12-12"},
		imports: []string{"./theme"}, refs: []string{"css @mixin.card", "css $primary", "css @keyframes.spin"},
	},
	"Dockerfile": {
		lang: "docker", symbols: []string{"stage docker.build 1-7"}, refs: []string{"task docker.build"},
	},
	"compose.yaml": {
		lang: "compose", symbols: []string{"service compose.web 2-5", "service compose.db 6-7"},
		refs: []string{"task compose.db"}, links: []string{"runs Dockerfile"},
	},
	".github/workflows/ci.yml": {
		lang: "workflow", symbols: []string{"job job.test 4-7", "job job.deploy 8-12"},
		refs: []string{"task make.test", "task job.test"}, links: []string{"runs scripts/deploy.sh"},
	},
	"Makefile": {
		lang: "make", symbols: []string{"target make.build 3-4", "target make.test 6-7", "target make.gen 9-10"},
		refs: []string{"task make.gen", "task make.build"},
	},
	"docs/guide.md": {
		lang: "markdown", symbols: []string{"section Guide 1-12", "section Setup 3-12"},
		refs: []string{"mention server.Start", "mention orbit.Solve"}, links: []string{"embeds img/arch.svg", "links to api.md#auth"},
	},
	"docs/intro.rst": {
		lang: "rst", symbols: []string{"section Intro 1-12", "section Usage 8-12"},
		links: []string{"links to guide.rst", "embeds img/logo.png"},
	},
	"api/openapi.yaml": {
		lang: "openapi", symbols: []string{"endpoint GET /users/{id} 4-11", "schema User 14-15"}, refs: []string{"use User"},
	},
	"prisma/schema.prisma": {
		lang: "prisma", symbols: []string{"model User 1-4", "model Post 6-9"}, refs: []string{"use Post", "use User"},
	},
	"scenes/main.tscn": {
		lang: "godot", symbols: []string{"node Main 6-6", "node Player 8-8"},
		imports: []string{"res://player.gd"}, links: []string{"loads res://art/ship.png"},
	},
}

func TestExtractLanguages(t *testing.T) {
	t.Parallel()
	require.Equal(t, sortedKeys(langSamples), sortedKeys(langWants), "every sample has expectations")
	for _, name := range sortedKeys(langSamples) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want := langWants[name]
			f := Extract(name, langSamples[name])
			var links []string
			for _, l := range f.Links {
				links = append(links, l.Kind+" "+l.Target)
			}
			require.Equal(t, want.lang, f.Language)
			require.Subset(t, symbolsOf(f), want.symbols)
			require.Subset(t, importsOf(f), want.imports)
			require.Subset(t, refsOf(f), want.refs)
			require.Subset(t, links, want.links)
			for _, r := range want.notRefs {
				require.NotContains(t, refsOf(f), r)
			}
		})
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// A notebook's code cells are read as its kernel's language, its Markdown
// cells as a document, each at its line in the .ipynb file.
func TestExtractNotebook(t *testing.T) {
	t.Parallel()
	src := `{
 "cells": [
  {
   "cell_type": "markdown",
   "source": [
    "# Orbit study\n",
    "Uses ` + "`solve`" + `.\n"
   ]
  },
  {
   "cell_type": "code",
   "source": [
    "from orbit import solve\n",
    "def run():\n",
    "    solve(1)\n"
   ]
  }
 ],
 "metadata": {"kernelspec": {"language": "python", "name": "python3"}}
}
`
	f := Extract("study.ipynb", src)
	require.Equal(t, "notebook", f.Language)
	syms := symbolsOf(f)
	require.Contains(t, strings.Join(syms, ","), "section Orbit study")
	require.Contains(t, strings.Join(syms, ","), "function run")
	require.Contains(t, importsOf(f), "orbit solve")
	require.Contains(t, refsOf(f), "call solve")
}

func TestExtractAssetMeasuresImages(t *testing.T) {
	t.Parallel()
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 120 40"><image href="tex/stars.png"/></svg>`
	f := ExtractAsset("icons/logo.svg", []byte(svg), int64(len(svg)))
	require.Equal(t, "image", f.Language)
	require.Equal(t, 120, f.Asset.Width)
	require.Equal(t, 40, f.Asset.Height)
	require.Len(t, f.Links, 1)
	require.Equal(t, "tex/stars.png", f.Links[0].Target)

	gltf := `{"asset": {"version": "2.0"}, "images": [{"uri": "ship_albedo.png"}], "buffers": [{"uri": "ship.bin"}]}`
	g := ExtractAsset("models/ship.gltf", []byte(gltf), int64(len(gltf)))
	var targets []string
	for _, l := range g.Links {
		targets = append(targets, l.Target)
	}
	require.Equal(t, []string{"ship_albedo.png", "ship.bin"}, targets)

	png := pngBytes(t, 16, 9)
	p := ExtractAsset("img/a.png", []byte(png), int64(len(png)))
	require.Equal(t, "image, png, 16x9, "+formatBytes(int64(len(png))), describeAsset(p.Language, p.Asset))
}

// A script's own lookup helper is followed to the element ids and
// selectors it is called with.
func TestExtractDOMLookupHelpers(t *testing.T) {
	t.Parallel()
	src := "const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;\n" +
		"function pick(sel) {\n  return document.querySelector(sel);\n}\n" +
		"const canvas = $<HTMLCanvasElement>('scene');\n" +
		"const hud = $('hud');\n" +
		"pick('.panel #close');\n" +
		"const other = lookup('nope');\n"
	f := Extract("src/app.ts", src)
	refs := refsOf(f)
	require.Subset(t, refs, []string{"element scene", "element hud", "css panel", "element close"})
	require.NotContains(t, refs, "element nope")
}

// HTML a script builds in a string uses classes, components and files
// like a page does, at the lines it is written on.
func TestExtractMarkupInStrings(t *testing.T) {
	t.Parallel()
	src := "export function panel(body) {\n" +
		"  el.innerHTML = `\n" +
		"    <p class=\"panel-kicker ${body.kind}\">${body.type}</p>\n" +
		"    <img src=\"/textures/${body.id}.png\" alt=\"\">\n" +
		"    <div class=\"fact\"><span class=\"fact-label\">Fact</span></div>`;\n" +
		"}\n" +
		"const notMarkup = \"a < b and c > d\";\n" +
		"const generic = \"Array<string>\";\n"
	f := Extract("src/app.ts", src)
	var css []string
	for _, r := range f.Refs {
		if r.Kind == RefCSS {
			css = append(css, fmt.Sprintf("%s@%d", r.Name, r.Line))
		}
	}
	require.Equal(t, []string{"panel-kicker@3", "fact@5", "fact-label@5"}, css)
	require.Len(t, f.Links, 1)
	require.Equal(t, "/textures/${body.id}.png", f.Links[0].Target)
	require.Equal(t, 4, f.Links[0].Line)
	require.Equal(t, []string{"function panel 1-6"}, symbolsOf(f))
}

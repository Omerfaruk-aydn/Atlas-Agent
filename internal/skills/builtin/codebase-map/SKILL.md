---
name: codebase-map
description: Use when starting work in an unfamiliar project or after a compaction, when looking for where something is defined or used, before changing or deleting a function, type, file, image, CSS class, table or heading, when checking a project's docs and assets for broken or unused links, or when tracing how two parts of a codebase connect — in any common language.
metadata:
  provenance: atlas-authored
---

# codebase-map — Finding your way around a project

Atlas maps the project's code with the `code_map` tool: every source file,
what it defines, what it imports and which definitions use which. One call
answers in a few hundred tokens what would otherwise take reading many
files. Use the map to decide which files to open, then read those files.

What it reads:

- **Code**: Go, TypeScript, JavaScript (with .vue, .svelte and .astro
  components), Python, Mojo, Java, Kotlin, C#, F#, Visual Basic, C, C++,
  CUDA, Objective-C, GLSL, HLSL, WGSL, Rust, Swift, PHP, Ruby, Crystal,
  Dart, Scala, Groovy, Lua, shell, PowerShell, Zig, V, Nim, Perl, Elixir,
  Erlang, Gleam, Haskell, Elm, OCaml, Clojure, Lisp/Scheme/Racket, Julia,
  R, Pascal/Delphi, Fortran, Solidity, Verilog/SystemVerilog, VHDL and
  GDScript, plus Jupyter notebooks.
- **Markup and styles**: HTML and templates (Jinja, Handlebars, ERB, Twig
  and the like), CSS, SCSS and Less.
- **Data and schemas**: SQL, GraphQL, Protobuf, Thrift, Prisma, OpenAPI.
- **Build, CI and infrastructure**: package.json scripts, Makefile,
  justfile, Taskfile, Dockerfile, Compose, GitHub Actions, GitLab CI,
  pyproject.toml, Cargo.toml, Terraform, Godot scenes.
- **Documents**: Markdown, reStructuredText, AsciiDoc.
- **Assets**: images, fonts, audio, video, 3D models and data files are
  measured (format, pixel size, bytes) and linked to every file that names
  them; their contents are not read.

Dependencies (node_modules, vendor, virtualenvs), build output and minified
files are left out.

Uses cross between kinds of files, each only within its own kind: a class
in HTML or a component links to the stylesheet's class, `getElementById`
to the element id, a query string in code to the SQL table, `npm run x`
in a document to the script, a fetch of `/api/users/1` to the OpenAPI
endpoint `/users/{id}`, an image path to the image. A heading named
`Install` never answers a call of `install()`.

## When to reach for it

| Situation | Call |
|-----------|------|
| New project, or context was just compacted | `overview` |
| "Where is X?" | `find` with the name, then `explain` |
| About to read files to understand a feature | `explain` on its main definition or file first |
| About to change a signature, behavior or delete something | `callers` |
| "How does A end up calling B?" | `path` from A to B |
| Which tests or files usually change with this one | `explain` on the file |
| Rename, move or delete an image, doc, stylesheet class or table | `callers` on it, then `explain` |
| "Are the docs' links and images intact?" | `overview` (broken links), `explain` on the document |
| "Which images are unused?" | `overview` (named by no file), then grep before deleting |
| "Where is this endpoint called?" | `explain "GET /path/{id}"` |

Skip the map when you already know the exact file and line, or the
question is about text rather than code (use grep).


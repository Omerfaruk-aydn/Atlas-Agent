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

## The procedure

1. **Orient.** `code_map {action: "overview"}` lists languages by kind,
   subsystems (code files grouped by how they use each other), entry
   points and the most depended-on files with their main definitions;
   then the documents, the assets (largest, and those no file names) and
   broken links. Raise `budget` (up to 8000) for a large project; the
   default is about 2000 tokens. Call it once per task, not every turn.
2. **Locate.** `find` matches definition and file names, exact names
   first: functions and types, but also headings, CSS classes, element
   ids, tables, scripts and jobs. Query names you expect, not sentences:
   `solveKepler`, `Orbit`, `auth`, `btn-primary`, `users`.
3. **Understand one thing.** `explain` accepts a name, `Container.Name`
   (`Orbit.step`, `npm.build`, `make.test`), `path:Name`
   (`src/app.ts:main`) when a name is defined more than once, or a file
   path. For a definition it returns the location and signature, who uses
   it, what it uses, and the `view` call to read it. For a document it
   lists its sections, every link and image with what it resolved to, and
   the code it mentions. For an asset it gives its size and dimensions and
   each place that names it.
4. **Read before you edit.** Open the lines `explain` points to with
   `view`. The map shows signatures and links, not bodies.
5. **Measure the blast radius.** Before changing behavior, run `callers`
   (`depth` 1-5, default 2). Each level is what the previous one reaches.
   For a file, an image or a heading the first level is every link that
   names it. Every caller you change must be read and, when it has tests,
   checked.
6. **Trace a connection.** `path` gives the shortest chain of uses between
   two definitions or files, in either direction.

The map refreshes itself on every call and re-reads only changed files,
so it is current after your own edits.

## How sure each link is

Every link is labeled. Act on it accordingly:

- **resolved** — through an import, the same file or the same package.
  Trust it.
- **inferred** — the only definition with that name in the project, in a
  language without imports at that point, or a name written in prose.
  Usually right; confirm when it matters.
- **ambiguous** — several definitions match, or a method is called on a
  value of unknown type (`receiver type unknown`). Treat it as a lead and
  read the call site before relying on it.

When a change is risky, count only resolved links as certain and open
the files behind the others.

## What it cannot see

The map reads the code without building or running it. It misses calls
through reflection, callbacks stored in variables, dependency injection,
generated code, string-based dispatch, file names built at run time
(beyond a simple `${name}` template), classes or ids assembled from
strings, and in Ruby and Elixir calls written without parentheses.
Definitions inside dependencies are not linked. It measures assets but
does not look at them: `view` an image to see what it shows. So:

- "Used by (0)" or "named by no file" means nothing the map found, not
  that nothing uses it. Before deleting, also grep for the name and the
  file's base name, including in config, templates and strings; an asset
  may be served to clients or loaded from a directory listing.
- A broken link may point at a file the build generates or a server
  provides. Check before "fixing" it.
- For type-exact answers in one language, `code_query` asks the language
  server when one is configured; prefer it for overloads and interface
  implementations.

## Reporting

When you describe the structure of a project or the impact of a change to
the user, say what you read and what came from the map, and mention any
ambiguous links you did not confirm.

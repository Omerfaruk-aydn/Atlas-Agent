# Design tool

Atlas has its own design tool: an infinite canvas in the desktop app where
the person and the agent design logos, websites and app screens together.
The agent edits the same file the person has open, so they watch the design
being made and can take over at any moment.

## Architecture

```
app/renderer/src/features/design/     the editor (React + zustand)
  model.ts      document format, sanitizing, tree edits (pure)
  geometry.ts   matrices, bounds, hit testing, snapping, align, resize (pure)
  ops.ts        group, ungroup, duplicate, paste, components (pure)
  text.ts       text layout and measuring
  render.tsx    SVG drawing of nodes: the canvas and every export use it
  Canvas.tsx    pan, zoom, select, move, resize, rotate, draw, text editing
  Layers.tsx    pages, layer tree, assets      Inspector.tsx  properties
  export.ts / exporter.ts   SVG, PNG (rasterized), PDF (printed, vector)
app/renderer/src/stores/design.ts     open file, selection, undo, save, live watch
app/main/design-export.cjs            saves exports; prints PDFs in a hidden window
internal/design/                      the same format in Go: parse, ops, outline, SVG
internal/webui/design.go              gateway: list, read (by revision), save, assets
internal/agent/tools/design_canvas.go the agent's tool: list, read, create, apply, export
```

- **One file, two editors.** A design is a JSON file in the project at
  `.atlas/design/files/<name>.json`. The app saves through the gateway with
  the revision it last read; the agent's tool writes the file directly. A
  save made on an old revision is refused (409), so neither side overwrites
  the other blindly.
- **Live.** While the Design tab is on screen it asks the gateway every
  700 ms whether the file's revision changed; an unchanged file answers
  without the document. The agent's edit is taken in, highlighted for a
  moment, and is one undo step like any other.
- **The agent opens it.** When a running chat calls `design_canvas` to write
  a file, the side panel brings the Design tab forward on that file.
- **What you see is what you export.** The canvas is SVG drawn by
  `render.tsx`; SVG export renders the same components to markup with the
  bundled fonts embedded, PNG rasterizes that SVG, PDF is printed from it by
  Chromium, so text stays text and shapes stay vector.
- **Immutable documents.** Every edit returns a new document; unchanged
  nodes keep their identity, so React redraws only the edited path and undo
  is a list of earlier documents.

## Document format (version 1)

`{format: "atlas-design", version: 1, name, pages: [{id, name, background, children}]}`

Nodes: `frame, group, rect, ellipse, line, polygon, star, path, text, image,
component, instance`, each with `id, type, name, x, y, width, height`
(x, y relative to the parent) and optional `rotation, opacity, visible,
locked, blend, fills, strokes, effects, radius, children, clip`. Text adds
`text, font, line_height, letter_spacing, align, valign, auto_resize,
decoration, transform`; images `src, fit`; paths `d, view_box`;
polygons and stars `sides, inner`; instances `component_id`. Both sides
sanitize on read, so a bad value is repaired, not fatal. The shared
fixture `app/test/fixtures/design-sample.json` is read by the Go and the
TypeScript tests.

## Roadmap

**Phase 1 — core (done)**
- Document model with pages, frames, shapes, text, images, groups,
  components and instances; JSON persistence in the project.
- Infinite canvas: pan (wheel, space, hand, middle button), zoom (ctrl+wheel,
  pinch, fit, 100%), click and marquee selection with frame-aware picking,
  move with snapping and guides, resize from eight handles (shift keeps the
  ratio, alt from the center), rotate (shift in 15° steps), drawing tools,
  in-place text editing, image placing (file, drop, paste).
- Layers (tree, reorder and nest by drag, rename, hide, lock), pages,
  assets; properties for position, size, rotation, radius, opacity, fills
  (solid, linear, radial), strokes, shadows and blur, text; align and
  distribute; undo and redo; copy, paste, duplicate, group, frame selection.
- Export to PNG (0.5–4×), SVG and PDF.
- The `design_canvas` agent tool and live following of the agent's work.

**Phase 2 — layout**: auto layout (direction, gap, padding, hug and fill),
constraints and resizing rules, layout grids, rulers and measurement
(alt-hover distances), smart selection spacing.

**Phase 3 — systems**: component properties and variants, instance
overrides that survive component edits, shared styles (colour, text,
effect), variables and modes (light/dark), libraries across files.

**Phase 4 — vectors**: pen and pencil, path point editing, boolean
operations (union, subtract, intersect, exclude), flatten, outline stroke,
masks, image fills and crops.

**Phase 5 — prototyping**: links between frames, interactions and smart
animate, overlays, scroll areas, a presentation view.

**Phase 6 — collaboration**: multiplayer cursors and edits (operations
instead of whole-file saves), comments, version history, sharing.

**Phase 7 — handoff**: inspect panel with CSS, Tailwind, SwiftUI and
Compose snippets, asset export presets, design-to-code for React and HTML,
and code-to-design import of a running page.

Throughout, the agent tool grows with the editor: each new property is part
of the format both sides read, and the tool's description says how to use
it well.

## Status

Phases 1-7 are implemented: vector editing, auto layout and constraints, styles/variables/variants, boolean ops and masks, prototyping and present mode, comments, version history and three-way merge, and code generation (CSS, Tailwind, SwiftUI, Compose, HTML/React export).

Outline stroke is built (round joins, butt caps).

Out of scope: live multiplayer over the network needs a sync server a local desktop app does not have, so only file-level three-way merge exists. Code-to-design is done by the agent reading the code and issuing design ops, not by a parser.

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
locked, blend, fills, strokes, effects, radius, children, clip`; frames and
components also `layout, grids`. Text adds
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

Outline stroke is built (round joins, butt caps). The design kit (themes, blocks, icons and lint) is below.

Out of scope: live multiplayer over the network needs a sync server a local desktop app does not have, so only file-level three-way merge exists. Code-to-design is done by the agent reading the code and issuing design ops, not by a parser.

## Design by hand (the studio)

The agent designs the way a person does: it opens this editor in a browser
and works it by mouse and keyboard, not by writing the file. `design_canvas`
action `studio` (file or title) creates the file if needed and starts
`internal/designstudio` for the project: a loopback server that serves the
renderer's standalone page `design.html` (`app/renderer/src/studio/`) and the
same design endpoints the desktop gateway mounts per workspace
(`designstudio.Routes`). The page is the Design tab itself, in English by
default (`?lang=tr` for Turkish), so the agent always sees the same labels.
The result carries an operating guide (`design_studio_guide.go`): tools and
shortcuts, the inspector, auto layout, grids, measuring, and how a senior
designer builds a screen. The agent then uses the `browser` tool's
`pointer_drag`, `pointer_click`, `insert_text`, `key` and `set_field`. The
person sees every edit live in the app's Design tab, which holds the same
file.

The server only answers its own `127.0.0.1:<port>` host, and state changes
need the token `/studio.json` hands the page (a cross-origin page can read
neither). It finds the renderer build in `ATLAS_DESIGN_STUDIO_DIR`, the
installed app (`app/resources/renderer`) or a checkout's
`app/renderer/dist`.

Figma parity added with it: layout grids on frames (columns, rows, square
grid; inspector section, `Ctrl+Shift+4`), Alt-hover distance measuring,
`Shift+A` auto layout (on frames, or wrapping a selection), and a canvas
right-click menu with the common commands.

Also: rulers (`Shift+R`) with draggable guides that moves snap to and that
are saved in the file's page (`guides`); sections (`Shift+S`, node type
`section`: a labelled, unclipped holder for frames); slices (`S`, node type
`slice`: exporting one gives the page as seen inside its box; it draws
nothing and codegen skips it); an eyedropper (`I`, plus the system picker in
every colour field); image crop (`crop: {zoom, x, y}` on image nodes, set by
double-click, drag and Alt+wheel, or the inspector); and angular gradients
(`type: "angular"`, drawn as wedges in SVG, `conic-gradient` in code export).

## Design scripts and phases

Building a system by clicking is slow and drifts; composing a screen by
script looks generated. So the work is split. `design_canvas` action
`script` (file, code) runs a short JavaScript program in a goja sandbox (no
file, network or clock; 5 s; at most 4,000 edits) whose API
(`internal/design/script_prelude.js`) records ordinary ops:
`colors()` (variables `c-*` with Light/Dark), `type()` (text styles `t-*`),
`space()`, `radius()`, `shadow()` (`e-*`), `row`/`col`/`frame`/`text`/
`icon`/`rect`/`image`, `component()`, `variants()` (every combination as a
set), `instance()` (text, icon swap, hide and fill overrides by layer name),
`library()`, `screen()` and `place()`. `internal/design/script.go` applies
them as one step:

- Ownership: every layer gets an id `<script>:<slug>`. A rerun deletes what
  the last run made and makes it again under the same ids, so instances the
  person placed keep pointing at the same mains. A root whose hash changed
  since the last run (layout- and measure-derived fields excluded) was edited
  by hand and is kept unless `force`. The declared system and root hashes
  are stored in `.atlas/design/systems/<file>.json`, the script in
  `.atlas/design/scripts/<file>.js`.
- Instances made without a size take the size their content needs in the
  main's layout (`InstanceSize`, with overrides applied), repeated until
  nested mains settle.
- Gate: `SystemLint` (`internal/design/system.go`) plus page lint on what
  the script made. Errors stop the save: a component with two or more
  layers and no auto layout, unstyled text in a component, a variant set
  with a missing or partial combination, unreadable contrast or overflow.
  Warnings: raw colours, spacing and radii off the declared scales, short
  controls, default layer names, three or more repeated structures that are
  not instances, a main inside a screen. `lint` includes the same checks.

Instances of a main with auto layout are now laid out again at their own
size (Go `layoutInstance` in `Resolved`, TS `layoutInstance` in
`resolveTokens`) instead of being scaled, in the editor, SVG export and
render: a longer label widens the button, a wider instance spreads its
content. Overrides gained `d` (icon swap).

Action `progress` reads the files and reports five phases with exit
conditions and call budgets: brief (`briefs/<file>.md`), system (4+ colour
tokens, 4+ text styles, a spacing scale), components (3+ mains, no
component errors), screens (device frames with instances), review (every
screens page rendered since its last change, lint 90+ with no errors). A
browser task's `design` requirement can wait on a phase with `@components`,
`@review` and so on. The studio guide sends the agent through the phases,
the system and components as a script, the screens by hand, and batches
studio edits into one `tool_pipeline` call per section.

### Starter kit, patterns and references

Three built-in skills raise the floor of what an agent draws. `design-kit`
is a ready design script: eleven components (button, input, switch, chip,
badge, avatar, card, list row, top bar, tab, tab bar) on auto layout with
their states as variants; a test runs it through the gate so it never rots.
`design-patterns` lists the proven structure of the usual screens
(onboarding, sign in, feed, list and detail, search, checkout, settings,
empty and error states, dashboard, landing page). The brief phase is not
done until the brief has a `References` section with two real products and
what to take from each.

Three things keep the agent from judging work it cannot see or has nothing
to measure against:

- **Real type in the render.** The SVG names each family with the face the
  bundled `@font-face` declares (`Fraunces` becomes `Fraunces Variable,
  Fraunces, serif`; `internal/design/fonts.go`), so the PNG shows the font
  the script chose. A family the renderer lacks raises the lint warning
  `font-missing`. Project images are embedded in the SVG, so photos show in
  the PNG too.
- **Shipped work beside the render.** `design_canvas` action `references`
  (file, query) keeps store screenshots of real apps with the design;
  `compare` sets the latest render beside them at phone height with measured
  questions. The review phase is not done until one round was logged against
  them under a `Reference:` line.
- **Material.** The design script draws the subject's own things as vectors
  in the file's tokens: `map()` (a street-map crop with route and pins),
  `chart("spark" | "bars" | "ring")`, `pattern("topo" | "dots" | "lines" |
  "grid" | "perforation" | "stars")`, `sprig()`, `path()`, and for the
  styles `chart("area" | "wave" | "zones")`, `qr()` (a real QR code from
  `rsc.io/qr`), `scrim()`, `notches()` and `trail()`. A phone screen with
  none of these, no image and no illustration raises the warning
  `no-material`.
- **Styles.** The `design-styles` skill holds ten whole screens as scripts
  (public service, kids, sport, ticket, food, travel, wellness, fintech,
  editorial, player) and `design-styles-plus` seventeen more (meal plan,
  calendar, reader, brutalist, boutique, recorder, focus, thermostat,
  stays, now playing, breathwork, EV map, terminal, flight, recovery,
  flashcard, inbox), each measured against a reference screen;
  `styles_test.go` runs all 27 through the gate and lint. Helpers added
  for them: `dial()`, `meter()`, `arcRoute()`, gauge and timer rings
  (`chart("ring", v, {sweep, dot})`), split bars (`zones` with `widths`)
  and value tags on maps (`kind: "tag"`). The lint also
  warns `mixed-language` for a screen that mixes Turkish and English copy.

## Code to design and swarm

- `design_canvas` action `import` turns HTML (typically a running web app's rendered page) into a frame: flexbox becomes auto layout, `<style>` rules, inline styles and CSS variables are resolved. Native UI code (SwiftUI, Compose, Flutter, XAML, ...) is mapped by the agent with the rules in `design_canvas.md`.
- The **Swarm** button in the Design tab asks the lead agent to design one brief as 2-4 variants, each on its own page by its own background sub-agent (optionally a named subagent per model), then compare them. `apply` retries on save races so parallel agents do not lose edits.

## Design kit

Generated designs looked improvised because every colour, size and layer was
invented on the spot. The kit (`internal/design/kit*.go`, data in
`internal/design/kit/`) gives the agent and the person established parts to
build from instead:

- **Themes** (11): Astryx's seven (neutral, butter, chocolate, gothic,
  matcha, stone, y2k), Radix, Zinc (the shadcn/ui look), iOS (HIG system
  colours and type ramp) and Material 3. `set_theme` writes 19 colour
  variables `c-*` in Light and Dark, 14 text styles `t-*` and three shadows
  `e-*` into the file, and recolours and re-fonts what is already bound to
  them; `accent` switches the accent to any of 25 Radix hues. Text colours
  that a source theme leaves under 4.5:1 are corrected when the theme loads.
- **Blocks** (66): 24 web (navbar, heroes, features, pricing, testimonials,
  FAQ, CTA, footer, sign-in, app shell, table, components), 31 iOS (the
  components plus the parts of an app screen: shell, hero, day strip,
  schedule, plan, badge, status list, notifications) and 11
  Material 3, all auto layout and bound to the theme. `insert` places one,
  filling its slots (texts and lists) from `props`; inside a column frame a
  section takes the frame's width.
- **Icons**: Lucide's 2,046 icons. An `add` node `{"icon":"bell"}` becomes a
  round-capped stroked path in the theme's text colour. Strokes gained `cap`
  and `join` for this.
- **Lint** (`design.Lint`, action `lint`): contrast, overflow, clipping,
  off-grid spacing, alignment near-misses, font, size and colour sprawl,
  touch targets on phones, line length, emoji and placeholder copy, with a
  score. Every block passes it in every theme and mode (`kit_test.go`).

The app's **Library** panel (Shift+I) offers the same themes, blocks (with
server-drawn previews) and icons; it sends kit ops to
`POST /design/kit/apply`, which returns the new document for the app to
commit as one undo step. The themes' typefaces are bundled (`fonts.ts`) and
embedded in exports.

`internal/design/kit/gen/generate.mjs` rebuilds the data from the upstream
packages; `LICENSES.md` there holds their notices. Because the theme is one
per file, a swarm's lead sets it once and the variants differ in
composition.

## Pictures, photographs and rules

There are two ways to make a design, used together on one file: by hand in
the studio, and as a design script. The HTML screen studio and painted
screens (`screen_image`, `screen_patch`) were removed in favour of these
two; pictures now go straight into the design file.

- **Generated pictures.** `design_canvas` action `image` (`file`, `image` =
  a name, `prompt`, `aspect` = square, portrait, tall, landscape or wide)
  draws an illustration or artwork and saves it to
  `.atlas/design/screens/<file>/assets/<name>.png`. A script places it with
  `image("assets/<name>.png", {w, h, radius})` or an image fill; render
  embeds it in the SVG and PNG. The picture also comes back in the tool
  result so the model can judge it.
- **Photographs.** Action `photo` (`file`, `query`, `image`, `aspect`,
  `pick`) uses Pexels (`PEXELS_API_KEY`) or Unsplash
  (`UNSPLASH_ACCESS_KEY`) when set, otherwise Openverse (openly licensed, no
  key; no-derivatives licences are dropped). The credit and licence of every
  saved photo is recorded in `assets/credits.json`, and the tool says when a
  credit must be shown.
- **Rules and tables.** Action `knowledge`: `topic: platform` returns written
  ios, android, tablet and web rules; `topic: ux|mobile|charts` with `query`
  searches the MIT-licensed ui-ux-pro-max tables; `topic: scale` with
  `scale: "base=17 ratio=1.2"` returns a type scale.

Third-party material and licences: `internal/screens/catalog/NOTICE.md`.

No image API key is involved. The generator (`internal/agent/imagegen.go`)
uses the subscriptions the person is already signed in to:

- ChatGPT: the Codex backend's `image_generation` tool, run inside a normal
  turn of `gpt-6.1-sol`. Returns a PNG.
- Antigravity: Cloud Code `gemini-3.1-flash-image` with
  `responseModalities` TEXT+IMAGE and `imageConfig.aspectRatio`. Returns a JPEG.

ChatGPT goes first and Antigravity is the fallback. Set
`ATLAS_IMAGE_PROVIDER=chatgpt` or `antigravity` to pin one. Each image uses a
little of that plan's quota. Expired tokens are refreshed first.

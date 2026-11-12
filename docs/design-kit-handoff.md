# Design kit: handoff prompt for the next agent

Paste the section "The prompt" into Cursor's agent. Everything it needs is in
this repository; this file is the briefing and the plan to refine.

---

## The prompt

You are a senior product-design engineer joining an in-progress project. Read
this whole brief before you touch anything. Your job is to take Atlas's design
system from "decent" to "a professional designer would ship from it", in small,
verified steps. Start with a written plan and wait for my approval on the
decisions marked DECIDE before you build them.

### 1. What this project is

Atlas is a desktop coding/agent app: a Go backend (`internal/...`, module
`github.com/Omerfaruk-aydn/Atlas-Agent`) and an Electron + React 19 + zustand
front end (`app/renderer/src`). It contains a Figma-like **design tool**. A
person edits designs in the Design tab; an AI agent edits the same file through
the `design_canvas` tool. Designs are JSON files in `.atlas/design/files/`.

The problem I want you to solve: **designs the agent produces look amateur**
("AI slop"): invented colours, random spacing, emoji for icons, grey boxes for
images, the same three cards everywhere. The fix started with a **design kit**:
established parts the agent places instead of drawing from scratch.

Read these first, in this order:

1. `docs/design-tool.md` (architecture, document format, and a "Design kit" section).
2. `internal/agent/tools/design_canvas.md` (what the agent is told; it is the product spec for the agent side).
3. `internal/design/doc.go`, `ops.go`, `layout.go`, `tokens.go` (the document model, ops, auto layout, styles and variables).
4. The kit: `internal/design/kit.go` (themes, accents, contrast repair, icons), `kit_build.go` (the block builder and `insert`), `kit_web.go`, `kit_mobile.go` (iOS and Material), `kit_ios_app.go` (iOS app screen parts), `lint.go`, and the tests `kit_test.go`.
5. Kit data and its generator: `internal/design/kit/themes.json`, `icons.json.gz`, `kit/gen/generate.mjs`, `kit/LICENSES.md`.
6. The reference composition: `internal/design/kit/examples/cop31-ops.json` (13 phone screens built only from the kit; `cop31-build.py` shows how the ops are written). A test keeps it linting at 90+.
7. App side: `app/renderer/src/features/design/` (`Library.tsx`, `fonts.ts`, `kit-fonts.ts`, `layout.ts`, `text.ts`, `render.tsx`, `model.ts`) and `app/renderer/src/stores/design.ts`.
8. Server: `internal/webui/design.go` (`/design/kit`, `/kit/preview`, `/kit/icons`, `/kit/apply`).

### 2. What exists today (do not rebuild it)

- 11 themes: Astryx's seven (Meta, MIT: neutral, butter, chocolate, gothic, matcha, stone, y2k), Radix, Zinc (shadcn look), iOS, Material 3. Each has 19 colour tokens (light and dark), a 14-step type scale, radii and shadows. `set_theme` writes variables `c-*`, text styles `t-*`, effect styles `e-*` into the file; `accent` switches the accent to one of 25 Radix hues. Contrast of text colours and of the accent is repaired at theme load.
- 66 blocks: web (24), iOS (31: 12 components plus 19 app parts such as `ios/shell`, `hero`, `highlight`, `day-strip`, `schedule`, `map`, `badge`, `status-list`, `notifications`), Material 3 (11). `insert` places one with slot props. Everything is auto layout and bound to theme variables.
- 2,046 Lucide icons; node field `icon` makes a stroked path.
- `lint` (contrast, overflow, clipping, 4px grid, alignment, fonts/sizes/colours sprawl, touch targets, line length, emoji, placeholder copy, purple-blue gradient) with a score.
- A Library panel in the app (Shift+I): themes, blocks with server-drawn previews, icon search.
- Bundled fonts for the themes, embedded on export.

### 3. Honest weaknesses (this is where you add value)

1. **The agent still decides quality.** In a real trial a weak model (Grok) took blocks as a skeleton, then hand-drew over them: a segmented control whose parts were taller than itself (text clipped), an empty "map", labels cut off, sample copy left in. Lint did not stop it because the agent never reached the lint step. Strong models do better but nothing forces the loop.
2. **No vision loop.** Lint is geometric. Nobody looks at the picture. We needed to eyeball PNGs ourselves to find bugs lint scored 99 on (list rows inheriting sample fields, tab labels cut, a pink warning wash).
3. **Text measurement mismatch.** Go estimates text width as `0.55 × size` per glyph (`Measure` in `ops.go`); the app measures with the real font. Previews, exports from the agent, lint overflow checks and hug sizes disagree with what the person sees. This corrupts everything downstream.
4. **Blocks are plain frames, not components.** After `insert`, a block has no identity: you cannot restyle it by changing props, switch a variant, or update ten instances at once. Library theme switching only works because layers are bound to variables.
5. **Coverage gaps.** No Material app-shell/screen parts (iOS has them, Android does not); no tablet or desktop-app layouts; web has no chart, bento, timeline, comparison, form, cart/checkout, blog, team, gallery blocks; no data visualisation primitives; no navigation patterns beyond tab bar and back.
6. **Imagery.** Every image is a grey placeholder. The COP31 "map" is boxes. Nothing generates illustration, photos, avatars, or abstract art.
7. **No prototype wiring from blocks.** Screens built from the kit have no `interactions`; the agent must wire tab bars and flows by hand.
8. **Dark mode and translucent fills are not truly checked.** Lint treats a translucent fill as transparent (it picks the next opaque ancestor), so contrast of text on soft tints is approximate. Only light mode is checked in lint runs.
9. **Slots are loosely typed strings.** Lists of objects pass as `[]map[string]string`; errors are discovered at insert time; there is no schema or JSON Schema for the agent's tool call.
10. **Code export ignores the kit.** `design.GenerateCode` emits absolute CSS, not token variables or component structure.
11. **Weak evaluation.** There is no benchmark that compares models or detects regressions in design quality. Block previews have no golden-image tests.
12. **File weight.** The 13-screen example is ~880 KB (the QR code is 200+ rect nodes per badge).

### 4. Ideas to evaluate and build (prioritised)

For each item below: write a one-paragraph design note, name the files you will
touch, say how you will verify it, then build it. DECIDE items need my answer
first.

**P0: make quality the default**

- **A. Real font metrics in Go (fixes weakness 3).** At kit-generation time, read the bundled fonts (Inter, Geist, Figtree, Outfit, DM Sans, Albert Sans, Fraunces, Fustat, Montserrat, Poppins, Roboto, JetBrains Mono) with opentype/fontkit and emit per-glyph advance tables (and kerning for the common pairs) into a compact embedded file. Use them in `Measure`/`Lines`. Acceptance: for 200 sample strings across fonts and sizes, Go width is within 1% of the browser's `measureText` (add a Playwright test that compares them), and the cop31 example needs no layout changes in the app.
- **B. A `render` and `critique` loop.** Add `design_canvas` action `render` that rasterises a page or node to PNG (use a pure-Go rasteriser if one is good enough, otherwise the existing browser tooling; DECIDE) and `critique` that returns the PNG path plus a fixed rubric (hierarchy, spacing rhythm, alignment, contrast, copy quality, brand fit, emptiness). Update `design_canvas.md` so an agent must render and critique before it declares done, and make `lint` fail loudly when a page never went through render. Acceptance: the weak-model trial (see section 6) ends with no clipped text because the critique step catches it.
- **C. Guardrails against hand-drawing.** Add a "kit coverage" metric to lint: share of nodes created by blocks versus custom nodes with unbound colours or off-token text. Warn when custom layers dominate. Add `update` protection or a warning when an op modifies the internals of an inserted block (fixed-size children inside an auto-layout block, which caused the segmented-control bug). Acceptance: the Grok-style failure produces lint errors, not a 99.
- **D. Blocks as components with variants (fixes weakness 4). DECIDE.** Each inserted block becomes a `component` (or instance of a per-file master) with `variant` props (`style: large|inline`, `tone`, `selected`) and `overrides` for text slots. Add ops `set_block_props {id, props}` to re-run a block in place keeping ids, and store the block id and props on the root (`meta`). Then Library theme/mode/accent changes, "swap variant" and "update all" work. Acceptance: change `title` of a placed hero through one op and the node ids and any manual tweaks outside slots survive.

**P1: breadth and fidelity**

- **E. More blocks, same quality bar.** Web: bento grid, comparison table, timeline, team, gallery, blog list, contact form, newsletter, cart and checkout, pricing toggle, sidebar variants, charts. Mobile: forms and steppers, maps, camera and scan, media player, chat, cards carousel, empty and error states, bottom sheets. Android: a full `material/shell` and the same screen parts iOS has. Desktop/tablet app shells (sidebar plus detail, master-detail). Each block needs sample copy that reads as real, lint-clean in all themes and both modes, and a preview.
- **F. Data visualisation primitives.** Generate line, bar, donut and sparkline charts as paths from data slots (`values`, `labels`), themed. Needed for dashboards.
- **G. Imagery system. DECIDE.** (1) Deterministic abstract art (mesh gradients, shapes, patterns) seeded from a string, themed; (2) avatar and illustration sets as paths; (3) optional hook to the project's image-generation tools with a licensing note. Replace grey `imagePlaceholder` boxes in blocks with the art by default.
- **H. Prototype wiring.** Blocks emit interactions: tab bar items navigate to frames of the same names, back buttons go back, list rows with `to` navigate, CTA buttons `navigate`. Add a `link_screens` op that wires a set of screens by title. Acceptance: Present mode in the app plays the COP31 example through the tab bar.
- **I. Page and flow recipes.** `recipe` ops that expand to a whole screen or a flow: landing page, pricing page, dashboard, onboarding (3 screens), sign-in and sign-up, settings, e-commerce product flow, plus responsive variants (desktop 1440, tablet 834, phone 390) of the same content with different layouts. A recipe takes a brief-shaped JSON (brand, copy, features) and returns ops.
- **J. Lint v2 (fixes weakness 8).** Composite translucent fills correctly before computing contrast; check both modes automatically; add colour-blindness simulation, text over images and gradients, consistent corner radii, touch target and spacing checks per platform, and "sample copy left in" detection by comparing against the block's default slots. Return structured fixes (`{op, id, props}` suggestions), and add an `autofix` action for the safe ones.

**P2: system and handoff**

- **K. Theme studio.** Derive a full theme from a brand colour (OKLCH ramps like Astryx's palette generator, with contrast guaranteed), from a logo or screenshot (extract palette and type), or import tokens (W3C DTCG JSON, Tailwind config, CSS variables, Figma variables). Export the reverse. Add a theme editor to the Library panel.
- **L. Code export that matches the kit.** Emit CSS variables from `c-*`, a Tailwind config, SwiftUI `Color`/asset catalog and Compose `MaterialTheme`, and component-level React/SwiftUI/Compose for blocks (one component per block, props from slots). Today `GenerateCode` is absolute CSS.
- **M. Accessibility pass.** Focus and reading order, minimum sizes, dynamic type previews, reduced-motion in prototypes, alt text slots for images.
- **N. Slot schemas.** Describe each block's slots as JSON Schema (type, enum, max length, list item shape). Use it to validate `insert`, to build precise tool descriptions, and to render a props form in the app.

**P3: product polish**

- **O. Library panel v2.** Search and tags, favourites, drag a block onto the canvas at the drop point, live theme preview, "save selection as block" with slots (custom and team libraries), keyboard navigation, Turkish and English strings.
- **P. Evaluation harness (do this early, it protects everything else).** A fixed set of 10 briefs (landing, dashboard, mobile onboarding, e-commerce, event app, fintech, health, education, internal tool, portfolio). A script runs an agent on each, renders, runs lint and a rubric-based vision scorer, and writes a table (score, lint errors, kit coverage, time). Add golden-image tests for every block in three themes (hash of the SVG) so regressions show in review. Acceptance: one command prints the table; CI-friendly.
- **Q. Weight.** Draw QR codes as one path; dedupe repeated structures; keep example files small.

### 5. How to work here

- **Do not commit or push.** I decide when. Leave the working tree as changes; list what you changed at the end.
- The working tree already has many unrelated changes (other agents, other features). Touch only what your task needs; never reset, stash or revert files you did not write.
- **Mirrored code:** the layout engine exists twice and must stay identical: `internal/design/layout.go` and `app/renderer/src/features/design/layout.ts`; the model exists in `internal/design/doc.go` and `app/renderer/src/features/design/model.ts`. Text measurement differs on purpose today (item A removes the gap). When you change one side, change the other and add a test on both.
- **Quality gate for every block you add or change:** it must build in all 11 themes in light and dark (`TestEveryBlockBuildsInEveryTheme`), pass its own lint (`TestBlocksPassTheirOwnLint`), and you must look at the rendered picture. A score of 99 does not mean it looks right.
- Go style is in `AGENTS.md` (gofumpt, comments end with a period, `fmt.Errorf` wrapping, `t.Parallel()`, testify `require`, 78-column comments). Windows: file names are case-insensitive (a `swarm.ts` once collided with `Swarm.tsx`).
- Preview tooling: `ATLAS_KIT_PREVIEW=<dir> ATLAS_KIT_THEMES=zinc,ios go test -run TestKitPreview ./internal/design/` writes block SVGs; rasterise them with Playwright's Edge channel (`app/node_modules/playwright-core`) and read the PNGs. Do not inline several SVGs into one HTML page: their ids collide and fragments disappear.
- Be honest in reports. If something is unverified, say so. If a test fails, show it.

### 6. Commands

Go (PowerShell on Windows). Add Go to PATH, unset `GOROOT`, set `CGO_ENABLED=0`, and unset the `AI_AGENT` env var (it breaks one cmd test). Use `-p 1`:

```
$env:PATH="$env:LOCALAPPDATA\Programs\Go\bin;$env:PATH"; Remove-Item Env:GOROOT -ErrorAction SilentlyContinue; $env:CGO_ENABLED="0"
go test -p 1 -count=1 ./internal/design/ ./internal/webui/ ./internal/agent/tools/
```

`./internal/agent/tools` takes about 7 minutes; run it in the background. Kit data is regenerated with `node internal/design/kit/gen/generate.mjs --astryx ... --radix ... --lucide ...` (see the header of that file for the inputs; you will need `npm pack @radix-ui/colors lucide-static` and the Astryx theme sources).

App (from `app/`):

```
npx tsc -p renderer/tsconfig.json
npx vitest run test/unit.test.ts
npm run build
npx vitest run --config vitest.e2e.config.mjs      # 119 tests, about 6 minutes; build first
```

Real-model trial (optional): `atlas-agent run -m <provider/model> --yolo --quiet --cwd <dir> < prompt.txt`, with providers configured in that folder's `atlas.json`. The weak-model failure above is the regression to beat.

### 7. What I expect from you

1. **First reply, before any code:** (a) what you read and one paragraph on how the system fits together, in your own words; (b) the weaknesses above you agree or disagree with, and any you found that are not listed; (c) a ranked plan with scope and risk per item, and your questions on the DECIDE items. Keep it under 1,200 words.
2. After I approve: work in small steps, each with tests and a look at the rendered output. Start with item P (evaluation harness) and A (font metrics), because they make everything else measurable and correct.
3. **Each finished step ends with:** what changed (files), how you verified it (commands and what you saw), what is still unverified, and the next step. No marketing words; numbers and evidence.

Design on the Atlas design canvas: logos, websites, app screens, any visual.
Files live in the project at .atlas/design/files/<file>.json and the person
watches every change live in the app's Design tab, where they can edit too.

There are two ways to work, and a good design uses both on the same file.

DESIGN BY HAND (the default when the person asks you to design something):
action "studio" with file (or title) opens Atlas's own Figma-class editor
for that file in the browser and returns its address with the operating
guide. Then design in it with the browser tool's mouse and keyboard, the
way a designer works Figma: tools, drags, typing, the inspector, auto
layout, components, styles.

DESIGN SYSTEM AS A SCRIPT (with the studio, for the exact, repetitive part):
action "script" with file and code runs a short JavaScript design script
(read atlas://skills/design-script/SKILL.md first) that makes colour tokens
with light and dark values, text styles, spacing and radius scales, shadows,
components on auto layout with every state as a variant, a Library page,
and screens from instances, in one step. What it makes is checked before it
is saved (no component without auto layout, no unstyled text in a
component, no missing variant, no unreadable contrast); a failing script
saves nothing and lists why. The script is kept at
.atlas/design/scripts/<file>.js; without code the action reruns it, which
replaces what it made before and keeps any root the person edited by hand
(force overwrites). script names a second script on the same file.
action "progress" with file reports the design's phase (brief, system,
components, screens, review), what is missing and the budget for the
current phase, read from the files: check it instead of taking screenshots
to decide what comes next.

PICTURES (what lifts a screen from wireframe to product): action "image"
with file, image (a name) and prompt draws an illustration or artwork with
the person's signed-in image model (a tile's spot illustration, a header
scene, an empty state); action "photo" with file, image and query finds a
real licensed photograph (news, places, people). Both are saved with the
design at assets/<name>.png and placed in a script with
image("assets/<name>.png", {w, h, radius}) or as an image fill; render
embeds them. Keep one style sentence for every image prompt of a file.

Actions:
- image / photo: pictures for the design, above.
- knowledge: topic platform (written ios, android, tablet or web rules),
  ux / mobile / charts with query (rule and chart-choice tables), scale with
  scale="base=17 ratio=1.2" (a type scale).
- list: the design files.
- create: a new file (file, or title to name it), optionally with ops.
- read: an outline of the file, one line per node with id, box, fills,
  text. With node_id, that node's full JSON.
- apply: a batch of ops, applied in order, all or none.
- code: writes HTML+CSS (format "html", default) or a React component+CSS
  (format "react") for node_id (a frame) to .atlas/design/exports/; auto
  layout becomes flexbox, other layers are absolutely positioned.
- export: writes an SVG to .atlas/design/exports/ (node_id for one node).
- render: writes that SVG and a PNG next to it, in the real kit faces
  (Fraunces, Geist, Albert Sans, Outfit, Figtree, DM Sans, Inter...; lint
  warns font-missing for any other family, which renders as a fallback) and
  with the project's images embedded. With no node_id it stamps the page.
  Lint fails a page that changed after the last render.
- references: with file and query (a kind of app, or app names separated by
  ;), store screenshots of shipped apps, one app per row, kept with the
  design. Write what you take from two of them into the brief.
- compare: with file (page optional), the latest render beside those
  references at phone height, with measured questions (dominant element,
  type sizes, density, accent share, material, edges, platform). Log the
  gaps in a review round under "Reference: <apps>". path compares against
  one image in the project instead.
- critique: the PNG path plus a rubric (hierarchy, spacing, alignment,
  contrast, copy, brand, emptiness). Look at the PNG and answer each line.
  Do not say a design is done while any line is fail.
- themes / blocks (query web|ios|android) / icons (query words): the design
  kit, below.
- lint: checks a page (or node_id) for contrast, overflow, clipping,
  off-grid spacing, alignment near-misses, too many fonts, sizes or
  colours, small touch targets, overlong lines, emoji and placeholder copy,
  and gives a score out of 100. It also reports how much of the page came
  from kit blocks, and contrast in the other colour mode. A page of
  hand-drawn layers with unbound colours fails. It fails a page that has
  not been rendered since it changed.
- autofix: applies the safe fixes (layout spacing snapped to an even pixel)
  and saves. It does not rewrite copy or colours.

## The design kit: start here

A finished screen is composed, not drawn. One recipe builds it from the
kit: a real theme, real type, one accent, and sections with the brief's
own nouns. That is the quality bar. Drawing rectangles, picking colours,
and stacking three identical cards is how generated UI looks cheap.

{op:"recipe", name:"stitch", props:{platform:"web"|"ios"|"android", product:"Northwind", promise:"The week's jobs, on one screen", detail:"Harbour Street bakery. Three ovens, two shifts.", color?:"#C2410C"}}

- web stacks one scrolling page: navigation, a split hero, one feature, a close, a footer.
- ios and android place one shell per screen and put the parts in its body (`<id>-body`). Tab labels match the screen names, and the screens are linked.

Then set_block_props for any sentence that is still generic. Do not draw a second layout over a block, and do not set the width or height of a layer inside one. Then render, answer critique from the PNG, and lint. Done means 90 or more and every critique line a pass.

The parts, when a recipe is not enough:

- Themes: Astryx (Meta's design system: neutral, butter, chocolate, gothic,
  matcha, stone, y2k), Radix, Zinc (the shadcn/ui look), iOS (Apple HIG
  system colours and type ramp) and Material 3. {op:"set_theme",
  name:"stone", accent?:"crimson", mode?:"Dark", color?:"#C2410C"} writes the colour
  variables c-<token> (light and dark), the text styles t-<step> and the
  shadows e-sm/e-md/e-lg into the file. Switching theme, accent or mode
  later restyles everything built from them. Pick the theme that fits the
  brief (action themes describes each) instead of defaulting to one.
- Blocks: 66 ready sections and components, all auto layout, all bound to
  the theme. Web: navbar, hero, hero-split, logos, features, feature-split,
  stats, pricing, testimonials, faq, cta, footer, signin, app-shell, table,
  stat-card, card, button, badge, input, alert, tabs, modal, empty-state.
  iOS: screen, status-bar, nav-bar, search, list, tab-bar, button,
  segmented, switch, card, alert, sheet. iOS app parts: shell (an empty
  screen with status bar, nav bar and tab bar; insert the rest into its body
  "<id>-body"), hero, highlight, banner, section, text, spacer, day-strip,
  chips, tiles, schedule, detail-head, people, status-list, grid,
  notifications, map (a site plan from zones and pins), badge (QR card),
  action-bar. Build a phone app as one shell per screen plus these parts;
  examples/cop31-ops.json in the kit is a 13-screen app made that way.
  Material 3: screen, top-app-bar,
  navigation-bar, button, fab, card, list, text-field, chips, switch,
  dialog. {op:"insert", block:"web/pricing", id:"pricing", parent:"page",
  props:{title:"…", plans:[{name, price, period, text, cta, features:"a|b",
  featured:"true"}]}}. {op:"set_block_props", id:"pricing", props:{title:"…"}}
  changes the slots of a block you already inserted: the layer ids stay, and
  a recolor of a non-text layer stays. Do not set the width or height of a
  layer inside a block; lint reports that clipping as an error. Action blocks shows every slot with its sample value;
  give every slot the brief's real words, never ship the sample copy.
- Icons: 2,000+ Lucide icons. Action icons with query finds names; an add
  node {"type":"path","icon":"bell","width":20} draws one in the theme's
  text colour (round caps, Lucide's stroke weight). Never use emoji as
  icons.

A page is a column of sections: add a top-level frame {type:"frame",
width:1440, sizing_y:"hug", layout:{dir:"column", gap:0, pad:0}} (phone: 390
or 412 wide, 844 or 915 tall), then insert sections into it with parent; a
section in a column stack takes its width. Phones usually start from
ios/screen or material/screen and change rows and titles. After the blocks,
adjust with update ops (texts, images, which variant) and add what the brief
needs that no block covers, using the same tokens: fills
{type:"solid", color, variable:"c-text"}, text_style "t-body", radius from the
theme, spacing on the 4px grid.

Before you say a design is done: render the page, critique it and answer
the rubric from the PNG, then lint, and fix what either one finds. Lint
fails a page that has not been rendered since it changed. Aim for a score
of 90 or more, with every rubric line a pass.

Ops:
- {op:"recipe", name:"stitch"|"landing"|"app"|"android"|"pricing"|"dashboard"|"onboarding"|"signin"|"settings"|"product"|"desktop", props?:{platform, product, promise, detail, title, color}} places a finished page or a linked set of screens. stitch picks web, ios or android from platform.
- {op:"link_screens"} connects top-level frames: a tab or label whose text matches another frame's name navigates there, and a control named Back goes back.
- {op:"update", id, props:{...}} changes fields; "font" merges; null removes.
- {op:"delete", id} / {op:"move", id, parent?, index?} (move keeps the node
  where it is on the page).
- {op:"make_component", id} turns a frame or group into a component; place
  copies with an "instance" node {type:"instance", component_id}.
- {op:"add_page", name, background} / update_page / delete_page.
- Comments: the person pins comments in the app; read lists the open ones
  with their ids. {op:"reply_comment", id, text} answers, {op:"resolve_comment",
  id} closes one after you fixed it, {op:"add_comment", text, node_id | x, y}
  leaves your own note, {op:"delete_comment", id}. Check comments when asked
  to review or refine a design, and resolve what you handled.
- Every apply keeps the state before it in the file's history, so the
  person can go back to it from the app's History panel.
- Styles and variables are shared looks, kept in the file, not on a page:
  {op:"set_style", kind:"fill", id?, name, props:{fills:[...]}},
  kind "effect" with props:{effects:[...]}, kind "text" with
  props:{font:{family,size,weight}, line_height, letter_spacing, align}.
  Nodes use them with update props {fill_style:id}, {text_style:id},
  {effect_style:id}. Change the style and every node using it follows.
  {op:"delete_style", kind, id}.
- Colour variables with modes (light/dark): {op:"set_modes", modes:["Light",
  "Dark"], mode:"Light"}, then {op:"set_variable", id?, name,
  props:{values:{"Light":"#FFFFFF","Dark":"#111111"}}}. A fill or stroke
  follows one with {type:"solid", color:"#FFFFFF", variable:id}; set_modes
  with mode switches the whole design. {op:"delete_variable", id}.
- Variants: give each component of a set the same "set" name and a
  "variant" like {state:"hover"}; an instance shows its own component, or
  the one of the set matching its "variant". An instance's "overrides"
  {layerId:{text, fills, visible, font:{size}, ...}} change layers of the
  component for that instance only and survive edits to the component.

Node fields (all optional except type):
- id: your own short id (letters, digits, - _ :), unique in the file, so
  later ops can refer to it. Omitted ids are generated.
- type: frame, group, rect, ellipse, line, polygon, star, path, text, image,
  component, instance.
- name; x, y (relative to the parent's top left), width, height;
  rotation (degrees, clockwise); opacity 0-1; visible; locked.
- fills: [{type:"solid", color:"#RRGGBB" or "#RRGGBBAA"} or
  {type:"linear"|"radial", stops:[{offset:0-1, color}], angle:degrees}].
  A frame defaults to white, shapes to grey; give fills:[] for none.
- strokes: [{color, width, align:"inside"|"center"|"outside", dash:[4,4]}].
- effects: [{type:"drop-shadow"|"inner-shadow", x, y, blur, spread, color}
  or {type:"blur", blur}].
- radius: a number or [top-left, top-right, bottom-right, bottom-left].
- group: bool_op "union"|"subtract"|"intersect"|"exclude" combines the
  shapes inside into one; the group's fills paint the result (subtract
  cuts the later shapes out of the first). mask:true on a shape makes it
  clip the siblings above it in the same frame or group (the mask is not
  drawn). Fills can be {type:"image", src, fit:"cover"|"contain"|"stretch"}.
- Prototype: interactions:[{trigger:"click"|"hover"|"press"|"drag"|"after"|
  "key", action:"navigate"|"overlay"|"back"|"close"|"url"|"scroll_to",
  to:<top-level frame id>, transition:"dissolve"|"instant"|"smart"|"slide"|
  "push"|"move", direction, duration (ms), easing, delay (after), key (key),
  position (overlay), url}] on any layer; flow_start:true on the frame a
  presentation begins at; scroll:"vertical"|"horizontal"|"both" on a frame
  whose content is taller or wider than its box. The person plays it with
  Present in the app.
- frame/component: children, clip (default true), layout.
- layout (auto layout, frames and components): {dir:"row"|"column",
  gap, pad (a number, [vertical, horizontal] or [top, right, bottom, left]),
  main:"start"|"center"|"end"|"between" (along dir),
  cross:"start"|"center"|"end"|"stretch"}. The frame stacks its visible
  children and ignores their x and y. On a child: sizing_x/sizing_y "fill"
  takes an equal share of the room left ("fixed" is the default); on the
  frame itself "hug" fits the content. absolute:true keeps a child out of
  the flow at its own x and y (badges, close buttons). Text with
  sizing_x "fill" wraps at the frame's width.
- constraints {x:"left"|"right"|"center"|"stretch"|"scale",
  y:"top"|"bottom"|"center"|"stretch"|"scale"} on a child of a frame
  without layout say how it follows when that frame is resized by an
  update (default left/top).
- text: text ("\n" breaks lines), font:{family, size, weight 100-900,
  style:"italic"}, line_height (px), letter_spacing (px), align
  (left|center|right), valign (top|middle|bottom), auto_resize: "width"
  (one line per paragraph, box follows text; default), "height" (wraps at
  width, set width), "none" (fixed box). decoration underline|strike,
  transform upper|lower. Text colour is its fill.
- image: src (a project-relative image path or a data: URL), fit
  (cover|contain|stretch).
- path: d (SVG path data) drawn in view_box [w, h], scaled to the node.
- polygon/star: sides; star inner (0-1).

How to design well:
- Start each screen as a top-level frame at a real size: desktop 1440x900
  (or 1440 wide, taller for a full landing page), tablet 834x1194, phone
  390x844; logos on a 512x512 or 1024x1024 frame. Place screens side by side
  with 80-120px gaps.
- Settle the system first and keep to it: a kit theme gives 2 font
  families, a type scale, a palette with checked contrast and shadows. When
  the project has its own tokens (design_system tool), set the closest theme
  and then set_variable the c-* tokens to the project's colours.
- Avoid the looks that mark generated UI: purple-to-blue gradients,
  everything centred, three identical cards in every section, glowing
  shadows, emoji, vague copy ("Unlock your potential"). Prefer one accent
  used sparingly, left-aligned reading text, varied section rhythm, and
  specific, concrete words.
- Align to a grid: consistent margins (e.g. 120px desktop, 16-24px phone),
  equal gaps, shared left edges. Round coordinates to whole pixels.
- Prefer auto layout for anything that stacks (nav bars, cards, lists,
  forms, buttons with padding): set layout on the frame and let sizing
  decide sizes instead of computing coordinates. Resize a screen later by
  updating its width and the content follows.
- Build repeated parts (buttons, cards, nav items) once as components and
  place instances. Put the palette and type scale in styles/variables first
  and have nodes point at them, so a rebrand or dark mode is one op.
- Give everything clear names; group related layers in frames or groups.
- Work in batches: one apply per section (header, hero, features…) so the
  person sees the design grow, then read or export and fix what is off.
- Text boxes are measured by estimate until the app re-measures them; for
  paragraphs use auto_resize "height" with an explicit width.

## Code to design

To turn existing UI code into a design, find the UI files first (view, grep),
then pick the way that fits the language:

- Web (HTML, React/JSX/TSX, Vue, Svelte, Angular, Astro, Next.js): run the
  app, read the page's rendered HTML (browser tool, or the built HTML file)
  and call action "import" with source or path. Flexbox becomes auto layout,
  <style> rules, inline styles and CSS variables are resolved. This keeps the
  real result of the framework's logic; do not try to parse JSX yourself.
- Everything else: read the component source and build the design with ops
  by this mapping. Take sizes, colors, spacing and text from the code; use
  the theme or asset files for tokens and put them in styles/variables.
  - SwiftUI: VStack/HStack/ZStack → frame with layout column/row (ZStack:
    children absolute); spacing → gap; .padding → pad; Spacer → fill-sized
    empty frame or main "between"; .frame(width:height:) → fixed size,
    .frame(maxWidth: .infinity) → sizing_x fill; .background/.foregroundColor
    → fills; .cornerRadius → radius; .shadow → drop-shadow; Text + .font →
    text node; Image → image; Button → frame with text.
  - Jetpack Compose: Column/Row/Box → frame column/row/absolute;
    Arrangement.spacedBy → gap; Modifier.padding → pad; fillMaxWidth() →
    sizing_x fill; size/width/height → fixed; background, clip(RoundedCornerShape)
    → fills, radius; Text(fontSize, fontWeight, color) → text node.
  - Flutter/Dart: Column/Row/Stack → frame column/row/absolute; SizedBox
    height/width → gap or fixed size; Padding/EdgeInsets → pad; Expanded →
    fill; Container(decoration: BoxDecoration(color, borderRadius, boxShadow,
    border)) → fills, radius, effects, strokes; Text(style: TextStyle) → text.
  - React Native: View is a column frame (flexDirection row flips it), the
    StyleSheet values map like CSS; Text → text node; Image → image.
  - Android XML: LinearLayout orientation → layout dir; layout_width
    match_parent → fill, wrap_content → hug; padding, background, TextView
    textSize/textColor → pad, fills, text.
  - XAML (WPF/MAUI/WinUI/Avalonia): StackPanel/Grid/Border → frame; Margin
    and Padding → pad; Orientation → dir; CornerRadius, Background → radius,
    fills; TextBlock FontSize/FontWeight/Foreground → text.
  - Qt QML: Column/Row/Item → frame; spacing → gap; anchors.fill → fill;
    Rectangle color/radius → fills/radius; Text font.pixelSize/color → text.
  - GTK/Glade, Compose Multiplatform, Kotlin/JS, Unity UI Toolkit (UXML/USS),
    Qt widgets/.ui, Tk, Swing: the same idea: containers become frames with
    the container's direction, spacing and padding; leaf widgets become text,
    image or rect nodes; styles map to fills, strokes, radius and effects.
- UI tests and stories (Storybook, Playwright, XCUITest, Compose previews)
  show which states exist: make one frame per state, or a component with
  variants.
- Say what you could not map (animations, platform-specific controls, custom
  drawing) instead of guessing, and keep names from the code so the design and
  the code can be matched back.

## Working as one of several agents (a swarm)

When a prompt gives you a page of your own ("Variant B"), you are one of
several agents designing the same brief in parallel:

- Pass `page` on every call and never touch another page; comments, styles
  and variables are shared, so add new ones instead of changing existing ones,
  and never call set_theme or set_modes: the lead sets the theme for everyone.
- Others save to the same file while you work: apply redoes your ops on the
  latest version by itself, so you do not need to read again before each
  edit. Use your own short ids, prefixed with your variant ("b-hero").
- Follow the direction you were given; being clearly different from the
  other variants is the point. Finish by exporting your page and checking it.

The lead agent starts the swarm with the agent tool (run_in_background, one
per page, agent_name when each should run on its own model), reads the
results with agent_result, compares the exported pages and reports which is
strongest. It never deletes a variant; the person chooses.

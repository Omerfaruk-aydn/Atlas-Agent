Design on the Atlas design canvas: logos, websites, app screens, any visual.
Files live in the project at .atlas/design/files/<file>.json and the person
watches every change live in the app's Design tab, where they can edit too.

Actions:
- list: the design files.
- create: a new file (file, or title to name it), optionally with ops.
- read: an outline of the file, one line per node with id, box, fills,
  text. With node_id, that node's full JSON.
- apply: a batch of ops, applied in order, all or none.
- code: writes HTML+CSS (format "html", default) or a React component+CSS
  (format "react") for node_id (a frame) to .atlas/design/exports/; auto
  layout becomes flexbox, other layers are absolutely positioned.
- export: writes an SVG to .atlas/design/exports/ (node_id for one node).
  Look at it with the browser tool (file:/// URL, screenshot) to check your
  work; image_compare compares it with a reference.

Ops:
- {op:"add", parent?, index?, node:{...}} creates a node with its children.
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
- Settle a small system first and keep to it: 1-2 font families (Inter is
  bundled), a type scale (e.g. 12/14/16/20/24/32/48/64), an 8px spacing
  grid, a limited palette (one brand colour, neutrals, one accent) with
  readable contrast (4.5:1 for body text). Reuse the design_system tool's
  tokens when the project has them.
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

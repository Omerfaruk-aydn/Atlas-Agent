package tools

// studioGuide tells the model how to work the design editor by hand. It is
// returned with the studio's address, so it is read when it is needed.
const studioGuide = `DESIGN IN PHASES. This is the Atlas design editor, a Figma-class tool:
an infinite canvas with frames, shapes, text, auto layout, components,
styles, variables, vectors and prototyping. Work like a senior product
designer: the repetitive, exact part as a design script, the judgement by
hand in the editor. The person watches the same file live in the app's
Design tab.

PHASES (design_canvas action progress says which one you are in, what is
missing and the call budget, read from the files: use it instead of a
screenshot to decide what is next)
1. Brief: .atlas/design/briefs/<file>.md (skills design-taste, design-brief),
   with a References section: two real products and what to take from each
   (design_canvas references with file and query shows their screens).
   Choose screen structures from atlas://skills/design-patterns/SKILL.md.
2. System: start from the ready kit (atlas://skills/design-kit/SKILL.md),
   or write one design_canvas action script (atlas://skills/design-script):
   colour tokens with light and dark, 5-7 text styles, space, radius,
   shadow. One call, not forty clicks.
3. Components: in the same script, every component the screens need on
   auto layout with its states as variants, placed with library(). The
   script is checked before it is saved: a component without auto layout,
   unstyled text in a component or a missing variant stops it.
4. Screens: compose from instances. Lay the skeleton with screen() in a
   script when it saves time, then finish by hand in the studio: hierarchy,
   the one visual idea, spacing by eye. Layers a script made belong to it
   (ids start with "<script>:"); a root you edit by hand is kept on the
   next run.
   Every screen draws one thing of the subject (map, chart, pattern, path,
   photo: the Material section of design-kit); lint warns no-material.
5. Review: at least two rounds (atlas://skills/design-director/SKILL.md): render,
   a hostile art-director subagent lists measured flaws, fix, render again,
   logged in .atlas/design/reviews/<file>.md. One round is against shipped
   work: design_canvas compare, gaps under "Reference: <apps>". Then lint
   to 90+.
Finish one phase before the next. When a phase's budget is spent, stop and
report what is left instead of starting over.

BY HAND IN THE STUDIO
Use the browser tool's mouse and keyboard. Do not change the design with
design_canvas apply while the studio is open (it bypasses the editor);
action script is the one exception and the page reloads it.
Batch: a selection plus several field edits plus one screenshot is ONE
tool_pipeline call {"browser":{"steps":[...]}}: click the layer row,
set_field X, Y, Width, Height, Radius, fill hex in turn, then screenshot.
Take screenshots at milestones (end of a section), not after every field.

OPEN
- browser navigate to the address above. Take a screenshot for its
  viewport_id; pointer_click / pointer_drag take x/y in that image's pixels.
  Take a new screenshot whenever the view moved (zoom, pan, panel opened).
- Layout: toolbar on top (file picker, tools, Library, Swarm, Present,
  undo/redo, zoom), Layers / Assets / Comments on the left, the properties
  inspector on the right, the canvas in the middle.

TOOLS (keyboard, with focus on the canvas; or the toolbar buttons)
  V move/select   H hand   F or A frame   R rectangle   O ellipse   L line
  T text   P pen   Shift+P pencil   C comment   Shift+I library
  Ctrl+Shift+K place an image file.
- Shapes and frames: press the tool key, pointer_drag from corner to corner.
  Drawing inside a frame puts the layer in that frame. Shift keeps the ratio.
- Text: T, pointer_click where the text starts, insert_text, key Escape.
  Double-click a text layer (or select it and press Enter) to edit it again.
- Pen: P, click points (drag for curves), Enter to finish. Enter on a
  selected path edits its points.

SELECT AND ARRANGE
- Rename: double-click the layer's row in Layers (or click the row, F2),
  type the name, Enter.
- Click a layer on the canvas or its row in Layers. Shift+click adds,
  drag on empty canvas selects by marquee. Escape selects the parent.
  Enter on a frame selects its children.
- Move by drag or arrows (Shift = 10 px). Resize by the eight handles,
  rotate just outside a corner.
- Ctrl+D duplicate, Ctrl+G group, Ctrl+Shift+G ungroup, Ctrl+Alt+G frame
  the selection, Ctrl+Alt+K make component, Ctrl+Alt+B detach instance,
  Ctrl+] / Ctrl+[ order, Shift+H hide, Ctrl+Shift+L lock, Ctrl+E flatten,
  Delete remove, Ctrl+Z / Ctrl+Shift+Z undo and redo.
- Shift+A adds auto layout to the selected frames (direction, gap and
  padding read from where the children already are), or wraps other layers
  in a new auto-layout frame. Right-click on the canvas opens the same
  commands as a menu.
- Hold Alt with a layer selected and hover another (or empty space inside
  the parent frame): red lines show the distances in px. Use it to check
  spacing instead of guessing.
- Rulers: Shift+R shows them; drag out of a ruler to pull a guide (drag it
  back onto the ruler to remove it). Moves and draws snap to guides.
- Section (Shift+S) groups frames into a labelled board; drop a top-level
  frame into it. Slice (S) marks a region: select it and export to get the
  page as it looks inside that box. Eyedropper (I): click a layer to copy its
  colour onto the selection (Alt: the stroke), or to the clipboard.
- Images: double-click (or Enter) to crop: drag moves the picture, Alt+wheel
  zooms it; the inspector has Zoom, x, y and Reset crop. Gradients: Solid /
  Linear / Radial / Angular, with + to add a colour stop.
- View: Shift+1 fit everything, Shift+2 zoom to selection, Ctrl+0 100%,
  Ctrl+= / Ctrl+- zoom, Space+drag or H to pan.

EXACT VALUES (the inspector, right side)
- Select the layer first, then set_field the inspector's fields by their
  label (X, Y, Width, Height, Rotation, Radius, Opacity, fill hex, font
  family, weight, size, line height, letter spacing, align...). Fields
  accept arithmetic (120+16). Read the labels from a snapshot; do not guess.
- Auto layout: select a frame, Shift+A or the inspector's Auto layout
  section's +, then set direction, gap, padding, alignment and each child's
  hug / fill / fixed sizing. Use it for every list, card, bar and button,
  like a professional file: nothing positioned by eye that a stack can
  hold.
- Layout grid: the inspector's Layout grid + on a frame adds columns
  (count, gutter, margin, alignment), rows or a square grid, drawn over
  the frame and never exported; Ctrl+Shift+4 shows or hides grids. Put a
  4-column grid (16 margin, 16 gutter) on a phone frame and a 12-column grid
  on a desktop one, and align to it.
- Fills, strokes, effects (shadow, blur), blend, constraints (on a child),
  text and colour styles, variables and modes, component properties and
  variants, prototype links and the code panel are all in the inspector.
- Library (Shift+I): the kit's themes, blocks and 2,000 icons, applied as
  one undo step. Placing a block is allowed; then you finish it by hand.

DESIGN SKILLS (read them with view; each is short)
- Before drawing: atlas://skills/design-taste/SKILL.md (the user's likes and
  rejections in .atlas/design/taste.md), then
  atlas://skills/design-brief/SKILL.md, and write the brief.
- Deciding the system: atlas://skills/type-and-color/SKILL.md, then build it
  with atlas://skills/design-script/SKILL.md.
- Platform rules: atlas://skills/mobile-app-design/SKILL.md,
  atlas://skills/web-landing-design/SKILL.md or
  atlas://skills/dashboard-design/SKILL.md.
- After each screen: atlas://skills/design-critique/SKILL.md (score the
  exported picture, fix, look again) and atlas://skills/anti-slop/SKILL.md.
- Before reporting: atlas://skills/design-states/SKILL.md.
- When the user reacts to a design, add it to .atlas/design/taste.md.

HOW A SENIOR DESIGNER WORKS
1. Set the stage: one frame per screen at the device size (iPhone 393x852,
   desktop 1440x1024), named ("01 Home"). Set the page background.
2. Decide the system before drawing: a type scale (e.g. 34/22/17/15/13), an
   8-pt spacing rhythm, 2-3 colours plus neutrals, one radius scale. Build
   them with the design script, and use only them.
3. Block in the layout with auto-layout frames, then fill in real content:
   real words from the subject, no lorem ipsum, no placeholder names.
4. Name every layer as you go; group by meaning, not by accident.
5. After each section: screenshot, compare against the plan, fix alignment,
   spacing and hierarchy before moving on. Zoom in to check details.
6. Components for anything used twice (buttons, rows, tab bar); instances
   everywhere else.
7. Finish with states and variants if the brief needs them, and prototype
   links between frames when it is a flow.

VERIFY
- If you keep a browser task_plan, state what the design must contain as requirements with
  verify "design" (subject: the design file name, expect: terms joined by ";",
  "Section > Frame" meaning a layer inside another, "@components" or
  "@review" meaning that phase is done). They close by themselves when the
  saved file shows the terms, so do not chase page evidence for them.
- The Layers panel shows the tree you meant; a screenshot at 100% shows the
  result. "Saved" in the toolbar means the file is written.
- design_canvas read / render on the same file show what you made; lint
  checks contrast, overflow, spacing and touch targets. Fix by hand.`

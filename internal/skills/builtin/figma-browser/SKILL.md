---
name: figma-browser
description: Build and edit Figma designs through the browser tool with observed targets, committed property values and verified results.
compatibility: Atlas browser tool (observe, set_field, tool_pipeline.browser, viewport screenshots). Figma web editor in a signed-in browser profile.
metadata:
  provenance: atlas-authored
  verified-against: local design-panel fixture (commit/readback, remounted inputs, scoped controls) and the live Figma web editor on 2026-10-09; live Figma behavior must be confirmed on screen every time
---

# figma-browser

Use this guide when the user asks for work inside Figma's web editor through
browser use. It describes how the editor usually behaves. It never replaces the
current observation: if a control is named, placed or behaves differently on
screen, follow the screen and say so. Do not move the work to the Figma REST API,
a plugin, an import file or the clipboard and count it as done. Do not hand the
design to Figma's own AI agent or chat ("Agents", "Prompt for AI agent", "Make
designs") unless the user asked for it: its output is not your verified work,
and it can rearrange layers you already built. Collapse that panel if it covers
the canvas.

## Before building

1. Open the file the user named, or create one only if they asked. An opened or
   new file is not progress on the design itself.
2. Call `task_plan` with one requirement per requested part: frame size, each
   section or component, text content, colors, layer names, Auto Layout,
   alignment, saving. Mark visual requirements `verify: visual`, values such as
   size, names and colors `verify: structural`.
   Start a separate user goal with a new `task_id` and `mode: new`; use
   `mode: extend` with the active task_id only for additions or revisions to
   that same goal. Revised requirements need fresh verification.
3. Take one viewport `screenshot`. Its viewport_id stays valid until the canvas
   scrolls or zooms, so several canvas inputs can reuse it.

## Product design sequence

For composed interfaces, load the UI/UX guidance and establish the audience,
primary flow, real product capabilities, density and target devices. Record a
brief, semantic palette, typography roles and a spacing scale before drawing.
Routine design choices are already authorized by the user's design request.

Build one representative screen completely before duplicating it. Critique
its hierarchy, real content, alignment, text wrapping and touch affordances.
Fix the foundation first; duplicating a broken screen multiplies defects.
Create real shared components and variants, then use instances in the other
screens. Layer names alone do not prove a component, variant or Auto Layout.
Read the selected object's properties and use structural `expect` markers in
the task contract for those claims. Use distinct requirements for each screen,
shared component states, overflow, contrast and exercised prototype flow.

Canvas dispatch does not prove delivery. At each coherent milestone, verify
and close the matching requirement with fresh evidence. Selecting layers,
zooming, clicking a toolbar or taking screenshots is not design progress.
Continue the authorized open requirements after summaries and model changes;
do not ask again whether to finish an already requested design.

For mobile screens, check long localized content, safe areas and the composer
with the keyboard open. A solid keyboard placeholder is a geometry fixture,
not a finished keyboard design. Distinguish a visual keyboard representation
from a working keyboard in a running application.

Measure text contrast from the actual foreground/background colors, including
opacity when relevant. Target 4.5:1 for ordinary text and 3:1 for qualifying
large text. Report the colors, ratio and text role; visual intuition is not a
measurement. Inspect every requested screen and narrow variant for clipping
and overlaps, including children in nested layouts. Intentional scroll content
may exceed a frame; validate its clipping and reachable scroll behavior.

Play each required transition in presentation mode and confirm the destination
and return path. An Interactions row proves a configured link, not a working
end-to-end prototype. Include loading, disabled and failure states when they
belong to the real flow. Capture clean final screenshots as well as structural
readbacks; overlays and ref labels are tool feedback, not design content.

Finish with the file/page link, the verified screens, component/state inventory,
measurements, played prototype paths and any genuinely unverified criteria.
Do not use aesthetic adjectives or a successful screenshot as a completion gate.

## Creating content

- Tools: the toolbar buttons have accessible names (Frame, Rectangle, Text, ...).
  Prefer clicking the observed button by ref or `semantic_click` with its name.
  Single-letter shortcuts (for example `f` frame, `r` rectangle, `t` text) work
  only while focus is on the canvas. After text editing Figma's hidden text
  editor can keep focus, and a letter key would be typed into the last text
  layer; Atlas refuses that key (`key_would_type`). Use the toolbar buttons.
- Place or draw with `pointer_click` / `pointer_drag` from the current
  screenshot. Shift while dragging constrains proportions; shift-click adds to a
  selection.
- Text: click the Text toolbar button, `pointer_click` an empty canvas point,
  `insert_text`, then `key` escape. The click opens a text-edit session for the
  editor it focused; escape, other clicks and navigation end it, and
  insert_text without an open session is refused (`text_edit_not_open`), so text
  cannot land in the previous layer. To edit existing text, double-click it
  (`click_count: 2`) and send advanced.mode `append`; without it insert_text is
  refused (`text_editor_not_empty`). Check `canvas_text.editor_text_after` and
  the layer list after each text.
- Panel scopes: `within` takes the panel label (`Right sidebar`) or the context
  string that results print (`region "Right sidebar" > group "Auto layout"`).
  Padding and gap inputs are named by their wrapping label ("Left padding"), so
  `label` or `name` both resolve them.
- Group these as one `tool_pipeline.browser` workflow with the screenshot's
  viewport bound to each pointer step, instead of one model turn per key.

## Properties (size, radius, color, text size)

- Use `set_field` on the right-panel field, scoped with `within` to the panel
  section ("Layout", "Appearance", "Fill", "Typography" or whatever the
  observation shows). Unnamed fields show their nearby label as `label?`.
- Trust only `accepted_value`. Figma inputs commit on Enter or blur and often
  re-render after the commit; `typed_value` before commit is not the design's
  value. `adjusted` means Figma clamped or rounded it.
- Panel fields are reused when the selection changes. Re-resolve fields after
  selecting a different layer; a change list that shows the same ref with a
  different label means the field now belongs to another property.
- Size fields change name with their mode: "Horizontal resizing" / "Vertical
  resizing" while the layer hugs or fills, "Width" / "Height" once a number
  fixes it. A set_field that fixes the size can return `outcome_unknown` with
  `relabeled_as`; read the named field instead of typing again.
- Hug / Fill / Fixed: clicking the size field only focuses its number (arrow
  keys then change the number). Use `select_option` with the mode: it opens a unique visible button or
  chevron in the field's immediate wrapper. If the opener is missing or
  ambiguous, inspect it and use `pointer_click` from a fresh screenshot, then
  select the option. Never repeat clicks in the number field to guess the menu. Figma keeps the number and shows the mode beside it; that is
  the readback (`acceptance: shown_mode`).
- Fields named with their value ("Font size: 12 px") are renamed by the commit;
  `renamed_readback` reads the renamed field of the same selection.
- Absolute position inside an Auto Layout frame: set the "Ignore auto layout"
  checkbox, then X-position / Y-position.
- After several edits, confirm the selected layer's values with `read_field`
  and the look with one screenshot.
- `settle_ms` (default 300 ms) is the full readback window. If an asynchronous
  edit is rejected or remains unstable, inspect its accepted_value and status;
  an unknown result is not permission to repeat the same edit blindly.
- `undo` checks the selected object's identity, not just its layer name and
  panel values. Two "Rectangle" layers can be different objects. A changed
  selection, remounted identity or another document may return `undo_unsafe`;
  return to the actual edited object and inspect it before retrying.
- A `fill_form` pass pins its documents before writing. If the editor navigates
  or a frame disappears, later fields report `not_attempted`; earlier edits
  remain possible. Successful or uncertain field edits invalidate earlier
  evidence even when another field fails. Re-read requirements marked reverify
  and attach fresh evidence op_ids rather than repeating the entire pass.
- Scoped `observe` and `find` results provide usable refs for the current tab
  and document. Resolve them again after navigation, reconnection or restart.

## Layers, Auto Layout and alignment

- Before repeating canvas gestures, create one item and read its actual parent,
  X/Y, W/H and layout mode. Verify the second item before batching the rest.
  Screen coordinates and frame-local coordinates are different, and Auto Layout
  can override the positions sent by a drag. If the first items overlap or land
  outside their intended parent, stop the batch and repair the structure.
  Prefer verified duplication and layout properties for repeated keys, rows or
  cards. A batch returning `dispatch: sent` proves input delivery, not placement.
- Rename the selected layer with ctrl+r (or double-click its row), `insert_text`,
  Enter; verify the row name with `find` role row (`inline_rename`).
- ctrl+alt+g wraps the selection in a frame, ctrl+d duplicates, ctrl+alt+k makes
  a component; a double-click on canvas selects inside a group or instance.
- Auto Layout is applied to a selected frame from the right panel control or the
  shortcut shown in its tooltip (commonly shift+a); verify by the Auto Layout
  section appearing in the panel, then set gap and padding with `set_field`.
- Align with the observed alignment buttons in the panel.

## Prototype links

Open the Prototype tab, select the source element, drag the circular node that
appears on it to the destination frame (`pointer_drag` from one screenshot).
The Interactions section then lists "Click → <frame>"; that row proves
configuration. Open presentation mode, exercise the link and the return path,
and confirm the actual destination before calling the prototype tested.

## Design acceptance

Agree measurable criteria in `task_plan` and attach evidence of the matching
kind:
- Structural (field readback): frame sizes (for example 390×844 and 360×800),
  padding and gap, Auto Layout present, components and instances used, layer
  names, touch targets at least 44×44 (read W/H of each control), color styles
  applied.
- Overflow: check child geometry and visible layout on every screen; a
  narrower variant needs its own check. Intentional scroll regions may exceed
  the viewport, but must clip correctly and remain reachable in the prototype.
- Visual (screenshot): hierarchy, alignment and wrapping of long text. Measure
  contrast separately from observed colors, including opacity; a screenshot
  alone does not prove a ratio. Record unmeasured contrast as unverified.
- Live (prototype): each link played in presentation view, confirming its
  destination and return path; panel configuration alone remains untested.
Name every criterion that was not checked.

## Verification and delivery

- Structural evidence (accepted values, layer names in the Layers tree) goes in
  `task_update` for structural requirements; a final viewport screenshot is the
  evidence for layout and visual quality.
- If the screenshot and the panel disagree (for example a radius that looks
  rounded while the field reads 0), do not report either: select the layer,
  read the field again and inspect the canvas once more.
- Figma saves automatically; confirm with the observed save status before
  saying it is saved. Report the file URL, what each requirement's evidence was,
  the real counts from `status`, and anything not verified.

## Plan limits and blockers

A "Need more files?" or upgrade dialog is an account limit
(`account_or_plan_limit`). Close it, tell the user, and continue with an existing
file already authorized by the user. If a new page is unavailable, use an
isolated section or copied frames in that authorized test area when this
preserves the existing designs. Revise the plan and verify the alternative;
an account limit on one route does not block every safe route. Never pay,
upgrade or delete files without explicit instruction.

## Delivery and uncertain recovery

Use structural/visual evidence for the design itself. Put the final file link and report under a separate `verify: "delivery"` requirement with expected report text; the saved final assistant message verifies that delivery, not a panel read or model note. Historical reference research can use source-scoped observation requirements, but never grants live canvas input authority.

If input may already have reached Figma, an `if_failed` pipeline may inspect the result with read-only calls. It does not clear uncertainty or permit downstream mutations by itself. Reconcile that exact operation before retrying; do not duplicate frames, layers or text merely because a later call succeeded.

---
name: design-critique
description: Judge a finished or in-progress screen the way a senior design reviewer does, from the rendered picture, against the brief, with a fixed rubric and scored findings, then fix and re-render until it passes. Load after each screen in the Atlas design editor or Figma, before reporting a design as done, and when asked to review or improve a design.
metadata:
  provenance: atlas-authored
---

# Design critique

A design is judged from what it looks like, not from what was intended. Look
at the exported picture at real size, compare it with the brief, score it,
fix the worst problem, and look again.

## Look at the right thing

1. Export or render the screen (`design_canvas render` or `export` on the
   frame) and view the PNG. An editor screenshot with selection handles,
   rulers and panels is not the picture the user sees.
2. Look at it three ways:
   - **At 100%**, for detail: alignment, text, edges, icons.
   - **Small** (about a quarter size, or squint): only the hierarchy is left.
     The first thing you see must be the brief's first read.
   - **In greyscale** (or imagine it): contrast and hierarchy without colour.
3. Re-read the brief (`.atlas/design/briefs/<file>.md`) and the taste notes
   before scoring. Without the brief, a critique is only taste.

## Rubric (score each 1-5, write one line of evidence for each)

1. **Purpose.** The one job is obvious within 3 seconds. The primary action
   is the most prominent interactive element. Nothing competes with it.
2. **Hierarchy.** Exactly one first read. Size, weight, colour and position
   agree about what matters; there are no two equally loud things.
3. **Typography.** Only scale sizes are used; line lengths 45-75 characters
   for body text; line height about 1.4-1.5 for body, 1.1-1.25 for display;
   no orphans in headings; numbers that are compared are tabular.
4. **Spacing and rhythm.** Every gap is a spacing step. Related things are
   closer to each other than to unrelated things (proximity tells the
   grouping). Edges line up along a few shared lines.
5. **Colour and contrast.** Body text at least 4.5:1, large text and icons at
   least 3:1. Colour is used for meaning and emphasis, at most one accent
   competing for attention per view.
6. **Content.** Real, specific, consistent strings; no placeholders; labels
   in the user's words; numbers formatted the same way everywhere.
7. **Craft.** Pixel alignment, consistent icon weight and size, no
   accidental clipping, optical centring of icons in buttons, consistent
   radius and stroke widths.
8. **Distinctiveness.** The brief's one visual idea is visible and carried
   through; it does not look like a template. Run the `anti-slop` list.
9. **Completeness.** Required states exist (see `design-states`); the tab bar,
   header and other shared parts match across screens.

A score of 3 means acceptable, 4 good, 5 would survive a design review at a
strong product company. Do not give 5 out of politeness.

## Findings

Write each finding as: **where** (layer name or region), **what is wrong**
(observable), **why it matters** (which rubric line, which user moment),
**fix** (a concrete change with values). "Header feels heavy" is not a
finding; "Header title 34/700 and the countdown 34/700 compete: drop the title
to 22/600 so the countdown is the first read" is.

Order findings by impact: broken purpose and hierarchy first, then contrast
and content, then craft, then polish.

## Loop

1. Fix the top three findings by hand in the editor.
2. Re-export, look again, re-score only the lines you touched plus hierarchy.
3. Stop when every line is at least 4 and the anti-slop list is clean, or
   after three rounds; then report the remaining findings honestly instead
   of claiming the design is finished.

Run `design_canvas lint` as well: it catches contrast, overflow, spacing and
touch-target errors the eye misses. Lint clean is required, not sufficient.

## Report

End with the scores, the three biggest changes made, and what is still
weak. A design report that lists no weaknesses is not believable.

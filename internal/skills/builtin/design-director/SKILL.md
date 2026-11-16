---
name: design-director
description: How a senior product designer with ten years of experience actually works on a screen - explore before committing, build the one big idea, then iterate in rounds with a hostile art director's eye until the details are right. Load before drawing any screen and again for every review round; it defines the review log the design phases check.
metadata:
  provenance: atlas-authored
---

# Design director

A clean file is not a good design. Lint, the gate and the rubric only prove
nothing is broken. A senior designer's screens look chosen: one idea carries
them, every detail agrees with it, and nothing is there by default. The
difference comes from process, not talent: explore, commit, then iterate in
rounds against an honest critic.

## 1. Explore before you commit

Before building, write three directions into the brief's `Direction`
section, each different in the idea and the layout model, not in colour:

- A: the expected answer for this kind of screen (`design-patterns`).
- B: the answer built around the brief's one visual idea, pushed further
  than is comfortable (a huge number, a timetable grid, an unusual crop).
- C: a different layout model (timeline instead of cards, one focus instead
  of a list, type-only instead of a container).

Pick one against the brief's first, second and third read and write two
lines on why. B or C usually wins; A wins only when the product is a utility
people use a hundred times a day. A design that could be any app's is A
with the colour changed: reject it.

## 2. Build the big thing first

Squint test: blur the render. You must see exactly one dominant shape, then
a second, then a third, in the order the brief ranks them. Make the first
read bigger than feels safe (two to three times the next size), give it room
(whitespace around it is part of its size), and take emphasis away from
everything else. Two things shouting is no hierarchy.

## 3. The craft pass (every screen, before the first critique)

- **Rhythm.** Spacing comes from the scale and repeats: the gap between
  groups is a multiple of the gap inside groups (inside 8, between 24).
  Equal gaps everywhere read as a table, not a design.
- **Type.** At most two faces, three or four sizes on one screen with real
  contrast between them (ratio 1.5 or more). Large type gets tighter
  tracking and line height (about 1.1); small caps and overlines get looser
  tracking. Numbers that change or compare are tabular. No more than 45-75
  characters per line. Never bold and large and coloured together.
- **Alignment.** One left edge for text; icons and images align to the
  text's cap height or optical centre, not their box. Check with Alt-distances.
- **Colour.** Neutrals do the work; one accent, used only for the action or
  the one thing that matters. Tint neutrals toward the brand hue; pure grey
  and pure black are for lint-clean, not for design.
- **Shape.** One radius family, nested radii (inner = outer - padding).
  Containers earn their existence; remove a card when spacing and a hairline
  would do. Fewer boxes is almost always better.
- **Imagery and detail.** At least one thing that is specific to this
  subject and not a component: a real map crop, a ticket stub, a photo, a
  data mark, an illustrated empty state. A screen of only rectangles and
  text is a wireframe (lint: `no-material`). The script draws them:
  `map()`, `chart("spark" | "bars" | "ring")`, `pattern("topo" | "dots" |
  "perforation" | "stars" ...)`, `sprig()`, `path()`, a real photo from
  design_canvas `photo`, or an illustration from design_canvas `image`
  (one style sentence for every picture of the file), placed with
  `image()` or an image fill (design-kit, sections Rich home and
  Material). Make it carry the idea: the map shows where the next room is,
  the ring is the time left.
- **Start from a proven style.** The design-styles skill has ten whole
  screens (public service, kids, sport, ticket, food, travel, wellness,
  fintech, editorial, player) as scripts, and design-styles-plus seventeen
  more (meal plan, calendar, reader, brutalist, boutique, hardware
  recorder, focus timer, soft-UI thermostat, stays, hi-fi player,
  breathwork, EV map, trading terminal, flight tracker, recovery,
  flashcard, issue inbox); when the brief is one of them, start from it and
  change copy, hues and one shape rule.
- **When the brief wants a rich, branded app** (an event, a city, a
  campaign, anything people open for its energy, not a quiet tool): use the
  design-kit's vivid look and its Rich home: a deep brand header with the
  status bar on it, colour tiles each with its own illustration, photos on
  the news, a motif from the place. Quiet is not always right; match the
  references the person shows.
- **Content.** Real words at real lengths: the longest name, a two-line
  title, an empty value, a long translation. Copy is design: cut every word
  that does not help the job.
- **Platform.** A phone screen is an app, not a web page: load `mobile-app-design` and read its first section. Status bar, large title, inset grouped lists, tab bar, home indicator; no full-width hero band. Safe areas, 44 px targets, thumb reach for the primary
  action, system status bar present.

## 4. Review rounds (the log the phase checks)

Run at least two rounds. Round = render, look, list, fix, render again.
Write each into `.atlas/design/reviews/<file>.md`:

```
## Round 1
- Flaw: <what, where, measured> -> Fix: <what changed>
- ... (five or more flaws)
## Round 2
- ...
```

To look honestly, do not grade your own work: start a subagent with
the render PNG and this prompt, and apply what it returns:

> You are a hostile art director with ten years of shipping consumer apps.
> Here is a screen and its brief. Name the five worst things about it,
> specific and measured (element, size, distance, colour), ordered by how
> much they hurt. Say what is generic, what a stranger would not notice is
> missing, and what you would cut. No praise, no scores.

Give the subagent the files, not your opinion of them: the render PNG, the
brief, and in the round against shipped work the compare PNG.

**One round is against shipped work.** Taste is relative: a screen looks
fine until it sits next to one that shipped. Before the first round, call
design_canvas `references` with the file and a query (the kind of app, or
the two products from the brief, separated by `;`). In a round, call
`compare` with the file: it sets your render beside them at the same height
and asks measured questions. Log that round with a line
`Reference: <the apps>` and its gaps as flaws. Take discipline (scale of the
one big thing, density, how little colour, how the subject is drawn), never
their layout, colours or illustrations. The review phase is not done
without this round.

Fix every flaw you can measure, in the script or by hand, then render
again. Round 2 must find different flaws than round 1; if it finds the same
ones, the fix did not work. Stop after round 3, or when a round finds
nothing you would defend in a portfolio review.

**Look at what you see, not what you meant.** The render uses the real
faces; if lint says `font-missing`, you are judging a fallback, so change
the family to one the kit has before judging type.

## 5. Report

Show the render and say: the direction chosen and why, the one idea, what
changed between rounds, and what you would still change with more time.
Never say "pixel perfect" or "professional"; show it.

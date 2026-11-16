---
name: design-brief
description: Decide what a screen or product design is for before drawing anything. Produces a one-page brief (audience, job, feeling, one visual idea, type scale, colours, spacing, radius, what to avoid) that every later step and critique is judged against. Load at the start of any new screen, app, landing page or visual direction in the Atlas design editor or Figma.
metadata:
  provenance: atlas-authored
---

# Design brief

Most weak designs are weak before the first rectangle: nobody decided what the
screen is for, who looks at it, or what should be seen first. Write the brief
first, keep it short, and make every later decision point back to it.

## When

- A new screen, flow, app, landing page, poster or visual direction.
- A redesign that the user asked for in words ("make it better", "more
  premium", "not like this").
- Not for a small fix inside an existing system: follow the existing styles.

## Read before writing

1. The user's request, word for word. Note every fact, name, number and
   constraint it gives. These are content, not decoration.
2. The project's taste notes: load the `design-taste` skill and read
   `.atlas/design/taste.md` if it exists. Rejected ideas listed there are
   off the table.
3. Any existing file, brand, product or screenshots the user pointed at.
   An existing identity is kept unless the user asked for a new one.

## The brief (write it, keep it under one screen)

Save it as `.atlas/design/briefs/<file>.md` (the design file's name) with
these headings, each one to three lines:

1. **Who and where.** The person, the moment and the device. "A delegate
   walking between halls at a conference, one hand, bright daylight, 30
   seconds" says more than "users".
2. **The one job.** What this screen must let them do or know. If there are
   three jobs, rank them; the first one gets the screen's best position and
   largest type.
3. **First, second, third read.** What the eye lands on, in order. The rest
   of the screen supports these three.
4. **Feeling, in three adjectives** that rule things out. "Calm, exact,
   official" rules out playful illustration and neon; "friendly" alone rules
   out nothing.
5. **One visual idea.** A single concrete device the design is remembered by,
   drawn from the subject, not from a trend: a strong typographic number, a
   timetable grid, a ticket-stub shape, a map fragment, a material, a
   colour taken from the place. One idea, used with discipline, beats five.
6. **System.** Pick these now and make them styles or variables before
   drawing (load `type-and-color` for the method):
   - Type: one family (two at most), a scale of 4-6 sizes, e.g. 34/22/17/15/13.
   - Colour: one or two brand colours plus a neutral ramp of 5-7 steps, and
     semantic colours only where the content needs them.
   - Spacing: a 4 or 8 point rhythm, with named steps (4, 8, 12, 16, 24, 32, 48).
   - Radius: one scale of at most three values, tied to element size.
   - Elevation: none, or at most two shadow levels with a reason.
7. **Content inventory.** The real strings, numbers, names, dates and images
   the screen shows. Write them out. No lorem ipsum, no "John Doe", no
   "Lorem Conference". If a fact is unknown, use a plausible role or label
   and mark it in the brief.
   **References.** A heading named `References` with two or more lines: a
   real product that solves the same job well, and what you take from it
   (its structure, a pattern, a rule), never its look. "Citymapper: the
   next step first, in one glance." Look before you write: design_canvas
   `references` with the file and a query (the kind of app, or the two
   names separated by `;`) shows their real store screenshots, kept for the
   review's `compare`. Load `design-patterns` to choose the
   screen structure. The design phase is not done without them.
   **Direction.** A heading named `Direction`: the look (from `design-kit`
   or your own), the display and body fonts, the shape rule (square,
   rounded, pill), how colour is used (one accent on neutrals, two-tone,
   dark-first) and the layout model. Take the three directions below, pick
   one and say why; the default blue card with Inter is not a direction.
8. **Not this.** Three to six things this design will not do, picked from the
   `anti-slop` list and the taste notes, specific to the subject.
9. **Done when.** The checks that decide it is finished: the states
   required (see `design-states`), the platform rules (see `mobile-app-design`,
   `web-landing-design` or `dashboard-design`), lint clean, critique score.

## Directions before detail

For a new visual direction, sketch three directions as quick greyscale
thumbnails or a sentence each, different in the visual idea, not in colour:

- A: the conservative, platform-native answer.
- B: the answer led by the one visual idea.
- C: an answer that changes the layout model (list to timeline, cards to
  table, scroll to single focus).

Pick one against the brief, write why in two lines, and drop the others.
When the user is present and the choice changes the product, show the three
and let them choose.

## Then

- Block the layout in greyscale first: only layout, type size and weight.
  If the hierarchy does not read in grey, colour will not save it.
- Add colour, imagery and detail after the grey version reads.
- Load `design-critique` after each screen and before you report.

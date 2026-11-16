---
name: type-and-color
description: Method for choosing a type scale, font pairing, colour palette, neutral ramp, contrast and elevation for an interface, and turning them into text and colour styles or variables before drawing. Load when a design brief needs its system decided, or when a design looks flat, noisy or low-contrast.
metadata:
  provenance: atlas-authored
---

# Type and colour

Typography and colour carry most of a screen's quality. Decide them once, as
a small system, make them styles or variables in the editor, and use nothing
else.

## Type

### Family

- One family is enough for most products. A second family only for a clear
  role (a display face for headlines, a monospace for codes and times).
- Choose by the brief's adjectives and the content:
  - Neutral, exact, product UI: Inter, SF Pro, Roboto, IBM Plex Sans,
    Source Sans 3.
  - Editorial, warm, trusted: Source Serif 4, Literata, Fraunces (display),
    Newsreader.
  - Technical, data, timetables: IBM Plex Mono, JetBrains Mono, Roboto Mono
    for numbers and codes only.
  - Strong display character: Space Grotesk, Manrope, Sora, DM Sans for
    headings with a neutral body face.
- Check the font exists in the editor and the export; a substituted font
  ruins the spacing.

### Scale

Pick a ratio and round to whole pixels. Mobile UIs use small ratios
(1.125-1.2), marketing pages larger ones (1.25-1.333).

| Role | Mobile (pt) | Web landing (px) | Weight | Line height |
| --- | --- | --- | --- | --- |
| Display | 34 | 56-72 | 700 | 1.1 |
| Title | 22-28 | 36-44 | 600-700 | 1.2 |
| Headline | 17-20 | 24-28 | 600 | 1.25 |
| Body | 15-17 | 16-18 | 400 | 1.45 |
| Caption | 12-13 | 13-14 | 500 | 1.35 |

- Four to six sizes per product. Name them by role, not by size.
- Hierarchy uses size, weight and colour together; never only bold.
- Large type gets tighter letter spacing (-1% to -2% at 28+), small caps
  labels get looser (+4% to +8%).
- Body line length 45-75 characters. Numbers in tables and timers use
  tabular figures.
- Align text to the baseline grid or at least to the spacing scale.

## Colour

### Build the palette

1. **One brand colour** chosen from the subject (the place, the material, the
   organisation's own colour), not from a trend. Optionally one secondary
   for contrast with a clear role.
2. **A neutral ramp** of 6-8 steps from paper to ink, slightly tinted
   towards the brand colour (warm or cool), never pure #000 on #FFF for
   long reading. Example warm ramp: #F7F5F0, #EEEAE2, #DAD5CA, #A8A194,
   #6E685E, #3D3A35, #1C1B19.
3. **Semantic colours** only where content needs them: success, warning,
   danger, info. Muted enough that they do not fight the brand colour.
4. For each colour, a tint for backgrounds and a shade for text on that tint.

### Proportion

- About 60% neutral surface, 30% secondary surfaces and text, 10% accent.
- The accent marks the one thing to do or notice. If everything is accent,
  nothing is.
- Dark mode is a second set of the same roles, not an inverted picture:
  surfaces lift (lighter) with elevation; reduce saturation of accents.

### Contrast (always measure)

- Body text 4.5:1 or more against its real background (WCAG AA).
- Large text (24 px+, or 19 px+ bold) and icons and control borders 3:1.
- Text over images or gradients: check the worst point, or add a scrim.
- Disabled is the only place low contrast is allowed.

## Gradients that are not slop

Use a gradient when it means something in the subject (dawn to day, cold to
warm, a heat map, light on a material), with colours from the palette and
small hue distance. Angular and multi-stop gradients work for rings, dials
and timers; a soft two-stop of near neighbours works for depth. Avoid
saturated two-hue gradients on large surfaces.

## Elevation

Zero to two levels. A real shadow is soft and tinted:
`0 1 2 rgba(ink, 0.06), 0 8 24 rgba(ink, 0.08)` for a raised card; larger
blur and offset for a sheet. Prefer a 1 px border or a surface step when the
element does not float.

## Make it real in the editor

Create colour styles (or variables with modes) named by role: `surface`,
`surface-raised`, `text`, `text-muted`, `brand`, `brand-on`, `line`,
`danger`. Create text styles named by role: `display`, `title`, `headline`,
`body`, `caption`, `label`. Then draw only with them. A raw hex or font
size outside the styles is a bug for `design-critique` to find.

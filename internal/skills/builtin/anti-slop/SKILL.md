---
name: anti-slop
description: Checklist of the patterns that make AI-made designs look generic (template layouts, default gradients, emoji icons, equal card rows, decoration without meaning, fake content) and what to do instead. Load when starting a visual direction and again before reporting any screen, page, deck or poster as done.
metadata:
  provenance: atlas-authored
---

# Anti-slop

"Slop" is what a design looks like when every choice was the first default
that came to mind. Each item below is a default. Using one is not always
wrong; using it without a reason from the brief is. Before reporting, go
through the list and either remove each match or write the reason it stays.

## Layout

- **Everything centred.** Centred text blocks, centred cards, centred
  headings on every section. Use a strong left edge; centre only short,
  symmetric content (a single number, a logo, an empty state).
- **The three equal cards.** Three same-size boxes with an icon, a title and
  two lines. Vary by importance: one large item and two small, a list, a
  table, a comparison. Equal weight means nothing is important.
- **Hero + three features + testimonial + CTA,** in that order, on every
  landing page. Start from what the visitor needs to believe, in order.
- **Cards inside cards.** Boxes nested to show grouping. Use spacing and a
  heading first; a border or fill only when the group must be separated.
- **Uniform spacing everywhere.** The same 16 or 24 between everything kills
  grouping. Tighter inside a group, looser between groups.
- **Filling every pixel.** Empty space is a material. Not every gap needs a
  badge, an illustration or a divider.

## Colour and surface

- **The purple-blue (or teal-pink) gradient** behind a hero or a button.
  Gradients are allowed when they mean something (light, time, temperature,
  a place) and use colours from the system.
- **Green-and-leaf for climate, blue-and-shield for security, globe for
  international.** The first metaphor everyone reaches for. Find the
  subject's own material: a timetable, a ticket, a map, a document.
- **Glassmorphism, neon glow, soft blobs** as decoration.
- **A shadow on everything.** Pick at most two elevations and use them only
  for things that actually float (sheets, menus, a primary card).
- **One radius for everything,** usually 12 or 16. Radius follows size:
  small controls small radius, sheets larger; nested radius = outer - padding.
- **Grey text on colour,** or light grey body text for "elegance". Check 4.5:1.
- **Rainbow semantics.** Five accent colours for five categories with no
  meaning. One brand colour, neutrals, and semantic colours only for status.

## Type

- **Inter at 16 regular for everything,** with hierarchy only from bold. Use
  the scale: size, weight and colour together.
- **Giant hero headline with nothing to say** ("Revolutionize your
  workflow"). Headlines state a specific fact or benefit from the content.
- **Title Case Everywhere And ALL CAPS LABELS** mixed at random. Pick one
  rule for headings and one for labels and keep it.
- **Too many sizes.** More than six text sizes on a screen means no scale.

## Icons and imagery

- **Emoji as icons** in a product UI.
- **Mixed icon sets,** filled next to outline, different stroke widths.
- **Icon above every label** in feature lists and settings. Icons help
  scanning in navigation and repeated rows, not as decoration on prose.
- **Generic 3D illustration or stock people** smiling at laptops.
- **Decorative circles and rings** that cross content for "depth".

## Content

- **Lorem ipsum, "John Doe", "Acme", "Lorem Conference", "$99/mo"** and
  other placeholders. Use real names, roles, dates, places, prices.
- **Vague microcopy:** "Get started", "Learn more", "Submit". Say what
  happens: "Get directions to Hall B", "Show my badge".
- **Fake precision:** "98.7% satisfaction", invented testimonials, invented
  logos of real companies. Never invent claims about real organisations.
- **Inconsistent formats:** 9:00 and 09:00 PM, Nov 9 and 9 November, on the
  same screen.

## Components and states

- **Only the happy path.** No empty, loading, error or long-content state.
- **Buttons that all look primary.** One primary per view, the rest
  secondary or text.
- **Tab bars with five or more tabs** of equal weight, or a tab bar that
  differs between screens.
- **Touch targets under 44 pt** (iOS) or 48 dp (Android).

## Quick test

Cover the logo and the product name. If the screen could belong to any
company in any field, it is slop: go back to the brief's one visual idea and
make it carry the design.

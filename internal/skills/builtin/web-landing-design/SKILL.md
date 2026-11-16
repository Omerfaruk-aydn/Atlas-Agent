---
name: web-landing-design
description: Structure and craft for marketing and landing pages - message order, hero, proof, sections, grid, type sizes, responsive breakpoints and calls to action - built from what the visitor must believe, not from a template. Load when designing a website, landing page, product page or event page.
metadata:
  provenance: atlas-authored
---

# Web and landing page design

A landing page is an argument. Each section answers the visitor's next
question, in order, until the call to action is the obvious next step.

## Write the argument first

Before layout, list the visitor's questions in the order they will ask them,
and the page's answer to each, with real facts:

1. What is this, and is it for me? (hero: one specific sentence + proof)
2. What do I get? (the two or three outcomes that matter, concretely)
3. How does it work? (steps, a product view, a real screenshot)
4. Why believe it? (real numbers, named customers or sources you have,
   specifics; never invented testimonials or logos)
5. What does it cost / what do I do now? (price or the single next step)
6. What if...? (objections: FAQ, guarantees, details)

Sections follow these answers. Drop any section that answers nothing.

## Hero

- Headline: a specific claim in the visitor's words, 6-12 words. Not
  "Revolutionize your X".
- Sub-line: who it is for and the concrete result, one or two lines.
- One primary action, labelled with what happens ("Book a demo", "Download
  the programme"); at most one secondary.
- A real visual of the product or subject: product UI, the place, the
  object. No abstract blobs.
- The hero fits in the first viewport at 1440 x 900 and 390 x 844.

## Grid and spacing

- Desktop frame 1440 wide, content max width 1120-1200, 12-column grid with
  24-32 gutters; tablet 768 with 8 columns; mobile 390 with 4 columns and
  16-20 margins.
- Section spacing 96-160 on desktop, 64-96 on mobile; vary it to group
  related sections.
- Text columns at most 640-720 wide for reading.
- Break the grid on purpose once or twice (a full-bleed image, an offset
  quote), not everywhere.

## Type for the web

- Display 56-80 / 1.05, -2% tracking; section titles 36-44; body 17-18 /
  1.5; small 14.
- Mobile display 36-44. Test the longest headline at both sizes.

## Sections that are not templates

- Vary section layouts by content: a split text/visual, a single big number,
  a timeline, a comparison table, a full-width product view.
- Alternate density: a dense, information-rich section after an airy one.
- Repeat the primary action after the proof and at the end, same label.

## Footer and trust

Real contact, legal, social and language links. Accessibility statement and
privacy for products that collect data.

## Responsive and accessible

- Design desktop and mobile frames for every section; reorder content on
  mobile by importance, not just stacking columns.
- Focus states on every link and button, skip link, alt text for meaningful
  images, 4.5:1 for text over images (add a scrim).

Run `anti-slop` and `design-critique` on the exported full-page picture.

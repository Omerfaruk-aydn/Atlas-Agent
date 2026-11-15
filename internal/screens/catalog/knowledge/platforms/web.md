# Web (desktop 1440 x 900, responsive down to 360)

Layout
- Breakpoints 360 (phone), 768 (tablet), 1024 (laptop), 1280 and 1440 (desktop). Design the narrowest first and decide what each wider width adds.
- Content container 1120 to 1280 px; text measure 60 to 75 characters (about 560 to 680 px at 17 px). Never full-bleed body text.
- A 12 column grid with 24 to 32 px gutters, or no grid at all but a consistent set of widths. Spacing on a 4 or 8 px scale.
- Sticky header 56 to 72 px; the first screenful (about 900 px) has one idea and one primary action.

Controls
- Targets: 44 px for touch layouts, never below 24 px for pointer-only (WCAG 2.2 target size minimum).
- Inputs 40 to 48 px high, label above the field (placeholders are not labels), error text under the field in words, not only colour.
- Every interactive element has hover, focus-visible and pressed states; focus rings are at least 2 px and meet 3:1 contrast.
- Navigation: at most seven top-level items; current page marked by more than colour; skip link; landmarks (header, nav, main, footer).

Type
- Body 16 to 18 px at line-height 1.5 to 1.6, headings 1.05 to 1.25. A modular scale (ratio 1.2 to 1.333) with fluid sizing between 320 and 1240 px (see type_scale).
- Two families at most; a variable font keeps weight steps cheap.

Images and motion
- Responsive images (srcset or fixed aspect boxes to avoid layout shift), real alt text.
- Motion answers an action; honour prefers-reduced-motion; no auto-playing carousels.

Show states that matter: hover, focus, empty, loading skeleton, error, and the same screen at phone width.

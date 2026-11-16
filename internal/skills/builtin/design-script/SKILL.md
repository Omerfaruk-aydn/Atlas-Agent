---
name: design-script
description: Build a design file's system and component library with a design script - a short JavaScript program run by design_canvas action script that creates colour tokens with light and dark values, text styles, spacing and radius scales, shadows, components on auto layout with every state as a variant, and optionally screen skeletons from instances - checked by a quality gate before it is saved. Load before setting up tokens or components for any design, and whenever a design needs a component library.
metadata:
  provenance: atlas-authored
---

# Design scripts

Professional files are built in two passes: a system once (tokens, type,
components with states), then screens composed from instances of it. The
first pass is repetitive and must be exact, so write it as a script; the
second is judgement, so do it by eye in the studio. One script call replaces
dozens of clicks and is always consistent.

Run it with `design_canvas` `{"action":"script","file":"<design>","code":"..."}`.
The script is saved as `.atlas/design/scripts/<design>.js`; call `script`
again without `code` to rerun it, or with new `code` to change it. Each run
replaces everything the previous run made (layer ids start with
`<design>:`), so fix the script, never the generated layers. A root the
person edited by hand since the last run is kept and reported; `force: true`
overwrites it.

## The gate

After the run, before saving, the result is checked. These stop the save
(the file stays unchanged and the errors are listed):

- a component with two or more layers and no auto layout;
- text inside a component without a text style;
- a variant set that misses a combination or repeats one;
- unreadable contrast, overflowing or clipped text in what the script made.

Warnings (fix them where they are real): raw `#hex` instead of tokens,
gaps and padding off the `space()` scale, radii off the `radius()` scale,
controls under 44 px tall, default layer names, three or more repeated
structures that should be a component, a main component inside a screen.

## API

Tokens (call these first):

- `colors({name: ["#light", "#dark"] | "#hex"})` makes colour variables
  `c-<name>` with Light and Dark modes. Name by role, never by hue:
  `bg`, `surface`, `surface-2`, `text`, `text-muted`, `line`, `brand`,
  `on-brand`, `accent`, `danger`, `success`. `text` is the default text colour.
- `type({family: "Inter", styles: {name: {size, weight, line, tracking,
  family, transform, italic}}})` makes text styles `t-<name>`. Name by role:
  `display`, `title`, `headline`, `body`, `callout`, `label`, `caption`.
  `line` is line height in px; `tracking` is letter spacing in px.
- `space([4, 8, 12, 16, 24, 32, 48])` declares the spacing scale.
- `radius({sm: 8, md: 12, lg: 20, full: 999})`; use the names as `radius: "md"`.
- `shadow(name, {y, blur, spread, color, x, inner})` or a list; use as
  `shadow: "raised"`.

Layers (each returns a node; nest by passing children arrays):

- `row(props, children)`, `col(props, children)`: auto-layout frames.
  Props: `name`, `gap`, `pad` (n, [v, h] or [t, r, b, l]), `main`
  (start, center, end, between), `cross` (start, center, end, stretch),
  `w` / `h` (a number, `"fill"` or `"hug"`; auto layout hugs by default),
  `fill`, `stroke` (`"line"` or `{color, width, align, dash}`), `radius`,
  `shadow`, `clip`, `opacity`, `absolute`, `x`, `y`.
- `frame(props, children)`: a frame without auto layout (use for
  decoration and absolute art only).
- `text(string, style, {color, w, name, align})`: text in a style; with
  `w: "fill"` it wraps.
- `icon(name, {size, color})`: a Lucide icon (`design_canvas` action
  `icons` searches the names).
- `rect(props)`, `ellipse(props)`, `image(src, props)` (a project path; a
  photo or illustration saved by design_canvas `photo` or `image` with
  `file` = the design is `"assets/<name>.<ext>"`, as its result says).
- `instance(ref, {fill, fills: {Layer: paint}, hide, icons, text, w, h})`:
  `fill` recolours the instance's own box (one Tile component, many hues);
  `fills` with `{type: "image", src}` puts a picture on a layer.
- `pattern("stars", ...)` (eight-pointed stars) and `sprig({w, h, leaves,
  fill, lean, flip})` (a branch of leaves) for headers and corners.
- `chart("area", values, {w, h, grid, glow})` (a fading curve over a grid),
  `chart("bars", values, {labels, values, behind, highlight: "all", glow})`,
  `chart("wave", 48 | amplitudes, {w, h})` (an audio waveform),
  `chart("zones", [colours], {value, labels, widths, bar, each})` (a
  segmented scale with a marker; widths makes a split bar such as sleep
  stages), `chart("ring", share, {sweep: 270, glow, dot: "danger"})` (a
  gauge, or a timer ring with a dot at the hand).
- `dial({w, labels, value, knob, accent, shadow})`: a rotary knob with a
  numbered tick scale. `meter({h, values: [l, r], peak, scale})`: a
  segmented level meter.
- `arcRoute([x0, y0], [x1, y1], {w, h, bend, progress, labels})`: a flight
  path, flown part lit, the rest dotted, a plane at progress.
- `map` pins with `kind: "tag"` are value callouts ("350 kW", "₺85"),
  `selected: true` adds a halo. `icon(name, {absolute, x, y, rotation})`.
- `shadow(name, [{x, y, blur: 0, color}])` gives a hard offset shadow
  (brutalist); two shadows, light top-left and dark bottom-right, give soft
  UI; `inner: true` makes a pressed well.
- `qr(text, {size, color, bg})`: a real, scannable QR code.
- `scrim({w, h, to: 0.8})`: a clear-to-dark gradient over a photo.
- `notches({w, y, r, color})`: a ticket's tear line with side bites.
- `trail([[x, y], ...], {w, h, width, nodes: [{label, color, ring}]})`: a
  winding road with stops, for levels or a journey.
- `screen(name, {device, h})`: h longer than the device makes a scrolling
  screen.
- Material, the subject's own things as vectors in your tokens:
  `map({w, h, seed, pins: [{x, y, kind: "you"}, {x, y, label}], route: true,
  labelStyle})`, `chart("spark" | "bars" | "ring", data, {w, h})`,
  `pattern("topo" | "dots" | "lines" | "grid" | "perforation", {w, h,
  color, gap, size})`, `path(d, {w, h, view, fill, stroke, weight})`.
  Details and when to use them: `design-kit`, section Material.
- Fonts the renderer has: Albert Sans, DM Sans, Figtree, Fraunces, Fustat,
  Geist, Inter, JetBrains Mono, Montserrat, Outfit, Playwrite US Trad,
  Poppins, Roboto. Any other family renders as a fallback (lint:
  `font-missing`).
- Colours anywhere are a token name, an existing variable id or `#hex`;
  gradients are `{type: "linear", angle, stops: ["brand", "accent"]}`.

Components:

- `component(name, props, children)`: one main component (frame props).
- `variants(name, {prop: [values]}, v => component(name, props, children))`:
  every combination of the properties as a variant set. Build each from `v`
  (`v.state`, `v.size`). Always include the states the brief needs:
  `default`, `pressed`, `disabled`, plus `selected` / `focused` / `error`
  where they apply.
- `instance(ref, {variant: {...}, text: {LayerName: "..."}, icons:
  {LayerName: "icon-name"}, hide: [LayerName], fills: {LayerName: token},
  w, h, name, absolute, x, y})`: an instance of a component or set, with
  overrides by layer name; `icons` swaps an icon layer for another icon.
  Name the layers you will override (`Label`, `Icon`, `Value`). An
  instance takes the size its content needs (a longer label makes a wider
  button) unless given `w` / `h`; `w: "fill"` stretches it in an auto-layout
  parent and its content reflows.

Pages and placing:

- `library([button, row, tabBar], {page: "Library"})` lays the components
  out on their own page. Every component must be placed somewhere.
- `screen(name, {device, fill, pad, gap, ...}, children)`: a device frame
  (`iphone` 393x852, `iphone-se`, `iphone-pro-max`, `android`, `tablet`,
  `desktop` 1440x1024, `laptop`) with a column grid; then
  `place(screenNode, {page: "Screens", x: 0, y: 0})`.
- `page(name, {background})` makes or updates a page.

The script is plain JavaScript (ES2015: `const`, arrow functions, template
strings). There is no file, network or clock access; build everything from
the brief's real content.

## Method

1. Read the brief and `type-and-color`. Decide the palette and type scale
   on paper first: 1 brand hue, 1 accent at most, neutrals with a slight
   tint of the brand, contrast checked for text roles.
2. Write tokens, then the smallest set of components the screens need
   (usually 4-8): button, icon button, list row, card, input, tab bar,
   top bar, chip/segment. Each on auto layout, sized by padding, with every
   state as a variant.
3. Run. Fix every gate error in the script; rerun. Read the warnings.
4. Optionally lay the screen skeleton with `screen()` and instances. Then
   finish the composition by hand in the studio: hierarchy, the one visual
   idea, spacing by eye, real imagery.
5. `design_canvas` action `progress` says which phase is next.

## Example

A conference companion app: system, five components with states, a tab bar,
and the Today screen's skeleton.

```js
colors({
  bg: ["#F6F4EF", "#0E1214"],
  surface: ["#FFFFFF", "#161B1E"],
  "surface-2": ["#EEEBE3", "#1F2629"],
  text: ["#16191B", "#ECEFEF"],
  "text-muted": ["#5A6166", "#9BA5AA"],
  line: ["#E2DED4", "#2A3236"],
  brand: ["#0B5D4B", "#4CC9A4"],
  "on-brand": ["#FFFFFF", "#06231B"],
  accent: ["#C2410C", "#FB923C"],
});
type({
  family: "Inter",
  styles: {
    display: { size: 34, weight: 700, line: 40, tracking: -0.8 },
    title: { size: 22, weight: 600, line: 28, tracking: -0.3 },
    headline: { size: 17, weight: 600, line: 22 },
    body: { size: 15, weight: 400, line: 21 },
    label: { size: 15, weight: 600, line: 20 },
    caption: { size: 13, weight: 500, line: 16 },
    overline: { size: 12, weight: 600, line: 16, tracking: 0.6, transform: "upper" },
  },
});
space([4, 8, 12, 16, 20, 24, 32, 48, 64]);
radius({ sm: 8, md: 12, lg: 20, full: 999 });
shadow("raised", [{ y: 1, blur: 2, color: "#0000000F" }, { y: 8, blur: 24, color: "#00000014" }]);

const button = variants("Button", { kind: ["primary", "secondary"], state: ["default", "pressed", "disabled"] }, (v) => {
  const primary = v.kind === "primary";
  const fill = v.state === "disabled" ? "surface-2" : primary ? (v.state === "pressed" ? "text" : "brand") : v.state === "pressed" ? "line" : "surface";
  const ink = v.state === "disabled" ? "text-muted" : primary ? (v.state === "pressed" ? "bg" : "on-brand") : "text";
  return component("Button", { dir: "row", gap: 8, pad: [12, 20], h: 48, main: "center", cross: "center", radius: "full", fill, stroke: primary ? null : "line" }, [
    icon("navigation", { size: 18, color: ink, name: "Icon" }),
    text("Get directions", "label", { color: ink, name: "Label" }),
  ]);
});

const listRow = variants("List row", { state: ["default", "pressed"] }, (v) =>
  component("List row", { dir: "row", gap: 12, pad: [12, 16], w: 361, cross: "center", fill: v.state === "pressed" ? "surface-2" : "surface" }, [
    col({ name: "Icon box", w: 40, h: 40, main: "center", cross: "center", radius: "md", fill: "surface-2" }, [
      icon("calendar", { size: 20, color: "brand", name: "Icon" }),
    ]),
    col({ name: "Copy", gap: 4, w: "fill" }, [
      text("Ocean finance plenary", "headline", { name: "Title", w: "fill" }),
      text("Hall A · 14:00", "caption", { name: "Meta", color: "text-muted", w: "fill" }),
    ]),
    icon("chevron-right", { size: 18, color: "text-muted", name: "Chevron" }),
  ]),
);
const tab = variants("Tab", { state: ["default", "selected"] }, (v) =>
  component("Tab", { dir: "column", gap: 4, pad: [4, 8], w: 88, h: 52, cross: "center", main: "center", fill: [] }, [
    icon("sun", { size: 22, color: v.state === "selected" ? "brand" : "text-muted", name: "Icon" }),
    text("Today", "caption", { color: v.state === "selected" ? "brand" : "text-muted", name: "Label" }),
  ]),
);

const tabBar = component("Tab bar", { dir: "row", pad: [8, 8, 32, 8], w: 393, h: 92, main: "between", fill: "surface", stroke: "line" }, [
  instance(tab, { variant: { state: "selected" }, name: "Today", text: { Label: "Today" } }),
  instance(tab, { variant: { state: "default" }, name: "Programme", text: { Label: "Programme" }, icons: { Icon: "calendar-days" } }),
  instance(tab, { variant: { state: "default" }, name: "Map", text: { Label: "Map" }, icons: { Icon: "map" } }),
  instance(tab, { variant: { state: "default" }, name: "Badge", text: { Label: "Badge" }, icons: { Icon: "id-card" } }),
]);

const chip = variants("Chip", { state: ["default", "selected"] }, (v) =>
  component("Chip", { dir: "row", pad: [12, 16], h: 44, cross: "center", radius: "full", fill: v.state === "selected" ? "text" : "surface", stroke: v.state === "selected" ? null : "line" }, [
    text("All sessions", "caption", { color: v.state === "selected" ? "bg" : "text", name: "Label" }),
  ]),
);

library([button, listRow, tab, tabBar, chip]);

const today = screen("01 Today", { device: "iphone", fill: "bg", gap: 24, pad: [64, 16, 0, 16] }, [
  col({ name: "Header", gap: 4, w: "fill" }, [
    text("Day 3 · Thursday 13 Nov", "overline", { color: "text-muted" }),
    text("Your next session starts in 48 min", "display", { w: "fill" }),
  ]),
  instance(button, { variant: { kind: "primary", state: "default" }, name: "Directions", text: { Label: "Directions to Room B2" } }),
  col({ name: "Up next", gap: 8, w: "fill" }, [
    text("Up next", "title"),
    instance(listRow, { variant: { state: "default" }, w: "fill", text: { Title: "Blue carbon markets", Meta: "Room B2 · 10:30" } }),
    instance(listRow, { variant: { state: "default" }, w: "fill", text: { Title: "Adaptation finance", Meta: "Hall A · 13:00" } }),
  ]),
  instance(tabBar, { name: "Tab bar", absolute: true, x: 0, y: 852 - 92 }),
]);
place(today, { page: "Screens", x: 0, y: 0 });
```

Helper functions that return layers are fine: every call makes new layers
with fresh ids. The tab bar sits in the screen as an instance, so changing
the main changes every screen. The skeleton is a start, not the design:
the hero still needs its one visual idea, finished by hand.

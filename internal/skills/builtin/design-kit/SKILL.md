---
name: design-kit
description: A ready component library for the Atlas design editor, written as a design script - buttons, inputs, switches, chips, badges, avatars, cards, list rows, tab bar and top bar, each on auto layout with every state as a variant, built from tokens. Load when a design needs standard UI components, then paste the script into design_canvas action script and change only the tokens and the copy.
metadata:
  provenance: atlas-authored
---

# Design kit

Standard components are the same in every good app: the anatomy, the states,
the sizes. Do not redraw them. Start from this script (the anatomy follows
common open practice: 44 px touch targets, a visible focus and error state,
a disabled state that is still readable, one radius family) and spend the
effort on the screens.

How to use it:

1. Run `design_canvas` `{"action":"script","file":"<design>","code":"<this script>"}`
   as it is to see the library on a `Library` page, then change it.
2. For phone screens build the native chrome from this kit (status bar, large header, tab bar, home indicator; see mobile-app-design). Choose the **look** (`const look = looks.<name>`): editorial, swiss,
   soft, night, civic or vivid (a bold brand app: see "Rich home" below). They differ in type pairing, shape, colour logic
   and depth, so two designs do not come out the same. Pick the one the
   brief's feeling words ask for, write its name under a `Direction`
   heading in the brief, then change its values: at least the accent, the
   display face and one shape rule must come from the brief's one visual
   idea. Do not ship the preset untouched, and do not use the same look as
   the file you made last. Keep the token names and the type roles
   (`display` ... `overline`): every component refers to them by role.
   Fonts must be kit fonts (Albert Sans, DM Sans, Figtree, Fraunces, Fustat,
   Geist, Montserrat, Outfit, Roboto, Poppins); others render as a fallback.
3. Delete the components the brief does not need (and their names from
   `library([...])`); rename copy and icons to the subject's real words.
4. Add the product's own components below the kit in the same script, in
   the same style, then compose screens from instances (see the
   `design-script` skill and the `design-patterns` skill).

Do not weaken the anatomy to look different: change colour, type, radius and
copy; the sizes and states are what make it usable.

## Script

```js
// ---- the look: pick ONE preset, then change its values to the brief ----
// Presets differ in type pairing, shape, colour logic and depth, not just
// hue. Never ship the same preset twice in a row, and never leave a preset
// untouched: the brief's visual idea decides at least the accent, the
// display face and one shape rule.
const looks = {
  // Warm paper, serif display, hairlines, square-ish corners.
  editorial: {
    c: { bg: ["#F4EFE6", "#14110E"], surface: ["#FBF8F2", "#1D1914"], "surface-2": ["#EAE3D6", "#28221B"], text: ["#1B1712", "#F1ECE2"], "text-muted": ["#6B6256", "#A89F90"], line: ["#D9D0BF", "#3A3329"], brand: ["#8A2F1B", "#E8896F"], "on-brand": ["#FFF8F0", "#2A0F08"], "map-water": ["#CBD9DA", "#1E2A2C"], "map-park": ["#DCE0C8", "#232A1C"], danger: ["#B3261E", "#FF8A80"], success: ["#2F6B3A", "#7BD08A"] },
    display: "Fraunces", body: "Geist", dw: 600, track: -0.6,
    r: { sm: 2, md: 4, lg: 6, xl: 8, full: 999 }, pill: "sm", ring: "line", lift: false, caps: true,
  },
  // White, black, one red, zero softness, grid.
  swiss: {
    c: { bg: ["#FFFFFF", "#0A0A0A"], surface: ["#FFFFFF", "#111111"], "surface-2": ["#F0F0F0", "#1E1E1E"], text: ["#0A0A0A", "#F5F5F5"], "text-muted": ["#5E5E5E", "#A0A0A0"], line: ["#0A0A0A", "#F5F5F5"], brand: ["#E8230A", "#FF5A44"], "on-brand": ["#FFFFFF", "#1A0300"], "map-water": ["#D6E4EC", "#16212A"], "map-park": ["#E4EADF", "#1A2118"], danger: ["#B00020", "#FF8A8A"], success: ["#0B6E3A", "#5ED99A"] },
    display: "Geist", body: "Geist", dw: 700, track: -1.2,
    r: { sm: 0, md: 0, lg: 0, xl: 0, full: 999 }, pill: "sm", ring: "line", lift: false, caps: true,
  },
  // Rounded, warm, friendly, soft depth.
  soft: {
    c: { bg: ["#FFF6EE", "#17110E"], surface: ["#FFFFFF", "#221A16"], "surface-2": ["#FFE9D6", "#2E231D"], text: ["#2A1D16", "#FBEFE6"], "text-muted": ["#7A6558", "#B9A596"], line: ["#F2DCC8", "#3B2E26"], brand: ["#B33A0A", "#FF9265"], "on-brand": ["#FFFFFF", "#2A0E03"], "map-water": ["#CFE6EE", "#1C2A30"], "map-park": ["#E3EDD3", "#232B1B"], danger: ["#D63A3A", "#FF8C8C"], success: ["#2E8B57", "#6FD6A0"] },
    display: "Outfit", body: "Figtree", dw: 700, track: -0.4,
    r: { sm: 12, md: 16, lg: 28, xl: 36, full: 999 }, pill: "full", ring: null, lift: true, caps: false,
  },
  // Dark-first, technical, green signal, tight radii.
  night: {
    c: { bg: ["#EEF2F0", "#080C0B"], surface: ["#FFFFFF", "#101715"], "surface-2": ["#E1E8E5", "#18211E"], text: ["#0C1412", "#E6EEEA"], "text-muted": ["#52605B", "#8DA09A"], line: ["#CBD6D1", "#24312D"], brand: ["#0E7A52", "#3DDC97"], "on-brand": ["#FFFFFF", "#03140D"], "map-water": ["#CFE0E6", "#0F2229"], "map-park": ["#D5E6DA", "#12261C"], danger: ["#B3261E", "#FF7A72"], success: ["#0E7A52", "#3DDC97"] },
    display: "Geist", body: "Geist", dw: 600, track: -0.8,
    r: { sm: 4, md: 8, lg: 12, xl: 16, full: 999 }, pill: "md", ring: "line", lift: false, caps: true,
  },
  // Calm, public, trustworthy, generous space, geometric sans.
  civic: {
    c: { bg: ["#F3F6F8", "#0D1418"], surface: ["#FFFFFF", "#151E24"], "surface-2": ["#E6ECF0", "#1E2A32"], text: ["#13222B", "#E9F0F4"], "text-muted": ["#526672", "#98ABB6"], line: ["#D5DEE4", "#2A3A44"], brand: ["#0B5D8A", "#6CC0EE"], "on-brand": ["#FFFFFF", "#04202F"], "map-water": ["#CDE1EE", "#14283A"], "map-park": ["#DCEAD9", "#16261C"], danger: ["#B3261E", "#FF8A80"], success: ["#1B7A4B", "#6FD6A0"] },
    display: "Albert Sans", body: "Albert Sans", dw: 700, track: -0.5,
    r: { sm: 6, md: 10, lg: 16, xl: 24, full: 999 }, pill: "md", ring: "line", lift: true, caps: false,
  },
  // Bold brand app (events, cities, campaigns): a deep brand header, colour
  // tiles with artwork, photographs, heavy rounded sans, big radii.
  vivid: {
    c: { bg: ["#F3F5F1", "#0B120E"], surface: ["#FFFFFF", "#141C17"], "surface-2": ["#E5EBE4", "#1D2720"], text: ["#0F1F17", "#EAF2EC"], "text-muted": ["#526159", "#9AABA1"], line: ["#D3DCD4", "#2A372F"], brand: ["#1A6B45", "#5FD29A"], "on-brand": ["#FFFFFF", "#03170D"], "map-water": ["#C9E2EC", "#12283A"], "map-park": ["#D4E8D2", "#15281B"], danger: ["#C2302A", "#FF8A80"], success: ["#1A6B45", "#5FD29A"] },
    display: "Figtree", body: "Figtree", dw: 800, track: -0.6,
    r: { sm: 10, md: 16, lg: 20, xl: 28, full: 999 }, pill: "full", ring: null, lift: true, caps: true,
  },
};
const look = looks.editorial;

colors(look.c);
type({
  family: look.body,
  styles: {
    display: { size: 34, weight: look.dw, line: 40, tracking: look.track, family: look.display },
    title: { size: 22, weight: look.dw - 100, line: 28, tracking: look.track / 2, family: look.display },
    headline: { size: 17, weight: 600, line: 22 },
    body: { size: 15, weight: 400, line: 21 },
    label: { size: 15, weight: 600, line: 20 },
    caption: { size: 13, weight: 500, line: 16 },
    overline: { size: 12, weight: 600, line: 16, tracking: 0.6, transform: look.caps ? "upper" : "none" },
  },
});
space([4, 8, 12, 16, 20, 24, 32, 48, 64]);
radius(look.r);
shadow("raised", look.lift ? [{ y: 1, blur: 2, color: "#0000000F" }, { y: 8, blur: 24, color: "#00000014" }] : [{ y: 0, blur: 0, color: "#00000000" }]);

// ---- Button: 3 kinds x 3 states ----
const button = variants("Button", { kind: ["primary", "secondary", "ghost"], state: ["default", "pressed", "disabled"] }, (v) => {
  const off = v.state === "disabled";
  const pressed = v.state === "pressed";
  const fill = v.kind === "primary" ? (off ? "surface-2" : pressed ? "text" : "brand") : v.kind === "secondary" ? (pressed ? "surface-2" : "surface") : pressed ? "surface-2" : [];
  const ink = off ? "text-muted" : v.kind === "primary" ? (pressed ? "bg" : "on-brand") : v.kind === "ghost" ? "brand" : "text";
  return component("Button", { dir: "row", gap: 8, pad: [12, 20], h: 48, main: "center", cross: "center", radius: look.pill, fill, stroke: v.kind === "secondary" ? "line" : null }, [
    icon("plus", { size: 18, color: ink, name: "Icon" }),
    text("Continue", "label", { color: ink, name: "Label" }),
  ]);
});

// ---- Input: default, focused, error ----
const input = variants("Input", { state: ["default", "focused", "error"] }, (v) =>
  component("Input", { dir: "column", gap: 4, w: 361 }, [
    text("Email", "caption", { color: "text-muted", name: "Label" }),
    row({ name: "Field", gap: 12, pad: [0, 16], h: 52, w: "fill", cross: "center", radius: "md", fill: "surface", stroke: v.state === "error" ? { color: "danger", width: 2 } : v.state === "focused" ? { color: "brand", width: 2 } : "line" }, [
      icon("mail", { size: 20, color: "text-muted", name: "Icon" }),
      text("name@example.com", "body", { w: "fill", name: "Value" }),
    ]),
    text(v.state === "error" ? "Enter a valid email address" : "We only use it to sign you in", "caption", { color: v.state === "error" ? "danger" : "text-muted", name: "Hint", w: "fill" }),
  ]),
);

// ---- Switch: on, off ----
const toggle = variants("Switch", { state: ["on", "off"] }, (v) =>
  component("Switch", { dir: "row", w: 64, h: 44, main: "center", cross: "center", fill: [] }, [
    row({ name: "Track", pad: 4, w: 52, h: 32, radius: "full", main: v.state === "on" ? "end" : "start", cross: "center", fill: v.state === "on" ? "brand" : "line" }, [
      ellipse({ name: "Knob", w: 24, h: 24, fill: v.state === "on" ? "on-brand" : "surface" }),
    ]),
  ]),
);

// ---- Chip: default, selected ----
const chip = variants("Chip", { state: ["default", "selected"] }, (v) =>
  component("Chip", { dir: "row", gap: 8, pad: [0, 16], h: 44, cross: "center", radius: look.pill, fill: v.state === "selected" ? "text" : "surface", stroke: v.state === "selected" ? null : "line" }, [
    text("Filter", "caption", { color: v.state === "selected" ? "bg" : "text", name: "Label" }),
  ]),
);

// ---- Badge: neutral, success, danger ----
const badge = variants("Badge", { tone: ["neutral", "success", "danger"] }, (v) =>
  component("Badge", { dir: "row", pad: [4, 8], h: 24, cross: "center", radius: "full", fill: v.tone === "success" ? "success" : v.tone === "danger" ? "danger" : "surface-2" }, [
    text("New", "caption", { color: v.tone === "neutral" ? "text" : "on-brand", name: "Label" }),
  ]),
);

// ---- Avatar ----
const avatar = component("Avatar", { dir: "row", w: 44, h: 44, main: "center", cross: "center", radius: "full", fill: "surface-2" }, [
  text("AB", "label", { color: "text", name: "Initials" }),
]);

// ---- Card ----
const card = component("Card", { dir: "column", gap: 8, pad: 16, w: 361, radius: "lg", fill: "surface", stroke: look.ring, shadow: look.lift ? "raised" : null }, [
  text("Overline", "overline", { color: "text-muted", name: "Overline" }),
  text("Card title", "headline", { name: "Title", w: "fill" }),
  text("One or two lines of supporting copy that explain the card.", "body", { color: "text-muted", name: "Body", w: "fill" }),
]);

// ---- List row: default, pressed ----
const listRow = variants("List row", { state: ["default", "pressed"] }, (v) =>
  component("List row", { dir: "row", gap: 12, pad: [12, 16], w: 361, h: 64, cross: "center", fill: v.state === "pressed" ? "surface-2" : "surface" }, [
    col({ name: "Icon box", w: 40, h: 40, main: "center", cross: "center", radius: "md", fill: "surface-2" }, [
      icon("circle", { size: 20, color: "brand", name: "Icon" }),
    ]),
    col({ name: "Copy", gap: 4, w: "fill" }, [
      text("Row title", "headline", { name: "Title", w: "fill" }),
      text("Supporting detail", "caption", { name: "Meta", color: "text-muted", w: "fill" }),
    ]),
    icon("chevron-right", { size: 18, color: "text-muted", name: "Chevron" }),
  ]),
);

// ---- Top bar ----
const topBar = component("Top bar", { dir: "row", pad: [8, 16], w: 393, h: 56, cross: "center", main: "between", fill: "bg" }, [
  icon("chevron-left", { size: 24, color: "text", name: "Back" }),
  text("Title", "headline", { name: "Title" }),
  icon("ellipsis", { size: 24, color: "text", name: "Action" }),
]);

// ---- Tab and tab bar ----
const tab = variants("Tab", { state: ["default", "selected"] }, (v) =>
  component("Tab", { dir: "column", gap: 4, pad: [4, 8], w: 88, h: 52, cross: "center", main: "center", fill: [] }, [
    icon("house", { size: 22, color: v.state === "selected" ? "brand" : "text-muted", name: "Icon" }),
    text("Home", "caption", { color: v.state === "selected" ? "brand" : "text-muted", name: "Label" }),
  ]),
);
const tabBar = component("Tab bar", { dir: "row", pad: [8, 8, 32, 8], w: 393, h: 92, main: "between", fill: "surface", stroke: "line" }, [
  instance(tab, { variant: { state: "selected" }, name: "Home", text: { Label: "Home" } }),
  instance(tab, { variant: { state: "default" }, name: "Search", text: { Label: "Search" }, icons: { Icon: "search" } }),
  instance(tab, { variant: { state: "default" }, name: "Saved", text: { Label: "Saved" }, icons: { Icon: "bookmark" } }),
  instance(tab, { variant: { state: "default" }, name: "Profile", text: { Label: "Profile" }, icons: { Icon: "user" } }),
]);

// ---- Native app chrome: every phone screen starts and ends with these ----
const statusBar = component("Status bar", { dir: "row", pad: [16, 32, 0, 32], w: 393, h: 59, main: "between", cross: "center", fill: [] }, [
  text("9:41", "label", { name: "Time" }),
  row({ name: "Indicators", gap: 8, cross: "center", fill: [] }, [
    icon("signal", { size: 16, color: "text", name: "Signal" }),
    icon("wifi", { size: 16, color: "text", name: "Wifi" }),
    icon("battery-full", { size: 20, color: "text", name: "Battery" }),
  ]),
]);
const homeIndicator = component("Home indicator", { dir: "row", w: 393, h: 34, main: "center", cross: "center", fill: [] }, [
  rect({ name: "Bar", w: 134, h: 4, radius: "full", fill: "text" }),
]);
// Large title header: the app's way to say where you are (not a web hero).
const largeHeader = component("Large header", { dir: "column", gap: 4, pad: [8, 16, 8, 16], w: 393, fill: [] }, [
  text("Caption above", "overline", { color: "text-muted", name: "Eyebrow" }),
  text("Large title", "display", { name: "Title", w: "fill" }),
]);
const searchField = component("Search field", { dir: "row", gap: 8, pad: [0, 12], w: 361, h: 44, cross: "center", radius: "md", fill: "surface-2" }, [
  icon("search", { size: 18, color: "text-muted", name: "Icon" }),
  text("Search", "body", { color: "text-muted", name: "Value", w: "fill" }),
]);
const segment = variants("Segment", { state: ["default", "selected"] }, (v) =>
  // The whole 44px height is the target; the pill inside it is what shows.
  component("Segment", { dir: "row", w: 112, h: 44, pad: [4, 0], cross: "center" }, [
    row({ name: "Pill", w: "fill", h: 36, main: "center", cross: "center", radius: "sm", fill: v.state === "selected" ? "surface" : [] }, [
      text("Option", "label", { color: v.state === "selected" ? "text" : "text-muted", name: "Label" }),
    ]),
  ]),
);
const segmented = component("Segmented control", { dir: "row", pad: [0, 4], w: 361, h: 44, cross: "center", radius: "md", fill: "surface-2" }, [
  instance(segment, { variant: { state: "selected" }, name: "First", w: "fill", text: { Label: "Now" } }),
  instance(segment, { variant: { state: "default" }, name: "Second", w: "fill", text: { Label: "Later" } }),
  instance(segment, { variant: { state: "default" }, name: "Third", w: "fill", text: { Label: "Done" } }),
]);
const sheet = component("Bottom sheet", { dir: "column", gap: 16, pad: [8, 16, 0, 16], w: 393, radius: "lg", fill: "surface", shadow: "raised" }, [
  row({ name: "Grabber area", w: "fill", h: 16, main: "center", cross: "center", fill: [] }, [rect({ name: "Grabber", w: 36, h: 4, radius: "full", fill: "line" })]),
  text("Sheet title", "title", { name: "Title", w: "fill" }),
  text("What this sheet lets you do, in one or two lines.", "body", { color: "text-muted", name: "Body", w: "fill" }),
]);

library([button, input, toggle, chip, badge, avatar, card, listRow, topBar, tab, tabBar, statusBar, homeIndicator, largeHeader, searchField, segment, segmented, sheet]);
```

## Using it on a screen

A phone screen is an app, not a web page: status bar on top, a large title,
content in inset cards or grouped lists on the screen background, the tab
bar and home indicator at the bottom. No full-width colour bands lower down
the screen (the lint reports `web-hero` and `app-chrome`); the one band an
app may have is a brand header at the very top with the status bar on it
(see "Rich home").

```js
place(screen("01 Sessions", { device: "iphone", fill: "bg", gap: 16, pad: [0, 0, 0, 0] }, [
  instance(statusBar, { name: "Status bar" }),
  instance(largeHeader, { name: "Header", w: "fill", text: { Eyebrow: "Friday 13 November", Title: "Sessions" } }),
  col({ name: "Content", gap: 16, pad: [0, 16], w: "fill" }, [
    instance(searchField, { name: "Search", w: "fill" }),
    instance(segmented, { name: "Filter", w: "fill" }),
    col({ name: "Group", w: "fill", radius: "lg", fill: "surface", clip: true }, [
      instance(listRow, { variant: { state: "default" }, w: "fill", icons: { Icon: "waves-horizontal" }, text: { Title: "Blue carbon markets", Meta: "Room B2 · 10:30" } }),
      instance(listRow, { variant: { state: "default" }, w: "fill", icons: { Icon: "landmark" }, text: { Title: "Adaptation finance", Meta: "Hall A · 13:00" } }),
    ]),
    // The subject's own thing: where the next session is, not another box.
    col({ name: "Next room", w: "fill", radius: "lg", fill: "surface", clip: true }, [
      map({ w: 361, h: 144, radius: 0, seed: "blue-zone", route: true, labelStyle: "caption",
        pins: [{ x: 0.2, y: 0.66, kind: "you" }, { x: 0.76, y: 0.32, label: "Room B2" }] }),
      row({ name: "Walk", gap: 8, pad: [12, 16], w: "fill" }, [
        icon("footprints", { size: 20, color: "text-muted" }),
        text("6 min walk · Blue Zone, level 1", "body", { w: "fill" }),
      ]),
    ]),
  ]),
  instance(tabBar, { name: "Tab bar", absolute: true, x: 0, y: 852 - 92 }),
  instance(homeIndicator, { name: "Home indicator", absolute: true, x: 0, y: 852 - 34 }),
]), { page: "Screens" });
```

Pass `w: "fill"` to stretch a component across its parent; the content
reflows instead of scaling. Hide the icon you do not want with
`hide: ["Icon"]`.

## Rich home: brand header, colour tiles, photographs

The home screen of an event, city or campaign app is the one place an app
spends colour freely: a deep brand header with the status bar on it, a
rounded sheet that rises over it, a grid of colour tiles where each tile has
its own artwork, a live strip and a row of news with real photographs.
Run it with `const look = looks.vivid` (change the hues to the brand), after
the kit script. It draws its artwork as vectors (`pattern("stars")`,
`sprig()`, a large icon on a tinted plate), so it is whole without pictures;
then lift it with pictures:

1. Tiles: one `design_canvas` `image` call per tile, all with the same style
   sentence, e.g. "Soft 3D clay illustration, rounded forms, gentle studio
   light, on a plain background of exactly #1F5C3D, no text, no letters:
   a calendar with a clock". Put it on the tile with
   `fills: { Art: { type: "image", src: "assets/tile-program.png" } }` and
   `hide: ["Icon"]`.
2. News: one `photo` call per story (a real photograph of the place or the
   event); `fills: { Photo: { type: "image", src: "<the src it returns>" } }`.
3. Header: an `image` of the place's landscape or motif, wide, as a
   `Scene` layer under the header text, with a dark scrim.

Every label in one language (the lint warns `mixed-language`); names stay as
they are. A home that scrolls past one screen takes its full length in the
script: `screen("02 Home", { device: "iphone", h: 1160, ... })`, with the tab
bar at `y: h - 92`.

```js
colors({
  "brand-deep": ["#123D2B", "#0A1F16"], "on-deep": ["#FFFFFF", "#EAF4EE"], "on-deep-muted": ["#B9D3C4", "#9DB8A9"],
  "tile-1": ["#1F5C3D", "#1A4A33"], "tile-2": ["#16708A", "#135C70"], "tile-3": ["#1D4C78", "#183F63"], "tile-4": ["#A8532A", "#8C4523"],
  "on-tile": ["#FFFFFF", "#FFFFFF"], live: ["#C2302A", "#FF8A80"], "live-soft": ["#FBE5E2", "#3A1C1A"],
});

// Status bar for a dark header: the same anatomy in the header's ink.
const statusBarOnDeep = component("Status bar on deep", { dir: "row", pad: [16, 32, 0, 32], w: 393, h: 59, main: "between", cross: "center", fill: [] }, [
  text("9:41", "label", { name: "Time", color: "on-deep" }),
  row({ name: "Indicators", gap: 8, cross: "center", fill: [] }, [
    icon("signal", { size: 16, color: "on-deep", name: "Signal" }),
    icon("wifi", { size: 16, color: "on-deep", name: "Wifi" }),
    icon("battery-full", { size: 20, color: "on-deep", name: "Battery" }),
  ]),
]);

// One tile, recoloured per instance (fill). Art fills the space above the
// title: an icon on a tinted plate until a picture replaces it.
const tile = component("Tile", { dir: "column", gap: 8, pad: [8, 8, 12, 8], w: 112, h: 120, radius: "lg", fill: "tile-1", clip: true }, [
  row({ name: "Art", w: "fill", h: "fill", main: "center", cross: "center", radius: "md", fill: { color: "on-tile", opacity: 0.14 } }, [
    icon("calendar-days", { size: 32, color: "on-tile", name: "Icon" }),
  ]),
  col({ name: "Label", pad: [0, 4], w: "fill" }, [
    text("Program", "headline", { color: "on-tile", name: "Title", w: "fill" }),
  ]),
]);

const newsCard = component("News card", { dir: "column", w: 264, radius: "lg", fill: "surface", clip: true, shadow: "raised" }, [
  frame({ name: "Photo", w: "fill", h: 128, fill: "surface-2" }, [
    pattern("stars", { w: 264, h: 128, gap: 32, color: "line", absolute: true, x: 0, y: 0 }),
  ]),
  col({ name: "Copy", gap: 4, pad: [12, 16, 16, 16], w: "fill" }, [
    text("Night market opens on the harbour wall", "headline", { name: "Title", w: "fill" }),
    text("2 h ago", "caption", { name: "Meta", color: "text-muted" }),
  ]),
]);

library([statusBarOnDeep, tile, newsCard], { page: "Library home" });

const R = look.r.xl;
place(screen("02 Home", { device: "iphone", fill: "brand-deep", gap: 0, pad: 0 }, [
  col({ name: "Header", w: "fill", gap: 8, pad: [0, 0, 24, 0], cross: "center", fill: "brand-deep", clip: true }, [
    pattern("stars", { w: 120, h: 220, gap: 40, color: "on-deep", opacity: 0.18, absolute: true, x: -36, y: 40 }),
    sprig({ w: 104, h: 150, leaves: 9, fill: "tile-1", seed: "olive", lean: -0.5, absolute: true, x: 300, y: 92 }),
    sprig({ w: 72, h: 104, leaves: 7, fill: "brand", seed: "olive-2", lean: 0.4, flip: true, absolute: true, x: 332, y: 150 }),
    instance(statusBarOnDeep, { name: "Status bar", w: "fill" }),
    row({ name: "Brand", gap: 12, cross: "center" }, [
      text("HARBOUR DAYS", "title", { color: "on-deep" }),
      row({ name: "Edition", pad: [4, 8], radius: "sm", stroke: { color: "on-deep", width: 1.5 } }, [text("2026", "overline", { color: "on-deep" })]),
    ]),
    col({ name: "Welcome", gap: 4, pad: [8, 24, 0, 24], cross: "center" }, [
      text("Welcome to the harbour", "display", { color: "on-deep", align: "center" }),
      text("12–16 June · Old Port, Pier 3", "body", { color: "on-deep-muted", align: "center" }),
    ]),
  ]),
  col({ name: "Sheet", w: "fill", h: "fill", gap: 16, pad: [16, 0, 0, 0], radius: [R, R, 0, 0], fill: "bg", clip: true }, [
    col({ name: "Top", gap: 12, pad: [0, 16], w: "fill" }, [
      row({ name: "Live strip", gap: 12, pad: [8, 16], w: "fill", cross: "center", radius: "md", fill: "live-soft" }, [
        icon("radio", { size: 28, color: "live" }),
        col({ name: "Live copy", gap: 4, w: "fill" }, [
          text("Live now", "overline", { color: "live" }),
          text("Day 3 of 5 · Saturday 14 June", "body", { w: "fill" }),
        ]),
      ]),
      col({ name: "Tiles", gap: 12, w: "fill" }, [
        row({ name: "Tiles top", gap: 12, w: "fill" }, [
          instance(tile, { name: "Program", w: "fill", text: { Title: "Program" } }),
          instance(tile, { name: "Live stage", w: "fill", fill: "tile-2", icons: { Icon: "video" }, text: { Title: "Live stage" } }),
          instance(tile, { name: "Map", w: "fill", fill: "tile-3", icons: { Icon: "map" }, text: { Title: "Map" } }),
        ]),
        row({ name: "Tiles bottom", gap: 12, w: "fill" }, [
          instance(tile, { name: "Artists", w: "fill", h: 196, fill: "tile-4", icons: { Icon: "users" }, text: { Title: "Artists" } }),
          col({ name: "Tiles side", gap: 12, w: "fill" }, [
            instance(tile, { name: "Recycling", w: "fill", h: 92, icons: { Icon: "recycle" }, text: { Title: "Recycling" } }),
            instance(tile, { name: "My pass", w: "fill", h: 92, fill: "tile-3", icons: { Icon: "id-card" }, text: { Title: "My pass" } }),
          ]),
        ]),
      ]),
      row({ name: "News head", w: "fill", main: "between", cross: "center" }, [
        text("Latest news", "title"),
        text("See all", "label", { color: "brand" }),
      ]),
    ]),
    // The row runs past the edge so the next story peeks in: it scrolls.
    row({ name: "News carousel", gap: 12, pad: [0, 16, 16, 16], w: "fill", clip: true }, [
      instance(newsCard, { name: "Story 1" }),
      instance(newsCard, { name: "Story 2", text: { Title: "Ferry timetable for the closing night", Meta: "5 h ago" } }),
    ]),
  ]),
  instance(tabBar, { name: "Tab bar", absolute: true, x: 0, y: 852 - 92 }),
  instance(homeIndicator, { name: "Home indicator", absolute: true, x: 0, y: 852 - 34 }),
]), { page: "Screens", x: 480 });
```

## Material: what makes it more than a wireframe

Every screen needs one thing drawn from the subject, not from the kit (lint
warns `no-material` without it). The script draws them as real vectors in
the file's tokens:

- `map({w, h, seed, pins: [{x, y, kind: "you"}, {x, y, label}], route: true,
  labelStyle: "caption"})`: a quiet street-map crop with a river, parks,
  streets, the route and pins. Positions are shares of the box (0-1). The
  seed picks the streets; colours come from `map-land` (else `surface-2`),
  `map-water`, `map-park`, `map-road` (else `surface`) and `brand`.
- `chart("spark", [12, 18, 15, 24], {w, h})`: a trend line with a soft area
  and a dot on the latest value. `chart("bars", values, {w, h, highlight})`:
  the highlight bar (default the last) in the accent, the rest in
  `surface-2`. `chart("ring", 0.62, {w: 64})`: a progress ring, e.g. the
  minutes left of a countdown.
- `pattern("topo" | "dots" | "lines" | "grid" | "perforation", {w, h, color,
  gap, size, seed})`: a texture as one vector. Contour lines under a big
  number, dots in a card's corner (`absolute: true` with x, y), the tear
  line of a ticket (`perforation` in the page colour).
- `path(d, {w, h, view, fill, stroke, weight})`: any SVG path, for an
  illustration you draw or a logo mark.
- `pattern("stars", {w, h, gap, color})`: eight-pointed stars, the
  geometric motif of Anatolian tile and carpet, for a header's edge.
  `sprig({w, h, leaves, fill, lean, flip})`: a branch of leaves (olive,
  laurel) for a header or tile corner.
- Pictures: design_canvas `photo` (a real photograph) or `image` (an
  illustration from the image model) with `file` set to the design, then
  `image("<the src it returns>", {w, h, radius})` or an image fill on a
  layer; render embeds it. Credit the photographer where the licence asks.

Use one, big enough to read at arm's length, and let it carry the screen's
idea (the map is where the next session is; the ring is the time left).
Decoration that says nothing is worse than none.

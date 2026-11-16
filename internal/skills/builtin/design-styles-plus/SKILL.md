---
name: design-styles-plus
description: Seventeen more proven phone-app styles as working design scripts on top of the design-kit and design-styles - meal plan, calendar day, book reader, neo-brutalist events, luxury boutique, hardware recorder, minimal focus timer, soft-UI thermostat, boutique stays, hi-fi now playing, breathwork, EV charger map, trading terminal, live flight tracker, sleep and recovery, language flashcard, issue inbox. Load when the brief names one of these kinds of app or this kind of look.
metadata:
  provenance: atlas-authored
---

# More design styles

Same contract as `design-styles`: run the design-kit script, then the
**Shared** block of `design-styles`, then the **Shared** block below, then
one style. Keep the structure and the signature detail; change copy, hues
and one shape rule to the brief. Every style here was drawn against a
reference screen at the quality bar (`.atlas/design/benchmarks/<style>.jpg`
when the project has them): render, `compare` with that path, close the
gaps.

The references themselves slipped in the usual places, so never copy
these: English labels on a Turkish app ("Today's Plan", "Start
Navigation"), emoji inside copy ("Electronic ⚡", "Rain on Cedar 🌧"),
broken dot-matrix letters, a mix of "1h 12m" and "1 sa 12 dk" formats.

| Style | For | Signature |
| --- | --- | --- |
| mealprep | meal plans, recipes, nutrition | the dish photo card and the prep checklist on a peach card |
| calendar | day planning, booking, shifts | the hour grid, event blocks with a colour edge, the red now line |
| reader | e-books, long reads, notes | the serif page, mono running head, yellow highlight |
| brutal | events, culture, youth brands | black outlines, hard offset shadows, one loud yellow |
| boutique | fashion, craft, luxury retail | spaced logo, one object on a plain ground, square black button |
| recorder | audio, tools, pro utilities | a hardware faceplate: screws, screen, dial, keys |
| focus | timers, habits, one-task apps | one thin ring with a red dot, a huge number |
| thermostat | smart home, devices | soft-UI plates and a glowing 270° gauge |
| stays | rentals, hotels, marketplaces | search pill, icon category row, serif section title, big photo |
| nowplaying | hi-fi music, multi-room audio | glowing cover on near-black, quality badge, room volumes |
| breathwork | meditation, sleep sounds | watercolour rings with sprigs, terracotta small caps |
| evmap | charging, parking, delivery maps | a full-bleed map with value tags and a bottom sheet |
| terminal | trading, crypto, analytics | huge balance, green area chart, watchlist with sparks |
| flight | flight and parcel tracking | a lit arc over a dark world, glass panels |
| recovery | health dashboards, wearables | dark metric cards: a ring, a curve, stage bar, bars |
| flashcard | language learning, quizzes | one card: picture, word, phonetics, audio pill |
| inbox | issue trackers, tasks, support | dense dark rows: priority, id, title, status pill, avatar |

## Shared

```js
// A pill: a short status or tag on a tinted plate.
function pill(label, fill, ink, style) {
  return row({ name: "Pill", pad: [4, 10], radius: "full", fill: fill, cross: "center" }, [text(label, style || "caption", { color: ink })]);
}
// A square checkbox, ticked or not, with its label.
function checkRow(label, done, ink, box) {
  return row({ name: "Check", w: "fill", gap: 12, cross: "center", h: 36 }, [
    row({ name: "Box", w: 22, h: 22, radius: 4, main: "center", cross: "center", fill: done ? ink : null, stroke: { color: ink, width: 2 } }, done ? [icon("check", { size: 16, color: box })] : []),
    text(label, "body", { color: ink, w: "fill" }),
  ]);
}
// A plain status bar at the top, absolute, for full-bleed screens.
function topStatus(color) {
  return statusBarIn(color, { absolute: true, x: 0, y: 0 });
}
```

## mealprep: a warm meal plan

Cream ground, sage plates, peach for food. A small greeting over a bold
title, a sage search pill, the dish as a photo card whose lower band is
blush with the name and two chips (time in sage, calories in peach), then
the prep list on a solid peach card with square checkboxes. Picture:
`photo` "salmon quinoa bowl with avocado and cucumber, top down".

```js
colors({ bg: ["#FCF7F1", "#15130F"], surface: ["#FFFFFF", "#1E1B17"], "surface-2": ["#E2EBE1", "#273026"], text: ["#1E2620", "#F2F1EC"], "text-muted": ["#59625B", "#B3B9B2"], line: ["#E8E1D6", "#35312B"], brand: ["#3E6B4E", "#9ACFAE"], "on-brand": ["#FFFFFF", "#0E1D13"], danger: ["#B4412F", "#FF8E7A"], success: ["#3E6B4E", "#9ACFAE"], blush: ["#F6E3DA", "#3A2A24"], peach: ["#EEC3AB", "#5A3A2C"], "peach-chip": ["#EDC0A8", "#6A4334"], ink: ["#1E2620", "#F2F1EC"] });
type({ family: "Roboto", styles: { display: { size: 36, weight: 700, line: 42, tracking: -0.6 }, title: { size: 24, weight: 700, line: 30 }, headline: { size: 20, weight: 700, line: 26 }, body: { size: 17, weight: 400, line: 24 }, label: { size: 16, weight: 500, line: 20 }, caption: { size: 14, weight: 500, line: 18 }, overline: { size: 12, weight: 700, line: 16 } } });
place(screen("Meal prep", { device: "iphone", fill: "bg", gap: 16, pad: 0 }, chrome([
  instance(statusBar, { name: "Status bar" }),
  row({ name: "Head", w: "fill", pad: [0, 24], cross: "center" }, [col({ name: "Hello", w: "fill", gap: 2 }, [text("Merhaba Ece", "body"), text("Bugünün planı", "display")]), instance(avatar, { name: "Avatar", text: { Initials: "EK" } })]),
  col({ name: "Body", w: "fill", gap: 20, pad: [0, 24] }, [
    instance(searchField, { name: "Search", w: "fill", h: 52, radius: 26, fill: "surface-2", text: { Value: "Tarif veya malzeme ara" } }),
    col({ name: "Dish", w: "fill", radius: "lg", fill: "blush", clip: true }, [
      frame({ name: "Photo", w: "fill", h: 180, fill: "peach" }),
      col({ name: "Copy", w: "fill", pad: [12, 20, 20, 20], gap: 10 }, [text("Fırında somon kase", "title", { w: "fill" }), row({ name: "Chips", gap: 8 }, [pill("25 dk", "surface-2", "ink", "label"), pill("480 kcal", "peach-chip", "ink", "label")])]),
    ]),
    text("Bugünkü hazırlık", "title"),
    col({ name: "Prep", w: "fill", pad: [16, 20], radius: "lg", fill: "peach" }, [
      checkRow("300 g somonu porsiyonla", true, "ink", "peach"),
      checkRow("Bir demet brokoliyi doğra", true, "ink", "peach"),
      checkRow("Bir kırmızı soğanı küp doğra", false, "ink", "peach"),
    ]),
  ]),
  tabsBar([["Ana Sayfa", "house"], ["Tarifler", "book-open"], ["Hazırlık", "chef-hat"], ["Market", "shopping-cart"], ["Profil", "user"]], 2),
])), { page: "Screens" });
```

## calendar: a day on the hour grid

White, ink and one blue and one violet. The month as a big bold title with
a plus, a week strip where today is a black rounded square, then hours in
muted tabular figures with hairlines across, events as tinted blocks with
a 4 px colour edge, title bold and time under it, and the now line in red
with the time in a red tag. Signature: the grid and the now line.

```js
colors({ bg: ["#FFFFFF", "#0E1015"], surface: ["#FFFFFF", "#171A21"], "surface-2": ["#F2F4F7", "#222631"], text: ["#111827", "#F3F4F6"], "text-muted": ["#5B6472", "#A7AFBC"], line: ["#E3E7ED", "#2B303B"], brand: ["#111827", "#F3F4F6"], "on-brand": ["#FFFFFF", "#111827"], danger: ["#E5383B", "#FF7072"], success: ["#1E7B45", "#6FD6A0"], blue: ["#2563EB", "#6EA0FF"], "blue-soft": ["#DBE7FE", "#1C2C4F"], violet: ["#7C3AED", "#B18CFF"], "violet-soft": ["#EBDDFE", "#2E2347"], "on-danger": ["#FFFFFF", "#2A0508"] });
type({ family: "Inter", styles: { display: { size: 34, weight: 800, line: 40, tracking: -0.8 }, title: { size: 20, weight: 700, line: 26 }, headline: { size: 17, weight: 700, line: 22 }, body: { size: 15, weight: 400, line: 20 }, label: { size: 15, weight: 500, line: 20 }, caption: { size: 13, weight: 500, line: 16 }, overline: { size: 12, weight: 600, line: 16 } } });
const day = (d, n, on) => col({ name: d, w: "fill", h: 64, gap: 2, radius: "md", main: "center", cross: "center", fill: on ? "brand" : null }, [text(d, "caption", { color: on ? "on-brand" : "text-muted" }), text(n, "title", { color: on ? "on-brand" : "text" })]);
const hour = (h, y) => row({ name: "Hour " + h, absolute: true, x: 16, y: y, w: 361, gap: 8, cross: "center" }, [text(h, "caption", { color: "text-muted", w: 44 }), rect({ name: "Rule", w: "fill", h: 1, fill: "line" })]);
const event = (title, time, tint, edge, y, h) => row({ name: "Event", absolute: true, x: 68, y: y, w: 309, h: h, radius: "sm", fill: tint, clip: true }, [rect({ name: "Edge", w: 4, h: "fill", fill: edge }), col({ name: "Copy", pad: [8, 12], gap: 2 }, [text(title, "headline"), text(time, "body", { color: "text-muted" })])]);
place(screen("Calendar", { device: "iphone", fill: "bg", gap: 8, pad: 0 }, chrome([
  instance(statusBar, { name: "Status bar" }),
  row({ name: "Head", w: "fill", pad: [0, 16], main: "between", cross: "center" }, [text("Kasım", "display"), icon("plus", { size: 28 })]),
  row({ name: "Week", w: "fill", pad: [0, 12], gap: 2 }, [day("Paz", "15"), day("Pzt", "16", true), day("Sal", "17"), day("Çar", "18"), day("Per", "19"), day("Cum", "20"), day("Cmt", "21")]),
  frame({ name: "Grid", w: "fill", h: 560 }, [
    hour("09:00", 8), hour("10:00", 88), hour("11:00", 168), hour("12:00", 248), hour("13:00", 328), hour("14:00", 408), hour("15:00", 488),
    event("Tasarım incelemesi", "09:30 – 10:30", "blue-soft", "blue", 56, 76),
    event("Odak çalışması", "11:00 – 13:00", "violet-soft", "violet", 176, 156),
    rect({ name: "Now line", absolute: true, x: 64, y: 224, w: 313, h: 2, fill: "danger" }),
    row({ name: "Now", absolute: true, x: 8, y: 214, pad: [2, 6], radius: "sm", fill: "danger" }, [text("11:42", "caption", { color: "on-danger" })]),
  ]),
  tabsBar([["Takvim", "calendar"], ["Görevler", "list-checks"], ["Odak", "timer"], ["Ayarlar", "settings"]], 0),
])), { page: "Screens" });
```

## reader: a quiet page

Warm paper, black ink, nothing else but one yellow. A thin progress bar
at the top, the running head in uppercase mono ("BÖL. 04 / KESİNLİK",
"S. 112"), the chapter title in a large serif, the byline in mono caps,
then body text in a serif at 19/29 with one sentence highlighted, and a
floating pill with "Aa" and "···" over a fade at the bottom. Signature:
the type itself.

```js
colors({ bg: ["#F4F0E6", "#16140F"], surface: ["#FBF8F1", "#1F1C16"], "surface-2": ["#E6E0D2", "#2C2820"], text: ["#1A1814", "#EDE7DA"], "text-muted": ["#57524A", "#B2AB9C"], line: ["#D8D1C1", "#3A352C"], brand: ["#1A1814", "#EDE7DA"], "on-brand": ["#F4F0E6", "#16140F"], danger: ["#9B2C2C", "#F08A8A"], success: ["#2F6B3A", "#7BD08A"], mark: ["#F6E58D", "#6B5C12"] });
type({ family: "JetBrains Mono", styles: { display: { size: 40, weight: 500, line: 46, family: "Fraunces", tracking: -0.8 }, title: { size: 22, weight: 600, line: 28, family: "Fraunces" }, headline: { size: 17, weight: 600, line: 22, family: "Fraunces" }, body: { size: 19, weight: 400, line: 29, family: "Fraunces" }, label: { size: 16, weight: 500, line: 20 }, caption: { size: 13, weight: 500, line: 16, tracking: 0.6, transform: "upper" }, overline: { size: 12, weight: 500, line: 16, tracking: 0.8, transform: "upper" } } });
place(screen("Reader", { device: "iphone", fill: "bg", gap: 16, pad: 0 }, chrome([
  instance(statusBar, { name: "Status bar" }),
  col({ name: "Page", w: "fill", gap: 16, pad: [0, 24] }, [
    frame({ name: "Progress", w: "fill", h: 6, radius: "full", fill: "surface-2", clip: true }, [rect({ name: "Read", w: 140, h: 6, fill: "text" })]),
    row({ name: "Running head", w: "fill", main: "between" }, [text("Böl. 04 / Kesinlik", "caption"), text("S. 112", "caption")]),
    text("Gündelik aletlerde kesinlik üzerine", "display", { w: "fill" }),
    text("Deniz Aras · 6 dk okuma", "caption", { color: "text-muted" }),
    text("Bir aletin değeri yalnızca işe yaramasıyla değil, kullanılırken gözden kaybolabilmesiyle ölçülür. 0,5 mm'lik bir kalemi ya da dengesi iyi bir mutfak bıçağını düşünün.", "body", { w: "fill" }),
    col({ name: "Highlight", w: "fill", pad: [2, 4], radius: 2, fill: "mark" }, [text("Bir alet kesinlikle yapıldığında dışarıdaki bir nesne olmaktan çıkar ve zihnin uzantısı olur.", "body", { w: "fill" })]),
  ]),
  row({ name: "Tools", absolute: true, x: 131, y: 852 - 104, w: 131, h: 48, radius: "full", fill: "surface", stroke: "line", shadow: "raised", main: "center", cross: "center", gap: 16 }, [text("Aa", "label"), rect({ name: "Rule", w: 1, h: 24, fill: "line" }), icon("ellipsis", { size: 22 })]),
])), { page: "Screens" });
```

## brutal: a loud event guide

Cream ground, pure black outlines (2 px) and hard offset shadows with no
blur, one loud yellow, a heavy uppercase grotesque set tight. Buttons and
chips are outlined boxes with the shadow; the pick of the night is a
yellow card with a duotone photo (yellow and deep violet). Signature: the
hard shadow. Picture: `photo` "indie concert stage with lights and
crowd", then a duotone (`image` with the photo as input: "duotone, deep
violet shadows, yellow highlights").

```js
colors({ bg: ["#FBF6E6", "#FBF6E6"], surface: ["#FFFDF5", "#FFFDF5"], "surface-2": ["#F2EBD3", "#F2EBD3"], text: ["#0D0D0D", "#0D0D0D"], "text-muted": ["#3D3A33", "#3D3A33"], line: ["#0D0D0D", "#0D0D0D"], brand: ["#0D0D0D", "#0D0D0D"], "on-brand": ["#FBF6E6", "#FBF6E6"], yellow: ["#FFD84A", "#FFD84A"], danger: ["#D7263D", "#D7263D"], success: ["#1E7B45", "#1E7B45"], violet: ["#2E2350", "#2E2350"] });
type({ family: "Montserrat", styles: { display: { size: 44, weight: 900, line: 42, tracking: -1.5, transform: "upper" }, title: { size: 26, weight: 800, line: 30 }, headline: { size: 17, weight: 800, line: 22, transform: "upper" }, body: { size: 16, weight: 500, line: 22 }, label: { size: 14, weight: 800, line: 18, transform: "upper" }, caption: { size: 12, weight: 700, line: 16, transform: "upper" }, overline: { size: 12, weight: 800, line: 16, transform: "upper" } } });
shadow("hard", [{ x: 4, y: 4, blur: 0, color: "#0D0D0D" }]);
const box = (kids, extra) => row(Object.assign({ name: "Box", pad: [8, 14], radius: "md", fill: "surface", stroke: { color: "line", width: 2 }, shadow: "hard", cross: "center", gap: 6 }, extra || {}), kids);
place(screen("Brutal", { device: "iphone", fill: "bg", gap: 14, pad: 0 }, chrome([
  instance(statusBar, { name: "Status bar" }),
  row({ name: "Top", w: "fill", pad: [0, 20], main: "between", cross: "center" }, [box([text("Keşfet", "label")]), row({ name: "Actions", gap: 12 }, [box([icon("search", { size: 20 })], { pad: 10 }), box([icon("user", { size: 20 })], { pad: 10 })])]),
  col({ name: "Title", w: "fill", pad: [0, 20], gap: 8 }, [text("Bu gece Kadıköy'de", "display", { w: "fill" }), box([icon("sparkle", { size: 14 }), text("16 Kasım — bağımsız ve plak radarı", "caption")], { pad: [4, 8], shadow: null })]),
  row({ name: "Filters", w: "fill", pad: [0, 20], gap: 10, clip: true }, [box([text("Tümü", "label")]), box([text("Elektronik", "label")], { fill: "yellow" }), box([text("Rock", "label")]), box([text("Caz", "label")])]),
  row({ name: "Inset", w: "fill", pad: [0, 20] }, [col({ name: "Pick", w: "fill", h: 330, pad: 14, gap: 10, radius: "md", fill: "yellow", stroke: { color: "line", width: 2 }, shadow: "hard" }, [
    row({ name: "Tag", gap: 6, cross: "center" }, [icon("star", { size: 16 }), text("Gecenin seçimi", "headline")]),
    frame({ name: "Photo", w: "fill", h: 170, radius: "sm", fill: "violet", stroke: { color: "line", width: 2 } }),
    text("Kiasmos — canlı set", "title", { w: "fill" }),
    row({ name: "Meta", w: "fill", main: "between", cross: "center" }, [text("Arkaoda · 20:00 · ₺650", "body"), box([text("Bilet al", "label"), icon("arrow-up-right", { size: 16 })], { pad: [6, 10] })]),
  ])]),
  tabsBar([["Bu gece", "sparkle"], ["Keşfet", "search"], ["Plak", "disc-3"], ["Favoriler", "heart"]], 0),
])), { page: "Screens" });
```

## boutique: one object, a lot of air

Warm off-white, a letter-spaced logo centred between a thin arrow and the
word "Sepet", one product photo in a narrow frame with real shadow light,
a spaced small-caps kind, the name in an uppercase serif, the price, two
lines of copy and a square black button the full text width. No radius
anywhere, no icons beyond the arrow. Signature: the spacing. Picture:
`photo` "handmade clay cup on travertine, hard window light".

```js
colors({ bg: ["#F2EFE8", "#141310"], surface: ["#F8F6F1", "#1C1A16"], "surface-2": ["#C9C2B6", "#2A2722"], text: ["#141310", "#F2EFE8"], "text-muted": ["#4F4B44", "#B8B1A5"], line: ["#D9D3C8", "#35322C"], brand: ["#141310", "#F2EFE8"], "on-brand": ["#F2EFE8", "#141310"], danger: ["#8E1B23", "#F07A80"], success: ["#2F6B3A", "#7BD08A"] });
type({ family: "Figtree", styles: { display: { size: 40, weight: 400, line: 44, family: "Fraunces", tracking: -0.8, transform: "upper" }, title: { size: 24, weight: 400, line: 30, tracking: 8, transform: "upper" }, headline: { size: 22, weight: 500, line: 28 }, body: { size: 15, weight: 400, line: 22 }, label: { size: 14, weight: 500, line: 18, tracking: 2, transform: "upper" }, caption: { size: 13, weight: 500, line: 16, tracking: 4, transform: "upper" }, overline: { size: 12, weight: 500, line: 16, tracking: 4, transform: "upper" } } });
place(screen("Boutique", { device: "iphone", fill: "bg", gap: 24, pad: 0 }, chrome([
  instance(statusBar, { name: "Status bar" }),
  row({ name: "Nav", w: "fill", pad: [0, 24], main: "between", cross: "center" }, [icon("arrow-left", { size: 26, weight: 1.25 }), text("Odelia", "title"), text("Sepet", "label")]),
  row({ name: "Photo wrap", w: "fill", main: "center" }, [frame({ name: "Photo", w: 240, h: 330, fill: "surface-2" })]),
  col({ name: "Copy", w: "fill", pad: [0, 64], gap: 12 }, [
    text("Nesne", "caption"),
    text("Seri 04 — taş kap", "display", { w: "fill" }),
    text("₺4.800", "headline"),
    text("Kehribar kilden elde biçimlendirilmiş, dokusu doğal bırakılmış bir kap.", "body", { color: "text-muted", w: "fill" }),
    row({ name: "Add", w: "fill", h: 52, fill: "brand", main: "center", cross: "center" }, [text("Sepete ekle", "label", { color: "on-brand" })]),
  ]),
])), { page: "Screens" });
```

## recorder: a hardware faceplate

A light grey metal panel with four screws, a mono model name, a dark
recessed screen with the waveform, the REC dot in orange, the level meter
and a big mono timer; under it a dial with numbered ticks between two
switch groups, and a row of square transport keys with the last one solid
orange. Mono type everywhere. Signature: `dial()` + `meter()` + the keys.

```js
colors({ bg: ["#E4E3DF", "#E4E3DF"], surface: ["#EDECE8", "#EDECE8"], "surface-2": ["#D6D5D0", "#D6D5D0"], text: ["#1A1A1A", "#1A1A1A"], "text-muted": ["#4E4E4B", "#4E4E4B"], line: ["#B9B8B3", "#B9B8B3"], brand: ["#E8501A", "#E8501A"], "on-brand": ["#FFFFFF", "#FFFFFF"], danger: ["#E8501A", "#E8501A"], success: ["#1E7B45", "#1E7B45"], screen: ["#1B1B1B", "#1B1B1B"], "on-screen": ["#F2F2F0", "#F2F2F0"], "screen-muted": ["#B5B5B0", "#B5B5B0"], "screen-off": ["#3A3A38", "#3A3A38"], key: ["#E9E8E4", "#E9E8E4"] });
type({ family: "JetBrains Mono", styles: { display: { size: 48, weight: 500, line: 52 }, title: { size: 18, weight: 600, line: 24 }, headline: { size: 16, weight: 600, line: 20, tracking: 1 }, body: { size: 14, weight: 500, line: 20 }, label: { size: 13, weight: 600, line: 16, tracking: 0.5 }, caption: { size: 11, weight: 500, line: 14 }, overline: { size: 11, weight: 600, line: 14, tracking: 1 } } });
shadow("key", [{ x: 0, y: 2, blur: 0, color: "#9C9B96" }]);
const screw = (x, y) => ellipse({ name: "Screw", absolute: true, x: x, y: y, w: 14, h: 14, fill: "surface-2", stroke: { color: "line", width: 1 } });
const key = (ic, label, hot) => col({ name: label, w: "fill", gap: 6, cross: "center" }, [text(label, "label"), row({ name: "Key", w: "fill", h: 56, radius: 4, fill: hot ? "brand" : "key", stroke: { color: "text", width: 2 }, shadow: "key", main: "center", cross: "center" }, [icon(ic, { size: 22, color: hot ? "on-brand" : "text" })])]);
const sw = (title, sub) => col({ name: title, gap: 6, cross: "center", w: 70 }, [text(title, "headline"), instance(toggle, { name: "Switch", variant: { state: "off" } }), text(sub, "caption")]);
place(screen("Recorder", { device: "iphone", fill: "bg", gap: 10, pad: 0 }, chrome([
  instance(statusBar, { name: "Status bar" }),
  row({ name: "Inset", w: "fill", pad: [0, 16] }, [text("Sesatlas // SA-01", "title")]),
  row({ name: "Inset", w: "fill", pad: [0, 12] }, [col({ name: "Panel", w: "fill", h: 700, pad: [16, 16], gap: 20, radius: "lg", fill: "surface", stroke: { color: "line", width: 2 }, cross: "center" }, [
    screw(10, 10), screw(343, 10),
    text("Saha ses kaydedici", "headline"),
    col({ name: "Screen", w: "fill", h: 290, pad: [14, 16], gap: 6, radius: "md", fill: "screen", stroke: { color: "text", width: 3 } }, [
      row({ name: "Top", w: "fill", gap: 16, cross: "center" }, [text("48 kHz / 24 bit", "body", { color: "on-screen" }), row({ name: "Rec", gap: 6, cross: "center" }, [ellipse({ name: "Dot", w: 10, h: 10, fill: "brand" }), text("Kayıt", "body", { color: "brand" })])]),
      row({ name: "Signal", w: "fill", gap: 12, cross: "center" }, [chart("wave", 60, { w: 232, h: 150, gap: 1, color: "on-screen", seed: "field" }), meter({ h: 150, values: [0.82, 0.74], peak: 0.86, bar: 8, gap: 6, color: "on-screen", track: "screen-off", accent: "brand" })]),
      text("00:04:18", "display", { color: "on-screen", w: "fill", align: "center" }),
    ]),
    row({ name: "Controls", w: "fill", main: "between", cross: "center" }, [sw("Giriş", "İç / Dış"), dial({ w: 190, labels: ["0", "2", "4", "6", "8", "10"], value: 0.5, knob: "key", accent: "brand", tick: "text", shadow: "raised", labelStyle: "caption" }), sw("Limit", "Açık")]),
    row({ name: "Transport", w: "fill", gap: 6 }, [key("rewind", "Geri"), key("square", "Dur"), key("play", "Çal"), key("pause", "Ara"), key("fast-forward", "İleri"), key("circle", "Kayıt", true)]),
  ])]),
])), { page: "Screens" });
```

## focus: one task, one ring

Off-white and black, one red dot. A mono counter at the top ("ODAK ·
02/04"), a large thin circle with the red dot riding on it at the elapsed
share, the minutes huge and light in the centre with the task under it,
one black pill button, then the task list with square boxes. Nothing else
on the screen. Signature: the ring with the dot.

```js
colors({ bg: ["#F4F4F2", "#0F0F0F"], surface: ["#FFFFFF", "#181818"], "surface-2": ["#E7E7E4", "#262626"], text: ["#0F0F0F", "#F4F4F2"], "text-muted": ["#55554F", "#ABABA5"], line: ["#D8D8D4", "#303030"], brand: ["#0F0F0F", "#F4F4F2"], "on-brand": ["#FFFFFF", "#0F0F0F"], danger: ["#E5484D", "#FF7A7E"], success: ["#1E7B45", "#6FD6A0"] });
type({ family: "Inter", styles: { display: { size: 84, weight: 400, line: 90, tracking: -3 }, title: { size: 22, weight: 500, line: 28 }, headline: { size: 18, weight: 600, line: 24, family: "JetBrains Mono", tracking: 4, transform: "upper" }, body: { size: 18, weight: 400, line: 24 }, label: { size: 17, weight: 500, line: 22 }, caption: { size: 13, weight: 700, line: 16, tracking: 1.2, transform: "upper" }, overline: { size: 12, weight: 700, line: 16, tracking: 1.2, transform: "upper" } } });
place(screen("Focus", { device: "iphone", fill: "bg", gap: 28, pad: 0 }, chrome([
  instance(statusBar, { name: "Status bar" }),
  text("Odak · 02/04", "headline", { w: "fill", align: "center" }),
  row({ name: "Timer wrap", w: "fill", main: "center" }, [frame({ name: "Timer", w: 300, h: 300 }, [
    chart("ring", 0.12, { w: 300, weight: 3, color: "text", track: "text", dot: "danger", absolute: true, x: 0, y: 0 }),
    col({ name: "Time", absolute: true, x: 0, y: 96, w: 300, cross: "center", gap: 0 }, [text("24:18", "display"), text("Q4 planını yaz", "title")]),
  ])]),
  row({ name: "Pause wrap", w: "fill", main: "center" }, [row({ name: "Pause", w: 156, h: 56, radius: "full", fill: "brand", main: "center", cross: "center" }, [text("Duraklat", "label", { color: "on-brand" })])]),
  col({ name: "Tasks", w: "fill", pad: [0, 32], gap: 4 }, [text("Görevler", "caption"), checkRow("Q4 planını yaz", false, "text", "bg"), checkRow("Arayüz geri bildirimlerini oku", false, "text", "bg"), checkRow("Sistem mimarisini güncelle", true, "text", "bg")]),
])), { page: "Screens" });
```

## thermostat: soft UI on grey

One cool grey for ground and plates; depth comes only from two shadows,
light from the top left and dark from the bottom right. A 270° gauge in
blue with a soft glow around a raised dial holding the temperature,
two room plates and an energy plate with a pressed icon well. Signature:
the gauge and the soft shadows. Keep text contrast high: soft UI fails
when the labels go grey on grey.

```js
colors({ bg: ["#E3E7EE", "#1B1E24"], surface: ["#E3E7EE", "#1B1E24"], "surface-2": ["#D3D9E3", "#252932"], text: ["#1C2433", "#E9EDF4"], "text-muted": ["#4E5869", "#A9B2C2"], line: ["#C9D0DB", "#2E333D"], brand: ["#2563EB", "#6EA0FF"], "on-brand": ["#FFFFFF", "#0B1630"], danger: ["#C8102E", "#FF6B7D"], success: ["#1E7B45", "#6FD6A0"] });
type({ family: "Figtree", styles: { display: { size: 64, weight: 700, line: 68, tracking: -2 }, title: { size: 26, weight: 700, line: 32 }, headline: { size: 18, weight: 700, line: 24 }, body: { size: 16, weight: 400, line: 22 }, label: { size: 16, weight: 600, line: 20 }, caption: { size: 13, weight: 500, line: 16 }, overline: { size: 12, weight: 600, line: 16 } } });
shadow("soft", [{ x: -6, y: -6, blur: 14, color: "#FFFFFFD9" }, { x: 6, y: 6, blur: 14, color: "#A3B1C6A6" }]);
shadow("well", [{ x: 3, y: 3, blur: 6, color: "#A3B1C699", inner: true }, { x: -3, y: -3, blur: 6, color: "#FFFFFFCC", inner: true }]);
const room = (name, temp, on) => col({ name: name, w: "fill", h: 104, pad: 16, gap: 4, radius: "lg", fill: "surface", shadow: "soft" }, [text(name, "headline"), row({ name: "Row", w: "fill", main: "between", cross: "center" }, [text(temp, "body", { color: "text-muted" }), on ? instance(toggle, { name: "Switch", variant: { state: "on" } }) : null])]);
place(screen("Thermostat", { device: "iphone", fill: "bg", gap: 24, pad: 0 }, chrome([
  instance(statusBar, { name: "Status bar" }),
  row({ name: "Nav", w: "fill", pad: [0, 24], gap: 16, cross: "center" }, [row({ name: "Back", w: 48, h: 48, radius: "md", fill: "surface", shadow: "soft", main: "center", cross: "center" }, [icon("arrow-left", { size: 22 })]), text("İklim", "title", { w: "fill" }), row({ name: "Settings", w: 48, h: 48, radius: "md", fill: "surface", shadow: "soft", main: "center", cross: "center" }, [icon("settings", { size: 22 })])]),
  row({ name: "Gauge wrap", w: "fill", main: "center" }, [frame({ name: "Gauge", w: 300, h: 300 }, [
    ellipse({ name: "Plate", absolute: true, x: 10, y: 10, w: 280, h: 280, fill: "surface", shadow: "soft" }),
    chart("ring", 0.72, { w: 268, sweep: 270, start: -135, weight: 8, color: "brand", track: "line", glow: true, absolute: true, x: 16, y: 16 }),
    ellipse({ name: "Dial", absolute: true, x: 60, y: 60, w: 180, h: 180, fill: "surface", shadow: "soft" }),
    col({ name: "Read", absolute: true, x: 60, y: 104, w: 180, cross: "center" }, [text("21,5°", "display"), text("Isıtma · Eko", "body", { color: "text-muted" })]),
  ])]),
  row({ name: "Rooms", w: "fill", pad: [0, 24], gap: 20 }, [room("Salon", "21,0°C", true), room("Yatak odası", "18,5°C")]),
  row({ name: "Inset", w: "fill", pad: [0, 24] }, [row({ name: "Energy", w: "fill", h: 96, pad: [0, 20], gap: 16, radius: "lg", fill: "surface", shadow: "soft", cross: "center" }, [
    row({ name: "Well", w: 60, h: 60, radius: "full", fill: "surface", shadow: "well", main: "center", cross: "center" }, [icon("zap", { size: 24 })]),
    col({ name: "Copy", gap: 2 }, [text("Bugün 6,4 kWh", "title"), text("Tasarruf modu açık", "body", { color: "text-muted" })]),
  ])]),
])), { page: "Screens" });
```

## stays: a calm marketplace

White, black ink, one serif for section titles. The search is a pill with
a soft shadow and a filter button beside it, then a row of line icons with
labels for categories over a hairline (the chosen one underlined), a
serif section title, one big rounded photo, the name, one muted line and
the rating with the price per night. Signature: the restraint. Picture:
`photo` "wooden riverside pavilion with paper doors, summer".

```js
colors({ bg: ["#FFFFFF", "#111111"], surface: ["#FFFFFF", "#1A1A1A"], "surface-2": ["#EFEDE8", "#262626"], text: ["#141414", "#F2F2F2"], "text-muted": ["#5E5E5E", "#ADADAD"], line: ["#E4E4E4", "#2E2E2E"], brand: ["#141414", "#F2F2F2"], "on-brand": ["#FFFFFF", "#111111"], danger: ["#C8102E", "#FF6B7D"], success: ["#1E7B45", "#6FD6A0"] });
type({ family: "Inter", styles: { display: { size: 30, weight: 400, line: 36, family: "Fraunces", tracking: -0.5 }, title: { size: 20, weight: 600, line: 26 }, headline: { size: 17, weight: 600, line: 22 }, body: { size: 16, weight: 400, line: 22 }, label: { size: 15, weight: 500, line: 20 }, caption: { size: 13, weight: 500, line: 16 }, overline: { size: 12, weight: 600, line: 16 } } });
shadow("pill", [{ y: 2, blur: 12, color: "#0000001F" }]);
const kind = (label, ic, on) => col({ name: label, gap: 6, cross: "center" }, [icon(ic, { size: 24, weight: 1.5 }), text(label, "label", { color: on ? "text" : "text-muted" }), rect({ name: "Underline", w: "fill", h: 2, fill: on ? "text" : "bg" })]);
place(screen("Stays", { device: "iphone", fill: "bg", gap: 16, pad: 0 }, chrome([
  instance(statusBar, { name: "Status bar" }),
  row({ name: "Search", w: "fill", pad: [0, 24], gap: 12, cross: "center" }, [row({ name: "Field", w: "fill", h: 56, pad: [0, 18], gap: 10, radius: "full", fill: "surface", stroke: "line", shadow: "pill", cross: "center" }, [icon("search", { size: 20 }), text("Kyoto · 12–16 Eki · 2 kişi", "label")]), icon("sliders-horizontal", { size: 24 })]),
  row({ name: "Kinds", w: "fill", pad: [0, 24], main: "between" }, [kind("Kulübe", "tent", true), kind("Brütalist", "building", false), kind("Sahil", "waves-horizontal", false), kind("Köşk", "sprout", false)]),
  col({ name: "Body", w: "fill", pad: [0, 24], gap: 12 }, [
    rect({ name: "Rule", w: 345, h: 1, fill: "line" }),
    text("Öne çıkan butik konaklamalar", "display", { w: "fill" }),
    frame({ name: "Photo", w: "fill", h: 300, radius: "lg", fill: "surface-2" }),
    col({ name: "Copy", gap: 2, pad: [0, 8] }, [text("Kamo Nehri Köşkü — Kyoto", "title"), text("Su kenarında sedir ağacından köşk", "body", { color: "text-muted" }), row({ name: "Meta", gap: 4, cross: "center" }, [icon("star", { size: 16 }), text("4,96 · ₺11.900 / gece", "body")])]),
  ]),
  tabsBar([["Keşfet", "compass"], ["Kayıtlı", "bookmark"], ["Mesajlar", "message-square"], ["Profil", "user"]], 0),
])), { page: "Screens" });
```

## nowplaying: hi-fi on near-black

Near-black with a cool glow behind the cover from its own colours, a
centred spaced overline, the cover square with soft corners and a faint
edge light, the title bold and centred, the artist muted, a small gold
outlined quality badge, a slider with a round thumb and times, five
controls with the middle one an outlined ring, then a darker panel with a
volume slider per room. Signature: the glow and the badge. Cover:
`photo` "city skyline at night, violet and blue".

```js
colors({ bg: ["#111216", "#111216"], surface: ["#1C1E24", "#1C1E24"], "surface-2": ["#2A2D35", "#2A2D35"], text: ["#F4F5F7", "#F4F5F7"], "text-muted": ["#9EA3AE", "#9EA3AE"], line: ["#3A3E48", "#3A3E48"], brand: ["#D6D8DE", "#D6D8DE"], "on-brand": ["#111216", "#111216"], danger: ["#FF6B6B", "#FF6B6B"], success: ["#43D17A", "#43D17A"], gold: ["#E3B25B", "#E3B25B"], night: ["#2B2470", "#2B2470"], glowc: ["#3A3FA8", "#3A3FA8"] });
type({ family: "Inter", styles: { display: { size: 30, weight: 700, line: 36, tracking: -0.5 }, title: { size: 26, weight: 500, line: 32 }, headline: { size: 18, weight: 500, line: 24 }, body: { size: 15, weight: 400, line: 20 }, label: { size: 14, weight: 600, line: 18, tracking: 2, transform: "upper" }, caption: { size: 13, weight: 500, line: 16 }, overline: { size: 13, weight: 600, line: 16, tracking: 2, transform: "upper" } } });
shadow("cover", [{ y: 0, blur: 60, spread: 10, color: "#3A3FA866" }]);
const slider = (share, w) => frame({ name: "Slider", w: w, h: 20 }, [rect({ name: "Track", absolute: true, x: 0, y: 8, w: w, h: 4, radius: 2, fill: "line" }), rect({ name: "Fill", absolute: true, x: 0, y: 8, w: Math.round(w * share), h: 4, radius: 2, fill: "brand" }), ellipse({ name: "Thumb", absolute: true, x: Math.round(w * share) - 10, y: 0, w: 20, h: 20, fill: "brand" })]);
const zone = (name, pct, share) => col({ name: name, w: "fill", gap: 6 }, [row({ name: "Head", w: "fill", main: "between" }, [text(name, "headline"), text(pct, "body", { color: "text-muted" })]), row({ name: "Row", w: "fill", gap: 12, cross: "center" }, [icon("volume-1", { size: 18, color: "text-muted" }), slider(share, 300)])]);
place(screen("Now playing", { device: "iphone", fill: "bg", gap: 12, pad: 0 }, chrome([
  instance(statusBar, { name: "Status bar" }),
  row({ name: "Nav", w: "fill", pad: [0, 20], main: "between", cross: "center" }, [icon("chevron-left", { size: 26 }), text("Şimdi çalıyor", "label", { color: "text-muted" }), row({ name: "More", w: 44, h: 44, radius: "full", fill: "surface-2", main: "center", cross: "center" }, [icon("ellipsis", { size: 20 })])]),
  row({ name: "Cover wrap", w: "fill", main: "center" }, [frame({ name: "Photo", w: 236, h: 236, radius: "md", fill: "night", shadow: "cover", stroke: { color: "line", width: 1 } })]),
  col({ name: "Track", w: "fill", gap: 6, cross: "center" }, [text("Gece Şehri", "display"), text("Atlas Orkestrası", "title", { color: "text-muted" }), row({ name: "Badge", pad: [2, 10], radius: "full", stroke: { color: "gold", width: 1 } }, [text("FLAC 24 bit / 96 kHz", "caption", { color: "gold" })])]),
  col({ name: "Progress", w: "fill", pad: [0, 32], gap: 4 }, [slider(0.36, 329), row({ name: "Times", w: "fill", main: "between" }, [text("1:24", "caption", { color: "text-muted" }), text("4:03", "caption", { color: "text-muted" })])]),
  row({ name: "Controls", w: "fill", pad: [0, 40], main: "between", cross: "center" }, [icon("shuffle", { size: 22, color: "text-muted" }), icon("skip-back", { size: 28 }), row({ name: "Play", w: 76, h: 76, radius: "full", stroke: { color: "text-muted", width: 2 }, main: "center", cross: "center" }, [icon("pause", { size: 30 })]), icon("skip-forward", { size: 28 }), icon("repeat", { size: 22, color: "text-muted" })]),
  row({ name: "Rooms wrap", w: "fill", pad: [0, 16] }, [col({ name: "Rooms", w: "fill", pad: [16, 16], gap: 12, radius: "lg", fill: "surface" }, [text("Salon + Stüdyo", "overline", { color: "text-muted" }), zone("Salon", "%70", 0.7), zone("Stüdyo", "%45", 0.45)])]),
])), { page: "Screens" });
```

## breathwork: watercolour calm

Cream paper under a sage band, terracotta small caps over a serif title,
a big circle of soft concentric rings in sage and terracotta held by a
thin outline with sprigs climbing it, the breath cue in the centre, the
sound's name in serif, a terracotta slider and three controls with the
middle a solid terracotta disc. Signature: the rings and sprigs. For true
watercolour: `image` "loose watercolour rings, sage green and terracotta,
on cream paper, no text".

```js
colors({ bg: ["#F7F2E8", "#161511"], surface: ["#FBF8F1", "#1E1C17"], "surface-2": ["#DCE3D6", "#2A3027"], text: ["#2E3A33", "#EFEDE6"], "text-muted": ["#5E665F", "#B2B8B0"], line: ["#DCD5C8", "#35322B"], brand: ["#A85A40", "#E99A7D"], "on-brand": ["#FFFFFF", "#2A0F06"], danger: ["#B4412F", "#FF8E7A"], success: ["#4F7A5E", "#8FD1AE"], sage: ["#7E9A86", "#7E9A86"], "sage-soft": ["#C9D6CB", "#3A4A3E"], terra: ["#C98466", "#C98466"], "terra-soft": ["#EBC9B8", "#4A3026"] });
type({ family: "Figtree", styles: { display: { size: 30, weight: 500, line: 36, family: "Fraunces", tracking: -0.4 }, title: { size: 40, weight: 400, line: 44, family: "Fraunces" }, headline: { size: 24, weight: 400, line: 30, family: "Fraunces" }, body: { size: 16, weight: 400, line: 22 }, label: { size: 15, weight: 600, line: 20 }, caption: { size: 13, weight: 500, line: 16 }, overline: { size: 13, weight: 600, line: 16, tracking: 1.6, transform: "upper" } } });
place(screen("Breathwork", { device: "iphone", fill: "bg", gap: 16, pad: 0 }, chrome([
  statusBarIn("text", { fill: "sage-soft" }),
  row({ name: "Nav", w: "fill", pad: [0, 24], main: "between" }, [icon("chevron-down", { size: 28, color: "sage" }), icon("x", { size: 28, color: "sage" })]),
  col({ name: "Title", w: "fill", gap: 6, cross: "center" }, [text("Nefes ve dinginlik", "overline", { color: "brand" }), text("Akşam gevşemesi · 8 dk", "display")]),
  row({ name: "Circle wrap", w: "fill", main: "center" }, [frame({ name: "Circle", w: 330, h: 330 }, [
    ellipse({ name: "Outline", absolute: true, x: 10, y: 10, w: 310, h: 310, fill: null, stroke: { color: "sage", width: 2 } }),
    chart("ring", 1, { w: 276, weight: 22, color: "terra-soft", absolute: true, x: 27, y: 27 }),
    chart("ring", 1, { w: 244, weight: 20, color: "sage-soft", absolute: true, x: 43, y: 43 }),
    chart("ring", 1, { w: 208, weight: 6, color: "sage", absolute: true, x: 61, y: 61 }),
    sprig({ w: 70, h: 110, leaves: 9, fill: "sage", seed: "left", lean: -0.4, absolute: true, x: 0, y: 40 }),
    sprig({ w: 70, h: 110, leaves: 9, fill: "sage", seed: "right", lean: 0.4, flip: true, absolute: true, x: 260, y: 190 }),
    col({ name: "Cue", absolute: true, x: 65, y: 130, w: 200, cross: "center" }, [text("Nefes al", "title", { color: "brand" }), text("4 sn", "body", { color: "text-muted" })]),
  ])]),
  text("Sedir üstünde yağmur", "headline", { color: "brand", w: "fill", align: "center" }),
  col({ name: "Progress", w: "fill", pad: [0, 24], gap: 4 }, [chart("zones", ["brand", "sage-soft"], { w: 345, widths: [0.18, 0.82], bar: 6, gap: 0 }), row({ name: "Times", w: "fill", main: "between" }, [text("1:24", "caption", { color: "text-muted" }), text("8:00", "caption", { color: "text-muted" })])]),
  row({ name: "Controls", w: "fill", gap: 40, main: "center", cross: "center" }, [icon("rotate-ccw", { size: 30, color: "sage" }), row({ name: "Pause", w: 64, h: 64, radius: "full", fill: "brand", main: "center", cross: "center" }, [icon("pause", { size: 26, color: "on-brand" })]), icon("rotate-cw", { size: 30, color: "sage" })]),
])), { page: "Screens" });
```

## evmap: a full-bleed map with a sheet

The map fills the screen under the status bar: pale land, white streets,
blue-grey water, green parks. A white search pill floats on it with a
filter icon, filter chips under it, value tags in teal on the stations
(the chosen one larger with a halo), the person as a blue dot, round zoom
and locate buttons at the right, and a white bottom sheet with a grabber:
the station name with distance, a green availability pill, the specs,
the price and amenity icons, one dark full-width button. Signature: the
tags (`map` pins with `kind: "tag"`).

```js
colors({ bg: ["#EEF1F4", "#0F1215"], surface: ["#FFFFFF", "#181C21"], "surface-2": ["#E5E9EE", "#232830"], text: ["#151B23", "#EEF1F4"], "text-muted": ["#545E6B", "#A7B0BC"], line: ["#DCE1E7", "#2C323A"], brand: ["#0F7A6C", "#4FD1BD"], "on-brand": ["#FFFFFF", "#04201B"], danger: ["#C8102E", "#FF6B7D"], success: ["#12805C", "#5BD69E"], "ok-soft": ["#D3F2E5", "#173A2C"], "on-ok": ["#FFFFFF", "#04201B"], ink: ["#151B23", "#151B23"], you: ["#2F6BF2", "#6EA0FF"], "map-land": ["#EEF1F4", "#1A1F25"], "map-water": ["#BFD0DD", "#22364A"], "map-park": ["#D7E6D6", "#1D2C22"], "map-road": ["#FFFFFF", "#2A3038"], "map-minor": ["#FFFFFF", "#232830"], white: ["#FFFFFF", "#FFFFFF"] });
type({ family: "Inter", styles: { display: { size: 30, weight: 700, line: 36 }, title: { size: 24, weight: 700, line: 30, tracking: -0.3 }, headline: { size: 17, weight: 600, line: 22 }, body: { size: 16, weight: 400, line: 22 }, label: { size: 16, weight: 600, line: 20 }, caption: { size: 14, weight: 600, line: 18 }, overline: { size: 12, weight: 600, line: 16 } } });
shadow("float", [{ y: 4, blur: 16, color: "#0000002E" }]);
const round = (ic) => row({ name: "Button", w: 52, h: 52, radius: "full", fill: "surface", shadow: "float", main: "center", cross: "center" }, [icon(ic, { size: 22 })]);
const filt = (label, ic) => row({ name: label, pad: [8, 14], gap: 6, radius: "full", fill: "surface", shadow: "float", cross: "center" }, ic ? [icon(ic, { size: 16 }), text(label, "label")] : [text(label, "label")]);
place(screen("EV map", { device: "iphone", fill: "bg", gap: 0, pad: 0 }, [
  map({ w: 393, h: 852, seed: "london", radius: 0, absolute: true, x: 0, y: 0, labelStyle: "caption", block: 34, pins: [
    { x: 0.18, y: 0.36, kind: "tag", label: "150 kW" }, { x: 0.32, y: 0.47, kind: "tag", label: "150 kW" }, { x: 0.4, y: 0.52, kind: "you" },
    { x: 0.57, y: 0.53, kind: "tag", label: "350 kW", selected: true }, { x: 0.78, y: 0.5, kind: "tag", label: "350 kW" }, { x: 0.3, y: 0.72, kind: "tag", label: "150 kW" }] }),
  topStatus("text"),
  row({ name: "Search", absolute: true, x: 16, y: 64, w: 361, h: 60, pad: [0, 18], gap: 12, radius: "full", fill: "surface", shadow: "float", cross: "center" }, [icon("search", { size: 22 }), text("Şarj istasyonu, rota ara", "body", { color: "text-muted", w: "fill" }), icon("sliders-horizontal", { size: 22 })]),
  row({ name: "Filters", absolute: true, x: 16, y: 136, w: 377, gap: 10, clip: true }, [filt("150 kW+", "zap"), filt("Şimdi boş"), filt("Favoriler"), filt("Filtre")]),
  col({ name: "Map tools", absolute: true, x: 325, y: 360, gap: 12 }, [col({ name: "Zoom", w: 52, radius: "full", fill: "surface", shadow: "float", cross: "center" }, [row({ name: "In", h: 52, main: "center", cross: "center" }, [icon("plus", { size: 22 })]), row({ name: "Out", h: 52, main: "center", cross: "center" }, [icon("minus", { size: 22 })])]), round("navigation")]),
  col({ name: "Sheet", absolute: true, x: 0, y: 852 - 300, w: 393, h: 300, pad: [10, 24, 0, 24], gap: 10, radius: [28, 28, 0, 0], fill: "surface", shadow: "float" }, [
    row({ name: "Grabber wrap", w: "fill", main: "center" }, [rect({ name: "Grabber", w: 40, h: 5, radius: 3, fill: "line" })]),
    row({ name: "Name", gap: 8, cross: "end" }, [text("Kadıköy Şarj Merkezi", "title"), text("· 1,2 km", "body", { color: "text-muted" })]),
    row({ name: "Spec", gap: 10, cross: "center" }, [pill("4 / 6 boş", "success", "on-ok", "label"), text("350 kW'a kadar · CCS2", "body")]),
    row({ name: "Price", gap: 12, cross: "center" }, [text("₺8,90 / kWh", "body"), icon("coffee", { size: 18 }), icon("shopping-basket", { size: 18 }), icon("baby", { size: 18 })]),
    row({ name: "Go", w: "fill", h: 60, gap: 10, radius: "full", fill: "ink", main: "center", cross: "center" }, [icon("navigation", { size: 20, color: "white" }), text("Yol tarifini başlat", "label", { color: "white" })]),
  ]),
  instance(homeIndicator, { name: "Home indicator", absolute: true, x: 0, y: 818 }),
]), { page: "Screens" });
```

## terminal: a trading desk

True black, white figures, one green for up and one red for down, both
also as soft tinted pills. A brand word with a caret, the total in very
large bold figures with the day's change in green under it, a period
segmented control with the chosen one a green tinted pill, the area
chart over a grid, then the watchlist: ticker bold, name muted, a spark
in the row's colour, price in mono, change in a tinted pill. Signature:
the glowing area chart and the pills.

```js
colors({ bg: ["#050505", "#050505"], surface: ["#101112", "#101112"], "surface-2": ["#1A1C1D", "#1A1C1D"], text: ["#F5F6F6", "#F5F6F6"], "text-muted": ["#9AA0A3", "#9AA0A3"], line: ["#1F2224", "#1F2224"], brand: ["#2FD08A", "#2FD08A"], "on-brand": ["#04170C", "#04170C"], danger: ["#F2555A", "#F2555A"], success: ["#2FD08A", "#2FD08A"], "up-soft": ["#0F2E20", "#0F2E20"], "down-soft": ["#341416", "#341416"] });
type({ family: "Inter", styles: { display: { size: 46, weight: 800, line: 52, tracking: -1.2 }, title: { size: 24, weight: 800, line: 30 }, headline: { size: 18, weight: 700, line: 22 }, body: { size: 15, weight: 400, line: 20 }, label: { size: 16, weight: 500, line: 20, family: "JetBrains Mono" }, caption: { size: 13, weight: 500, line: 16 }, overline: { size: 13, weight: 600, line: 16, tracking: 0.6, transform: "upper" } } });
const tick = (sym, name, price, chg, up, seed) => row({ name: sym, w: "fill", h: 64, pad: [0, 20], gap: 10, cross: "center" }, [
  col({ name: "Who", w: 112, gap: 2 }, [text(sym, "headline"), text(name, "caption", { color: "text-muted" })]),
  chart("spark", seed, { w: 72, h: 28, color: up ? "brand" : "danger" }),
  text(price, "label", { w: "fill", align: "right" }),
  row({ name: "Change", w: 82, h: 32, radius: "sm", fill: up ? "up-soft" : "down-soft", main: "center", cross: "center" }, [text(chg, "label", { color: up ? "brand" : "danger" })]),
]);
const period = (p, on) => row({ name: p, w: "fill", h: 40, radius: "full", fill: on ? "up-soft" : null, main: "center", cross: "center" }, [text(p, "headline", { color: on ? "brand" : "text-muted" })]);
place(screen("Terminal", { device: "iphone", fill: "bg", gap: 12, pad: 0 }, chrome([
  instance(statusBar, { name: "Status bar" }),
  row({ name: "Head", w: "fill", pad: [0, 20], gap: 6, cross: "center" }, [text("Terminal", "title", { w: "fill" }), icon("search", { size: 24 }), icon("plus", { size: 26 })]),
  col({ name: "Total", w: "fill", pad: [0, 20], gap: 2 }, [text("Toplam portföy", "overline", { color: "text-muted" }), text("₺4.268.015,40", "display"), text("+₺58.320,12 (+%1,38) bugün", "body", { color: "brand" })]),
  row({ name: "Periods", w: "fill", pad: [0, 20] }, [period("1G", true), period("1H"), period("1A"), period("1Y"), period("Tümü")]),
  row({ name: "Inset", w: "fill", pad: [0, 20] }, [chart("area", [20, 18, 23, 24, 22, 26, 25, 29, 28, 31, 30, 34, 33, 36, 39, 38, 42, 44], { w: 353, h: 150, grid: 3, glow: true })]),
  col({ name: "Watchlist", w: "fill", gap: 0 }, [
    row({ name: "Head", w: "fill", pad: [8, 20], main: "between" }, [text("İzleme listesi", "overline", { color: "text-muted" }), text("Düzenle", "body", { color: "text-muted" })]),
    rect({ name: "Rule", w: 393, h: 1, fill: "line" }),
    tick("THYAO", "Türk Hava Yolları", "₺312,50", "+%1,12", true, [3, 4, 3.6, 4.4, 4.2, 5, 5.4]),
    tick("ASELS", "Aselsan", "₺148,20", "+%4,15", true, [2, 2.4, 2.2, 3, 3.6, 4.4, 5.6]),
    tick("BIMAS", "BİM Mağazalar", "₺512,00", "−%1,24", false, [6, 5.4, 5.6, 4.8, 4.6, 4, 3.6]),
    tick("TUPRS", "Tüpraş", "₺171,05", "−%2,31", false, [5, 5.2, 4.4, 4.6, 3.8, 3.4, 3.1]),
  ]),
  tabsBar([["Terminal", "terminal"], ["Listeler", "list"], ["Al-sat", "arrow-left-right"], ["Piyasalar", "trending-up"], ["Diğer", "ellipsis"]], 0),
])), { page: "Screens" });
```

## flight: a live flight over the night

A dark navy world map (an `image` "minimal dark navy world map, country
borders as faint lines, no labels") with the route lit from origin to the
plane and dotted after it, codes at each end. Panels are glass: a navy
fill at high opacity, a hairline light edge, a blur behind. Top: a glass
bar with back, a spaced title and a search button. Bottom: the flight
number with the airline mark and the status in green, the two times huge
with gate and terminal, a lit progress line with the plane on it and the
remaining time, and three stats. Signature: `arcRoute()` + glass.

```js
colors({ bg: ["#0C1424", "#0C1424"], surface: ["#18233A", "#18233A"], "surface-2": ["#24314D", "#24314D"], text: ["#F2F5FA", "#F2F5FA"], "text-muted": ["#A6B1C6", "#A6B1C6"], line: ["#3B4A68", "#3B4A68"], brand: ["#A9CCFF", "#A9CCFF"], "on-brand": ["#0C1424", "#0C1424"], danger: ["#E30A17", "#E30A17"], success: ["#4BE39A", "#4BE39A"], land: ["#16213A", "#16213A"], glass: ["#1E2A44E6", "#1E2A44E6"], white: ["#FFFFFF", "#FFFFFF"] });
type({ family: "Inter", styles: { display: { size: 40, weight: 600, line: 44, tracking: -1 }, title: { size: 24, weight: 700, line: 30 }, headline: { size: 18, weight: 600, line: 24 }, body: { size: 15, weight: 400, line: 20 }, label: { size: 14, weight: 600, line: 18, tracking: 1.4, transform: "upper" }, caption: { size: 13, weight: 500, line: 16 }, overline: { size: 12, weight: 600, line: 16, tracking: 1, transform: "upper" } } });
const glassy = (props, kids) => (props.dir === "row" ? row : col)(Object.assign({ radius: "lg", fill: "glass", stroke: { color: "line", width: 1 } }, props), kids);
const stat = (k, v) => col({ name: k, w: "fill", gap: 2 }, [text(k, "overline", { color: "text-muted" }), text(v, "headline")]);
place(screen("Flight", { device: "iphone", fill: "bg", gap: 0, pad: 0 }, [
  frame({ name: "Map/world", absolute: true, x: 0, y: 0, w: 393, h: 852, fill: "land" }, [pattern("dots", { w: 393, h: 852, gap: 10, size: 1, color: "surface-2", absolute: true, x: 0, y: 0 })]),
  arcRoute([0.15, 0.25], [0.8, 0.47], { w: 393, h: 852, bend: 0.2, progress: 0.55, labels: ["LHR", "IST"], labelStyle: "headline", absolute: true, x: 0, y: 0 }),
  topStatus("text"),
  glassy({ dir: "row", name: "Top bar", absolute: true, x: 20, y: 60, w: 353, h: 56, pad: [0, 8, 0, 16], cross: "center", gap: 12 }, [icon("chevron-left", { size: 22 }), text("Canlı uçuş takibi", "label", { color: "text-muted", w: "fill", align: "center" }), row({ name: "Search", pad: [8, 14], radius: "md", fill: "surface-2" }, [text("Ara", "headline")])]),
  col({ name: "Panels", absolute: true, x: 20, y: 440, w: 353, gap: 14 }, [
    glassy({ dir: "row", name: "Flight", w: "fill", pad: [16, 20], gap: 12, cross: "center" }, [text("TK 1985", "title"), ellipse({ name: "Mark", w: 28, h: 28, fill: "danger" }), text("· Zamanında", "title", { color: "success" })]),
    glassy({ name: "Times", w: "fill", pad: [16, 20], gap: 12 }, [
      row({ name: "Row", w: "fill", main: "between", cross: "end" }, [col({ name: "From", gap: 0 }, [text("14:25", "display"), text("IST · Kapı F4", "body", { color: "text-muted" })]), icon("arrow-right", { size: 22, color: "text-muted" }), col({ name: "To", gap: 0, cross: "end" }, [text("16:35", "display"), text("LHR · T5", "body", { color: "text-muted" })])]),
      frame({ name: "Progress", w: "fill", h: 24 }, [rect({ name: "Track", absolute: true, x: 0, y: 10, w: 313, h: 4, radius: 2, fill: "surface-2" }), rect({ name: "Flown", absolute: true, x: 0, y: 10, w: 204, h: 4, radius: 2, fill: "brand", effects: [{ type: "drop-shadow", x: 0, y: 0, blur: 8, color: "#A9CCFFCC" }] }), icon("plane", { size: 24, color: "brand", absolute: true, x: 196, y: 0, rotation: 45 })]),
      row({ name: "Left", w: "fill", main: "between" }, [text("%65 · 1 sa 12 dk kaldı", "body", { color: "text-muted" }), text("Varış 16:35", "body", { color: "text-muted" })]),
    ]),
    glassy({ dir: "row", name: "Stats", w: "fill", pad: [14, 20], gap: 12 }, [stat("İrtifa", "36.000 ft"), stat("Hız", "824 km/sa"), stat("Uçak", "A321neo")]),
  ]),
  instance(homeIndicator, { name: "Home indicator", absolute: true, x: 0, y: 818 }),
]), { page: "Screens" });
```

## recovery: a wearable's morning report

Near-black ground, dark cards with large radius, white numbers very big
with their units smaller beside them, one mint accent, uppercase muted
labels. A recovery card with the share and a mint ring, two half cards
(variability with a mint area curve; sleep with a stage bar in four
blues and a mint), and a resting heart card with grey bars, the latest in
mint with its value in a tooltip. Signature: number + unit pairs and the
ring.

```js
colors({ bg: ["#0B0D12", "#0B0D12"], surface: ["#181B22", "#181B22"], "surface-2": ["#2A2E37", "#2A2E37"], text: ["#F4F6F8", "#F4F6F8"], "text-muted": ["#A0A6B1", "#A0A6B1"], line: ["#2A2E37", "#2A2E37"], brand: ["#55E0B5", "#55E0B5"], "on-brand": ["#04170F", "#04170F"], danger: ["#FF6B6B", "#FF6B6B"], success: ["#55E0B5", "#55E0B5"], "ok-soft": ["#173A31", "#173A31"], s1: ["#26357E", "#26357E"], s2: ["#3355B8", "#3355B8"], s3: ["#4A7FD8", "#4A7FD8"], s4: ["#4FB2E6", "#4FB2E6"] });
type({ family: "Inter", styles: { display: { size: 56, weight: 800, line: 60, tracking: -1.6 }, title: { size: 32, weight: 800, line: 38, tracking: -0.6 }, headline: { size: 24, weight: 700, line: 30 }, body: { size: 16, weight: 400, line: 22 }, label: { size: 15, weight: 600, line: 20 }, caption: { size: 13, weight: 600, line: 16 }, overline: { size: 13, weight: 700, line: 16, tracking: 1, transform: "upper" } } });
const num = (v, unit, style) => row({ name: "Value", gap: 4, cross: "end" }, [text(v, style || "display"), text(unit, "headline")]);
place(screen("Recovery", { device: "iphone", fill: "bg", gap: 12, pad: 0 }, chrome([
  instance(statusBar, { name: "Status bar" }),
  row({ name: "Head", w: "fill", pad: [0, 20], cross: "center" }, [col({ name: "Title", w: "fill", gap: 2 }, [text("Uyku ve toparlanma", "title"), text("Bugün, 16 Kasım", "caption", { color: "text-muted" })]), instance(avatar, { name: "Avatar", text: { Initials: "AY" } })]),
  col({ name: "Cards", w: "fill", pad: [0, 16], gap: 12 }, [
    row({ name: "Recovery", w: "fill", h: 172, pad: [18, 20], radius: "lg", fill: "surface", cross: "center" }, [
      col({ name: "Copy", w: "fill", gap: 6 }, [text("Toparlanma", "caption", { color: "text-muted" }), text("%88", "display"), pill("İdeal · antrenmana hazır", "ok-soft", "brand", "label")]),
      frame({ name: "Ring", w: 104, h: 104 }, [chart("ring", 0.88, { w: 104, weight: 8, color: "brand", track: "surface-2", absolute: true, x: 0, y: 0 }), text("%88", "headline", { absolute: true, x: 0, y: 37, w: 104, align: "center" })]),
    ]),
    row({ name: "Pair", w: "fill", gap: 12 }, [
      col({ name: "HRV", w: "fill", h: 214, pad: [16, 0, 16, 0], gap: 8, radius: "lg", fill: "surface", clip: true }, [row({ name: "Head", pad: [0, 16] }, [text("Değişkenlik", "caption", { color: "text-muted" })]), row({ name: "Pad", pad: [0, 16] }, [num("64", "ms", "title")]), chart("area", [3, 4, 3.2, 2.6, 4.6, 5.2, 3.8, 5.4], { w: 172, h: 50 }), row({ name: "Foot", pad: [0, 16] }, [text("Düne göre +4 ms", "body", { color: "brand" })])]),
      col({ name: "Sleep", w: "fill", h: 214, pad: 16, gap: 12, radius: "lg", fill: "surface" }, [text("Uyku", "caption", { color: "text-muted" }), row({ name: "Value", gap: 2, cross: "end" }, [text("7", "title"), text("sa", "body"), text("42", "title"), text("dk", "body")]), chart("zones", ["s1", "s2", "s3", "s4", "brand"], { w: 140, widths: [0.4, 0.12, 0.15, 0.1, 0.23], bar: 36, gap: 0, radius: 6 }), text("Uyku puanı 84", "body", { color: "text-muted" })]),
    ]),
    row({ name: "Heart", w: "fill", h: 172, pad: [18, 20], radius: "lg", fill: "surface", cross: "end" }, [
      col({ name: "Copy", w: "fill", gap: 4 }, [text("Dinlenik nabız", "caption", { color: "text-muted" }), num("52", "bpm"), text("İdeal · ort. 54 bpm", "body", { color: "text-muted" })]),
      chart("bars", [5, 4.8, 6, 4.4, 5.2, 4.6, 3.9], { w: 140, h: 108, gap: 6, radius: 8, values: ["", "", "", "", "", "", "52"], track: "surface-2" }),
    ]),
  ]),
  tabsBar([["Ana Sayfa", "house"], ["Analiz", "chart-column"], ["Aktivite", "flame"], ["Profil", "user"]], 0),
])), { page: "Screens" });
```

## flashcard: one word at a time

A pale blue-white ground, a close icon, the lesson and the count, a
thick blue progress bar with the share inside it, then one white card
with a soft shadow holding a friendly illustration, the word very large
and bold, the phonetics in grey and an audio pill with a waveform. One big
blue button and a quiet skip link. Signature: the single card. Art:
`image` "Friendly flat vector illustration with thin brown outlines and
soft shading, no text: a cup of coffee on a saucer with steam".

```js
colors({ bg: ["#F4F7FB", "#0E1218"], surface: ["#FFFFFF", "#171C24"], "surface-2": ["#E3EDFC", "#1E2A3D"], text: ["#1D2738", "#EEF2F8"], "text-muted": ["#5A6475", "#A9B2C1"], line: ["#E2E7EF", "#2A313D"], brand: ["#2F6FE4", "#7AA8FF"], "on-brand": ["#FFFFFF", "#071530"], danger: ["#C8102E", "#FF6B7D"], success: ["#1E7B45", "#6FD6A0"], track: ["#E1E7F0", "#232A35"], cream: ["#F3E6D3", "#3A3026"] });
type({ family: "Inter", styles: { display: { size: 44, weight: 800, line: 50, tracking: -1 }, title: { size: 24, weight: 700, line: 30 }, headline: { size: 18, weight: 600, line: 24 }, body: { size: 22, weight: 400, line: 28 }, label: { size: 18, weight: 700, line: 24 }, caption: { size: 15, weight: 600, line: 20 }, overline: { size: 12, weight: 600, line: 16 } } });
place(screen("Flashcard", { device: "iphone", fill: "bg", gap: 20, pad: 0 }, chrome([
  instance(statusBar, { name: "Status bar" }),
  row({ name: "Nav", w: "fill", pad: [0, 20], cross: "center" }, [icon("x", { size: 26, color: "text-muted" }), text("Almanca · Alıştırma", "headline", { w: "fill", align: "center" }), text("13 / 20", "headline", { color: "text-muted" })]),
  row({ name: "Inset", w: "fill", pad: [0, 20] }, [frame({ name: "Progress", w: "fill", h: 22, radius: "full", fill: "track", clip: true }, [row({ name: "Done", w: 230, h: 22, pad: [0, 10], radius: "full", fill: "brand", main: "end", cross: "center" }, [text("%65", "caption", { color: "on-brand" })])])]),
  row({ name: "Inset", w: "fill", pad: [0, 20] }, [col({ name: "Card", w: "fill", h: 400, pad: [32, 24], gap: 12, radius: "lg", fill: "surface", stroke: "line", shadow: "raised", cross: "center" }, [
    frame({ name: "Art", w: 170, h: 150, radius: "lg", fill: "cream" }),
    text("das Frühstück", "display"),
    text("/ˈfryːʃtʏk/", "body", { color: "text-muted" }),
    row({ name: "Audio", pad: [12, 24], gap: 12, radius: "full", fill: "surface-2", cross: "center" }, [icon("volume-2", { size: 24, color: "brand" }), chart("wave", [0.3, 0.6, 0.4, 0.9, 0.5, 1, 0.7, 0.4, 0.3, 0.5, 0.3, 0.2], { w: 96, h: 28, gap: 4, color: "brand" })]),
  ])]),
  col({ name: "Actions", w: "fill", pad: [32, 20, 0, 20], gap: 20, cross: "center" }, [
    row({ name: "Speak", w: "fill", h: 64, gap: 10, radius: "lg", fill: "brand", main: "center", cross: "center" }, [text("Şimdi söyle", "label", { color: "on-brand" }), icon("mic", { size: 22, color: "on-brand" })]),
    text("Geçmek için dokun", "caption", { color: "text-muted" }),
  ]),
])), { page: "Screens" });
```

## inbox: a dense dark work list

Near-black, rows divided by hairlines that start after the priority
column, a bold title with the count, a filter and a search icon, three
outlined filter chips with the chosen one filled. Each row: a priority
mark coloured by urgency (orange double chevron, red up arrow, grey
chevron, blue down arrow), a muted mono id, the title in white, a status
pill tinted by state (amber in progress, blue review, grey backlog, red
blocked), the time, an avatar or initials at the right and the comment
count. Signature: the rhythm of the rows.

```js
colors({ bg: ["#0E0F11", "#0E0F11"], surface: ["#17181B", "#17181B"], "surface-2": ["#26282C", "#26282C"], text: ["#F1F2F4", "#F1F2F4"], "text-muted": ["#9A9EA6", "#9A9EA6"], line: ["#26282C", "#26282C"], brand: ["#F1F2F4", "#F1F2F4"], "on-brand": ["#0E0F11", "#0E0F11"], danger: ["#F2555A", "#F2555A"], success: ["#43D17A", "#43D17A"], urgent: ["#F28C38", "#F28C38"], low: ["#7C9CC9", "#7C9CC9"], "amber-soft": ["#3D3115", "#3D3115"], amber: ["#F2C14E", "#F2C14E"], "blue-soft": ["#1C2E4A", "#1C2E4A"], blue: ["#86B2F2", "#86B2F2"], "grey-soft": ["#2A2C30", "#2A2C30"], "red-soft": ["#3E1A1C", "#3E1A1C"], av1: ["#6B5446", "#6B5446"], av2: ["#3B5578", "#3B5578"] });
type({ family: "Inter", styles: { display: { size: 36, weight: 800, line: 42, tracking: -0.8 }, title: { size: 16, weight: 500, line: 22 }, headline: { size: 17, weight: 600, line: 22 }, body: { size: 14, weight: 400, line: 20 }, label: { size: 14, weight: 500, line: 18 }, caption: { size: 13, weight: 500, line: 16 }, overline: { size: 13, weight: 500, line: 16, family: "JetBrains Mono" } } });
const prio = { urgent: ["chevrons-right", "urgent"], high: ["arrow-up", "danger"], mid: ["chevron-right", "text-muted"], low: ["arrow-down", "low"] };
const issue = (p, id, title, state, when, who) => row({ name: id, w: "fill", gap: 14, pad: [12, 20, 12, 16], cross: "start" }, [
  icon(prio[p][0], { size: 22, color: prio[p][1] }),
  col({ name: "Copy", w: "fill", gap: 4 }, [
    text(id, "overline", { color: "text-muted" }), text(title, "title", { w: "fill" }),
    row({ name: "Meta", gap: 10, cross: "center" }, [pill(state[0], state[1], state[2], "label"), when ? text(when, "body", { color: "text-muted" }) : null]),
    rect({ name: "Rule", w: "fill", h: 1, fill: "line" }),
  ]),
  row({ name: "Who", w: 40, h: 40, radius: "full", fill: who[1], main: "center", cross: "center" }, [text(who[0], "label")]),
]);
place(screen("Inbox", { device: "iphone", fill: "bg", gap: 12, pad: 0 }, chrome([
  instance(statusBar, { name: "Status bar" }),
  row({ name: "Head", w: "fill", pad: [0, 20], gap: 18, cross: "center" }, [row({ name: "Title", w: "fill", gap: 8, cross: "end" }, [text("Gelen", "display"), text("· 6", "display", { color: "text-muted" })]), row({ name: "Filter", gap: 6, cross: "center" }, [icon("list-filter", { size: 22, color: "text-muted" }), text("Filtre", "label", { color: "text-muted" })]), icon("search", { size: 24, color: "text-muted" })]),
  row({ name: "Chips", w: "fill", pad: [0, 20], gap: 8 }, [row({ name: "On", pad: [8, 14], radius: "full", fill: "surface-2" }, [text("Bana atanan", "label")]), row({ name: "Chip", pad: [8, 14], radius: "full", stroke: "surface-2" }, [text("Bahsedilen", "label", { color: "text-muted" })]), row({ name: "Chip", pad: [8, 14], radius: "full", stroke: "surface-2" }, [text("Okunmamış", "label", { color: "text-muted" })])]),
  col({ name: "List", w: "fill", gap: 0 }, [
    rect({ name: "Rule", w: 393, h: 1, fill: "line" }),
    issue("urgent", "ENG-412", "Soket bağlantısını hızlandır", ["Sürüyor", "amber-soft", "amber"], "2 sa önce", ["AM", "av1"]),
    issue("high", "ENG-410", "Grafikte bellek sızıntısı", ["Sürüyor", "amber-soft", "amber"], "", ["DK", "av2"]),
    issue("mid", "ENG-407", "Kimlik katmanını düzenle", ["İncelemede", "blue-soft", "blue"], "", ["KD", "av2"]),
    issue("low", "ENG-405", "Çekmece animasyonunu hızlandır", ["Bekleyen", "grey-soft", "text-muted"], "", ["JL", "surface-2"]),
    issue("high", "ENG-398", "Testte kilitlenen işlemi çöz", ["Engelli", "red-soft", "danger"], "1 gün önce", ["EM", "av1"]),
  ]),
  tabsBar([["Gelen", "inbox"], ["Konular", "list"], ["Arama", "search"], ["Profil", "user"]], 0),
])), { page: "Screens" });
```

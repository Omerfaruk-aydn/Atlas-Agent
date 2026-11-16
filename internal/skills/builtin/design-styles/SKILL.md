---
name: design-styles
description: Ten proven phone-app styles as working design scripts on top of the design-kit - calm public service, kids learning, dark sport data, transit ticket, food delivery, full-bleed travel, soft wellness, dark fintech, editorial news, music player - each with its palette, type, structure, signature detail, picture prompts and the mistakes to avoid. Load when the brief names one of these kinds of app, or to pick a style before writing the system.
metadata:
  provenance: atlas-authored
---

# Design styles

Each style below is a whole screen in a design script. Run the design-kit
script first (it defines the components and the chrome), then this file's
**Shared** block, then one style's block. Change the copy, the hues and one
shape rule to the brief; keep the structure and the signature detail, which
are what make the style read as itself.

Every style was measured against a reference screen at the quality bar.
What lifts a screen from a kit demo to that bar is the same every time:

- **One signature object** drawn from the subject, big: the QR on a ticket,
  the route on a run, the balance curve, the album cover, the trail of
  levels. Use the helpers: `qr()`, `chart("area" | "bars" | "zones" |
  "wave" | "ring")`, `map()`, `trail()`, `notches()`, `scrim()`,
  `pattern("stars")`, `sprig()`.
- **Real pictures** where the subject is visual: `design_canvas photo` for
  places, food and news; `design_canvas image` for illustrations (one style
  sentence for every picture of the file). The blocks below hold a `Photo`
  or `Art` layer you fill with `fills: { Photo: { type: "image", src } }`
  or `image(src, {...})`.
- **Numbers set big and exact**: "₺48.250,75", "10,42 km", "08:40". Turkish
  formats: decimal comma, thousands dot, "16 Kasım 2026", "₺12.400".
- **One language**: the references slipped ("Start", "Finish", "Bazyit",
  an emoji in a promo). Never: every label in the person's language, no
  emoji, no invented words, brand names spelled as the brand spells them.

When the project has a reference screen for the style (for example
`.atlas/design/benchmarks/<style>.jpg`), render, then run `design_canvas`
`compare` with `path` set to it, and close the gaps the comparison shows
before calling the screen done.

Pick by the app's job, not by taste:

| Style | For | Feeling |
| --- | --- | --- |
| civic | public services, banks' simple flows, utilities | calm, sure, accessible |
| kids | learning, games for children | playful, chunky, safe |
| sport | runs, rides, workouts, any metric-heavy recap | dark, fast, proud |
| ticket | transit, events, boarding passes | official, scannable |
| food | delivery, restaurants, groceries | warm, appetising, busy |
| travel | destinations, hotels, experiences | wide, sunlit, aspirational |
| wellness | meditation, habits, health | soft, slow, kind |
| fintech | balances, cards, payments | dark, precise, premium |
| editorial | news, magazines, long reads | serious, typographic |
| player | music, podcasts, audiobooks | immersive, cover-led |

## Shared

```js
// A tab bar from the kit's Tab: items are [label, icon], on is the index.
function tabsBar(items, on, y) {
  return row({ name: "Tab bar", absolute: true, x: 0, y: y === undefined ? 852 - 92 : y, w: 393, h: 92, pad: [8, 8, 32, 8], main: "between", fill: "surface" },
    items.map(function (it, i) {
      return instance(tab, { name: it[0], w: "fill", variant: { state: i === on ? "selected" : "default" }, text: { Label: it[0] }, icons: { Icon: it[1] } });
    }));
}
// The status bar in any ink, for a dark ground or a photo.
function statusBarIn(color, extra) {
  return row(Object.assign({ name: "Status bar", pad: [16, 32, 0, 32], w: 393, h: 59, main: "between", cross: "center" }, extra || {}), [
    text("9:41", "label", { color: color }),
    row({ name: "Indicators", gap: 8, cross: "center" }, [icon("signal", { size: 16, color: color }), icon("wifi", { size: 16, color: color }), icon("battery-full", { size: 20, color: color })]),
  ]);
}
function chrome(children, h) {
  return children.concat([instance(homeIndicator, { name: "Home indicator", absolute: true, x: 0, y: (h || 852) - 34 })]);
}
```

## civic: public services

Calm and sure: white cards on a cool grey ground, one institutional red,
grouped lists with a soft icon plate, status pills in tinted green and
amber, generous 16 px insets. No decoration; the order of the lists is the
design. Signature: the status pills and the emblem in the header.

```js
colors({ bg: ["#F3F4F7", "#0E1014"], surface: ["#FFFFFF", "#171A20"], "surface-2": ["#ECEEF3", "#232731"], text: ["#151821", "#EEF0F5"], "text-muted": ["#5C6270", "#A3A9B6"], line: ["#E3E6EC", "#2C313C"], brand: ["#C8102E", "#FF6B7D"], "on-brand": ["#FFFFFF", "#2A0008"], danger: ["#C8102E", "#FF6B7D"], success: ["#1E7B45", "#6FD6A0"], "ok-soft": ["#DFF3E6", "#163324"], "ok-ink": ["#1E6B3E", "#8FE0B0"], "wait-soft": ["#FCEFD7", "#3A2C12"], "wait-ink": ["#8A5A0B", "#F2C46B"] });
type({ family: "Figtree", styles: { display: { size: 34, weight: 800, line: 40, tracking: -0.8 }, title: { size: 22, weight: 700, line: 28, tracking: -0.3 }, headline: { size: 17, weight: 700, line: 22 }, body: { size: 15, weight: 400, line: 21 }, label: { size: 15, weight: 600, line: 20 }, caption: { size: 13, weight: 500, line: 16 }, overline: { size: 12, weight: 700, line: 16, tracking: 0.6, transform: "upper" } } });
const status = (label, tone) => row({ name: "Status", pad: [4, 12], h: 28, cross: "center", radius: "full", fill: tone + "-soft" }, [text(label, "caption", { color: tone + "-ink" })]);
place(screen("Civic", { device: "iphone", fill: "bg", gap: 0, pad: 0 }, chrome([
  instance(statusBar, { name: "Status bar" }),
  col({ name: "Header", w: "fill", gap: 12, pad: [8, 16, 16, 16], fill: "surface" }, [
    row({ name: "Brand", w: "fill", gap: 12, cross: "center" }, [
      row({ name: "Emblem", w: 40, h: 40, radius: "full", fill: "brand", main: "center", cross: "center" }, [icon("landmark", { size: 22, color: "on-brand" })]),
      text("e-Hizmetler", "title", { w: "fill" }),
      icon("bell", { size: 24, color: "text" }),
    ]),
    instance(searchField, { name: "Search", w: "fill", text: { Value: "Hizmet, kurum veya kelime ara" } }),
  ]),
  col({ name: "Content", w: "fill", gap: 12, pad: [20, 16] }, [
    text("Sık kullanılanlar", "headline"),
    col({ name: "Favourites", w: "fill", radius: "lg", fill: "surface", clip: true }, [
      instance(listRow, { w: "fill", icons: { Icon: "file-user" }, text: { Title: "Nüfus kayıt örneği", Meta: "e-Devlet Kapısı" } }),
      instance(listRow, { w: "fill", icons: { Icon: "receipt" }, text: { Title: "Vergi borcu sorgulama", Meta: "Gelir İdaresi" } }),
      instance(listRow, { w: "fill", icons: { Icon: "calendar-heart" }, text: { Title: "Hastane randevusu al", Meta: "Sağlık Bakanlığı" } }),
    ]),
    text("Başvurularım", "headline"),
    col({ name: "Applications", w: "fill", radius: "lg", fill: "surface", clip: true }, [
      row({ name: "Application", w: "fill", h: 56, pad: [0, 16], cross: "center", gap: 12 }, [text("İkametgâh belgesi", "body", { w: "fill" }), status("Onaylandı", "ok")]),
      row({ name: "Application", w: "fill", h: 56, pad: [0, 16], cross: "center", gap: 12 }, [text("Sürücü belgesi yenileme", "body", { w: "fill" }), status("Bekliyor", "wait")]),
    ]),
  ]),
  tabsBar([["Ana Sayfa", "house"], ["Hizmetler", "list"], ["Mesajlar", "message-circle"], ["Belgelerim", "file-text"], ["Profilim", "user"]], 0),
])), { page: "Screens" });
```

## kids: learning for children

A sky and a hill, a mascot that greets the child by name, a winding trail
of numbered levels (done ones bright with a star, the current one ringed,
the next ones grey with a lock), then three chunky subject buttons and a
reward bar. Rounded heavy type (Fustat 800), saturated primaries, every
target at least 56 px. Signature: `trail()` and the mascot.
Mascot and subject art: `image` with "Cute flat vector children's book
illustration, thick clean outlines, soft shading, no text, no letters: an
owl with round glasses holding a book, waving".

```js
colors({ bg: ["#BFE6FB", "#0F2433"], surface: ["#FFFFFF", "#18324A"], "surface-2": ["#E8F4FB", "#21405A"], text: ["#22205A", "#F1F0FF"], "text-muted": ["#5E5C8F", "#B8B6E6"], line: ["#D2E4F0", "#2C4A63"], brand: ["#2B7BEF", "#7DB4FF"], "on-brand": ["#FFFFFF", "#05172F"], danger: ["#E5483C", "#FF8A80"], success: ["#2FA84F", "#7BE09A"], hill: ["#F6CF4E", "#7A6420"], road: ["#FFF1BF", "#5E5530"], math: ["#F0533E", "#F0533E"], read: ["#5B4BE0", "#5B4BE0"], sci: ["#33B249", "#33B249"], locked: ["#C9C3BC", "#5A5650"], gold: ["#F6B92B", "#F6B92B"], white: ["#FFFFFF", "#FFFFFF"] });
type({ family: "Fustat", styles: { display: { size: 30, weight: 800, line: 36 }, title: { size: 24, weight: 800, line: 30 }, headline: { size: 18, weight: 800, line: 24 }, body: { size: 16, weight: 600, line: 22 }, label: { size: 16, weight: 800, line: 20 }, caption: { size: 13, weight: 700, line: 16 }, overline: { size: 12, weight: 800, line: 16 } } });
// A tall subject stacks its art over the name; a short one sets them side by side.
const subject = (name, label, fill, ic, h) => h > 100
  ? col({ name: name, w: "fill", h: h, pad: [16, 20], main: "between", radius: "lg", fill: fill, shadow: "raised" }, [icon(ic, { size: 48, color: "white" }), text(label, "title", { color: "white", w: "fill" })])
  : row({ name: name, w: "fill", h: h, pad: [0, 20], gap: 12, cross: "center", radius: "lg", fill: fill, shadow: "raised" }, [text(label, "title", { color: "white", w: "fill" }), icon(ic, { size: 32, color: "white" })]);
place(screen("Kids", { device: "iphone", fill: "bg", gap: 0, pad: 0 }, chrome([
  instance(statusBar, { name: "Status bar" }),
  frame({ name: "World", w: "fill", h: 420 }, [
    ellipse({ name: "Hill", absolute: true, x: -120, y: 250, w: 633, h: 400, fill: "hill" }),
    row({ name: "Mascot", absolute: true, x: 24, y: 8, w: 120, h: 120, radius: "full", fill: "surface-2", main: "center", cross: "center" }, [icon("bird", { size: 64, color: "text" })]),
    row({ name: "Bubble", absolute: true, x: 160, y: 28, pad: [12, 20], radius: "full", fill: "surface" }, [text("Merhaba Deniz!", "title")]),
    trail([[0.14, 0.86], [0.42, 0.9], [0.3, 0.6], [0.64, 0.74], [0.86, 0.5], [0.56, 0.36]], { name: "Trail", absolute: true, x: 16, y: 120, w: 361, h: 280, width: 26, color: "road", labelStyle: "title",
      nodes: [{ label: 1, color: "math", ink: "white" }, { label: 2, color: "read", ink: "white" }, { label: 3, color: "brand", ink: "white", ring: "white", size: 56 }, { label: 4, color: "locked", ink: "white" }, { label: 5, color: "locked", ink: "white" }, { label: 6, color: "locked", ink: "white" }] }),
  ]),
  col({ name: "Subjects", w: "fill", gap: 12, pad: [0, 16] }, [
    row({ name: "Row", w: "fill", gap: 12 }, [
      subject("Math", "Matematik", "math", "calculator", 132),
      col({ name: "Side", w: "fill", gap: 12 }, [subject("Read", "Okuma", "read", "book-open", 60), subject("Science", "Bilim", "sci", "flask-conical", 60)]),
    ]),
    row({ name: "Rewards", w: "fill", gap: 16, pad: [12, 16], cross: "center", radius: "lg", fill: "surface" }, [
      icon("gift", { size: 40, color: "gold" }),
      col({ name: "Progress", w: "fill", gap: 8 }, [
        text("Maceran", "headline"),
        frame({ name: "Bar", w: "fill", h: 16, radius: "full", fill: "surface-2", clip: true }, [rect({ name: "Fill", w: 150, h: 16, radius: 8, fill: "gold" })]),
        text("3 / 5 ödül", "caption", { color: "text-muted" }),
      ]),
    ]),
  ]),
  tabsBar([["Ana Sayfa", "house"], ["Ödüller", "gift"], ["Oyunlar", "gamepad-2"], ["Ayarlar", "settings"]], 0),
])), { page: "Screens" });
```

## sport: a workout recap on black

Black ground, one electric accent (lime) and one cool second (cyan), a
huge number in heavy grotesque with tight tracking, a stat strip divided by
hairlines, the route as a glowing line on a dark map, split bars with their
paces, heart-rate zones as a five-colour bar with a marker, and one
gradient button that glows. Signature: the number and the glowing route.

```js
colors({ bg: ["#0A0B0B", "#0A0B0B"], surface: ["#151718", "#151718"], "surface-2": ["#222527", "#222527"], text: ["#F5F7F6", "#F5F7F6"], "text-muted": ["#99A19E", "#99A19E"], line: ["#2A2E30", "#2A2E30"], brand: ["#C8F531", "#C8F531"], "on-brand": ["#0A0B0B", "#0A0B0B"], danger: ["#EF4444", "#EF4444"], success: ["#43D17A", "#43D17A"], cyan: ["#22E3F0", "#22E3F0"], "map-land": ["#16191B", "#16191B"], "map-water": ["#10263A", "#10263A"], "map-park": ["#14231A", "#14231A"], "map-road": ["#272B2E", "#272B2E"], "map-minor": ["#1E2224", "#1E2224"], z1: ["#5BC0F8", "#5BC0F8"], z2: ["#43D17A", "#43D17A"], z3: ["#F7D046", "#F7D046"], z4: ["#F7832A", "#F7832A"], z5: ["#EF4444", "#EF4444"] });
type({ family: "Geist", styles: { display: { size: 84, weight: 800, line: 84, tracking: -4 }, title: { size: 20, weight: 700, line: 26 }, headline: { size: 17, weight: 600, line: 22 }, body: { size: 15, weight: 400, line: 21 }, label: { size: 15, weight: 600, line: 20 }, caption: { size: 12, weight: 500, line: 16 }, overline: { size: 12, weight: 600, line: 16, tracking: 0.6 } } });
shadow("glow", [{ y: 0, blur: 24, color: "#C8F53166" }]);
const stat = (v, l) => col({ name: "Stat", w: "fill", gap: 2, cross: "center" }, [text(v, "title"), text(l, "caption", { color: "text-muted" })]);
place(screen("Sport", { device: "iphone", fill: "bg", gap: 16, pad: [0, 0, 0, 0] }, chrome([
  instance(statusBar, { name: "Status bar" }),
  row({ name: "Nav", w: "fill", pad: [0, 16], main: "between", cross: "center" }, [icon("chevron-left", { size: 24 }), text("Koşu özeti", "headline"), icon("sliders-horizontal", { size: 22 })]),
  col({ name: "Body", w: "fill", gap: 12, pad: [0, 16] }, [
    row({ name: "Distance", gap: 8, cross: "end", main: "center", w: "fill" }, [text("10,42", "display"), text("km", "title", { color: "text-muted" })]),
    row({ name: "Stats", w: "fill", pad: [12, 8], radius: "md", fill: "surface", stroke: "line" }, [stat("52:18", "süre"), rect({ name: "Rule", w: 1, h: 40, fill: "line" }), stat("5:01 /km", "tempo"), rect({ name: "Rule", w: 1, h: 40, fill: "line" }), stat("684", "kcal")]),
    map({ w: 361, h: 140, seed: "bebek", route: true, accent: "brand", ink: "text", halo: "surface", labelStyle: "caption", pins: [{ x: 0.22, y: 0.62, kind: "you" }, { x: 0.78, y: 0.3, label: "Bitiş" }] }),
    text("Kilometre tempoları", "headline"),
    chart("bars", [5.1, 4.9, 5.0, 5.0, 4.8, 4.0, 5.0, 4.6, 5.1, 5.9], { w: 361, h: 124, gap: 6, highlight: "all", behind: true, labels: ["1", "2", "3", "4", "5", "6", "7", "8", "9", "10"], values: ["5:10", "4:58", "5:02", "5:02", "4:48", "4:03", "5:02", "4:38", "5:08", "5:55"] }),
    row({ name: "Zones head", w: "fill", main: "between" }, [text("Nabız bölgeleri", "headline"), text("Aerobik · 158 bpm", "label", { color: "text-muted" })]),
    chart("zones", ["z1", "z2", "z3", "z4", "z5"], { w: 361, value: 0.66, labels: ["Dinlenme", "Isınma", "Yağ", "Aerobik", "Maks."] }),
  ]),
  row({ name: "Share", absolute: true, x: 16, y: 852 - 90, w: 361, h: 56, radius: "full", main: "center", cross: "center", gap: 8, shadow: "glow", fill: { type: "linear", angle: 0, stops: ["brand", "cyan"] } }, [text("Paylaş", "title", { color: "on-brand" }), icon("share", { size: 20, color: "on-brand" })]),
])), { page: "Screens" });
```

## ticket: a transit pass

Deep navy ground and one gold, a white ticket card with the route set huge
("Kadıköy → Karaköy"), the departure and the pier as the two big facts, a
real QR code, the tear line with bites out of both sides, the date and the
fare type under it. Below: the route on a small map and the next
departures with times in tabular figures. Signature: `qr()` + `notches()`.

```js
colors({ bg: ["#0E2350", "#0A1A3C"], surface: ["#FFFFFF", "#FFFFFF"], "surface-2": ["#EEF1F7", "#EEF1F7"], text: ["#0E1A33", "#0E1A33"], "text-muted": ["#5A6580", "#5A6580"], line: ["#D7DCE7", "#D7DCE7"], brand: ["#8C6519", "#8C6519"], "on-brand": ["#0E2350", "#0E2350"], gold: ["#C99A3B", "#E2B65A"], danger: ["#C8102E", "#FF6B7D"], success: ["#1E7B45", "#6FD6A0"], "on-bg": ["#FFFFFF", "#FFFFFF"], "on-bg-muted": ["#AFC0E3", "#AFC0E3"], panel: ["#183168", "#142A5A"], "map-land": ["#183168", "#142A5A"], "map-water": ["#2C5BA8", "#24508F"], "map-park": ["#1D3A75", "#1A346B"], "map-road": ["#24427F", "#203B72"], "map-minor": ["#1D3A75", "#1A346B"] });
type({ family: "Albert Sans", styles: { display: { size: 34, weight: 800, line: 40, tracking: -0.8 }, title: { size: 22, weight: 700, line: 28, tracking: -0.3 }, headline: { size: 17, weight: 700, line: 22 }, body: { size: 15, weight: 400, line: 21 }, label: { size: 15, weight: 600, line: 20 }, caption: { size: 13, weight: 500, line: 16 }, overline: { size: 12, weight: 700, line: 16, tracking: 0.6, transform: "upper" } } });
const departure = (t) => row({ name: "Departure", w: "fill", h: 52, cross: "center", gap: 12 }, [text(t, "title", { color: "on-bg" }), icon("ship", { size: 18, color: "on-bg-muted" }), text("Kadıköy → Karaköy", "body", { color: "on-bg", w: "fill" }), row({ name: "Seat", pad: [4, 12], radius: "full", fill: "panel" }, [text("Müsait", "caption", { color: "on-bg" })])]);
place(screen("Ticket", { device: "iphone", fill: "bg", gap: 16, pad: 0 }, chrome([
  statusBarIn("on-bg"),
  row({ name: "Title", w: "fill", pad: [0, 16], main: "between", cross: "end" }, [text("Biletlerim", "display", { color: "on-bg" }), text("Dijital bilet", "label", { color: "on-bg-muted" })]),
  col({ name: "Pass wrap", w: "fill", pad: [0, 16] }, [col({ name: "Pass", w: "fill", h: 380, radius: "lg", fill: "surface", clip: true, pad: [20, 20], gap: 12 }, [
    text("Kadıköy → Karaköy", "title", { w: "fill" }),
    row({ name: "Facts", w: "fill", main: "between" }, [
      col({ name: "Departure", gap: 2 }, [text("Kalkış", "caption", { color: "text-muted" }), text("08:40", "display", { color: "on-brand" })]),
      col({ name: "Pier", gap: 2, cross: "end" }, [text("İskele", "caption", { color: "text-muted" }), text("2", "display", { color: "on-brand" })]),
    ]),
    row({ name: "Code", w: "fill", main: "center" }, [qr("https://sehirhatlari.istanbul/bilet/2026-11-16-0840", { size: 132, color: "text" })]),
    notches({ w: 361, y: 318, r: 12, color: "bg" }),
    row({ name: "Meta", absolute: true, x: 20, y: 338, w: 321, main: "between" }, [text("16.11.2026", "label"), text("Tam bilet", "label", { color: "text-muted" })]),
  ])]),
  col({ name: "Next", w: "fill", pad: [0, 16], gap: 0 }, [text("Sonraki seferler", "headline", { color: "on-bg" }), departure("09:00"), departure("09:20")]),
  tabsBar([["Ana Sayfa", "house"], ["Biletlerim", "ticket"], ["Seferler", "clock"], ["Kartım", "credit-card"], ["Profil", "user"]], 1),
])), { page: "Screens" });
```

## food: delivery home

Cream ground, tomato red, round category photos in coloured discs, a promo
card with a cut-out food photo, restaurant cards led by a real photo with
the rating chip on it. Dense and appetising, but every card on one grid.
Signature: the round category row and the photos. Pictures: `photo` for
each dish; categories as `image` "3D food render, centred, soft studio
light, on a plain <hex> background, no text: a döner wrap".

```js
colors({ bg: ["#FFF7EE", "#17110D"], surface: ["#FFFFFF", "#211913"], "surface-2": ["#F6E9DA", "#2D231B"], text: ["#1F1410", "#FBEFE6"], "text-muted": ["#6E5A4E", "#BDA898"], line: ["#EFDFCF", "#3A2E25"], brand: ["#E8432B", "#FF7A62"], "on-brand": ["#FFFFFF", "#2A0A04"], danger: ["#C8102E", "#FF6B7D"], success: ["#2E8B57", "#6FD6A0"], c1: ["#E8432B", "#E8432B"], c2: ["#F59E2B", "#F59E2B"], c3: ["#D9643A", "#D9643A"], c4: ["#6B4A35", "#6B4A35"], c5: ["#3FA65A", "#3FA65A"], gold: ["#F6B92B", "#F6B92B"] });
type({ family: "Outfit", styles: { display: { size: 34, weight: 800, line: 40, tracking: -0.8 }, title: { size: 22, weight: 700, line: 28, tracking: -0.3 }, headline: { size: 17, weight: 700, line: 22 }, body: { size: 15, weight: 400, line: 21 }, label: { size: 15, weight: 600, line: 20 }, caption: { size: 13, weight: 500, line: 16 }, overline: { size: 12, weight: 700, line: 16, tracking: 0.6, transform: "upper" } } });
const cat = (label, fill) => col({ name: label, gap: 8, cross: "center", w: 64 }, [frame({ name: "Photo", w: 64, h: 64, radius: "full", fill: fill }), text(label, "caption")]);
const shop = (name, meta, h) => col({ name: "Restaurant", w: "fill", radius: "lg", fill: "surface", clip: true, shadow: "raised" }, [
  frame({ name: "Photo", w: "fill", h: h, fill: "surface-2" }, [row({ name: "Rating", absolute: true, x: 12, y: 12, pad: [4, 8], gap: 4, radius: "full", fill: "surface", cross: "center" }, [icon("star", { size: 14, color: "gold" }), text("4,8 (500+)", "caption")])]),
  col({ name: "Copy", pad: [12, 16], gap: 4, w: "fill" }, [text(name, "headline", { w: "fill" }), text(meta, "caption", { color: "text-muted" })]),
]);
place(screen("Food", { device: "iphone", fill: "bg", gap: 16, pad: 0 }, chrome([
  instance(statusBar, { name: "Status bar" }),
  col({ name: "Body", w: "fill", gap: 16, pad: [0, 16] }, [
    row({ name: "Address", w: "fill", gap: 8, cross: "center" }, [icon("map-pin", { size: 20, color: "brand" }), text("Kadıköy, Moda Cd.", "headline", { w: "fill" }), icon("shopping-bag", { size: 22 })]),
    instance(searchField, { name: "Search", w: "fill", fill: "surface", text: { Value: "Ne yemek istersin?" } }),
    row({ name: "Categories", w: "fill", main: "between" }, [cat("Döner", "c1"), cat("Pide", "c2"), cat("Tatlı", "c3"), cat("Kahve", "c4"), cat("Salata", "c5")]),
    row({ name: "Promo", w: "fill", h: 120, pad: [16, 20], radius: "lg", fill: "brand", clip: true, cross: "center" }, [
      col({ name: "Copy", w: "fill", gap: 4 }, [text("İlk siparişe %40 indirim", "title", { color: "on-brand", w: "fill" }), text("Bu akşama kadar geçerli", "caption", { color: "on-brand" })]),
      pattern("dots", { w: 96, h: 96, gap: 12, size: 2, color: "on-brand", opacity: 0.4 }),
    ]),
    text("Yakındaki restoranlar", "title"),
    shop("Moda Pide ve Lahmacun", "25–35 dk · Ücretsiz teslimat", 120),
  ]),
  tabsBar([["Ana Sayfa", "house"], ["Arama", "search"], ["Sepetim", "shopping-cart"], ["Favoriler", "heart"], ["Profilim", "user"]], 0),
])), { page: "Screens" });
```

## travel: a destination over a full-bleed photo

The photograph is the screen: it runs under the status bar to two thirds
of the height, the name sits on it in a big serif over a scrim, the back
and share buttons float on it as round translucent plates. A white sheet
rises with rounded corners: name, rating, chips, two lines of copy, and a
fixed bottom bar with the price and the one button. Signature: the photo
and the scrim. Picture: `photo` "Cappadocia hot air balloons at sunrise"
with aspect tall.

```js
colors({ bg: ["#FFFFFF", "#111111"], surface: ["#FFFFFF", "#1A1A1A"], "surface-2": ["#F3EFEA", "#262321"], text: ["#141210", "#F6F2EE"], "text-muted": ["#6B6560", "#B5ADA6"], line: ["#E7E1DA", "#34302C"], brand: ["#D9542C", "#FF8A62"], "on-brand": ["#FFFFFF", "#2A0B02"], danger: ["#C8102E", "#FF6B7D"], success: ["#2E8B57", "#6FD6A0"], gold: ["#F2A93B", "#F2A93B"], sky: ["#5A3A22", "#5A3A22"], white: ["#FFFFFF", "#FFFFFF"], glass: ["#FFFFFF59", "#FFFFFF33"] });
type({ family: "Figtree", styles: { display: { size: 48, weight: 600, line: 52, family: "Fraunces", tracking: -1 }, title: { size: 22, weight: 700, line: 28 }, headline: { size: 17, weight: 600, line: 22 }, body: { size: 16, weight: 400, line: 23 }, label: { size: 15, weight: 600, line: 20 }, caption: { size: 13, weight: 500, line: 16 }, overline: { size: 12, weight: 600, line: 16 } } });
const plate = (ic) => row({ name: "Plate", w: 44, h: 44, radius: "full", fill: "glass", main: "center", cross: "center" }, [icon(ic, { size: 20, color: "white" })]);
const chipOf = (label) => row({ name: "Chip", pad: [8, 14], radius: "full", stroke: "line", fill: "surface-2" }, [text(label, "label")]);
place(screen("Travel", { device: "iphone", fill: "bg", gap: 0, pad: 0 }, chrome([
  frame({ name: "Photo header", w: "fill", h: 560, fill: "sky", clip: true }, [
    frame({ name: "Photo", absolute: true, x: 0, y: 0, w: 393, h: 560, fill: "sky" }),
    scrim({ w: 393, h: 300, absolute: true, x: 0, y: 260, to: 0.75 }),
    statusBarIn("white", { absolute: true, x: 0, y: 0 }),
    row({ name: "Actions", absolute: true, x: 16, y: 64, w: 361, main: "between" }, [plate("arrow-left"), plate("share")]),
    col({ name: "Title", absolute: true, x: 20, y: 430, gap: 4 }, [text("Kapadokya", "display", { color: "white" }), text("3 gün · 2 gece", "label", { color: "white" })]),
  ]),
  col({ name: "Sheet", absolute: true, x: 0, y: 536, w: 393, h: 316, radius: [28, 28, 0, 0], fill: "surface", pad: [20, 20, 0, 20], gap: 14 }, [
    row({ name: "Head", w: "fill", main: "between", cross: "center" }, [text("Kapadokya turu", "title"), row({ name: "Rating", gap: 4, cross: "center" }, [icon("star", { size: 16, color: "gold" }), text("4,9 (2.140)", "label")])]),
    row({ name: "Chips", w: "fill", gap: 8 }, [chipOf("Balon turu"), chipOf("Yeraltı şehri"), chipOf("Vadiler")]),
    text("Peri bacalarının arasında yürüyün, gün doğumunda balonla vadilerin üstünden geçin.", "body", { color: "text-muted", w: "fill" }),
  ]),
  row({ name: "Book bar", absolute: true, x: 0, y: 852 - 104, w: 393, h: 104, pad: [12, 20, 34, 20], cross: "center", gap: 16, fill: "surface", stroke: "line" }, [
    col({ name: "Price", w: "fill", gap: 2 }, [text("₺12.400'den", "title"), text("kişi başı", "caption", { color: "text-muted" })]),
    instance(button, { name: "Book", variant: { kind: "primary", state: "default" }, text: { Label: "Rezervasyon yap" }, hide: ["Icon"] }),
  ]),
])), { page: "Screens" });
```

## wellness: a soft morning

Warm cream and sage, a serif greeting in terracotta, nested activity rings
in three muted hues, a clay illustration of a person at rest, a gradient
breathing card with a waveform and a big round play button, small cards
for an affirmation and a mood slider, a floating pill tab bar. Signature:
the rings and the waveform. Illustration: `image` "Soft 3D clay
illustration, pastel sage and peach, gentle light, no text: a woman
meditating cross-legged beside a plant".

```js
colors({ bg: ["#EEF2EC", "#121512"], surface: ["#FFF8EF", "#1C1F1B"], "surface-2": ["#DCE6DB", "#262B25"], text: ["#2B2622", "#F2EEE8"], "text-muted": ["#6F6760", "#B7AFA6"], line: ["#E2DCD2", "#33372F"], brand: ["#2F4A3E", "#9FD1B6"], "on-brand": ["#FFFFFF", "#0D1A14"], danger: ["#C2412D", "#FF8A73"], success: ["#4F8A6B", "#8FD1AE"], clay: ["#B5603F", "#E99A78"], sage: ["#8DB5A6", "#8DB5A6"], dusk: ["#6A6FA8", "#9BA0D6"], peach: ["#F0A47E", "#F0A47E"], "peach-soft": ["#FBDCCB", "#3A2A22"] });
type({ family: "Figtree", styles: { display: { size: 34, weight: 500, line: 40, family: "Fraunces", tracking: -0.5 }, title: { size: 24, weight: 500, line: 30, family: "Fraunces" }, headline: { size: 17, weight: 600, line: 22 }, body: { size: 15, weight: 400, line: 21 }, label: { size: 15, weight: 600, line: 20 }, caption: { size: 13, weight: 500, line: 16 }, overline: { size: 12, weight: 600, line: 16 } } });
place(screen("Wellness", { device: "iphone", fill: "bg", gap: 16, pad: 0 }, chrome([
  instance(statusBar, { name: "Status bar" }),
  col({ name: "Body", w: "fill", gap: 16, pad: [0, 16] }, [
    col({ name: "Greeting", gap: 2 }, [text("Günaydın, Ceylin", "display", { color: "clay" }), text("16 Kasım Pazartesi", "body", { color: "text-muted" })]),
    row({ name: "Today", w: "fill", h: 220, pad: 16, gap: 16, radius: "lg", fill: "surface", cross: "center" }, [
      frame({ name: "Rings", w: 150, h: 150 }, [
        chart("ring", 0.68, { w: 150, color: "sage", track: "surface-2", weight: 12, absolute: true, x: 0, y: 0 }),
        chart("ring", 0.82, { w: 114, color: "dusk", track: "surface-2", weight: 12, absolute: true, x: 18, y: 18 }),
        chart("ring", 0.4, { w: 78, color: "peach", track: "surface-2", weight: 12, absolute: true, x: 36, y: 36 }),
      ]),
      col({ name: "Legend", gap: 10, w: "fill" }, [
        col({ name: "Steps", gap: 0 }, [text("Adım", "label"), text("5.420", "caption", { color: "text-muted" })]),
        col({ name: "Sleep", gap: 0 }, [text("Uyku", "label"), text("7 sa 12 dk", "caption", { color: "text-muted" })]),
        col({ name: "Water", gap: 0 }, [text("Su", "label"), text("1,2 L", "caption", { color: "text-muted" })]),
      ]),
    ]),
    col({ name: "Breath", w: "fill", gap: 12, pad: 20, radius: "lg", fill: { type: "linear", angle: 0, stops: ["peach-soft", "peach"] } }, [
      text("Bugünkü nefes egzersizi · 5 dk", "title", { color: "clay", w: "fill" }),
      row({ name: "Player", w: "fill", gap: 16, cross: "center" }, [
        chart("wave", 40, { w: 233, h: 36, color: "clay", seed: "breath" }),
        row({ name: "Play", w: 64, h: 64, radius: "full", fill: "brand", main: "center", cross: "center" }, [icon("play", { size: 28, color: "on-brand" })]),
      ]),
    ]),
  ]),
  row({ name: "Tab bar", absolute: true, x: 16, y: 852 - 100, w: 361, h: 64, pad: [0, 8], radius: "full", fill: "surface", shadow: "raised", main: "between", cross: "center" }, [
    instance(tab, { name: "Bugün", w: "fill", variant: { state: "selected" }, text: { Label: "Bugün" }, icons: { Icon: "sun" } }),
    instance(tab, { name: "Egzersiz", w: "fill", variant: { state: "default" }, text: { Label: "Egzersiz" }, icons: { Icon: "activity" } }),
    instance(tab, { name: "Profil", w: "fill", variant: { state: "default" }, text: { Label: "Profil" }, icons: { Icon: "user" } }),
  ]),
])), { page: "Screens" });
```

## fintech: a dark balance

Near-black with a faint green cast, the balance in large tabular figures,
the 30-day curve as a glowing area chart over a grid, the card as a
metallic gradient object with the neighbours peeking at the edges, four
round actions with a soft glow, transactions with brand marks and amounts
right-aligned. Signature: the glowing curve and the card.

```js
colors({ bg: ["#0B0E0D", "#0B0E0D"], surface: ["#151918", "#151918"], "surface-2": ["#1F2422", "#1F2422"], text: ["#F2F5F3", "#F2F5F3"], "text-muted": ["#8E9894", "#8E9894"], line: ["#262C2A", "#262C2A"], brand: ["#2EE57A", "#2EE57A"], "on-brand": ["#04170C", "#04170C"], danger: ["#FF6B6B", "#FF6B6B"], success: ["#2EE57A", "#2EE57A"], steel1: ["#1D4E55", "#1D4E55"], steel2: ["#3E8E8A", "#3E8E8A"], steel3: ["#16323B", "#16323B"], m1: ["#B4500F", "#B4500F"], m2: ["#5D3EBC", "#5D3EBC"], m3: ["#1E6FD9", "#1E6FD9"], white: ["#FFFFFF", "#FFFFFF"] });
shadow("glow", [{ y: 0, blur: 18, color: "#2EE57A40" }]);
const action = (label, ic) => col({ name: label, gap: 8, cross: "center", w: "fill" }, [row({ name: "Plate", w: 56, h: 56, radius: "full", fill: "surface-2", stroke: { color: "brand", width: 1 }, shadow: "glow", main: "center", cross: "center" }, [icon(ic, { size: 22, color: "text" })]), text(label, "caption")]);
const txn = (mark, fill, name, kind, amount, when) => row({ name: "Transaction", w: "fill", h: 64, gap: 12, pad: [0, 16], cross: "center" }, [
  row({ name: "Mark", w: 40, h: 40, radius: "full", fill: fill, main: "center", cross: "center" }, [text(mark, "label", { color: "white" })]),
  col({ name: "Who", w: "fill", gap: 2 }, [text(name, "label"), text(kind, "caption", { color: "text-muted" })]),
  col({ name: "Amount", gap: 2, cross: "end" }, [text(amount, "label"), text(when, "caption", { color: "text-muted" })]),
]);
place(screen("Fintech", { device: "iphone", fill: "bg", gap: 16, pad: 0 }, chrome([
  instance(statusBar, { name: "Status bar" }),
  row({ name: "Hello", w: "fill", pad: [0, 16], main: "between", cross: "center" }, [text("İyi akşamlar, Arda", "headline"), instance(avatar, { name: "Avatar", text: { Initials: "AY" } })]),
  col({ name: "Balance", w: "fill", gap: 4, cross: "center" }, [text("Toplam bakiye", "caption", { color: "text-muted" }), text("₺48.250,75", "display")]),
  col({ name: "Trend", w: "fill", gap: 6, pad: [0, 16] }, [
    chart("area", [31, 33, 32, 36, 35, 40, 44, 41, 39, 43, 47, 50], { w: 361, h: 110, grid: 3, glow: true }),
    row({ name: "Legend", w: "fill", main: "between" }, [text("Son 30 gün", "caption", { color: "text-muted" }), text("+%4,8", "caption", { color: "brand" })]),
  ]),
  frame({ name: "Cards", w: "fill", h: 200, clip: true }, [
    rect({ name: "Card prev", absolute: true, x: -276, y: 16, w: 300, h: 168, radius: 16, fill: "surface-2" }),
    col({ name: "Card", absolute: true, x: 46, y: 8, w: 300, h: 184, pad: 20, gap: 12, radius: 16, stroke: { color: "steel2", width: 1 }, fill: { type: "linear", angle: 30, stops: ["steel3", "steel1", "steel2", "steel3"] } }, [
      row({ name: "Top", w: "fill", main: "between" }, [text("Atlas Kart", "label", { color: "white" }), text("atlas", "title", { color: "white" })]),
      rect({ name: "Chip", w: 40, h: 30, radius: 6, fill: "text-muted" }),
      text("•••• 4820", "title", { color: "white" }),
      text("ARDA YILMAZ", "caption", { color: "white" }),
    ]),
    rect({ name: "Card next", absolute: true, x: 369, y: 16, w: 300, h: 168, radius: 16, fill: "surface-2" }),
  ]),
  row({ name: "Actions", w: "fill", pad: [0, 16] }, [action("Gönder", "send"), action("İste", "arrow-left-right"), action("Öde", "qr-code"), action("Yatırım", "trending-up")]),
  col({ name: "Recent", w: "fill", gap: 0 }, [
    row({ name: "Head", w: "fill", pad: [0, 16], main: "between" }, [text("Son işlemler", "headline"), text("Tümü", "label", { color: "brand" })]),
    txn("M", "m1", "Migros", "Market", "−₺1.240,50", "Bugün 18:45"),
  ]),
  tabsBar([["Ana Sayfa", "house"], ["Transfer", "arrow-left-right"], ["Kartlarım", "credit-card"], ["Yatırım", "trending-up"], ["Menü", "menu"]], 0),
])), { page: "Screens" });
```

## editorial: a news front page

Paper ground, a huge serif masthead with the date beside it, one hairline
under it, the lead story as a wide photo with a small red category flag,
the headline in a large serif across the full width with a one-line
standfirst, then a two-column grid of stories divided by hairlines, each
with a red small-caps category, a serif headline and a thumbnail.
Signature: the masthead and the type. Pictures: `photo` for the lead and
each thumbnail.

```js
colors({ bg: ["#F6F1E8", "#141210"], surface: ["#FBF8F2", "#1C1916"], "surface-2": ["#E7DFD1", "#2A2520"], text: ["#141210", "#F3EEE6"], "text-muted": ["#5F574D", "#B3AA9D"], line: ["#CFC6B6", "#3A342D"], brand: ["#8E1B23", "#F07A80"], "on-brand": ["#FFFFFF", "#2A0507"], danger: ["#8E1B23", "#F07A80"], success: ["#2F6B3A", "#7BD08A"] });
type({ family: "Geist", styles: { display: { size: 64, weight: 700, line: 64, family: "Fraunces", tracking: -2 }, title: { size: 30, weight: 600, line: 34, family: "Fraunces", tracking: -0.6 }, headline: { size: 18, weight: 500, line: 23, family: "Fraunces" }, body: { size: 16, weight: 400, line: 23, family: "Fraunces" }, label: { size: 15, weight: 600, line: 20 }, caption: { size: 12, weight: 500, line: 16 }, overline: { size: 11, weight: 700, line: 14, tracking: 0.8, transform: "upper" } } });
const story = (kind, head) => col({ name: "Story", w: "fill", gap: 6, pad: [0, 4] }, [
  row({ name: "Top", w: "fill", gap: 8 }, [text(kind, "overline", { color: "brand", w: "fill" }), frame({ name: "Photo", w: 56, h: 56, radius: 4, fill: "surface-2" })]),
  text(head, "headline", { w: "fill" }),
]);
place(screen("Editorial", { device: "iphone", fill: "bg", gap: 12, pad: 0 }, chrome([
  instance(statusBar, { name: "Status bar" }),
  col({ name: "Masthead", w: "fill", pad: [0, 16], gap: 4 }, [
    text("16 Kasım 2026 Pazartesi", "caption", { color: "text-muted" }),
    text("Gündem", "display"),
    rect({ name: "Rule", w: 361, h: 1, fill: "text" }),
  ]),
  col({ name: "Lead", w: "fill", pad: [0, 16], gap: 10 }, [
    frame({ name: "Photo", w: "fill", h: 200, radius: 4, fill: "surface-2", clip: true }, [row({ name: "Flag", absolute: true, x: 12, y: 12, pad: [2, 8], fill: "brand" }, [text("Ulaşım", "overline", { color: "on-brand" })])]),
    text("İstanbul'da yeni metro hattı açıldı", "title", { w: "fill" }),
    text("İki yakayı birleştiren hat bugün ilk yolcularını taşıdı.", "body", { color: "text-muted", w: "fill" }),
  ]),
  col({ name: "Grid", w: "fill", pad: [0, 16], gap: 12 }, [
    rect({ name: "Rule", w: 361, h: 1, fill: "line" }),
    row({ name: "Row", w: "fill", gap: 12 }, [story("Ekonomi", "Enflasyon verilerinde yeni dönem beklentisi"), rect({ name: "Rule", w: 1, h: 110, fill: "line" }), story("Kültür", "AKM'de bu hafta: uluslararası caz festivali")]),
  ]),
  tabsBar([["Gündem", "newspaper"], ["Kategoriler", "list"], ["Keşfet", "compass"], ["Kaydedilenler", "bookmark"], ["Profil", "user"]], 0),
])), { page: "Screens" });
```

## player: now playing

The cover owns the screen: a large square with soft corners, the whole
background a dark gradient taken from the cover's colours (or the cover
itself, blurred, under a dark scrim), the title big and left-aligned, the
artist under it, a thin progress line with times, five controls with the
middle one a large round plate, then a shelf of what plays next.
Signature: the cover and its colour. Cover: `image` "A woven Anatolian
kilim, rust, indigo and cream, flat square, no text".

```js
colors({ bg: ["#2A140C", "#2A140C"], surface: ["#3A1F14", "#3A1F14"], "surface-2": ["#4A2A1C", "#4A2A1C"], text: ["#FFF4EC", "#FFF4EC"], "text-muted": ["#D5B8A6", "#D5B8A6"], line: ["#5C3A29", "#5C3A29"], brand: ["#FFF4EC", "#FFF4EC"], "on-brand": ["#2A140C", "#2A140C"], danger: ["#FF8A73", "#FF8A73"], success: ["#8FD1AE", "#8FD1AE"], rust: ["#B4532A", "#B4532A"], indigo: ["#1F2A4A", "#1F2A4A"], cream: ["#F2DCC0", "#F2DCC0"] });
const shelfItem = (title, by) => col({ name: "Next", w: 120, gap: 6 }, [frame({ name: "Cover", w: 120, h: 120, radius: 12, fill: "surface-2" }), text(title, "label", { w: "fill" }), text(by, "caption", { color: "text-muted", w: "fill" })]);
place(screen("Player", { device: "iphone", gap: 16, pad: 0, fill: { type: "linear", angle: 90, stops: ["rust", "bg", "bg"] } }, chrome([
  instance(statusBar, { name: "Status bar" }),
  row({ name: "Nav", w: "fill", pad: [0, 16], main: "between", cross: "center" }, [icon("chevron-down", { size: 26 }), col({ name: "Source", cross: "center" }, [text("Âşık Veysel radyosu", "caption", { color: "text-muted" }), text("Şimdi çalıyor", "label")]), icon("ellipsis", { size: 24 })]),
  row({ name: "Cover wrap", w: "fill", main: "center" }, [
    frame({ name: "Cover", w: 300, h: 300, radius: 16, fill: "rust", clip: true, shadow: "raised" }, [pattern("stars", { w: 300, h: 300, gap: 50, color: "cream", absolute: true, x: 0, y: 0 })]),
  ]),
  col({ name: "Track", w: "fill", pad: [0, 24], gap: 4 }, [
    row({ name: "Title row", w: "fill", cross: "center" }, [col({ name: "Names", w: "fill", gap: 2 }, [text("Uzun İnce Bir Yol", "title"), text("Âşık Veysel", "body", { color: "text-muted" })]), icon("heart", { size: 26 })]),
    frame({ name: "Progress", w: "fill", h: 4, radius: "full", fill: "line", clip: true }, [rect({ name: "Played", w: 140, h: 4, fill: "brand" })]),
    row({ name: "Times", w: "fill", main: "between" }, [text("1:42", "caption", { color: "text-muted" }), text("4:05", "caption", { color: "text-muted" })]),
  ]),
  row({ name: "Controls", w: "fill", pad: [0, 24], main: "between", cross: "center" }, [
    icon("shuffle", { size: 22, color: "text-muted" }), icon("skip-back", { size: 30 }),
    row({ name: "Play", w: 76, h: 76, radius: "full", fill: "brand", main: "center", cross: "center" }, [icon("pause", { size: 32, color: "on-brand" })]),
    icon("skip-forward", { size: 30 }), icon("repeat", { size: 22, color: "text-muted" }),
  ]),
  col({ name: "Up next", w: "fill", pad: [0, 24], gap: 10 }, [text("Sıradaki", "headline"), row({ name: "Shelf", w: "fill", gap: 12, clip: true }, [shelfItem("Gesi Bağları", "Selda Bağcan"), shelfItem("Kara Toprak", "Âşık Veysel"), shelfItem("Allı Turnam", "Neşet Ertaş")])]),
])), { page: "Screens" });
```

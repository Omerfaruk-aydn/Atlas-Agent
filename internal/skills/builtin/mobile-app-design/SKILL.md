---
name: mobile-app-design
description: Platform rules and craft for designing iOS and Android app screens - frame sizes, safe areas, navigation patterns, touch targets, type sizes, lists, sheets, tab bars and layout grids - so screens feel native instead of like shrunk web pages. Load when designing phone or tablet app screens in the Atlas design editor or Figma.
metadata:
  provenance: atlas-authored
---

# Mobile app design

A phone screen is used with one thumb, in motion, in bad light, for seconds.
Design for that moment first, then for the platform's conventions.

## An app, not a website (read this first)

Models drift to web layouts on a phone. These are the tells, and the fixes:

| Website habit | App way |
| --- | --- |
| Full-width coloured hero band at the top | Status bar, then a large title on the screen background; the one important thing sits in an inset card (16 margin) |
| Big floating cards with shadows everywhere | Inset grouped lists: one rounded group, hairline separators inset to the text, no shadow |
| Button in the middle of the page | Primary action in the thumb zone: pinned above the tab bar, or the last item of the content, inset 16 |
| Section headings in caps with a rule | Small muted section header above a grouped list, sentence case |
| A footer, links, "Contact us" | A tab bar (3-5 destinations) and the home indicator |
| Hover and underlines | Pressed states, chevrons, swipe actions, sheets |
| One long scrolling page | Few things per screen; detail opens a push or a sheet |

Every phone screen has, from top to bottom: status bar (59), navigation
(large title or a 56 top bar with back), content on the screen background,
tab bar (49 + 34) when it is a top-level screen, home indicator (34). The
kit has these as components (`design-kit`: Status bar, Large header, Top
bar, Search field, Segmented control, Bottom sheet, Tab bar, Home
indicator); build every screen from them. The lint warns `app-chrome` and
`web-hero` when a screen is missing them or opens with a full-width band.
Check the render against an Apple stock app (Wallet, Maps, Health, Reminders)
and ask: would this sit among them?

## Frames and safe areas

| Device | Frame | Top safe area | Bottom safe area |
| --- | --- | --- | --- |
| iPhone 15/16 | 393 x 852 | 59 (status bar + Dynamic Island) | 34 (home indicator) |
| iPhone 15/16 Pro Max | 430 x 932 | 59 | 34 |
| iPhone SE | 375 x 667 | 20 | 0 |
| Android (common) | 412 x 915 | 24-48 status bar | 24-48 gesture bar |

- Nothing tappable or important in the safe areas; backgrounds may run
  under them.
- Layout grid: 4 columns, 16 margin, 16 gutter on phones (iOS often uses 20
  margins); 8 or 12 columns on tablets.

## Navigation

- **Tab bar** (iOS) or **navigation bar** (Android): 3-5 top-level
  destinations, same on every top-level screen, icon + short label, the
  current tab marked by colour and filled icon. Height 49 + 34 safe area on
  iOS, 80 dp on Android.
- **Navigation bar / top app bar**: back on the left, title, at most two
  actions on the right. Large titles (34/700) on iOS top-level screens,
  collapsing to 17/600 on scroll.
- **Sheets** for focused subtasks: grabber, title, a clear dismiss; medium
  and large detents. Do not stack sheets.
- Primary action goes where the thumb is: bottom of the screen, full width
  minus margins, 50-56 pt tall, or a floating action button on Android.

## Touch and type

- Touch targets at least 44 x 44 pt (iOS) / 48 x 48 dp (Android), with at
  least 8 between neighbours. The visible shape may be smaller than the hit
  area.
- iOS text styles: Large Title 34, Title 1 28, Title 2 22, Title 3 20,
  Headline 17 semibold, Body 17, Callout 16, Subheadline 15, Footnote 13,
  Caption 12. Body under 15 is hard to read outdoors.
- Support Dynamic Type: rows grow in height, labels wrap rather than clip.

## Lists and content

- List rows 44-72 pt tall: leading visual (icon, avatar, time), title,
  secondary line, trailing value or chevron. Separators inset to the text.
- Group related rows in inset grouped sections with a small header.
- Show the most time-critical information large and early: a countdown, the
  next item, the gate, the room.
- Cards only when items are independent and tappable as a whole.

## Visual quality on mobile

- Fewer, bigger things. One focal element per screen.
- Generous outer margin (16-20) and consistent inner padding (12-16).
- Contrast for daylight: prefer near-black text on light surfaces; check
  coloured text at 4.5:1.
- System-like icons (SF Symbols weight, Material Symbols) at 20-24 pt in a
  single weight.
- Respect the platform: iOS back is a swipe from the edge and a chevron;
  Android has the system back. Do not draw a fake status bar with wrong
  time and battery; use the platform's status bar or a neutral one (9:41).

## Build it in the editor

1. A frame per screen at the device size, named `01 Today`.
2. Layout grid (4 / 16 / 16), status bar and home indicator placeholders.
3. Header, content and tab bar as auto-layout frames; tab bar and list rows
   as components with instances.
4. Check with Alt-measure that margins and gaps match the spacing steps.
5. Run `design-states` for empty, loading, error and long-content versions.

---
name: design-states
description: Checklist for designing the states and edge cases that separate a professional design from a happy-path mockup - empty, loading, error, offline, success, long and localized content, permissions, first run - and how to show them as variants. Load when a screen's main version is done and before reporting a flow or app design as complete.
metadata:
  provenance: atlas-authored
---

# States and edge cases

A screen with only its ideal content is a mockup. The real product spends
much of its life empty, loading, failing or overfilled. Design those states
deliberately, with the same type, spacing and voice as the main screen.

## Which states

For each screen, decide which of these exist in the product, and design
every one that does:

| State | What to show |
| --- | --- |
| First run / empty | Why it is empty, what to do first, one action. No sad mascots unless the brand has one. |
| Loading | Skeletons that match the real layout, not a centred spinner on a blank screen. Keep headers and navigation visible. |
| Partial | Some data loaded, some still coming or missing; mark the missing part in place. |
| Error | What happened in plain words, whether data was lost, one recovery action (Retry, Edit, Contact). Keep the user's input. |
| Offline | What still works, when it was last updated ("Updated 08:42"), what will sync later. |
| Success | Confirmation near where the action happened; then return to the task. Avoid full-screen celebrations for routine actions. |
| Long content | Longest real name, title, address and number; two-line wrapping, truncation with full text available, large Dynamic Type. |
| Localized | German or Turkish strings run 30-40% longer; right-to-left mirrors layout and icons with direction. |
| Permission | Location, notifications, camera: explain the benefit before the system prompt; design the "denied" state too. |
| Disabled / restricted | Why it is unavailable and how to make it available. |
| Many items | 1, 3, 30 and 300 items: list density, grouping, search or filters when it grows. |

## Interactive components

Every interactive component has: default, hover (web), pressed, focused
(visible focus ring, 2 px, 3:1 against its neighbours), disabled, and when it
applies, selected, loading and error. Make them component variants in the
editor with a property per state, so instances can switch.

## How to present them

- One frame per important state, named `03 Programme / Empty`,
  `03 Programme / Error`, placed in a row next to the main screen in the
  same section.
- Shared parts (header, tab bar) stay identical across states; only the
  changing region differs.
- Write the real copy for each state, in the product's voice.

## Done when

Each screen in the brief has its required states drawn, interactive
components have their variants, and the longest real content fits without
clipping. `design_canvas lint` reports no overflow on the long-content frames.

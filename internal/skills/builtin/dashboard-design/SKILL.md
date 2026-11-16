---
name: dashboard-design
description: Design data-dense product screens - dashboards, admin panels, tables, analytics and monitoring views - with clear question-first layout, KPI hierarchy, honest charts, readable tables, filters and states. Load when designing a dashboard, back-office tool, analytics page or any screen whose main content is data.
metadata:
  provenance: atlas-authored
---

# Dashboard and data-dense design

A dashboard answers a few questions for someone who checks it often. Design
for the questions, not for the amount of data available.

## Start from questions

Write the three to five questions the user opens this screen to answer, in
order of frequency ("Is anything broken right now?", "Are we on target this
month?", "Which accounts need attention?"). Each question gets one region;
the most frequent gets the top-left.

## Layout

- Desktop 1440 x 900 or 1280 x 800, 12-column grid, 24 gutters, 24-32
  margins; side navigation 240-280 wide, collapsible.
- Top: page title, time range, primary filters, one primary action.
- Then a KPI row (3-5 numbers), then the main chart, then the table or list
  that explains it. Detail drills down; it does not crowd the overview.
- Density is fine when aligned: 13-14 px table text, 32-40 row height,
  8-12 cell padding. Consistent alignment matters more than whitespace.

## KPIs

- Each KPI: label, value (28-36, tabular figures), change with direction
  and period ("+4.2% vs last week"), and a tiny trend line only if it adds
  information.
- Colour change only when it means good or bad for this metric (cost up is
  bad). Never colour by sign alone.

## Charts that tell the truth

- Pick by the question: trend over time, line; comparison of categories,
  horizontal bar sorted by value; part of whole with 2-4 parts, stacked bar
  (pie only for 2-3 parts); distribution, histogram; relation, scatter.
- Bars start at zero. Axes labelled with units. Gridlines light. Direct
  labels instead of legends where possible.
- One highlight colour for the series that answers the question; others in
  neutral greys. Colour-blind safe palettes; never red/green only.
- Show the current value and the target or previous period when that is
  the question.

## Tables

- Left-align text, right-align numbers, align decimals; units in the header.
- Sticky header, sortable columns marked, row hover, selected row, bulk
  actions appearing on selection.
- First column is the identifier people scan for; status as a small dot +
  word, not a coloured full row.
- Pagination or virtual scroll with the count ("1-50 of 1,284").

## States

Empty (no data yet, how to connect it), loading skeletons per widget, partial
failure per widget (one broken chart does not blank the page), stale data
with "Updated 2 min ago", no-permission. See `design-states`.

## Restraint

No 3D charts, no gauges for numbers that are not limits, no decorative
gradients on charts, no more than one accent colour plus status colours.
Every chart has a title that states its question.

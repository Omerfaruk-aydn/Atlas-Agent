---
name: design-patterns
description: Proven screen patterns for apps and sites - onboarding, sign in, home feed, list and detail, search, form and checkout, settings, empty and error states, dashboards, landing pages - with the order of the elements, the sizes and the one thing each must get right. Load before laying out any screen so it starts from a known-good structure instead of an invented one.
metadata:
  provenance: atlas-authored
---

# Design patterns

A screen of a known type has a known structure; people expect it, and
inventing a new one rarely helps them. Pick the pattern, keep its order,
then spend the originality on the brief's one visual idea, the content and
the type. Sizes are for a 393 px phone unless a line says otherwise.

## Before you start

- Name the screen's job in one verb ("choose", "pay", "find"). The pattern
  below that matches the verb is the starting point.
- Write down the references in the brief (see `design-brief`): two real
  products that solve the same job well, and what you take from each (the
  structure, not the look).
- Build with the kit (`design-kit`) instances; add a component only when
  the pattern needs one the kit lacks.

## Patterns

**Onboarding (3 steps at most).** One idea per step: a large visual or
number on top (about 45 % of the height), a title (display or title size),
one line of body copy, then a primary button pinned to the bottom with a
quiet secondary action ("Skip") above or beside it. A progress indicator of
dots or a bar at the top. Never ask for permissions before showing value.

**Sign in / sign up.** Top bar with back; title; the fields (labels above,
44-52 px tall, one per line); the primary button full width; alternatives
below ("Continue with ..."); the legal line last in caption size. Show the
error under the field in the danger colour with words that say how to fix
it. Primary button is disabled-looking only while the form is incomplete,
and still readable.

**Home / feed.** A header that answers "where am I and what is next" (date,
greeting, the single most useful fact) in display size; then the primary
action; then a list or cards in order of importance; the tab bar last. The
first screenful must contain the thing the person opened the app for.

**List and detail.** The list row is 64-72 px: icon or avatar 40-44, title
(headline) over detail (caption, muted), chevron or trailing value. Group
rows under an overline header, separated by space or hairlines, not both.
The detail screen: hero (image or number), title, key facts in a 2-column
grid, description, then the primary action as a sticky bottom bar.

**Search.** The field at the top (52 px, magnifier icon, clear button when
there is text); filter chips in a horizontal row under it (44 px targets);
results as list rows; recent searches before typing; a helpful empty
result ("No match for X - try ...") with one next step.

**Form and checkout.** Group fields into sections with a title each; one
column; the order summary visible before paying; the total at headline size
and the primary button repeating the amount ("Pay EUR 48.00"); show
progress in steps when there are more than one; never hide the price.

**Settings.** Grouped list rows: label left, value or switch right (switch
44 px high target, track 52x32); section titles in overline; destructive
actions last and in the danger colour, with a confirmation step.

**Empty, loading, error.** Every list screen needs all three. Empty: a
small illustration or icon, one sentence on why it is empty, one action.
Loading: skeleton rows with the real layout, not a spinner on a blank
screen. Error: say what happened and what to do, with a retry button;
keep the person's input.

**Dashboard (desktop 1440).** A 12-column grid; a row of 3-4 key numbers
at the top (display size value, caption label, a delta with colour and an
arrow so it is not colour only); one main chart wide (8 columns) beside a
ranked list (4 columns); filters at the top right. Show the period.

**Landing page (desktop 1440).** Hero: a title of at most 12 words, one
sentence, one primary and one secondary button, one strong visual; then
proof (logos or numbers); then three benefits, each a title and a line; one
detail section per benefit; testimonial; the closing call to action; footer.
Content width 1120-1200, section spacing 96-128.

## Check

- The first read, second read and third read from the brief are obvious
  when you squint at the render.
- One primary action per screen, and it is the most prominent thing that
  can be tapped.
- Touch targets are 44 px or more; spacing comes from the file's scale.
- States exist for what the screen can be: empty, loading, error, long
  text, and the other variants of every component it uses.

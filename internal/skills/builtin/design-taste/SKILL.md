---
name: design-taste
description: The user's own design taste, kept as project notes in .atlas/design/taste.md - what they liked, what they rejected and why - read before every design brief and updated whenever the user approves or rejects a design. Load at the start of any design work and whenever the user reacts to a design.
metadata:
  provenance: atlas-authored
---

# Design taste

Good design for this user is design this user would choose. Their reactions
are the most valuable reference there is: keep them, read them, apply them.

## Where it lives

`.atlas/design/taste.md` in the project. Create it with the headings below if
it does not exist. The user can edit it by hand; their edits win.

```markdown
# Design taste

## Rejected (never again)
- 2026-11-16 Flat coloured blocks as the hero: "looks cheap". Use depth,
  strong type or a real image instead.

## Liked (do more of)
- 2026-11-16 Angular gradient ring around the countdown on 01 Today.

## Rules the user gave
- No plugins or multi-user cursors in the editor (scope, not taste).

## Open questions
- Serif or sans for editorial screens? Not decided.
```

## Read

Before writing a brief (`design-brief`), read the whole file. Turn each
"Rejected" line into an item of the brief's "Not this", and each "Liked"
line into a candidate for the one visual idea or the system. If a request
conflicts with a note, follow the request and mention the conflict.

## Write

Add a dated line whenever the user:

- rejects or criticises a design ("bunu beğenmedim", "too generic", "ai slop"),
  with the specific element and their words;
- approves or praises a part, with the element;
- states a rule about style, tone, colour, type or tools.

Record what the user said, not your interpretation of it, plus one line on
what to do differently. Keep entries short; merge duplicates; never record
secrets or personal data beyond the design preference.

## Use with critique

`design-critique` scores against the brief and these notes. A design that
repeats a rejected pattern fails, whatever its other scores.

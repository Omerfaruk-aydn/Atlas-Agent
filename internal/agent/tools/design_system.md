Project design memory, measured design checks and delivery packages.

Use it for any screen design work, including designs made in an external
generator such as Google Stitch or Figma (also on a remote computer).

Before generating:
1. `get` first. If a design system exists, keep its platform, direction and
   tokens; do not silently switch.
2. If none exists, settle the platform with the user: phone (ios/android/
   mobile), tablet, web, desktop or tui. "Mobile app screens" means portrait
   phone frames; in a generator, pick its mobile/app device type, never Web.
3. Offer 2-3 short, distinct visual directions (question tool) and let the
   user choose. Record it with `set` (direction.chosen_by "user"; "default"
   only when they asked you to decide).

`set` stores the complete system in .atlas/design/system.json as a
CANDIDATE: product, platform, direction (+ the alternatives shown), color
tokens (#RRGGBB), every foreground/background pair actually used, type
scale, spacing, radius, elevation, components and their states, icon style,
safe_area (required for phones/tablets), viewports, min_touch_target, the
accessibility target, references (ids of existing Figma/Stitch files or
token sets exactly as the service reported them, never invented), the
user's preferences and the approaches they rejected.

`approve` (expected_version: the current candidate version, note: what the
user said) makes the candidate the approved version. It requires explicit
accepted human design approval, such as "tasarımı onaylıyorum" / "approve
this design". A model-written note cannot grant approval. Call it only after
the user explicitly approves; your own proposal
is never approved by default. A later `set` is a revision: the approved
version stays in use until the user approves the new one. `history` lists
approved versions; `reset` clears the approved and candidate systems only
when the user asks (history is kept). A damaged memory is reported as an
error and left untouched: do not guess, ask the user and `set` again (the
damaged file is moved aside as a backup).

`check` measures, not judges: WCAG 2.x contrast for each pair (text 4.5:1,
large text and UI 3:1), safe area, touch target size, phone viewport and
type scale. Fix failing tokens before generating more screens.

`package` {name, screens, open_questions} assembles design-delivery/<name>/:
verified screen images (decoded, sized, SHA-256), design-system.json,
manifest.json with the design system version, whether it was approved,
the quality gate, and a README. Packaging the same screens again returns the
existing package; a package is never overwritten. Phone designs delivered
as landscape frames fail the gate. Failures stay visible in the package;
report them to the user instead of calling the work finished. Pull remote
screenshots into the workspace first (remote_support pull).

`directions` {directions:[2-3 complete systems], brief, width, height}
renders the same representative conversation/permission screen using each
system's tokens. Results are real browser PNGs, HTML sources and a comparison
gallery. These are direction previews, not finished product screens. They
run in a separate automation tab and preserve the earlier tab. Show the
previews before storing a choice; never invent a user selection.

`audit` {url, width, height, source_files, prototype_steps, assertions}
executes at most 16 real click/key steps, tests visible text/focus conditions,
then measures the DOM and captures that viewport. It reports overflow,
small/unnamed targets, text contrast (unknown backgrounds stay unknown),
truncation and document identity. Source hashes are checked before/after.
This is not an aesthetic score, a canvas/Figma layer audit or native phone
keyboard verification. Use browser layout_audit through remote_support for
remote DOM measurements; local design audit does not silently switch PCs.

`package` also accepts required_screens, source_files, source_urls and
audit_ids. Sources are copied into the package and checked against observed
audits. Missing approval, required screens, source-bound passing audits or
an observed prototype interaction plus assertion yields completion:partial.
Even a complete package is assembled_locally_not_presented: assembling it
does not prove the user saw it. Review actual screenshots and report any
unmeasured states, native behavior and visual quality explicitly.

Audit JSON is a persisted report, not authority by itself. Packaging requires
the exact observation recorded by this adapter process (bounded to its most
recent 256 audits). Edited records and records from an earlier process need
fresh auditing. Completion is scoped to the declared inventory and measured
DOM/prototype checks, not an assertion that every product requirement or
every native-device state has been tested.

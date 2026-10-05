---
name: visual-quality
description: Validate real rendered interfaces against explicit criteria.
compatibility: Atlas native tools, permitted commands and explicitly connected MCP services; optional authoring/rendering dependencies must be verified.
metadata:
  provenance: atlas-authored-adaptation
  reviewed-sources: figma-design-to-code, documents, presentations
---

# visual-quality

Use this procedure only for an assignment whose deliverable needs it. Read the
actual role contract, owned paths, user constraints and current tool descriptions.
The role's preferred skill binding is a starting recipe, not additional access.

## Capability and input preflight

- Identify the exact source, reference, requested output and acceptance criteria.
- Inspect available native tools, permitted commands and connected MCP schemas.
- Check optional runtimes/renderers with scoped commands before depending on them.
- Preserve user references, repository conventions and unrelated working changes.
- Existing authentication and permission/hook controls apply to every action.
- Report missing capability precisely; continue independent authorized work.

## Domain procedure

1. Resolve the exact implementation, reference, viewport, scale, theme, locale and states. Capture only the agreed composition and user flow.

2. Use browser, ui_verify, a11y_audit, interaction_audit and visual_diff when actually available; read each tool's documented inputs before use.

3. Inspect actual screenshots for spacing, hierarchy, clipping, alignment, content completeness and asset fidelity. A DOM assertion is not visual inspection.

4. Compare equivalent states and viewport dimensions; record intentional differences and mask only justified dynamic regions.

5. Exercise keyboard navigation, focus visibility, labels, zoom, text expansion and reduced motion. Include error recovery when the task requires it.

6. Use real browser state and fresh screenshots after navigation or mutation. Never attach a generated mockup as execution evidence.

7. Report the accessibility engine and its limitations. Partial DOM checks cannot establish full accessibility or measured contrast.

8. For TUI work inspect terminal captures at narrow/wide sizes, display-cell width, resize, scrolling and shortcut behavior.

9. For document and slide outputs inspect rendered pages at legible scale, including table breaks, headers, figures and font substitution.

10. Classify defects by observable consequence, reference region and reproducible steps. Visual preference alone is a proposal, not a blocking defect.

11. Verify each targeted correction with a fresh capture. Preserve the final coherent artifact and remove temporary fixtures only within owned paths.

12. Return a criterion-to-evidence matrix, artifact locations, defects and unverified checks; independent validation does not edit the implementation.

## Inspection record

Before testing, record revision, view, fixture, viewport, display scale, theme,
locale and reference. Save observations against that identity. Comparing two
different datasets or font environments can produce differences that are not
implementation regressions.

Build a matrix from the assigned acceptance criteria:
| Criterion | State to observe | Suitable evidence |
| --- | --- | --- |
| Primary action works | Real end-to-end journey | Interaction and resulting state |
| Layout matches reference | Same content and viewport | Rendered comparison |
| Keyboard remains usable | Focus and dialog transitions | Keyboard trace |
| Accessible state is exposed | Custom controls | Available accessibility inspection |
| Animation is stable | Moving and interrupted states | Timed observation |

Choose only relevant rows and add domain-specific states. Do not run every tool
just because it is available. A missing required evidence path remains a blocker.

## Finding construction

Locate a defect with exact steps and the smallest state that reproduces it.
Record expected behavior, actual behavior and user impact. Link the observed
artifact and the corresponding view or source path. A screenshot needs enough
context to identify the affected control.

Distinguish antialiasing variation from geometry, content or interaction defects.
Use visual_diff as a detector and inspect flagged areas. Do not approve or reject
solely from a percentage threshold.

Prioritize broken flows, missing content and inaccessible controls above decorative
differences. A personal preference is not a reference mismatch unless the assigned
criteria establish it.

## Retest and reporting

Independent review observes and reports; implementation belongs to the assigned
owner. After a correction, rerun the same reproduction and inspect nearby states
that share the changed layout or control. Keep revision identity current.

For documents, inspect rendered pages; for TUI, inspect terminal resize and
wrapping; for desktop, inspect actual windows. Browser success cannot substitute
for a native check.

Example: if long Arabic labels overlap an action button, retain the locale and
viewport fixture, report the hidden action and retest both RTL and the normal
layout after the owner fixes it. Mark untested locales accurately.

Return a criterion-based verdict with checks and limitations. Use the workflow's
actual JSON schema when required; do not invent an incompatible report format.

## Verification and recovery

Bind each important criterion to a real check and a navigable artifact or source.
Distinguish fresh executed results, cached results, structural checks, inspected
renders, skipped checks and user-reported observations. When a prerequisite fails,
stop dependent actions and retain the first failure, current identity and recovery
options. Retry within bounds only after identifying a relevant changed condition.
Do not repeat uncertain writes, suppress failures or fabricate missing evidence.

## Delivery

Return the assignment's requested output schema. For workflow tasks use the Atlas
JSON handoff with actual changed_files, checks, dependencies, risks and decision.
Include artifact paths and source provenance in evidence. A ready handoff is a
reported result for coordinator verification, never authenticated completion.
In quality-only runs do not implement fixes, change tests or certify unseen output.

---
name: desktop-automation
description: Operate desktop applications with fresh targets and verified outcomes.
compatibility: Atlas native tools, permitted commands and explicitly connected MCP services; optional authoring/rendering dependencies must be verified.
metadata:
  provenance: atlas-authored-adaptation
  reviewed-sources: computer-use
---

# desktop-automation

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

1. Use Atlas computer windows/observe/health capabilities to identify the intended running application and a unique target window.

2. Resolve application identifiers from returned inventory or an explicit approved launcher. Do not guess handles, executable paths or target metadata.

3. Check health and capture availability before repeated screenshot attempts. Window enumeration alone does not prove pixels or accessibility are readable.

4. Observe the target, inspect its semantic elements and choose a supported action pattern. Focus must be verified before keyboard input.

5. Use semantic controls when supported; otherwise use fresh screenshot/OCR coordinates tied to that observation. Re-observe after focus, resize, scroll or navigation.

6. Read stale_target, wrong_window and accessibility_unavailable as distinct recovery conditions. Refresh the target and change the strategy when evidence supports it.

7. Prefer bounded tool_pipeline desktop recipes for preparation, field input plus observation and verified submission. Keep permission/hook checks intact.

8. Stop dependent input immediately when focus or target verification fails. Never send blind keys to whichever window happens to be foreground.

9. After an action inspect the expected state: selected profile, exact track, form value, playback status or application-specific confirmation.

10. Distinguish requested item, current selection and actual active result. Opening a page does not prove playback or submission.

11. Handle secrets through the existing credential vault when available. Secure desktop, CAPTCHA and second-factor challenges require supported authorized flows or user handoff.

12. Report tool calls, recovery steps, elapsed time, observed final state and blockers. Atlas uses its own computer backend; this skill does not install @oai/sky.

## Observation-to-action checklist

For each uncertain boundary, establish:
1. Approved application and current window identity.
2. Whether capture, accessibility and focus checks are available.
3. The control or grounded hit region matching the user's intent.
4. Coordinate frame, scale and monitor for pixel-based input.
5. The expected visible change and evidence required before the next action.

Avoid observing after every deterministic keystroke when a stable control is
already grounded. Conversely, do not batch across navigation whose outcome
determines the next target.

## Failure-specific recovery

| Returned condition | Investigate next | Avoid |
| --- | --- | --- |
| wrong_window | Current foreground identity and intended focus | Blind key replay |
| accessibility_unavailable | Supported patterns and current pixel evidence | Declaring every control unusable |
| Unreadable screenshot | Capture/session health and raw backend error | Guessing that a particular overlay caused it |
| Stale handle or target | Fresh window and control observation | Reusing previous task handles |
| Input timeout | Whether the action occurred | Duplicating a consequential action |

Use the backend's actual error names and schema where they differ.
Window enumeration may succeed while capture fails; that combination narrows
capability state but does not establish a single root cause.

Do not loop on unchanged failures. Try an evidence-backed alternative that still
satisfies the requested application. If no current readable state can ground
input, perform the supported human handoff.

## Application workflow acceptance

Search and selection need business identity, not merely visible text.
For music, verify title, artist and active playback in the actual player.
For a file dialog, verify path and intended file type.
For a settings change, verify saved state after the dialog closes.

Example: select an observed search result for a requested track, then inspect
the player. If another track starts, correct using a fresh target observation.
Do not infer the correct album row from a previous screenshot.

Credentials and challenges follow the legitimate protected account flow.
Operating-system consent and user handoff remain real dependencies.

Record actual final application state and any instrumented duration. Separate
provider latency and backend work only when the measurements support it.

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


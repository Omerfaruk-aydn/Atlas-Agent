---
name: motion-design
description: Implement responsive motion with explicit timing and interruption behavior.
compatibility: Atlas native tools, permitted commands and explicitly connected MCP services; optional authoring/rendering dependencies must be verified.
metadata:
  provenance: atlas-authored-adaptation
  reviewed-sources: figma-use-motion, figma-implement-motion, figma-shaders
---

# motion-design

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

1. Determine what each animation communicates: action feedback, orientation, transition or continuous activity. Specify the resting and final states.

2. Inspect duration, easing, keyframes, transform origin and animation ownership. Use the project's tokens and rendering backend.

3. When connected Figma tools expose motion, correlate node IDs and timeline groups with actual component structure; do not infer timing from sibling order.

4. Keep pointer motion and application input synchronized. The displayed cursor must reach the observed target before input commits.

5. Define press, release, click, drag, cancellation and interruption independently. Preserve continuous feedback through provider waits where the activity is still active.

6. Drive related effects from one clock; use elapsed time rather than frame count. Frame rate changes must not alter total duration.

7. Handle rapid input by retargeting from the current rendered state. Prevent teleportation, repeated pulses and stale animation completion callbacks.

8. Use reduced-motion alternatives without losing state meaning. Cleanup timers, animation frames, observers and graphics resources at cancellation and shutdown.

9. Verify native and browser effects on actual frames; sample intermediate position and click response instead of reading metadata alone.

10. Validate DPI scaling, multi-monitor coordinates, 4K output, z-order and cursor recovery for native overlays when applicable.

11. Measure achieved frame intervals and interaction latency. Report environment and percentile evidence rather than guaranteeing 60 FPS from a timer setting.

12. Deliver timing/state specifications, editable code, observed frame evidence and known backend limitations.

## Animation specification

For each effect record the event, property owner, start/end state, duration or
continuous phase, easing, interruption and teardown. Explain the purpose:
feedback, orientation, state continuity or the user's requested ambient treatment.
Keep decorative effects from hiding actionable content.

Use monotonic elapsed time for continuous animation. Frame counts vary with load
and cannot define a stable phase. Separate rendering cadence from simulation
state and keep callbacks from restarting an effect when unrelated data changes.

For a target change during movement, begin the new interpolation from the current
rendered position. Preserve the actual hit location. Overshoot, trails and settling
must not misrepresent where an input action will occur.

## Desktop and browser coordinate checks

Identify screen, window, viewport and capture coordinate frames. Trace scaling,
browser zoom, monitor origins and DPI. Verify the pointer hotspot rather than
aligning the asset's bounding-box center to the target.

An overlay's visibility follows use-session ownership, not the duration of one
action. Keep active, waiting, handoff, cancellation and release distinct.
Restore cursor visibility and remove owned windows on every terminal path.
Never remove an unrelated user's overlay or change global cursor state without
the intended scoped backend behavior.

Click feedback must come from the actual executed action. Test rapid clicks,
stationary waiting and movement interruption. Keyboard-only work should not
display fictitious click feedback.

## Quality observations

Inspect edges, corner continuity, alpha blending, clipping and banner stability.
Independent top and side gradients often create diagonal seams; use continuous
geometry or blended fields consistent with the real window outline.

Measure observed frame intervals under the intended resolution and display setup.
Report target cadence separately from observed delivery. A schedule requesting
60 FPS is not a benchmark result.

Example: exercise an overlay during two actions separated by provider waiting,
then cancel with the supported control. Verify it remains visible while active
and restores cursor state on cancellation. Inspect both stationary and moving
states rather than approving a single still frame.

Deliver editable implementation, timing/lifecycle decisions and actual observation
results. Honor reduced-motion preferences with stable meaningful status.

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


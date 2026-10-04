# Evidence-based desktop control

The Windows computer tool uses native window enumeration and foreground
activation independently of UI Automation. A successful `focus` response means
the requested HWND was observed in the foreground. Windows can deny activation;
`focus_denied` does not authorize input into whichever window is currently active.
Keep `automation.window_id` on pixel and keyboard actions.

## Resolve a target before acting

`inspect` defaults to 150 controls; `automation.max_elements` accepts 1-500.
Inspection and lookup report `truncated` and `scanned_count`. Incomplete trees
cannot establish uniqueness or absence. Mutations and assertions refuse an
incomplete name/role search. An exact observed runtime `element_id` can stop
traversal at that unique identity, but must be refreshed after UI changes.

Control descriptions include `supported_patterns`. `invoke` requires Invoke;
`set_value` requires Value. Missing patterns return `unsupported_pattern` rather
than an undifferentiated provider failure. Recovery changes the method instead
of repeating the same unsupported action.

Non-password `set_value` operations verify readback after a brief settling
interval. Rejected/reset values return `value_not_applied` before a pipeline can
submit an unchanged field. A verified value is not a submitted search: responses
report `keyboard_focused` and `submission_required`. Click a freshly observed
field before Enter when its keyboard focus is not confirmed. Password values are
never read back. Offscreen duplicates are excluded from ordinary assertions and
mutations; hidden assertions still inspect all matching controls, and multiple
visible matches remain ambiguous.

For densely packed results, crop the relevant list and request an OCR text match:

```json
{
  "action": "ocr",
  "x": 200,
  "y": 250,
  "width": 700,
  "height": 400,
  "automation": {"name": "God's Plan"}
}
```

Matches provide text bounds, centers and surrounding line text. Matching
normalizes case, whitespace and straight/curly apostrophes. Two matching labels
remain ambiguous (`unique_match: false`); matching never clicks automatically.
Add `image_origin` to OCR coordinates to obtain screenshot input coordinates.
For UIA coordinates, subtract `screen_origin` instead. Validate the surrounding
artist/album or other distinguishing context before selecting a result. A text
match alone does not establish that a row is actionable.

## Bound waiting and measure cost

`assert` defaults to five seconds. `automation.wait_ms` accepts 1-15000 ms and
cannot extend the enclosing tool deadline. A timeout does not prove that an
earlier action failed; observe the state before any retry.

Accessibility/OCR results include `elapsed_ms`; interaction traces retain
per-action `duration_ms`. These measure tool execution, not model latency.
Approximately one minute is a planning target for simple tasks under ordinary
conditions, not a forced cutoff or guaranteed completion time. Prefer one
targeted observation, a supported action and a completion check over repeated
full-window inspections. Verify the requested result and stop.

UIA and OCR still use bounded PowerShell invocations. Native window listing,
focus and screenshot capture do not. The Apple Music task has not been timed
end-to-end as part of these regression tests; owned GUI and synthetic OCR
fixtures verify the tool contracts without interacting with the user's media.
# Session-bound observations and health

## Desktop recipes

`tool_pipeline.desktop` offers three bounded recipes. `prepare` finds an exact
application title (or explicit window ID), optionally launches the exact installed
Start-app name once, waits for the window, confirms focus and returns controls
and a crop. Dynamic document titles require an explicit window ID. Installed
application IDs come from Windows, not a model-provided shell command. Ambiguous
titles or unsupported installed IDs stop rather than choosing a first match.

`act` performs one already-resolved input and returns a new observation in the
same model turn. `fill_submit` focuses a supported value field, verifies readback
and field focus, then sends guarded Enter and observes. An optional same-window
`wait_for` assertion must pass before observation. Without it, a 250 ms settling
interval is explicitly not proof of readiness. Every recipe child goes through
the existing coordinator schema validation, hooks, permissions and desktop lease.
Denials, handoffs and failures stop the recipe without replaying inputs.

The recipes are preferred in the tool instructions to avoid separate model turns
for launching, keystrokes and post-input screenshots. Target selection on unseen
search results remains a model decision. No controls are chosen merely because
they appear first. A successful recipe is not a final task-completion assertion.

The embedded PowerShell driver script is loaded through a short encoded loader
from a temporary file with private permissions. The file is removed after use;
request data remains on stdin. This avoids Windows command-line length limits
without changing PowerShell execution-policy settings.

The computer tool now supports `observe` for an explicit window. It returns a
native visible window crop and structured controls numbered in the JSON list.
The crop can be occluded by another window; it is not a background window capture.
Focus the intended application through the existing verified focus action when
necessary. Snapshot identity, partial coverage and crop origin travel with the
image. This does not install or depend on Hermes/cua-driver.

Numbered `invoke`, `set_value` and `assert` requests use `snapshot_id` and
`element`. Each reference belongs to its conversation, expires after 30 seconds,
and is checked against a fresh runtime-ID lookup. Changed name, process, role,
automation ID, rectangle, enabled/visible state, or missing identity rejects the
request. Input attempts clear cached references. At most 64 recent conversations
and 500 descriptors per observation are retained, without screenshots or values.
Runtime-ID revalidation reduces stale-target errors but cannot make separate UIA
calls atomic against an application that changes between lookup and dispatch.

Pipeline `require_passed` gates accept only computer assertions reporting the
boolean `passed:true`. `observation_from` accepts only an earlier single observe
step. Dispatch stays behind existing hooks and permissions. A final observation
can carry its image and full structured controls; oversized selected output fails
instead of silently hiding target identities. Input requires a fresh subsequent
observation before another numbered action. Existing explicit selectors remain
available for expected-state checks after a mutation.

`health` reports independent read-only display, capture, enumeration, foreground
and optional window-provider checks. Failures degrade overall health and include
timings and recovery hints. Skipped capabilities are explicit. It does not infer
locked desktop or overlay interference from unrelated observations.

These changes establish correctness contracts; they do not establish a one-minute
Apple Music completion time. Compare model turns, tool time, wall time, wrong
actions and verified final state on repeated equivalent application tasks.


# Checkpoint-first desktop pipelines

The 3:09 benchmark session 2e0c7afe-f422-4d32-98f2-e26478875905 used semantic
Calculator observations. Explorer calls repeatedly requested only 30–60 elements,
produced truncated trees and consequently returned images in auto mode. The
problem was not a mandatory screenshot after every Pipeline call. Low scan limits
and overly conservative semantic navigation selection forced many image turns.

## Integrated behavior

- Desktop recipes now default to auto; explicit visual and semantic requests
  retain their semantics. Raw Computer observe keeps its existing visual default.
- An auto observation with an explicitly small truncated scan expands once to
  150 controls before selecting a screenshot. Failed or still-incomplete reads
  retain fallback; wrong response window identity is rejected.
- Complete grounded named Button/ListItem/TreeItem/MenuItem controls with usable
  bounds support semantic navigation. An unrelated unreadable Edit/Document no
  longer forces a screenshot when usable controls exist. A content-readback
  warning makes clear that unreadable field/document content is not verified.
- Sequence result:checkpoints validates actual native readback for every logical
  operation, then fresh final foreground. It omits all tree/screenshot reads,
  returns actual results and never manufactures a snapshot reference. Missing,
  truncated or mismatched assertion content stops with progress evidence.
- A sequence step's focus_window:true activates an explicit already-observed
  window, confirms focus and then sends its inputs. Known workflows can span
  multiple applications without one model turn per activation. It never launches
  another app or silently substitutes an ambiguous handle.

## Choosing a mode

Use sequence/result:checkpoints for fully resolved keyboard/control operations
with explicit acceptance checkpoints. Keep sequence with observation for the next
decision needing new UI controls. Use adaptive for conditional state or live
pattern selection, and transition for a new dialog. Do not put unseen clicks or
unknown dialog targets into a known sequence. Explicit visual requests conflict
with a checkpoints-only result and are rejected before input.

All existing permission, hook, foreground, timeout and child-budget controls
remain. Checkpoint success does not imply the user's whole task is complete.
The read-only expansion adds a native inspection; it is intended to avoid a
separate visual inference turn, not guarantee a specific duration.

Candidate: D:/Atlas/.atlas/atlas-desktop-flow-v14-dev.exe. Live elapsed duration
has not been measured. Latest user measurement remains 3:09.

Validation: full tools, prompt and subagents suites passed (85.517s, 9.716s,
1.402s). Scoped lint reported zero issues. Build, version, formatting and diff
checks passed. Regression tests cover multi-window actual readback without image
calls, missing/truncated/wrong-window results, focus denial, explicit visual
conflicts, successful bounded scan expansion, retained fallback, wrong expanded
window identity and unchanged explicit observation modes.

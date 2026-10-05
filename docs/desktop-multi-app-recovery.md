# Multi-application desktop recovery

The 2026-10-05 calculation/file task was run with
`.atlas/atlas-control-question-dev.exe`, session
`6f7dfd55-6375-4390-9986-a1201355f22a`.
Its recorded start/end were 19:31:53–19:36:36, or 283 seconds. The conversation
contains 29 assistant turns and 28 top-level tool calls. The desktop journal
contains 96 child operations, with 27,578 ms of recorded operation duration:

| Operation | Count | Recorded total (ms) |
| --- | ---: | ---: |
| windows | 40 | 22 |
| focus | 9 | 81 |
| observe | 19 | 19,075 |
| type | 8 | 4,406 |
| key | 7 | 161 |
| hotkey | 7 | 472 |
| click | 4 | 1,696 |
| launch_app | 2 | 1,665 |

The remaining wall time is not a measurement of provider latency alone. It
includes model/tool orchestration, application waits and local recipe settling.
The journal does not establish a complete provider/tool timing partition.

## Observed recovery costs

- Notepad preparation launched the application but timed out waiting for an
  exact display-title match. Its window title already contained document text.
- Save As activated a separate dialog. Typing with the parent Notepad window
  ID failed with `wrong_window`, followed by enumeration and a corrected input.
- After closing the requested windows, the agent prepared an old window handle
  and received `target_missing`.

## Changes

Native Windows enumeration includes `process_name` and `owner_window_id`.
Process image names are read from observed PIDs using limited query access,
once per PID per enumeration. Failure leaves the identity unknown. No
PowerShell or UIA call is added for this metadata.

For known Notepad display names, preparation matches `notepad.exe` and excludes
owned dialog windows. It preserves foreground preference among eligible main
windows and stops on other ambiguity. An explicit `window_id` remains pinned.
An unrelated process with a familiar title is not eligible.

`observe` includes `foreground_window` from the same enumeration used to crop
the requested window. The original `window_id`, controls and crop do not change.
The metadata reports focus at capture time; it does not authorize input or prove
that a foreground dialog is part of the task. Existing live foreground guards
still reject input to the wrong window. Missing metadata leaves focus unknown.

The main prompt, desktop role and tool descriptions instruct the agent to
inspect new dialogs, track task-owned windows and treat absence after closure
as closure evidence. They preserve intermediate results and final file-content
verification, and do not substitute shell/file mutations for requested GUI work.

## Validation boundaries

Regression fixtures cover changing Notepad titles, an unrelated process with
the same display name, owned dialogs, foreground capture metadata, preservation
of the requested observation target, and reuse of one window enumeration.
Windows tests read actual process identity and native ownership metadata without
typing into or closing user applications. Existing stale-target, focus, pipeline
gate and cancellation tests remain applicable.

These checks establish the corrected boundaries. An improved end-to-end task
time requires a new run with the development executable; no speedup is claimed
from fixtures or prompt changes alone.

## Follow-up run: 20:18:41–20:23:16

Session `9b164983-d939-4cfc-9de3-6a98fb4379fc` took 275 seconds with
`atlas-desktop-multiapp-dev.exe`. It contains 30 assistant turns, 29 top-level
tool calls and 76 desktop child operations with 29,956 ms recorded duration.
Windows enumerations fell from 40 to 17. The previous `wrong_window` and stale
closed-handle errors did not recur, but total assistant turns rose by one.

The remaining native error was launching `Notepad` on a Turkish installation
whose registered name was `Not Defteri`. The launcher now resolves known Notepad
aliases through registered legacy or packaged Notepad IDs, preserving exact
name precedence, installed-registration checks and ambiguity rejection. It does
not synthesize a launcher or use executable-name suffix matching.

The run also typed the same three-line text twice, first with CRLF and then LF.
The computer tool now normalizes CRLF pairs to one LF before keyboard typing.
LF input and standalone CR characters retain their prior behavior. Fresh
foreground and closure evidence should be reused rather than enumerated again;
the main prompt, role and tool guidance now say this explicitly.

The follow-up executable is `atlas-desktop-multiapp-v2-dev.exe`. The observed
275-second run is the baseline for this executable, not a measurement of its
performance. End-to-end improvement still requires another controlled run.

## Comparison after the v2 run

The completed v2 session is `54564a58-62f1-4355-93c6-acd5f0f64c05`.
The first user message was created at 20:34:27 and the last assistant message
finished at 20:38:40 (253 seconds); session bookkeeping completed at 20:38:41.
The user's measured 4:13 agrees with message completion. The intervening session
`3520bc82-6269-4cf3-bd49-cb6ffe9dfae1` ended with `context canceled` and is
excluded from comparisons of completed tasks.

| Metric | Question dev | Multi-app dev | Multi-app v2 dev |
| --- | ---: | ---: | ---: |
| Completion time | 4:43 | 4:35 | 4:13 |
| Assistant turns | 29 | 30 | 27 |
| Top-level tool calls | 28 | 29 | 26 |
| Journaled desktop operations | 96 | 76 | 69 |
| Journaled windows operations | 40 | 17 | 13 |
| Journaled observe operations | 19 | 20 | 20 |
| Recorded desktop duration | 27.578 s | 29.956 s | 27.257 s |
| Failed top-level tool calls | 3 | 1 | 0 |

All three completed sessions record `chatgpt` / `gpt-6.1-sol`. This verifies
provider/model identity, not identical request latency, effort settings or
initial desktop state. Journal counts exclude internal backend calls that do
not pass through the journaled computer dispatch.

The latest run launched `Notepad` successfully, typed the three-line note once,
and did not repeat the earlier save-parent or disappeared-window failures.
Relative to the previous run it used three fewer assistant turns, three fewer
top-level calls and seven fewer journaled desktop operations. Recorded desktop
execution fell by 2.699 s; the 22-second wall-time improvement is therefore
consistent with fewer model/orchestration transitions as well as less tool
execution. It is not a direct measurement of provider latency savings or an
isolated causal proof for each code change.

### Remaining observed opportunities

- Six top-level `computer windows` calls remain, including one after Calculator
  preparation, one after Notepad preparation and one after a closing pipeline
  that already returned a window list. Some are candidates for elimination;
  availability/freshness of the returned evidence must still be checked.
- Opening Explorer with Win+E is followed by another model turn to enumerate
  windows. A future recipe could return the newly observed foreground, without
  guessing its identity or automatically sending subsequent input.
- Folder creation is followed by a separate F2/name/Enter sequence for `deneme`.
  Whether this corrected a real field-state problem requires examination of
  the returned observation; success responses alone do not prove redundancy.
- There are still 20 observed checkpoints. They include intermediate calculation
  results and save/rename verification, so reducing the count indiscriminately
  would weaken the user's acceptance criteria.

This comparison changes documentation only. It does not introduce another
development binary or claim a new performance improvement.

## Compact evidence output

`atlas-desktop-state-v3-dev.exe` adds optional `desktop_state` summaries to
desktop recipes and selected computer observation/window results in pipelines.
The summary exposes the requested target, foreground identity and observation
timestamp directly rather than requiring another model turn to extract these
from nested output. Existing content and image evidence remain available.

Within one pipeline, successful scoped Alt+F4 calls record candidate handles.
A subsequent existing Windows list may report `absent_closed_window_ids` for
those handles. Listed handles are not considered closed, malformed output yields
no summary, and a list reaching the native 500-window cap cannot prove absence.
No extra windows query, input replay or permission/hook bypass is introduced.
The evidence is scoped to that observation; it is not a permanent window cache.

Prompt and role instructions prefer this summary when sufficient, preserve
new-dialog inspection and refresh observations when focus or identity changes.
Regression tests cover single-pass closure evidence, false closure prevention,
incomplete lists and distinct observed/foreground window identities. Actual
speedup must be measured against the 4:13 v2 baseline.

## Measured v3 run: 3:30

Completed session `44ec09e4-54bc-454a-9aa2-24506f2ab000` started at
20:53:07 and finished at 20:56:37: exactly 210 seconds in the message record.
The provider/model remained `chatgpt` / `gpt-6.1-sol`.

| Metric | v2 (4:13) | v3 (3:30) |
| --- | ---: | ---: |
| Assistant turns | 27 | 24 |
| Top-level tool calls | 26 | 23 |
| Journaled desktop operations | 69 | 66 |
| Journaled observe operations | 20 | 15 |
| Journaled windows operations | 13 | 15 |
| Recorded desktop duration | 27.257 s | 22.796 s |
| Failed top-level tool calls | 0 | 1 |

The 43-second reduction accompanies three fewer assistant turns and five fewer
observations. Notes were typed and Save invoked in one grounded batch; file
rename steps were also grouped before their observation. The final closing
pipeline supplied its verification list without an additional standalone
windows call. These are concrete flow differences, not just elapsed-time claims.

The remaining failure was preparation by the English name `File Explorer` on
the localized installation. The agent recovered with Win+E and fresh window
selection. Standalone windows queries still followed Calculator and Notepad
preparation and new dialogs. Windows enumeration count therefore increased by
two; v3 does not eliminate every repeated query, despite the faster overall run.

Recorded desktop execution fell by 4.461 seconds. The other 38.539 seconds of
wall-time difference is not a provider-only measurement: it includes model
generation, orchestration, recipe waits and other unjournaled work. These paired
runs support an observed improvement, while differing initial desktop states
and request latency prevent attributing every second to one code change.

Relative to the initial 4:43 run, the completed run is 73 seconds faster, with
29 to 24 assistant turns, 96 to 66 journaled desktop operations and 19 to 15
observations. Against the user's reported Hermes 3:50 run, its measured wall
time is 20 seconds shorter; Hermes internals were not available for this audit.

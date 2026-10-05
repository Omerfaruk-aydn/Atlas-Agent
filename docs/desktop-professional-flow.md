# Verified desktop flows

The selected desktop improvements extend the existing computer tool and guarded
tool_pipeline. They use the normal tool palette, hooks, permissions, desktop
ownership and cancellation. No persistent helper or permanent window cache is
introduced.

## Implemented behavior

1. **Verified action blocks:** sequence accepts 1-12 already-resolved operations,
   each with 1-8 known inputs followed by a required checkpoint and fresh
   observation, capped at 64 normal child operations. The complete
   recipe is validated before the first mutation. Actual assertion values and
   matching readback are preserved for every step, including completed steps
   before a later failure. Intermediate images are explicitly marked omitted;
   the final image is the only attached image.
2. **Window transitions in one call:** prepare already finds, launches, focuses
   and observes the target application. Transition now records a baseline,
   sends one known input and observes its expected foreground application or
   exactly named related dialog. Shell dialogs with hidden helper owners require
   a new #32770 window, exact title and verified same source/owner process;
   existing or foreign-owner windows do not qualify. It never sends follow-up
   typing automatically.
3. **Localized application identity:** File Explorer aliases resolve through the
   installed Microsoft.Windows.Explorer identity. Open folder windows must match
   explorer.exe and native CabinetWClass; desktop, worker and taskbar windows are
   excluded. Existing Calculator and Notepad identity handling is retained.
4. **Readable UIA content:** observations expose bounded ValuePattern values,
   TextPattern text, availability, truncation and keyboard focus. Password
   names/content are redacted without reading their patterns. Pattern failures
   remain local to each field. Assertions return actual observed values.
6. **Condition readiness:** assertions wait only until an explicit condition is
   met, with a maximum 15-second wait and parent cancellation. Field set_value
   checks immediately and polls retained value for at most 500 ms. Ordinary act
   no longer adds a fixed settling delay; an immediate observation does not
   certify readiness.
7. **Observation selection:** visual remains the compatible default. Semantic
   avoids capture and reports current native foreground identity. Auto chooses
   semantic only for a complete, readable tree on the intended foreground
   window; incomplete or inaccessible content falls back to a visual crop.
   Snapshot caches retain descriptors rather than value/text content.
8. **Bounded recovery:** typed input failures can collect one fresh native window
   list and one semantic observation within five seconds. Recovery uses normal
   guarded dispatch, preserves the original failure and never replays input.
   Prepare retains its existing focus/handle recovery without duplicate probes;
   explicit handles remain pinned. Denial, handoff and cancellation stop reads.

## Use

The embedded computer/tool_pipeline descriptions and main/desktop role prompts
include the contracts and examples. These are model tools, not new slash commands.
Select auto for content-oriented work when no layout-dependent decision is
needed. Use a recent visual observation or OCR before choosing pixel coordinates.
Use stable observed element IDs for checkpoints; a changing display label is not
a stable identity. Stop a sequence before a new result requires target selection.

An in-window WinUI panel does not necessarily create a separate owned window.
For that case, use act or fill_submit with wait_for on a freshly identified
control in the same window. Transition does not invent an owner relationship.
Provider-unavailable visual fallback has no numbered snapshot controls.

## Verification

Regression fixtures cover whole-sequence validation, actual intermediate results,
failed gates, partial progress, wrong-owner dialogs, timeout, cancellation,
denial/handoff, schema access, semantic selection, provider fallback, password
redaction and descriptor-only caches. Native tests cover localized installed
Explorer routing, window class metadata and isolated UIA pattern failures.

The opt-in Windows owned WPF fixture also validates live ValuePattern/TextPattern
readback and actual-value assertions. It only mutates its own temporary window.
Physical keyboard input is skipped if the fixture loses foreground ownership.

The first professional development build was measured in the user's subsequent
6:22 session. The regression and resulting v4 changes are recorded below. V4 has
not yet been benchmarked; no new wall-time improvement is claimed before measurement.

Validated on Windows on 2026-10-05:

- Full affected package tests passed: internal/agent/tools, internal/computer,
  internal/agent, internal/agent/prompt and internal/subagents, with count=1.
- Newly changed desktop/observation tests passed again after final edits.
- The owned WPF fixture passed with live value and document-text readback.
  A separate fixture run verified physical Ctrl+A; the last run skipped physical
  input after foreground changed, preserving the guard.
- Scoped golangci-lint reported zero issues; git diff --check passed.
- Development build and --version passed:
  D:/Atlas/.atlas/atlas-desktop-professional-dev.exe,
  v0.15.6-desktop-professional-dev.

Build/test temporary files were placed outside the repository to preserve the
meaning of tests that intentionally run outside a Git checkout. Source and
existing uncommitted changes remain in D:/Atlas on main.

## 6:22 regression audit and v4 correction

Session 5ff3e017-ea3f-4b14-880c-cbea9d373b33 ran 21:32:12-21:38:34.
Comparison session 44ec09e4-54bc-454a-9aa2-24506f2ab000 ran in 3:30.

| Recorded property | Previous 3:30 | Professional build 6:22 |
| --- | ---: | ---: |
| Model | gpt-6.1-sol | gpt-6-sol |
| Assistant messages | 24 | 41 |
| Top-level tool calls | 23 | 40 |
| Journaled desktop operations | 66 | 149 |
| Observations | 15 | 36 |
| Native window enumerations | 15 | 71 |
| Recorded desktop duration | 22.796 s | 50.241 s |

The provider was ChatGPT in both records; model identities differed. Reasoning
effort equivalence was not established. The records do not support attributing
all 172 additional seconds to code, tools or provider latency alone.

Observed flow defects were concrete:

- The model issued separate act calls for known rename/typing/Enter steps and
  never used sequence. V4 supports act.inputs and step.inputs at a logical
  operation boundary, validates the whole group, and observes once at its end.
  Tool, main and desktop role guidance now names these cases explicitly.
- Four successful dismissal inputs were followed by target_missing errors on
  the already-disappeared source window. V4 returns verified absence and fresh
  foreground state. It handles asynchronous disappearance from recovery evidence
  without another input or duplicate recovery read.
- The Explorer properties window belonged to the same explorer process but had
  a hidden helper owner rather than the folder window handle. The strict direct
  owner check waited five seconds and missed it. V4 verifies the newly appeared
  native dialog/title/process/helper relationship.
- Notepad returned a focused content pane with document text in its UIA name,
  rather than Value/Text patterns. Frame-only retries were unnecessary. V4
  recognizes this exposed focused content; unreadable or oversized content still
  requires visual evidence.
- During rename, GetFirstChild failed on a changing UIA branch. V4 marks that
  branch incomplete and continues bounded traversal. Assertion reads can retry
  transient incomplete/provider state twice; unsupported patterns fail directly.

Regression tests cover grouped validation, input denial, child budget, actual
intermediate evidence, synchronous/asynchronous dismissal, hidden helper owners,
foreign/existing dialogs, focused legacy content and transient traversal/read
failures. Five affected package suites passed; targeted tests passed again after
final changes. Scoped lint reported zero issues. V4 executable:
D:/Atlas/.atlas/atlas-desktop-flow-v4-dev.exe.

## 3:18 audit and v5 correction

Session 39e715b3-5e6b-4b40-88eb-c9857ec9547f lasted 198 seconds and used
gpt-6.1-sol. It made 22 top-level calls, including four standalone window lists
after observations and a three-result calculator sequence. The three calculated
values were verified as 69104, 8638 and 9000; final file content was also read.

The calculator sequence previously performed assert plus observe three times.
For auto/semantic sequences, v5 omits an intermediate observation only when the
assertion returns the exact expected, non-truncated string from the same window
and explicit element used by the next keyboard-only step. Intermediate actual
values remain in checkpoints; failure retains completed evidence. Visual mode,
different targets, pointer input and missing/incomplete readback retain the
observation boundary. The final observation is always preserved.

Explorer creation required recovery after a shortcut without verified content
focus. Main and desktop-role guidance now distinguishes foreground-window focus
from file-list focus and requires the observed name editor when creation is
uncertain. These instructions do not authorize guessed coordinates or replay.

The next candidate is D:/Atlas/.atlas/atlas-desktop-flow-v5-dev.exe. Its end-to-end
duration has not been measured; the previous 3:18 remains the latest benchmark.
Three affected package suites (agent/tools, agent/prompt, subagents) passed.
Sequence regressions passed after the final test formatting change; scoped lint
reported zero issues. The candidate build and --version check passed.

## 4:34 audit and v6 correction

Session bd168346-4518-4be5-abe7-2b7e2499c889 used gpt-6.1-sol and made 30
top-level calls, compared with 22 in the preceding 3:18 record. Database session
timestamps span 275 seconds; the user measured 274 seconds. V5's stable-result
optimization did execute: both intermediate calculator checkpoints contain
observation_omitted:true and actual values. The top-level calculator sequence
spanned about 14 seconds in both records; no end-to-end speedup is established.

The slower run included an input/inputs schema error, extra Explorer focus and
observation recovery, two todo updates, and a failed file read using an address
label that omitted the ampersand in the actual account directory. The agent then
closed and reopened Notepad to verify the renamed document. These are observed
additional decisions, not a measurement of pure provider latency.

V6 adds display_path_warning to observations containing Windows path labels.
Original UIA Name and exact Value remain unchanged; no filesystem path is guessed
or repaired. Main, tool and role guidance distinguish display labels from exact
address-edit Value/Text or resolved directory evidence, and reuse verified content
when fresh while still verifying the final artifact. The act schema explicitly
requires choosing input or inputs; strict conflicting-input rejection is retained.

Candidate: D:/Atlas/.atlas/atlas-desktop-flow-v6-dev.exe. End-to-end duration has
not been measured. Targeted desktop/observation regressions passed, including the
display-path-versus-exact-value red/green test.

## 3:14 audit and v7 candidate

Session 761d3c2c-e8ac-42fb-a3f9-65b385995a0f used gpt-6.1-sol with 23 top-level
calls, no tool errors, four standalone window lists and three todo updates.
User duration was 194 seconds; database timestamps span 195 seconds. Calculator
intermediate readback omission was active and all three results were verified.
No bad-path file read or duplicate Notepad reopening occurred in this record.

V7 observes a directly owned native #32770 foreground dialog after act/fill_submit
in the same recipe call. It does not redirect input, focus or mutate the dialog.
The dialog snapshot, content and image replace the previous observation together;
source_window_id and workflow.input_window_id retain the original input target.
Assertions already performed retain condition_window_id for their source target.
Unrelated owners, non-dialogs, missing foreground identity and minimized windows
retain the original observation. Invalid returned identity or a denied read stops
the recipe without replay. Indirect helper owners still require transition's
explicit matching; this path does not relax those checks.

Prompt/role guidance also reserves persisted todos for explicit tracking needs
and independent/deferred work instead of updating a board per short GUI phase.
Fresh result evidence and requested intermediate/final verification remain required.
Candidate: D:/Atlas/.atlas/atlas-desktop-flow-v7-dev.exe. No v7 task duration has
been measured; the latest end-to-end benchmark remains 3:14.
V7's three affected full package suites passed, as did the targeted owned-dialog
regression. Scoped lint reported zero issues; build, --version and diff-check
completed successfully. No live desktop benchmark was replayed during development.

## 2:54 audit and v8 candidate

Session b8c3b965-5b05-4f17-aaca-844d6814b781 used gpt-6.1-sol, 20 top-level
calls and no tool errors. Both user timing and database timestamps span 174
seconds. V7 returned the directly owned Save As dialog in the typing/save call;
the agent reused its ID without another window list. No persisted todo updates
were used. Calculator intermediate results and the saved document were observed.

Remaining Explorer address attempts used Alt+D twice and Ctrl+L once. Inspection
of those returned trees showed truncated:false: the provider represented the
address as focused Pane display text, not readable Value/Text in ControlView.
This is a provider-view limitation, not evidence of a truncated tree in this run.

V8 adds a separately scoped focused_element from UIA FocusedElement. RawView
ancestry must reach the exact requested window runtime ID within 32 ancestors
and a 250 ms traversal budget before its content is described. Foreign/unfocused
controls and incomplete ancestry yield no focused read. Describe retains password
redaction and 4096-character Value/Text bounds. Go also bounds/redacts this field
and omits its live content entirely from the snapshot cache. It has no numbered
snapshot reference and does not claim the main tree is complete. The provider
may still expose no exact address; prompt guidance prohibits repeated identical
shortcuts in that case and requires another grounded resolution technique.

Candidate: D:/Atlas/.atlas/atlas-desktop-flow-v8-dev.exe. No v8 end-to-end task
duration has been measured. The latest benchmark remains 2:54.
Four affected full package suites passed (agent/tools, computer, agent/prompt,
subagents); prompt/subagent suites passed again after final guidance edits.
Scoped lint reported zero issues. Build, version and diff checks passed.

## 2:48 audit and v9 candidate

Session 27e0fee6-df58-43a7-a52e-1157d004bf32 made 18 top-level calls without
tool errors. User duration was 168 seconds; database timestamps span 169 seconds.
Compared with the preceding run, Explorer address retries fell to one click
after the initial Ctrl+L. This correlation does not isolate code from provider
or model variation. Three standalone window lists and a saved-document prepare
remained.

V9 lets transition use a validated group of 1-8 known same-window inputs. A
focused filename entry and Enter can return the expected application observation
in the same model turn. Every input still uses the normal permission/hook path;
whole-group scope validation precedes any action, and denial stops later inputs.
Window matching, baseline, bounded wait and readback remain unchanged. Returned
document/title evidence must establish save results; disappearing dialogs alone
are insufficient.

Embedded skills now publish atlas://skills/ addresses. The view resolver accepts
legacy crush://skills/ references, normalizes response metadata, and the TUI
renders old view-call headers with the Atlas prefix. Disk paths and other schemes
are unchanged. Tests cover canonical reads, legacy compatibility, builtin
discovery, rendering and grouped-transition denial/scope.

Candidate: D:/Atlas/.atlas/atlas-desktop-flow-v9-dev.exe. No v9 end-to-end duration
has been measured; the latest benchmark remains 2:48.

## 3:13 audit and v10 candidate

Session 474de000-b56f-4405-a558-d6227da36963 used gpt-6.1-sol, made 20
top-level calls and had no tool errors. The user measured 193 seconds; database
timestamps span 195 seconds. Explorer initially showed Downloads instead of the
previous run's Desktop, requiring an extra navigation call. Text entry and Ctrl+S
were split into separate calls. The grouped save transition executed successfully
and returned Notepad readback. This record does not establish a code-caused
25-second regression: starting state and model-selected operations differed.

Three standalone window enumerations followed observations already containing
the needed IDs. V10 serializes recipe decision identities before large element
trees: window, snapshot, foreground, desktop state and focused control. All JSON
fields and values remain unchanged, and the input evidence map is not mutated.
This ordering aims to make identity reuse clearer; reduction in model calls is
not yet demonstrated. Main guidance explicitly groups known document text and
save shortcut when no intermediate decision is needed, preserving saved-content
verification and separate decisions for newly observed dialog fields.

Candidate: D:/Atlas/.atlas/atlas-desktop-flow-v10-dev.exe. No v10 end-to-end
duration has been measured. The latest benchmark is 3:13; the best remains 2:48.
Both affected full package suites passed, targeted desktop regressions passed,
and scoped lint reported zero issues. Build, --version and diff checks passed.

## 2:55 audit and v11 candidate

Session f8dd6cbc-1810-47e9-b5a4-66fe56b1cbcf used gpt-6.1-sol and made 21
top-level calls without tool errors. User duration was 175 seconds; database
timestamps span 176 seconds. Document entry and Ctrl+S were grouped and the save
transition returned Notepad content. Two window lists followed usable prepare
observations. Explorer address navigation still consumed several calls.

Alt+Enter opened Properties through a hidden Explorer helper owner. The act
result described the folder controls while exposing the separate foreground
dialog identity; the model spent another window-list turn discovering it. V11
reads this dialog in the same act call only after verifying a new window against
a pre-input baseline, current source process/class, current foreground and a
same-process hidden owner. The source and dialog identities remain separate and
the guarded child dispatcher performs the read. It never redirects input.

Tests cover valid adoption, pre-existing/foreign dialogs, visible or unverified
owners, changed source identity, stale foreground, malformed lists and permission
denials before or after the shortcut. Denial never replays a mutation. This saves
a potential model discovery turn at the cost of bounded native window reads;
an end-to-end speed improvement has not been measured.

Candidate: D:/Atlas/.atlas/atlas-desktop-flow-v11-dev.exe. The latest measured
benchmark is 2:55; the best recorded time remains 2:48.

Validation: full tools and prompt package tests passed, scoped lint reported
zero issues, and build, version and diff checks passed. The new regression first
failed on missing dialog adoption and then passed with the implementation.

## 3:18 audit and v12 candidate

Session d017a793-32e8-4c58-8f50-aafd2b5764f2 used gpt-6.1-sol, made 25
top-level calls and returned three tool errors. User duration was 198 seconds;
database timestamps span 199 seconds. Initial Explorer preparation failed twice
with focus_denied (actual foreground Program Manager), then the old handle
disappeared. From initial Explorer prepare to successful preparation of its
replacement, message timestamps span approximately 51 seconds. This includes
model and tool time; it is not an isolated native-execution measurement.

V11's Properties path did run successfully: rename/Alt+Enter returned the dialog
snapshot in one call without a following discovery list. It does not explain the
initial activation failures. The actual Windows cause of those failures remains
unproven; no focus bypass or guessed click workaround was added.

Prepare already re-enumerated after automatic focus failure, but discarded the
fresh window list when selection returned the same handle. V12 preserves that
list in error Content and metadata. Explicit-handle focus failures collect one
guarded fresh list without redirecting or retrying focus. A changed automatic
handle can still be focused once; if that fails, evidence is refreshed after
the attempt. Permission denial and StopTurn stop recovery. Guidance discourages
retrying the same failed activation without changed state or a verified method.

Candidate: D:/Atlas/.atlas/atlas-desktop-flow-v12-dev.exe. No v12 end-to-end
duration has been measured. Tests cover automatic and explicit selection,
metadata preservation, malformed/oversized lists and denied recovery reads.

Validation: full tools and prompt package suites passed (84.203s and 8.777s),
scoped lint reported zero issues, and build, version and diff checks passed.
The evidence regression failed before the implementation and passed afterward.

## V13 integrated adaptive execution

User selected application adapters, conditional execution, automatic method
selection, result verification and smart recovery. Their integrated design,
contracts, limits and regression coverage are documented in
`docs/adaptive-desktop-execution.md`. Known stable sequences retain their existing
path; variable workflows can use adaptive with complete preflight evidence and
required checkpoints. The default is semantic/visual auto selection, not an image
per operation. No v13 live benchmark duration or success rate has been measured.

Candidate: D:/Atlas/.atlas/atlas-desktop-flow-v13-dev.exe. Full tools, prompt and
subagents tests passed; scoped lint, build, version and diff checks passed.

## V14 checkpoint-first pipelines

The latest 3:09 record showed repeated small UIA scan limits forcing visual
fallback. V14 adds bounded auto scan expansion, complete grounded semantic
navigation evidence and checkpoint-only multi-window sequences. Detailed behavior
and validation are in `docs/desktop-checkpoint-pipeline.md`. No v14 live duration
has been measured; the latest user measurement remains 3:09.

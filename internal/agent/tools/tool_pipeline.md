Run a bounded declarative tool program. Supply steps with unique id, an allowed tool name and structured arguments. An optional items array repeats a step with the exact string "$item" substituted in arguments; no shell interpolation is performed. if_success may reference a preceding step. return selects the step outputs to expose.

For ordered browser work, supply `browser:{tab_id,origin,steps:[...]}` instead
of `desktop` or manual `steps`. There are 1-32 logical steps and up to 64
ordinary browser calls including verification. Each step has unique `id`,
`action`, semantic `target` (the browser advanced fields), optional `text`,
`url`, `key`, and optional `verify` DOM assertion. Every child retains hooks,
permissions, interaction ownership and cancellation. The first error or failed
postcondition stops all later input; dispatched mutations never automatically
retry. `return` can select logical step IDs or `id/verify` results.

Allowed workflow actions: navigate, semantic_click, semantic_type, find,
assert, wait_for, tabs, tab_new, tab_select, tab_close, frames, upload,
download_start, download_wait, popup_wait, dialog_wait, dialog_handle, key.
Actions requiring a new target choice should end the group. These workflows
do not capture screenshots automatically; DOM postconditions reduce unnecessary
visual analysis. Use explicit visual evidence for visual tasks.

Example a known field and saved-state check:
`{"browser":{"steps":[{"id":"fill","action":"semantic_type","text":"Atlas","target":{"scope":"#profile","role":"textbox","name":"Name"},"verify":{"scope":"#profile","role":"textbox","name":"Name","condition":"value","expected":"Atlas"}},{"id":"save","action":"semantic_click","target":{"scope":"#profile","role":"button","name":"Save"}},{"id":"done","action":"wait_for","target":{"selector":"#status","condition":"text","expected":"Saved"}}]},"return":["done"]}`

Use actual observed targets. Workflow tab_id/origin become expected_tab_id/
expected_origin guards on the current page. After explicit tab selection,
override/bind the new expected_tab_id on subsequent steps. A document_id guard
intentionally expires on navigation; resolve the new document before reuse.

`bindings` on workflow or manual steps copies a bounded nonempty string from
an earlier JSON result. Syntax: `{"advanced.tab_id":"popup#/tab_id"}`.
Only advanced.tab_id, expected_tab_id, frame_id, document_id, download_id and
newer_than destinations are supported. Sources use JSON Pointer object fields;
missing/malformed evidence stops before dispatch. No scripts or expressions are
evaluated, and bindings cannot be combined with items. Verification inherits
its action's tab/frame bindings unless overridden by an explicit verify target.
Observe existing tabs before opening a popup. Example select a newly observed
popup and wait for its own document:
`{"browser":{"steps":[{"id":"popup","action":"popup_wait"},{"id":"select","action":"tab_select","bindings":{"advanced.tab_id":"popup#/tab_id"}},{"id":"ready","action":"wait_for","target":{"condition":"ready"},"bindings":{"advanced.expected_tab_id":"popup#/tab_id"}}]}}`
For downloads, arm with download_start before triggering the transfer, then
bind `advanced.newer_than` to `arm#/started_at` on download_wait. The exact file
and completed selected-tab transfer are checked together. File selection and
transfer completion do not establish server receipt or correct file contents.

For desktop application work, choose a recipe at a logical operation boundary.
Use `act.inputs` to group already-resolved field entry or shortcut steps,
then obtain one assertion/observation. Use `sequence` to preserve multiple requested
intermediate results in one model turn. Use `prepare` once per application, and
reuse its fresh identity until state changes require new selection.
Computer also exposes the checkpoint sequence engine directly as `action:batch`
with `batch.groups` (up to 24 groups, 16 inputs each, 128 normal child calls,
120 seconds). Use one ordered call for known operations with actual result
checkpoints; do not issue a model turn for every key or run desktop inputs in
parallel. Pipeline itself accepts up to 64 steps and 128 invocations.
Do not supply `steps` or `return` with a recipe. Every recipe child still uses
the same tool filtering, schema validation, hooks, permissions and desktop lease.
Ordinary recipes have a 30-second overall limit; sequence has 120 seconds and
transition has its configured 1-15000 ms deadline (default 5000).
Denials and handoffs always stop it. Platform recovery gathers fresh evidence;
it never replays an uncertain mutation.

`adaptive` combines conditional work, method selection and mandatory readback.
Supply 1-8 `adaptive_steps`, each with `checkpoint` and either a single semantic
`input` (invoke/set_value) or 1-16 already-resolved `inputs`. Optional `when` uses
the same window and an explicit selector/condition against the current complete
observation. False skips; unavailable content, password controls, ambiguity or
incomplete observation stops. `when.wait_ms` must be omitted; result checkpoint
waits remain bounded. The entire plan is validated before the first child call.

For a semantic input, live Invoke/Value support selects accessibility. Otherwise
only Button/Edit roles have automatic alternatives. A focused Button uses a
focus assertion then Enter; an unfocused Button uses one derived center click,
never click followed by Enter (which would activate it twice). An Edit replacement
uses its observed center, click, exact focus assertion, then Ctrl+A/type.
Missing bounds, disabled/offscreen controls and failed focus stop before text/Enter.
A runtime error after any mutation NEVER selects
another method or replays the attempted action. All children retain hooks,
permissions, foreground checks and the desktop lease.

Every executed step must pass its actual result assertion. Later steps stop on
failure, malformed/truncated evidence or modal foreground changes. Reports retain
`adaptive_progress` with verified checkpoints and attempted/skipped step states.
Use those facts for recovery; do not infer completion from input delivery.
Adaptive defaults to observation:auto, has a 60-second limit and a prevalidated
64-child normal-operation budget. Current final observations can be reused by
the following same-window step. Intermediate images do not become separate model
turns; explicit visual observation requests remain visual. Pixel-dependent or
incomplete evidence still requires inspection. Stable known workflows should use
sequence to avoid unnecessary adaptive target-resolution reads.

Prepare returns `application_adapter` when a native foreground identity is
available. Explorer, Notepad and Calculator profiles describe normal supported
methods and verification requirements; generic profiles make no app-specific
claims. Profiles contain no persistent handles or task-specific outputs.

Example replace an observed field only if its current value is empty:
`{"desktop":{"mode":"adaptive","adaptive_steps":[{"when":{"window_id":"OBSERVED_ID","element_id":"OBSERVED_FIELD","condition":"value","expected":""},"input":{"action":"set_value","automation":{"window_id":"OBSERVED_ID","element_id":"OBSERVED_FIELD","text":"User text"}},"checkpoint":{"window_id":"OBSERVED_ID","element_id":"OBSERVED_FIELD","condition":"value","expected":"User text"}}]}}`
These identities must come from observation. A skipped predicate does not prove
that the user's intended text is already present; reconcile that state separately.

For a fully resolved known workflow, sequence supports `result:"checkpoints"`.
It returns native actual readbacks for every required checkpoint and a fresh final
foreground identity, without intermediate/final screenshots or control trees.
Readbacks must have the correct window ID, actual content and no truncation.
Missing readback, failed assertion or changed final foreground stops with progress
evidence; request an observation to reconcile. This mode cannot be combined with
explicit observation:visual. It does not resolve unseen next-step controls.
Each sequence step may set `focus_window:true` to activate its already-observed
explicit window before input. Focus is confirmed; no launch or target substitution
occurs. This supports bounded known workflows across applications in one turn.

Desktop recipes now default to observation:auto. A small max_elements scan that
truncates may expand once to 150 before falling back to a screenshot. Explicit
semantic/visual requests keep their behavior. Complete named actionable controls
can support semantic navigation even when another field is unreadable; the
content_readback_warning explicitly prevents treating that field as verified.

- `prepare`: supply `application` (exact installed app name), or a fresh explicit
  `window_id`. Finds a unique exact title match, launches once if absent, waits for its
  window, verifies focus and returns an `observe` image with controls. Known
  localized Calculator titles are supported. Among matching windows, an observed
  foreground match is preferred; other ambiguities require an explicit ID.
  Notepad's changing document titles are resolved by observed `notepad.exe`
  process identity; owned dialogs are excluded from main-window selection.
  Known File Explorer names resolve through installed identity; folder windows
  require observed explorer.exe with CabinetWClass. Desktop/taskbar shell
  windows do not qualify as folder windows.
  A platform focus/target error allows one fresh selection if the handle changed.
  Explicit IDs remain pinned. Small frame-only UIA trees allow at most two fresh
  observations without replaying input. Persistent frame-only results report
  `accessibility_status: shell_only` and recommend screenshot/OCR inspection.
  Launch resolves a registered installed AppID and returns its canonical name;
  arbitrary identifiers or commands are never synthesized. No blind Windows search.
- `observe`: read the explicit `window_id`, with optional `max_elements` and
  `observation`. Defaults to semantic. Dispatches only the guarded Computer
  observation; no focus, launch, input or assertion parameters are allowed.
- `act`: supply one already-known mutating computer `input`, or 1-16 `inputs` in
  the same explicit window. The whole group is validated before any input;
  performs the logical operation and returns one fresh observation. Pixel coordinates must come from
  a recent observation of that same window.
  `focus_window:true` optionally activates and verifies that exact observed
  window before input. An outer `window_id`, if provided, must agree with input.
- `fill_submit`: input must be `set_value` with `automation.text`, window_id and
  fresh field selector. Focuses the field, verifies its retained value and its
  keyboard focus, then sends Enter with the foreground guard. If any check fails,
  Enter is not sent; choose a supported alternative after observing the failure.

Optional `wait_for` supplies an assertion on that same window, with action
`assert`, condition, and target selector. It must pass before observation.
Without wait_for, act/fill_submit observe immediately and make no readiness
claim. Supply an explicit condition when subsequent work depends on readiness.
Conditions include visible, hidden, enabled, text (control name), value,
document_text (exact TextPattern document content, up to 4096 characters) and
keyboard_focused. Assertion results include actual readback and truncation.
`text` compares the complete UIA.Name. For changing labels, select the observed
element_id without the old name and keep actual localized number/prefix format.
An exact name selector requesting a different expected text is rejected before
any input. Checkpoints may contain one equivalent `automation` wrapper; duplicate
fields must agree and missing selectors/expectations are never invented.
`max_elements` defaults to 80; maximum 500. Prepare's launch `wait_ms` defaults
to 8000, maximum 15000. The result includes actions and condition_verified.

- `sequence`: supply 1-24 `steps`, each containing one known scoped `input` or
  up to sixteen known same-window `inputs`, and
  a mandatory `checkpoint` with the same explicit window_id, selector and
  condition. The entire recipe is validated before any input. Each input runs
  once and its assertion must pass. In auto/semantic mode, consecutive keyboard
  operations on the same explicit result element can use a complete exact
  assertion readback without a second tree read. Such checkpoints carry
  readback_source:assertion and observation_omitted:true, without a snapshot ID.
  Changed targets, pointer input, missing/truncated readback and visual mode
  retain the observation boundary; the final observation is always collected.
  `checkpoints` preserves every actual assertion and, when observed, matching
  control readback; only the final image is attached. Completed checkpoints are
  retained on failure. Never place a new target choice inside the sequence.
- `transition`: supply one known scoped `input` or 1-16 same-window `inputs`,
  and `transition.expected_title`
  for an owned dialog, or `transition.expected_application` for an application.
  The recipe validates all inputs, records the window baseline, executes each
  input once and waits for
  the expected foreground window. A dialog must match the exact title and source
  owner; application selection uses prepare's strict identity matching. When
  both expectations are supplied, both must match. It observes the resolved
  window in the same call; no typing is automatically sent into the new window.
  A newly appeared #32770 dialog can also qualify by exact title, source process
  and a verified same-process hidden helper owner; a different visible owner,
  existing dialog or mismatched process does not qualify through this fallback.

`observation` selects `visual` (default), `semantic`, or `auto` on any recipe.
The returned focused_element can provide the complete live address/edit value
even when the main element list is truncated. Its ancestry is verified against
the requested window; it is not a numbered snapshot reference or completeness
claim. Inspect its value_available/text_available and truncation flags before
repeating a shortcut just to read the same field.
After act/fill_submit, a different foreground native dialog directly owned by
the input window is observed in the same call. The returned window_id, snapshot
and image then belong to the dialog; source_window_id and workflow.input_window_id
retain the input target. No input is redirected or replayed. Unrelated windows,
non-dialogs and indirect helper owners are not generally adopted through this path.
For an act group ending with Alt+Enter, a new Explorer Properties dialog can be
read after checking the pre-input window baseline, current foreground identity,
Explorer process/class and same-process hidden helper ownership. Existing dialogs,
visible alternate owners and mismatched processes do not qualify. The dialog read
uses the normal child permission and hook checks; no focus or input is redirected.
Use transition for other expected dialogs requiring explicit identity checks.

After an input that may dismiss the source (Enter, Escape, invoke or Alt+F4),
the recipe checks its presence with native enumeration. A complete list that
omits it returns `window_status:absent`, source_window_id and fresh foreground
identity; it does not attempt to inspect a vanished handle. Alt+F4 additionally
reports absent_closed_window_ids. Dialog absence after Enter is not proof of a
successful save or submission. Verify the requested outcome using the returned
state and task acceptance criteria. An asynchronous dismissal can reuse fresh
recovery enumeration without replaying input or requesting another list.

Semantic returns current UIA content and native foreground identity without a
capture. Auto uses semantic only for a complete, readable tree on the foreground
target; missing content, clipped trees or unavailable providers use visual
fallback. Partial observations never certify task completion. Values/text are
bounded and password fields redacted. Snapshot caches contain only descriptors.

Example a verified known input (repeat steps for other already-known inputs):
`{"desktop":{"mode":"sequence","observation":"auto","steps":[{"input":{"action":"key","key":"enter","automation":{"window_id":"OBSERVED_ID"}},"checkpoint":{"window_id":"OBSERVED_ID","element_id":"OBSERVED_RESULT_ID","condition":"value","expected":"69104","wait_ms":3000}}]}}`

Example a known Save action followed by an expected owned dialog:
`{"desktop":{"mode":"transition","observation":"auto","input":{"action":"hotkey","key":"s","modifiers":"ctrl","automation":{"window_id":"OBSERVED_ID"}},"transition":{"expected_title":"OBSERVED_LOCALIZED_SAVE_TITLE","wait_ms":5000}}}`

Expected IDs, names and dialog titles must come from observed application state
or verified application behavior, never these illustrative placeholders.
On typed focus, target, pattern or checkpoint errors, the recipe gathers at most
one recovery window list and one semantic observation within five seconds using
normal child dispatch. `desktop_recovery` reports the reason, fresh evidence and
guidance. Permission failure, cancellation and handoff stop recovery reads too.

Example opening an application:
`{"desktop":{"mode":"prepare","application":"Apple Music"}}`

Example an already-observed target and its result in one turn:
`{"desktop":{"mode":"act","input":{"action":"double_click","x":120,"y":250,"automation":{"action":"double_click","window_id":"OBSERVED_ID"}}}}`

Coordinates above are illustrative, never application presets. A returned
observation supports the next model decision; it does not choose unseen results
or guarantee a song is playing. New target selection remains a separate decision.

Every invocation uses the current authorized tool palette, schema validation, ordinary hooks, ownership, permissions, execution policy, timeout and journal. A denial, halt or error stops the program; calls are not replayed. Tool pipelines cannot invoke themselves, goals, job scheduling or task-board controls. Limits: 64 steps, 128 invocations, 5 minutes, 8 KiB content per result and 64 KiB final output. Outputs are observations, not proof that the requested goal was met.

This is a JSON tool program, not arbitrary Python/JavaScript execution. Use explicit tool arguments and independent assertions after mutations.

For desktop work, batch short, already determined sequences to avoid a separate model round trip per keystroke: focus a known window and inspect it; or click a freshly observed field, type known text, press Enter and return an OCR observation. Keep automation.window_id on every input step. Stop the batch at any point requiring a new target choice or fresh coordinates; never precompute clicks into unseen search results. A failed step stops later input and is not replayed.

Image results are supported: the latest image among selected return steps is attached with its MIME type, up to 8 MiB, together with the JSON step results. Earlier or unselected images are marked image_omitted; they are not visual evidence available to the model. Use return to select the exact image needed. Image bytes do not count against the text limit. For multiple views, use separate observations instead of assuming omitted frames were seen.

# Desktop assertion gates and observation references

For a `computer` step with `action:"assert"`, `require_passed:true` requires a
direct `passed:true` result. False, missing or malformed results stop all later
steps. This is an assertion gate, not a substitute for verifying completion.
Browser assert, wait_for, download_wait, popup_wait and dialog_wait support
the same gate. Browser workflows add it automatically to condition steps and
verification calls. Mutation delivery alone never satisfies a condition gate.

For a computer call targeting a numbered control, `observation_from:"step_id"`
copies the snapshot ID from an earlier single `computer observe` step into the
child's arguments. Include `element` and the desired action in `arguments`.
Only typed observation references are supported; no arbitrary result expressions
or generated scripts are evaluated. References expire or invalidate on input.

## Verified Explorer rename

Use `{"desktop":{"mode":"rename","window_id":"OBSERVED_EXPLORER_ID","rename":{"old_name":"hesap","new_name":"sonuc"}}}`
for an exact observed Explorer ListItem in the foreground folder window.
Use `hesap.txt` / `sonuc.txt` instead if extensions are shown. Both names must
follow the observed display policy. No guessed coordinates or filesystem writes
are used. Do not mix this recipe with other desktop parameters.

The recipe finds a unique visible item, rejects an existing replacement, selects
exactly that item using SelectionItem, verifies selection and keyboard focus,
sends F2 once, proves a focused inline editor within that item's bounds, replaces
its text, reads it back, submits once and verifies editor closure and final name.
Every child still passes through normal hooks and permissions. Failure stops all
dependent input; mutations never automatically retry. Successful results contain
semantic evidence without screenshots. The overall bound is 45 seconds.

## Branching and durable desktop flows

Use `desktop:{"mode":"flow","flow":{"nodes":[...]}}` for conditional or
multi-window work whose expected states are known. This mode does not execute
generated code. Nodes have unique IDs, `kind` and forward-only edges; 1-64 nodes,
384 estimated operations, 512 actual child calls and a five-minute deadline.
All nodes validate before any child dispatch. Cycles and recursive calls fail.

- `prepare`: find/launch/focus an installed application and bind its fresh
  foreground identity to this node ID. Use `application` and optional `wait_ms`.
  No further model turn is needed before already determined operations.
- `resolve`: find an already-open application by `application` identity or exact
  `title`. `owner_ref` binds an exact-title foreground dialog to a previously
  resolved owner with matching observed process. `next` selects the next node.
  This does not launch apps; use a preceding prepare node when needed.
- `branch`: use `window_ref` and a targeted `checkpoint` as an immediate native
  predicate. True selects `then`, false selects `else`; an omitted edge ends
  that path. Unavailable text, ambiguity and incomplete scans stop the flow.
- `operation`: `window_ref`, `input` or 1-16 `inputs`, mandatory `checkpoint` and
  optional `focus:true`. Omit child window IDs; runtime bindings supply them.
  Accessibility inputs resolve a unique fresh non-password control before use.
- `verify`: read a targeted checkpoint without input. Explicit field/row/result
  selectors avoid full screenshots. Assertion deadlines cap waiting. Native
  Windows events wake assertion reads; unsupported events use bounded polling.
- `rename`: `window_ref`, exact displayed `rename:{old_name,new_name}`;
  automatically focuses the bound Explorer window and verifies single selection,
  editor and committed name.
  Its outcome is its checkpoint; omit other inputs/checkpoint parameters.
- `close`: `window_ref`; automatically focuses this exact bound window, sends
  Alt+F4 once and proves window absence before continuing. An owned dialog stops
  with fresh evidence instead of accepting or dismissing it blindly. No supplied
  input/checkpoint is needed. Chain close nodes to close several task-owned apps.

Checkpoint conditions and bounds are the same as sequence. `max_elements` can
be supplied inside a checkpoint selector, not at the desktop flow level.
`failure_crop:true` requests a target-only diagnostic image after a failed
checkpoint. It retains failure status and correct virtual-screen coordinates.
No image is requested on a successful checkpoint path.

Window refs may name earlier prepare or resolve nodes. Example for known
Notepad controls, including preparation and verified closing in one call:

```json
{"desktop":{"mode":"flow","flow":{"run_id":"draft-1","nodes":[
  {"id":"editor","kind":"prepare","application":"Notepad","next":"content"},
  {"id":"content","kind":"operation","window_ref":"editor","focus":true,
   "input":{"action":"set_value","automation":{"name":"Document field","text":"Hello"}},
   "checkpoint":{"name":"Document field","condition":"value","expected":"Hello"},
   "next":"finish"},
  {"id":"finish","kind":"close","window_ref":"editor"}
]}}}
```

Use the actual observed field name; the example is not an app-specific selector.
Save the document through a grounded dialog workflow before closing when the
assignment requires it. The minimal example stops at any unsaved-changes dialog.
For durable work, choose `run_id`; after interruption reuse the exact same
nodes with `resume:true`. State is scoped to the current session, locked across
processes and written atomically before input and after verified checkpoints.
It stores plan hashes, node IDs, cursor and attempt status, not input text,
images or HWNDs. Missing, corrupt, changed or concurrently used runs fail closed.
Completed runs return their status without replaying input. Only bindings still
needed by remaining work are reconstructed; earlier closed apps are not reopened.

Recovery is operation-specific: transient reads can retry at most twice per
flow; mutations do not retry within the call. An interrupted effect (click,
key, type, invoke or multi-input operation) stops on resume as uncertain. Do not
invent a new run identity to bypass this. A single `set_value` is reconcilable
only when its checkpoint reads the same selected field and desired value. A
fresh matching value skips the write; a mismatching value can be replaced on
explicit resume, with at most two durable attempts. Permission denial or
cancellation prevents recovery input. Successful flow status establishes its
specified checkpoints, not unprovided acceptance criteria or file contents.

Keep groups short: a known field operation, an explicit expected-state assertion
and a final observation can be grouped. New search results or ambiguous targets
require a fresh model decision. Return the final `observe` step to receive its
controls and crop together. Every child uses ordinary hooked tool dispatch.
An explicit flow can continue through a predefined uniquely resolved dialog
or branch; unknown results still require a fresh model decision.

`observe` includes `foreground_window` from the window list used for its crop.
It reports the current foreground identity without changing the requested target
or authorizing input. A modal transition ends an ordinary batch: inspect the newly
observed dialog before typing; do not reuse the parent window ID. Closing a
task-owned handle followed by a fresh list that omits it confirms closure;
do not spend a recovery turn preparing an already-closed window.

For Explorer, `explorer_location.path` with `verified:true` is
the shell's actual filesystem folder for that exact foreground window/process.
Prepare/act/observe return it without address-bar input. Reuse it to plan saves;
do not spend additional turns on Ctrl+L, Alt+D, clicks or screenshots for the
same path. Virtual/ambiguous/changing folders omit this field. If absent, one
resolved address Value/Text read may establish it; avoid repeated guesses.
Desktop results also expose a compact `desktop_state` alongside the unchanged
control content. Recipe JSON places window_id, snapshot_id, foreground_window,
desktop_state and focused_element before the potentially large element list.
Use these identities directly when fresh and sufficient; field order does not
change their scope, freshness, completeness or input authorization.
The state identifies the observed target and foreground. A closing pipeline's
fresh windows result can include `absent_closed_window_ids` for handles targeted
by successful Alt+F4 steps in that pipeline. A still-listed handle is not closed;
malformed or capped enumeration does not prove absence. These fields reuse
existing results and do not issue extra reads or inputs. Use the state directly
when it is fresh and sufficient; do not request another windows call solely to
extract these same IDs. Changed focus, unknown identity or incomplete evidence
still requires a new observation. State fields do not authorize blind dialog
input and do not replace intermediate calculation or file-content checks.

## Call shape and errors

The recipe fields go directly inside `desktop`; `desktop` never contains another `desktop`. A computer action is placed in `input` (or `inputs`/`steps[].input`), never beside `mode`. `mode` is one of observe, prepare, act, fill_submit, sequence, adaptive, transition, flow, rename. Every `window_id` is a numeric handle taken from `prepare`/`windows`/`observe`, and every checkpoint repeats the same `window_id` as its input. Unknown fields, wrong nesting, an invalid mode, a fabricated window id and a missing checkpoint target are rejected before any input with `input_sent:false` and a corrected example. After an observation of a background window use `focus` (or `prepare`/`focus_window`) before keyboard input: observing does not focus.

```json
{"desktop":{"mode":"prepare","application":"<application name>","observation":"auto"}}
{"desktop":{"mode":"act","window_id":"<window_id from prepare>","input":{"action":"key","key":"enter","automation":{"window_id":"<same window_id>"}}}}
```

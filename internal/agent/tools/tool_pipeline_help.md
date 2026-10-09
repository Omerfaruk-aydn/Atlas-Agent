## help: desktop_flow

File checks, rename digest reads and keep_both disk probes use ordinary
`computer` child calls: hooks and file-path permissions run for each read. A
question, permission pause or hook halt stops immediately with its metadata
preserved. `file_check` also supports `digest` to observe size and SHA-256
without comparing an expected digest. Content checks are limited to 1 MiB;
the compared content and hash come from one read, with cancellation and file
change checks. `capture.pattern` must match the complete readback, including
its prefix and suffix. An already-correct replacement still captures the
verified value before the durable journal advances.

`desktop:{"mode":"flow","flow":{"nodes":[...]}}` runs conditional or multi-window desktop work whose expected states are known. It never executes generated code. Nodes have unique IDs, a `kind` and forward-only edges (`next`, or `then`/`else` for a branch); 1-{%.FlowNodes%} nodes, {%.FlowEstimate%} estimated operations and {%.FlowCalls%} actual child calls. The deadline is `timeout_ms` when given (up to {%.MaxTimeoutMS%} ms), otherwise {%.FlowDefaultMS%} ms. The whole graph is validated before any child call: unknown kinds, missing checkpoints, cycles, backward edges, references to later nodes and use of a binding after the node that closes its window are refused while nothing has been sent.

- `prepare`: find, launch once if absent, focus and bind an installed application by `application`, with optional `wait_ms`. An already-open matching window is reused, not relaunched.
- `resolve`: bind an already-open window by `application` identity and/or exact `title`, waiting up to `wait_ms` (default 5000, max 15000). `owner_ref` binds an exact-title foreground dialog owned by an earlier binding with the same process. It never launches.
- `operation`: `window_ref`, one `input` or 1-{%.Inputs%} `inputs`, a mandatory `checkpoint` and optional `focus:true`. Omit window IDs in inputs and checkpoint; the binding supplies them. `invoke` and `set_value` resolve one fresh, enabled, non-password control from their selector first. `checkpoint_ref` reads the checkpoint in another binding. `closes_window:true` declares that the inputs dismiss the `window_ref` window: its absence is proven (an owned dialog that appears instead stops the flow), then `checkpoint_ref` must be foreground again, then its checkpoint is read.
- `verify`: a read-only checkpoint, optionally in `checkpoint_ref`.
- `branch`: an immediate native predicate (`checkpoint` without wait); true selects `then`, false `else`; an omitted edge ends that path. Unreadable or ambiguous state stops the flow.
- `rename`: `window_ref` and `rename:{old_name,new_name,expected_full_name,on_conflict}` for an Explorer item; see `desktop_recipes`.
- `file_check`: `file:{path,condition,expected,line_endings}` reads the file system only, never a window: `exists`, `absent`, `content` (UTF-8 text; `line_endings` `normalize` by default treats CRLF as LF and ignores one final newline, `exact` compares bytes) or `sha256` (hex). `path` is absolute with the real extension and takes no placeholders. A mismatch reports size, sha256 and the first differing line.

Values between nodes: `capture:{name,pattern,number}` on an operation or verify node stores its checkpoint readback under `name` (lowercase, at most 32). `pattern` is an RE2 expression with exactly one group (e.g. `^Ekran değeri (.+)$`). `number:{thousands,decimal}` states the separators the application shows and passes a plain number ("9.000" with thousands "." becomes 9000; "8,5" with decimal "," becomes 8.5). A verify node may omit `checkpoint.expected` to read the current text/value/document_text of one control once; a text capture selects by element_id or role, not by the name it reads. Later nodes write `{{name}}` in `input.text`, `automation.text`, `checkpoint.expected` or `file.expected`; selectors, application, title, keys and paths never take placeholders, so a read value cannot choose a target. Validation refuses a placeholder that is not captured on every path before its node, a duplicate capture or a malformed `{{ }}`, naming the node and field. At run time an unreadable or non-matching readback stops before the next input. The journal of a durable run keeps captured values (at most 1 KiB each), so resume never re-reads a completed node.

A value read in one window and used in another, with the saved file checked on disk and an explicit name-conflict choice. The calculator result is read once without guessing it, written as a plain number, compared with the saved file, and the rename keeps an existing "sonuc.txt" untouched:
```json
{"desktop":{"mode":"flow","flow":{"nodes":[
 {"id":"calc","kind":"resolve","application":"Hesap Makinesi","next":"read"},
 {"id":"read","kind":"verify","window_ref":"calc","checkpoint":{"element_id":"<result_element_id>","condition":"text"},"capture":{"name":"result","pattern":"^Ekran değeri (.+)$","number":{"thousands":".","decimal":","}},"next":"editor"},
 {"id":"editor","kind":"prepare","application":"Not Defteri","next":"write"},
 {"id":"write","kind":"operation","window_ref":"editor","focus":true,"inputs":[{"action":"type","text":"Sonuç: {{result}}"},{"action":"hotkey","modifiers":"ctrl","key":"s"}],"checkpoint":{"role":"ControlType.Document","name":"Metin düzenleyicisi","condition":"document_text","expected":"Sonuç: {{result}}"},"next":"dialog"},
 {"id":"dialog","kind":"resolve","title":"Farklı Kaydet","owner_ref":"editor","wait_ms":5000,"next":"name"},
 {"id":"name","kind":"operation","window_ref":"dialog","inputs":[{"action":"set_value","automation":{"name":"Dosya adı:","role":"ControlType.Edit","text":"C:\\Users\\me\\Desktop\\Atlas-Test\\hesap.txt"}},{"action":"key","key":"enter"}],"closes_window":true,"checkpoint_ref":"editor","checkpoint":{"role":"ControlType.Window","name":"hesap - Not Defteri","condition":"visible"},"next":"saved"},
 {"id":"saved","kind":"file_check","file":{"path":"C:\\Users\\me\\Desktop\\Atlas-Test\\hesap.txt","condition":"content","expected":"Sonuç: {{result}}"},"next":"folder"},
 {"id":"folder","kind":"resolve","application":"File Explorer","title":"Atlas-Test","next":"rename"},
 {"id":"rename","kind":"rename","window_ref":"folder","rename":{"old_name":"hesap","new_name":"sonuc","expected_full_name":"sonuc.txt","on_conflict":"keep_both"}}]}}}
```
Use `on_conflict:"keep_both"` only when the user accepts a numbered name; the default `fail` reports the existing file's digest so you can ask which outcome the user wants.
- `close`: `window_ref`; focuses that exact window, sends Alt+F4 once and proves absence. An unsaved-changes dialog stops with its identity instead of being answered. A window that was open before the task and was not opened by it is refused with `not_task_owned` before focus or input, unless the node sets `allow_unowned:true` because the user explicitly asked to close that window.

Checkpoint conditions are visible, hidden, enabled, text (complete UIA name), value, document_text (TextPattern, up to 4096 characters), keyboard_focused and file_name. `wait_ms` (max 15000) bounds a checkpoint's wait; native change events wake it. `failure_crop:true` requests a target-only diagnostic crop after a failed checkpoint; it never proves success. Durable flows (`run_id`) need named/role selectors, not runtime element IDs.

Ownership: a window is task-owned when it was absent before this flow call started, or the input tracker reported it as opened by an earlier input of this session (for example a launch). Ownership is never inferred from a title.

Durable runs: choose `run_id`; after an interruption reuse the exact same nodes with `resume:true`. State is scoped to this session, locked, and written before input and after each verified checkpoint. It stores plan hashes, node IDs, the cursor and attempts, never input text, images or window handles. Completed nodes are not repeated; windows needed by remaining nodes are resolved again (earlier closed apps are not reopened). Each effect node is classified from the computer tool's own dispatch evidence, never from error text. Not sent: every input of the node was refused before dispatch (permission, question, hook, cancellation, a refused target) — the node is blocked, canceled or failed with `dispatch:"none"`, no attempt is used, and resume runs it again with permissions, hooks and fresh window resolution. Uncertain: any input of the node may have been sent (also the first of several inputs, a timeout, a transport error or a missing outcome) — an interrupted click, key, type, invoke, prepare, rename or close stops on resume as `uncertain_effect`. A single `set_value` whose checkpoint reads the same live window and the same control is reconciled from that checkpoint (a matching value skips the write; at most two durable attempts); a checkpoint in another window (`checkpoint_ref`) or on another control never reconciles or skips it. Do not invent a new run_id to bypass this.

Result: `flow.nodes` lists every node's state (bound, evaluated, verified, uncertain, failed, blocked, canceled, not_run, skipped_branch), `flow.steps` holds per-node evidence and checkpoint readbacks, and `flow.resume` says what to do after a failure. When a child stops the flow, its content, error and stop contract, media and metadata (question, permission, hook, waiting_for_user) are returned unchanged; the ledger is added under `flow` (a child's own `flow` key moves to `child_flow`, non-JSON metadata is kept as `native_metadata`), and `atlas_outcome` describes the whole call: it says sent or unknown when an earlier node may have taken effect, keeping the child's as `child_atlas_outcome`. Large readbacks, then per-node evidence, are dropped first; node states and interaction metadata are never dropped.

## help: desktop_recipes

Choose a recipe at a logical operation boundary. Every child still uses the same schema validation, hooks, permissions, foreground checks and desktop lease. Recipe fields go directly inside `desktop`; a computer action goes in `input`, `inputs` or `steps[].input`, never beside `mode`. Every `window_id` is a numeric handle from prepare/windows/observe, and every checkpoint repeats the input's `window_id`. Unknown fields, wrong nesting, invalid modes, fabricated window IDs and missing checkpoint targets are refused before any input with `input_sent:false` and a corrected example.

- `prepare`: `application` (exact installed name, localized Calculator names supported) or an explicit `window_id`. Finds a unique match, launches once if absent, waits (`wait_ms` default 8000, max 15000), verifies focus and observes. Notepad is matched by process, Explorer folder windows by explorer.exe with CabinetWClass. An ambiguous match needs an explicit window_id. Results include `application_adapter` with supported methods and verification needs.
- `observe`: read an explicit `window_id` (default semantic); no focus, launch or input.
- `act`: one known mutating `input`, or 1-{%.Inputs%} same-window `inputs`, then one observation. `focus_window:true` activates and verifies the exact window first. Optional `wait_for` (an assertion on the same window) must pass before the observation. After act, a foreground native dialog owned by the input window is observed in the same call.
- `fill_submit`: `set_value` with text, verifies the retained value and keyboard focus, then Enter; any failed check means Enter is not sent.
- `sequence`: 1-{%.SequenceSteps%} steps, each one `input` or 1-{%.Inputs%} same-window `inputs` and a mandatory `checkpoint`; validated before any input, {%.SequenceSeconds%} s limit. `result:"checkpoints"` returns actual readbacks and the final foreground without screenshots. A step may set `focus_window:true` for an already observed window.
- `transition`: inputs, then the expected owned dialog (`transition.expected_title`) or application (`transition.expected_application`); it observes the new window without typing into it.
- `adaptive`: 1-8 conditional steps with mandatory checkpoints; method selection (accessibility vs. verified keyboard) happens before input; a runtime error after a mutation never switches methods or replays.
- `rename`: `window_id` of the foreground Explorer folder and `rename:{old_name,new_name}` using the displayed names (with an extension only if Explorer shows it). It selects exactly that item, sends F2 once, proves the inline editor, replaces and reads back the text, submits once and verifies the final name. `expected_full_name` (e.g. "sonuc.txt" while Explorer shows "sonuc") is then read with the `file_name` condition from this window's shell view item; a display label, a "Metin Belgesi" type column or a *.txt filter never prove the extension. The result lists `requirements.folder`, `requirements.full_name` and `requirements.content` separately; a rename never proves content. When an item named `new_name` already exists, `on_conflict` decides before any input: `fail` (default) refuses with `name_conflict` and both files' full name, size and sha256; `keep_both` renames to the first free "new_name (N)" (checked in the view and on disk, the hidden extension kept) and reports `actual_name` and `requested_name`; `use_existing_if_identical` sends no input and reports success only when the existing file is byte-identical, keeping the source. There is no replace policy: a rename never deletes or overwrites a file. If the task does not say which outcome the user wants, ask the user that question; never pick replacement on your own.

Recipe limits: act/prepare/fill_submit {%.RecipeSeconds%} s, sequence {%.SequenceSeconds%} s, adaptive 60 s, rename 45 s, transition its own wait (1-15000 ms, default 5000). Observation modes: auto (default; semantic for a complete readable tree, visual otherwise), semantic, visual. `explorer_location.path` with `verified:true` is the shell's real folder for that exact window; reuse it instead of reading the address bar. It identifies the folder, not a file's extension or content. After Enter, Escape, invoke or Alt+F4 the recipe checks the source window with native enumeration and reports `window_status:absent`; dialog absence alone does not prove a save.

Example (three results, each checked, no screenshots):
```json
{"desktop":{"mode":"sequence","result":"checkpoints","observation":"auto","steps":[
 {"input":{"action":"type","text":"1234*56=","automation":{"window_id":"<calculator_window_id>"}},"checkpoint":{"window_id":"<calculator_window_id>","element_id":"<result_element_id>","condition":"text","expected":"Ekran değeri 69.104","wait_ms":3000}},
 {"input":{"action":"type","text":"/8=","automation":{"window_id":"<calculator_window_id>"}},"checkpoint":{"window_id":"<calculator_window_id>","element_id":"<result_element_id>","condition":"text","expected":"Ekran değeri 8.638","wait_ms":3000}},
 {"input":{"action":"type","text":"+362=","automation":{"window_id":"<calculator_window_id>"}},"checkpoint":{"window_id":"<calculator_window_id>","element_id":"<result_element_id>","condition":"text","expected":"Ekran değeri 9.000","wait_ms":3000}}]}}
```

Example (verified rename with the hidden extension checked):
```json
{"desktop":{"mode":"rename","window_id":"<explorer_window_id>","rename":{"old_name":"hesap","new_name":"sonuc","expected_full_name":"sonuc.txt"}}}
```

## help: browser

`browser:{tab_id,origin,steps:[...]}` runs 1-{%.BrowserSteps%} logical browser steps (plus an automatic assert per `verify`). Each step has a unique `id`, an `action`, a semantic `target` (role/name/label/selector/within, value, commit, viewport_id, x/y, ...), optional `ref` from the latest observation, `text`, `url`, `key` (a key or chord such as "escape" or "ctrl+a"), `direction`/`amount` and an optional `verify` DOM assertion. Every child is an ordinary browser call with the same hooks, permissions, ownership and cancellation.

Actions: navigate, back, forward, click, type, key, scroll, semantic_click, semantic_type, set_field, read_field, select_option, fill_form, submit, find, assert, wait_for, observe, screenshot, collect, health, tabs, tab_new, tab_select, tab_close, tab_return, frames, upload, download_start, download_wait, popup_wait, dialog_wait, dialog_handle, pointer_move, pointer_click, pointer_drag, key_combo, insert_text. eval, cdp and handoff are not allowed inside a workflow.

The first error or failed postcondition stops later steps. `retry_on` allows one retry after a fresh observe for a semantic target and target_not_found, target_resolution_failed, condition_timeout or stale_ref, only when nothing was sent. `when`, `if_success`, `if_failed`, `on_error:"continue"` and `bindings` work as in generic steps. Bindings carry `advanced.tab_id`, `expected_tab_id`, `frame_id`, `document_id`, `download_id`, `newer_than`, `viewport_id`, `advanced.value`, `advanced.expected`, `text` and `ref` (only when exactly one element matched). Missing or ambiguous evidence stops before dispatch. Workflow `tab_id`/`origin` become guards on every step; after `tab_select` bind the new `expected_tab_id`. Refs expire on navigation. `fill_form` pins the tab and frame documents; navigation or an uncertain write stops the remaining fields as not_attempted (a partial result, not a rollback). `set_field` honors its settle window; typing alone is not a verified result. For downloads arm `download_start` before the trigger and bind `advanced.newer_than` to its `started_at`; a finished download does not prove server receipt.

Example (fill, read back, follow a popup, verify there):
```json
{"browser":{"origin":"https://forms.example.test","steps":[
 {"id":"fill","action":"fill_form","target":{"fields":[{"label":"Name","value":"Atlas"},{"label":"Country","value":"Türkiye","kind":"option"}]}},
 {"id":"name","action":"read_field","target":{"label":"Name"},"verify":{"label":"Name","condition":"value","expected":"Atlas"}},
 {"id":"open","action":"semantic_click","target":{"role":"button","name":"Preview"}},
 {"id":"popup","action":"popup_wait"},
 {"id":"select","action":"tab_select","bindings":{"advanced.tab_id":"popup#/tab_id"}},
 {"id":"ready","action":"wait_for","target":{"condition":"text","selector":"#summary","expected":"Atlas"},"bindings":{"advanced.expected_tab_id":"popup#/tab_id"}}]},"return":["name","ready"]}
```

## help: browser_flow

`flow:{name,version,inputs,steps,items,save}` runs a template of browser workflow steps once per item (1-{%.FlowItems%} items), substituting `{{input}}` placeholders in string fields. Templates never contain refs, viewport ids or pointer input; targets resolve by role/name/label/within at run time. Each item's result is stored; `flow:{name,rerun:"<run_id>"}` reruns only failed and uncertain items with their recorded inputs. Observe the page before rerunning uncertain items. A saved flow runs by name for the same major version.

## help: steps

Generic `steps` run tool calls in order: each has a unique `id` (64-byte JSON budget), an allowed `tool`, and structured `arguments` (an object). `items` repeats a step with the exact string "$item" substituted; no shell interpolation. `bindings` copy one scalar from an earlier step's JSON result into an argument path, e.g. `{"automation.window_id":"launch#/input_settle/new_window_ids/0"}`; a missing, failed or ambiguous source stops before input, and a failed iteration never leaves its old output for later bindings. `when` runs a step only if an earlier field equals, differs or exists. `if_success` and `if_failed` select dependent and recovery branches; `on_error:"continue"` marks an independent step. `require_passed:true` turns a computer `assert` or browser condition step into a gate. `observation_from` copies the snapshot of an earlier single computer `observe` step. `return` selects which results (and the one image) are returned.

The whole plan is checked before the first call: identities, references, bindings, conditions, gates, browser arguments and computer arguments (decoded with the computer contract, so an unknown action or field fails at preflight, not at step 15). Bound values are checked again when they arrive. tool_pipeline cannot call itself, goals, job scheduling or the task board.

Example (generic program with gates and bindings):
```json
{"steps":[
 {"id":"launch","tool":"computer","arguments":{"action":"launch_app","automation":{"name":"Hesap Makinesi"}}},
 {"id":"focus","tool":"computer","arguments":{"action":"focus"},"bindings":{"automation.window_id":"launch#/input_settle/new_window_ids/0"}},
 {"id":"look","tool":"computer","arguments":{"action":"observe","observation":"semantic"},"bindings":{"automation.window_id":"launch#/input_settle/new_window_ids/0"}}],"return":["look"]}
```

## help: results

Desktop flow responses always include an adapter-owned `atlas_outcome` for the whole call, even if a child has no dispatch metadata. It reports `none`, `sent` or `unknown` and the actual mutation subjects; the last read's window is not the mutation target. A pending intent is unknown. A flow terminal alone never marks the entire task verified. Child outcome metadata is retained separately. The shared resend guard covers mutating desktop, browser and generic pipeline envelopes, while read-only help and observation remain available. Short UIA role names such as `Edit` normalize to `ControlType.Edit`; unknown synonyms are rejected before input. Diagnostic crops preserve the failed checkpoint and any question or permission required to continue.

Browser workflows and generic pipelines also aggregate native dispatch evidence across every executed child. An uncertain child makes the whole call `unknown`; an earlier mutation remains accounted for even when a later read or permission pause is returned. A refusal with proof that nothing was sent stays `none`. Whole-call metadata never inherits the last child's verification flag, and question/permission metadata and media are retained.

After a pause with earlier mutations, resending the identical whole pipeline is refused with `partial_progress`. Continue with only the remaining steps or a durable flow's `resume:true`; a refusal before any input may be retried after its cause is fixed. Old verification evidence is invalidated for the mutations already sent, even while the workflow waits.

Every generic step reports `status` (succeeded, failed, skipped, uncertain, stopped) and `outcome` (planned, dispatched, applied, verified, unknown, blocked, canceled, failed); only `verified` is a checked result. Desktop flows report node states in `flow.nodes`. On failure the failed result carries `resume`: what already took effect (never repeat it), whether the failed step may have taken effect, and what was not run. Continue with a new call holding only the remaining work. Uncertain outcomes are sticky: only read-only recovery may run until the exact operation is reconciled from fresh evidence. A question, permission halt, cancellation or transport failure keeps the full ledger and the original halt metadata. Results are limited to {%.ContentKiB%} KiB per step and {%.OutputKiB%} KiB overall; larger details are shortened before ids, statuses, outcomes and dispatch are touched. The latest selected image is attached (up to 8 MiB); other images are marked image_omitted and were not seen.

## help: examples

A complete desktop task in two workflow calls after one observation. Call 1 observes the calculator (`{"desktop":{"mode":"prepare","application":"Hesap Makinesi","observation":"semantic"}}`) and yields `<result_element_id>`. Call 2 does the calculations with three checkpoints, writes and saves the note, verifies the return to the main window, renames the file with its real extension checked, reopens and reads it, and closes only the windows this task opened, proving each is gone:
```json
{"timeout_ms":300000,"desktop":{"mode":"flow","flow":{"nodes":[
 {"id":"calc","kind":"resolve","application":"Hesap Makinesi","next":"mul"},
 {"id":"mul","kind":"operation","window_ref":"calc","focus":true,"input":{"action":"type","text":"1234*56="},"checkpoint":{"element_id":"<result_element_id>","condition":"text","expected":"Ekran değeri 69.104","wait_ms":3000},"next":"div"},
 {"id":"div","kind":"operation","window_ref":"calc","input":{"action":"type","text":"/8="},"checkpoint":{"element_id":"<result_element_id>","condition":"text","expected":"Ekran değeri 8.638","wait_ms":3000},"next":"add"},
 {"id":"add","kind":"operation","window_ref":"calc","input":{"action":"type","text":"+362="},"checkpoint":{"element_id":"<result_element_id>","condition":"text","expected":"Ekran değeri 9.000","wait_ms":3000},"next":"editor"},
 {"id":"editor","kind":"prepare","application":"Not Defteri","next":"write"},
 {"id":"write","kind":"operation","window_ref":"editor","focus":true,"inputs":[{"action":"type","text":"1234 * 56 = 69104\n69104 / 8 = 8638\n8638 + 362 = 9000"},{"action":"hotkey","modifiers":"ctrl","key":"s"}],"checkpoint":{"role":"ControlType.Document","name":"Metin düzenleyicisi","condition":"document_text","expected":"1234 * 56 = 69104\n69104 / 8 = 8638\n8638 + 362 = 9000"},"next":"dialog"},
 {"id":"dialog","kind":"resolve","title":"Farklı Kaydet","owner_ref":"editor","wait_ms":5000,"next":"name"},
 {"id":"name","kind":"operation","window_ref":"dialog","inputs":[{"action":"set_value","automation":{"name":"Dosya adı:","role":"ControlType.Edit","text":"hesap.txt"}},{"action":"key","key":"enter"}],"closes_window":true,"checkpoint_ref":"editor","checkpoint":{"role":"ControlType.Window","name":"hesap - Not Defteri","condition":"visible"},"next":"close_editor"},
 {"id":"close_editor","kind":"close","window_ref":"editor","next":"folder"},
 {"id":"folder","kind":"resolve","application":"File Explorer","title":"Atlas-Test","next":"rename"},
 {"id":"rename","kind":"rename","window_ref":"folder","rename":{"old_name":"hesap","new_name":"sonuc","expected_full_name":"sonuc.txt"},"next":"open"},
 {"id":"open","kind":"operation","window_ref":"folder","focus":true,"input":{"action":"invoke","automation":{"name":"sonuc","role":"ControlType.ListItem"}},"checkpoint":{"name":"sonuc","role":"ControlType.ListItem","condition":"visible"},"next":"reopened"},
 {"id":"reopened","kind":"resolve","application":"Not Defteri","title":"sonuc - Not Defteri","wait_ms":5000,"next":"content"},
 {"id":"content","kind":"verify","window_ref":"reopened","checkpoint":{"role":"ControlType.Document","name":"Metin düzenleyicisi","condition":"document_text","expected":"1234 * 56 = 69104\n69104 / 8 = 8638\n8638 + 362 = 9000"},"next":"close_reopened"},
 {"id":"close_reopened","kind":"close","window_ref":"reopened","next":"close_calc"},
 {"id":"close_calc","kind":"close","window_ref":"calc"}]}}}
```
Titles and labels ("Farklı Kaydet", "Dosya adı:", "Metin düzenleyicisi", "Ekran değeri ...") are the localized names observed on this system; use the names your own observation shows. Turn a known long task into one dependent flow like this, with a checkpoint wherever a later step relies on a result; do not send it input by input. To continue after a halt (permission, question), give the flow a `run_id` from the start (durable runs need named/role selectors instead of element IDs) and send the same plan with `resume:true` after the user's answer. The Explorer folder window comes from an earlier observed step (it was opened by the task); the rename verifies folder and full name, the `content` node verifies content, and the close nodes verify absence.

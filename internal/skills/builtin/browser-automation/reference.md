# Browser tool reference

The browser tool's own description holds the rules every call must follow.
This file holds the per-action details. Read the section you need with
`view atlas://skills/browser-automation/reference.md`.

## Observation

- navigate/click/type/key/back/forward return the page state after the action.
  The first observation of a document is the full element list; later ones list
  only `changed`/`added`/`removed` elements against `base_observation_id`.
  Unlisted refs stay valid. "No changes" after an input means it may not have
  taken effect: verify before repeating it.
- `observe` with advanced.mode `delta` (default), `full`, or `scope` plus
  advanced.scope (a CSS container, e.g. the properties panel). `full: true`
  includes offscreen elements. A truncated list says has_more and the
  advanced.cursor to continue. Navigation, tab change or a browser restart makes
  the next result full again. `snapshot` is the full list.
- Lines are grouped under their container (`dialog "Share"`, `region "Layers"`).
  `(label? "Corner radius")` is nearby text for an unnamed field; `{focused}`,
  `checked=`, `expanded=`, `invalid` and `error=` carry state. `attention:` lists
  dialogs, alerts, invalid fields and focus first.
- Scoped observations and find results authorize their refs for the current
  browser, tab and document without replacing the full-page delta base.

## Fields

- `set_field` types, commits with a method chosen for the field
  (advanced.commit auto/enter/tab/blur/none), waits for asynchronous commits
  (advanced.settle_ms, default 300, honored in full) and reads back what the page
  kept. status `applied`/`normalized` (same value reformatted, e.g. "#f7f3ea") is
  success; `adjusted` means the app changed it (clamped, rounded): decide whether
  it meets the request; `not_applied` is an error showing the value still there;
  `typed_uncommitted` means commit none. advanced.mode replace (default) /
  append / clear / replace_selection. typed_value is the echo before commit,
  accepted_value is the result.
- A field whose application rebuilds or renames it after the commit is read
  back again without retyping (acceptance `renamed_readback`, `remount_readback`,
  `inline_rename`, `shown_mode`). When the edited node now carries another name,
  the result names it in `relabeled_as` and stays outcome_unknown until that
  field is read.
- `select_option` handles native selects and custom comboboxes (option resolved
  after opening). `fill_form` sets advanced.fields in order (each with its own
  value) with readback and reports each field's validity;
  advanced.only_invalid=true fixes only rejected fields. It pins all documents
  before writing and stops later fields as `not_attempted` when the context
  changes or an earlier write is uncertain; earlier fields may already have
  changed.
- `submit` clicks the submit control once per identical form state (a repeat is
  refused unless allow_resubmit after verifying the first failed); acceptance
  still needs wait_for on the confirmation.
- `undo` restores the last field Atlas set, only if it still holds Atlas's
  value, checks the selected object's identity as well as tab, document and
  frame, and verifies the restore. `undo_unsafe` sends nothing and keeps the
  record.

## Conditions and waits

assert visible/hidden/enabled/text/text_contains/value/value_not/url/title/
checked/selected/count/not_busy using condition/expected and timeout_ms (default
10000, maximum 30000). Expected values are strings. `wait_for` also supports
ready, stable (DOM quiet for 500 ms) and network_idle (no request started or
pending for 400 ms; never reached on pages with persistent traffic such as
editors). Element conditions need a target; ready/stable/network_idle/url/title
do not. A timeout names the condition and the last observed state. Waiting is
bounded by what can change: a `within` container that does not exist fails
after about 2 s with the containers that do; an absent target fails after
about 2 s unless timeout_ms asks to wait for it; a control behind the same
blocker fails after about 2.5 s naming the blocker.

## Tabs, popups and dialogs

- `tabs` marks each tab owned (opened by this automation, with purpose/opener)
  or the user's; never treat the user's tabs as task tabs. `tab_new` accepts
  advanced.purpose. `popup_wait` returns an unobserved page whose opener is the
  selected tab (or advanced.tab_id); it does not select it. When a popup closes
  itself (OAuth), page actions return tab_closed and `tab_return` selects its
  opener (or the latest open owned tab). tab_select/tab_close require an
  observed tab of the same browser context. Optional expected_tab_id and
  expected_origin guard the selected page; `find` returns document_id, which
  rejects a replaced document.
- `dialog_wait` observes a pending alert/confirm/prompt/beforeunload.
  `dialog_handle` requires an observed pending dialog; advanced.expected can
  require the exact message, with advanced.accept and advanced.prompt_text for
  the response. `dialog` answers one directly (accept, prompt_text).

## Lists, research, files

- `collect` scans a scrolling or virtualized list (advanced.row_selector,
  optional scope, row_key, next_selector for pagination, max_items, max_scrolls)
  keeping row identity. It reports complete, end_reached, gaps, conflicting_keys
  and a cursor; an incomplete scan is not the whole list.
- `research` with advanced.urls (1-8) reads independent pages in background
  tabs, three at a time, and returns per-source requested/final URL, title,
  dates and an excerpt. It never types, clicks or changes the controlled tab.
- upload uses a unique file input selector plus advanced.paths and reads back
  selected count/names/sizes (local selection, not server receipt).
  download_start arms the selected tab with one workspace directory and returns
  started_at; download_wait uses one expected file and newer_than from that
  result, optional download_id. All paths are workspace-confined.
- capture_region uses advanced.x/y/width/height (CSS pixels; maximum 4096 per
  dimension).

## Canvas input

`pointer_move`, `pointer_click`, `pointer_drag`, `key_combo`, `insert_text` are
for content without DOM refs. Take a `screenshot` first; its Viewport line gives
`viewport_id`. Set advanced.viewport_id and advanced.x/y (pointer_drag also
to_x/to_y) in that screenshot's image pixels. A grant stays valid while the
viewport is unchanged. Optional advanced.button, click_count (2 = double
click), hold_ms, steps and modifiers (alt/ctrl/meta/shift). Each call is one
complete gesture. `cleanup_status=released` confirms release of held input;
`pending` or `input_cleanup_failed` means release is unconfirmed and further
input waits for recovery. `stale_viewport` means the page scrolled, zoomed,
resized, navigated or changed tab: take a new screenshot.

Canvas text: in a page whose canvas covers most of the viewport, the text
editor is a hidden or canvas-overlaid DOM element that can stay focused after
the application left text editing. insert_text into it needs an open text-edit
session: the pointer_click (or click) that put focus into it, in this tab and
document. Escape, other clicks, navigation and tab changes end the session
(`text_edit_not_open`). A session opened on existing text refuses to add to it
unless advanced.mode is `append` (`text_editor_not_empty`). The result reports
`canvas_text.editor_text_before/after`. A single-character `key` while focus is
in a text editor is refused (`key_would_type`): pick tools with their toolbar
buttons, or leave text editing first.

## Keys

`key` takes one key or chord: a letter or digit ("f"), punctuation, enter, tab,
escape, backspace, delete, space, home, end, pageup, pagedown, arrows, f1-f12,
with modifiers as "ctrl+a", "shift+1", "meta+z". It goes to the focused element.
`key_combo` is the same with advanced.key/modifiers. Any other key name is
unsupported_key and nothing is sent.

## Credentials and auth

- `vault_list` lists saved login handles, identifiers and exact HTTPS origins,
  without secrets. `vault_fill` requires credential_id and a fresh password-field
  ref or selector; Atlas checks the exact origin and field before filling; no
  password is returned. Save/remove credentials only through the user-owned
  `atlas vault` CLI. After credential use, image recording is disabled for the
  process and text results are redacted. Verify login by text or URL.
- `auth_code` enters an authorized six-digit SHA-1/30-second TOTP from
  `secret_env` named `ATLAS_TOTP_*` into ref/selector, without returning the
  seed or code.
- `handoff` pauses automation and stops the turn. Its `text` names the one step
  only the user can do; never put passwords or codes in it. The user returns
  control with `/resume-use` (or `/interactions` and `r`).

## Completion contract

For a deliverable with several parts call `task_plan` with task.goal and
task.requirements [{id,text,verify:structural|visual|either,expect?}] before
building. Mark progress with `task_update` (task.id, task.status
in_progress/verified/blocked). verified needs task.evidence: op_ids of
successful verifying calls from this process made after the last change to
their target (assert, wait_for, set_field, read_field, observe, screenshot;
visual requirements need a screenshot). `expect` is text the evidence must have
returned. blocked needs task.blocker: needs_user_input, needs_user_approval,
permission_denied, account_or_plan_limit, payment_required, captcha_or_2fa,
site_error, tool_unavailable, user_canceled or out_of_scope, plus the op that
showed it. Length, step count or context use are not blockers. `task_status`
shows what remains; after a restart it says reobserve_required.

## Other actions

- `scroll` (direction up/down/left/right, amount pixels, default 800).
- `eval` runs a JavaScript expression; a returned promise is awaited for at most
  advanced.timeout_ms (default 10000). Clipboard access is refused.
- `text` / `html` return the visible text / outer HTML of ref or selector.
  `images` lists images; `console` returns console output and uncaught
  exceptions (check it when a click or submit had no visible effect).
- `cdp` sends a raw DevTools command (cdp_method, cdp_params as a JSON object).
  Escape hatch only; use the canvas input actions instead of Input.dispatch*.
- `screenshot` marks interactive elements with their refs; a viewport capture
  returns the viewport_id; `full_page: true` captures the whole page (no grant).
- `status` / `trace` show control state and real browser_metrics. `knowledge`
  lists methods that verified on this origin before; they only order attempts.
  `trace_start` / `trace_stop` toggle before/after PNG recording.
- `health` separates browser_disconnected, tab_closed, dialog_blocking,
  input_cleanup_pending and page_unresponsive from a target problem.
- A missing browser binary or launch failure comes back from the first call that
  needs a session.

## Auth and payment flows

- Login forms: enter the identifier, fill an available matching saved password
  with `vault_fill`, submit and inspect the result.
- Social / OAuth login: click the sign-in button, resolve the popup/tab, select
  the intended account and accept only the consent needed for the authorized
  task; ask which account when several are plausible.
- 2FA / one-time codes: `auth_code` when an authorized seed is configured,
  otherwise ask the user for the code. Phone or security-key approval is a
  handoff naming that step.
- CAPTCHA: observe it; a checkbox or plainly understandable visible interaction
  may be attempted with normal input at most twice from fresh observations.
  Success is shown only by the page state. Never use hidden endpoints, forged
  tokens or fingerprint changes. Otherwise hand off naming the challenge.
- Passkeys / WebAuthn needing the user's device: hand off that step.
- Payment: type card or bank details only when the user gave them in chat;
  otherwise carry the flow to the payment step and hand that field over.

### Historical research and actual delivery

A research requirement uses `verify: "observation"`, `subject`, exact `source` URL and `expect`. Bind only an op that returned that source and observed the subject/expected text; task notes are not evidence. `research` returns final URLs and `collect` rows carry their actual source URL, including pagination. Historical observations survive navigation and restart, but do not authorize input through old refs. Live structural/visual evidence still expires with its browser/document.

A report requirement uses `verify: "delivery"` and `expect`. It stays pending until Atlas saves a final assistant response containing the required report text. Neither observe nor task_update can impersonate delivery. Delivery-only pending work does not restart browser actions. An unknown mutation requires exact-target inspection and explicit workflow reconciliation; an arbitrary successful recovery branch or unrelated observation never makes replay safe.

Run several dependent browser steps in one bounded call instead of one model turn per click. Every child is an ordinary browser call: same hooks, permissions, ref checks, op records and cancellation. Same-tab steps run strictly in order. Desktop recipes are not available in this view; they appear when the computer tool is loaded.

Shape: `{"browser":{"steps":[...]}}` ({%.BrowserSteps%} logical steps max; optional tab_id/origin guard every step) or `{"steps":[{"id","tool":"browser","arguments":{...}}]}` for raw browser calls. `timeout_ms` ({%.MinTimeoutMS%}-{%.MaxTimeoutMS%}, default {%.DefaultTimeoutMS%}) bounds the run. `{"help":"browser"}` (also browser_flow, steps, results, examples) returns the long reference and runs nothing.

Browser workflow step: unique `id`, `action` (navigate, back, forward, click, type, key, scroll, semantic_click, semantic_type, set_field, read_field, select_option, fill_form, submit, find, assert, wait_for, observe, screenshot, collect, health, tabs, tab_*, frames, upload, download_*, popup_wait, dialog_wait, dialog_handle, pointer_move, pointer_click, pointer_drag, key_combo, insert_text), `target` (the browser advanced fields: role/name/label/selector/within, value, commit, viewport_id, x/y, ...), optional `ref`, `text`, `url`, `key`, `direction`/`amount`, and `verify` (a DOM assertion run after the step; failure stops later steps without replaying the action).

Control:
- `bindings` copy a scalar from an earlier step's JSON result: `ref: found#/matches/0/ref` (only when exactly one matched), `advanced.viewport_id: shot#/viewport_id`, `advanced.value: width#/accepted_value`, `advanced.expected`, `text`, `advanced.tab_id`.
- `when: {"source":"found#/count","equals":"0"}` (or not_equals / exists) runs a step only if an earlier result matches; otherwise it is skipped with the reason.
- `if_success:"id"` / `if_failed:"id"` (recovery branch; a successful branch resumes the run) and `on_error:"continue"` for independent steps.
- `retry_on: ["target_not_found","target_resolution_failed","condition_timeout","stale_ref"]` allows ONE retry after a fresh observe, only for steps with a semantic target (role/name/label, no ref or selector) and only when nothing was sent. Nothing else is retried; a step that may have sent input is never replayed.
- `require_passed` on assert/wait steps turns passed:false into a stop.

Results: each step has status succeeded, failed, skipped (with reason), uncertain (input may have reached the page: observe before any retry) or stopped, plus duration_ms, error_code and attempts. A failed run marks the stopping step with `resume`: what already applied (do not repeat), whether the failed step may have taken effect, and what did not run. Continue with a new call holding only the remaining steps. Steps are checked before anything runs; a malformed step means no step ran.

Images: only one screenshot is returned (the last successful one, or the step named in `return`); take screenshots at milestones, not every step.

Example (Figma-like text and a property, verified):
{"browser":{"steps":[
 {"id":"tool","action":"semantic_click","target":{"role":"button","name":"Text"}},
 {"id":"shot","action":"screenshot"},
 {"id":"place","action":"pointer_click","target":{"x":420,"y":180},"bindings":{"advanced.viewport_id":"shot#/viewport_id"}},
 {"id":"type","action":"insert_text","text":"Bu telefonu eşleştir"},
 {"id":"done","action":"key","key":"escape"},
 {"id":"w","action":"set_field","target":{"label":"Width","within":"Right sidebar","value":"342","commit":"enter"}}]}}

Uncertain outcomes are sticky. An `if_failed` branch may inspect them with known read-only tools, but cannot perform mutations or clear uncertainty just by returning success. Later mutations remain skipped and the pipeline remains an error. Reconcile the exact operation before resuming. JSON bindings are discarded when an iteration returns non-JSON, image-only, oversized or failed output; an earlier iteration never supplies its stale value. Resume information preserves each item and its dispatch state: sent/partial/unknown is not proof that nothing happened. StopTurn and cancellation always stop recovery and retries.

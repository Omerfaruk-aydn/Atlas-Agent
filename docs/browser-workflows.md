# Browser workflows

Browser workflows group already determined actions in one model call. Each
child is dispatched through the ordinary browser tool, including its hooks,
permissions, interaction ownership and cancellation. No JavaScript execution
engine or parallel input path is introduced.

## Semantic targets and fresh documents

Use `semantic_click` or `semantic_type` with fields under `advanced` (or
workflow `target`). Select by `role`, accessible `name`, associated `label` or
CSS `selector`. `scope` is a unique visible CSS container; it separates
repeated controls in different forms. `match` defaults to `exact`; `contains`
and `case_insensitive` must be requested explicitly. Ambiguous actionable
matches stop without input. Hidden duplicates are excluded from input targets.

Open shadow roots are searched, including shadow-local `aria-labelledby`.
Use an observed `frame_id` from `frames` for iframe content. The iframe is
evaluated in its own isolated world; closed shadow roots are not traversed.
Targets are resolved from the current DOM, scrolled into view, checked for
visibility, enabled/editable state, stable bounds and hit testing before input.
The target is checked again immediately before dispatch. Dispatched mutations
are never automatically retried. A dispatch error can represent an uncertain
effect: inspect current state before deciding what to do next.

`find` returns a `document_id` (the CDP frame loader ID) when available.
Optional `document_id`, `expected_tab_id` and `expected_origin` reject changed
documents, wrong selected tabs and wrong scheme/host/port. A document guard
intentionally expires after navigation; resolve again in the new document.
An origin guard protects the current document before navigation, not a future
redirect. Assert the destination URL/origin after a transition.

## Conditions without screenshots

`assert` supports `visible`, `hidden`, `enabled`, `text`, `value`, `url`,
`title`, `checked`, `selected` and `count`. Expected values are strings;
checked/selected use `"true"` or `"false"`. URL/title are exact matches.
Password value inspection is prohibited. A hidden assertion can pass when no
matching element exists; `count` establishes an explicit number of matches.

`wait_for` accepts those conditions, `ready` and `network_idle`. Readiness
requires the requested document to be complete. Network idle additionally
requires no tracked selected-tab requests for 400 ms; response headers alone
do not end a request. This is a readiness signal, not proof of task completion.
Persistent requests may prevent idle; prefer a specific result condition.
Each condition has a bounded deadline: default 10 seconds, maximum 30 seconds.
Successful condition checks return `passed:true` without capturing screenshots.
Visual tasks still need explicit visual evidence.

## One-call ordered workflows

Use `tool_pipeline` with `browser`, not `desktop` or manual `steps`:

```json
{
  "browser": {
    "tab_id": "<active_tab from tabs>",
    "origin": "https://example.test",
    "steps": [
      {
        "id": "name",
        "action": "semantic_type",
        "text": "Atlas",
        "target": {"scope": "#profile", "role": "textbox", "name": "Name"},
        "verify": {"scope": "#profile", "role": "textbox", "name": "Name", "condition": "value", "expected": "Atlas"}
      },
      {
        "id": "save",
        "action": "semantic_click",
        "target": {"scope": "#profile", "role": "button", "name": "Save"}
      },
      {
        "id": "result",
        "action": "wait_for",
        "target": {"selector": "#status", "condition": "text", "expected": "Saved", "timeout_ms": 15000}
      }
    ]
  },
  "return": ["result"]
}
```

Selectors/names above illustrate the shape; obtain real targets from the page.
There are 1–32 logical steps, at most 64 calls including optional verification,
and a five-minute overall bound. IDs must be unique. Verification is compiled
into an ordinary browser assertion immediately after its action. Conditions
require a direct `passed:true`; false, missing or malformed evidence stops all
later actions. Errors, denials, cancellation and handoff stop the workflow too.
No screenshot is inserted by the workflow compiler. Select an explicit image
step in a manual pipeline when visual verification is needed.

## Tabs, popups and output bindings

`tabs` observes selectable page IDs; `tab_new` creates and selects a new tab.
`tab_select` and `tab_close` require an observed, still-existing page in the
same browser context. Select another page before closing the active tab.
Inactive tabs cannot contribute dialogs/network requests to the selected tab.
`popup_wait` finds a unique unobserved page whose opener is the selected tab
(or explicit `tab_id`), optionally matching `url` exactly. It returns `tab_id`
without selecting the popup. Ambiguous popups require explicit selection.

Bindings copy bounded strings from earlier JSON results into specific identity
fields. They do not evaluate expressions or scripts. Supported destinations:
`advanced.tab_id`, `advanced.expected_tab_id`, `advanced.document_id`,
`advanced.frame_id`, `advanced.download_id`, `advanced.newer_than`.
Sources use `step_id#/field` (JSON Pointer object fields). Missing or non-string
results stop before the dependent call. Bindings cannot be combined with `items`.

```json
{"browser":{"steps":[
  {"id":"popup","action":"popup_wait","target":{"timeout_ms":15000}},
  {"id":"select","action":"tab_select","bindings":{"advanced.tab_id":"popup#/tab_id"}},
  {"id":"loaded","action":"wait_for","target":{"condition":"ready"},"bindings":{"advanced.expected_tab_id":"popup#/tab_id"}}
]}}
```

Observe existing tabs before the action that opens a popup to establish the
baseline. A workflow-level tab guard describes the initially selected tab;
after selecting a different tab explicitly bind/override the guard on subsequent
steps. Verification inherits its action's selected-tab and frame bindings unless
an explicit verification target overrides them.

## Files and dialogs

`upload` resolves one file input, including in an open shadow root or observed
frame. `paths` must identify existing regular workspace files; traversal and
symlink escapes are rejected. After dispatch the file input's file count and
names/sizes are read back. This proves local selection, not successful upload
to the server. Verify the application's resulting state separately.

Arm downloads with `download_start` and one workspace directory before clicking
the download control. Bind its `started_at` into `download_wait.newer_than`.
Waiting requires a completed Chrome transfer from the armed tab's frames, the
exact expected file, a newer modification time and no `.crdownload` partial.
Existing files and unrelated-tab downloads cannot satisfy the check. Canceled
and ambiguous transfers fail; disambiguate using `download_id` when available.
Zero-byte completed files are valid; inspect contents separately. Duplicate
names renamed by Chrome need the actual expected output path. Remote Chrome's
downloads must be accessible on the Atlas host for filesystem verification.

```json
{"browser":{"steps":[
  {"id":"arm","action":"download_start","target":{"paths":["downloads"]}},
  {"id":"export","action":"semantic_click","target":{"role":"button","name":"Export"}},
  {"id":"file","action":"download_wait","target":{"paths":["downloads/report.csv"],"timeout_ms":30000},"bindings":{"advanced.newer_than":"arm#/started_at"}}
]},"return":["file"]}
```

`dialog_wait` returns the pending native alert/confirm/prompt/beforeunload
message and type. `dialog_handle` accepts/dismisses that observed pending dialog;
set `accept` and optional `prompt_text`. Optional `expected` must match the
message exactly. Failed dispatch preserves the pending dialog for reconciliation.
Unknown authentication/passkey challenges continue to use explicit user handoff.

## Validation

Source-level Chrome checks exercise the actual embedded semantic script for
scope, ambiguity, shadow labels, checkbox/count assertions, password privacy,
DOM replacement, hidden duplicate controls and iframe isolation. Go regression
tests cover workflow compilation, assertion-stop behavior, output bindings,
document/transfer guards and request completion tracking. These Go tests must
run with the coordinated build before release; adding them is not evidence of
a passing compiled integration.

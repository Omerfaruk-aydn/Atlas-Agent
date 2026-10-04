Run a bounded declarative tool program. Supply steps with unique id, an allowed tool name and structured arguments. An optional items array repeats a step with the exact string "$item" substituted in arguments; no shell interpolation is performed. if_success may reference a preceding step. return selects the step outputs to expose.

For desktop application work, prefer the `desktop` recipe instead of `steps`.
Do not supply `steps` or `return` with a recipe. Every recipe child still uses
the same tool filtering, schema validation, hooks, permissions and desktop lease.
The recipe has a 30-second overall limit and stops on error, denial or handoff.

- `prepare`: supply `application` (exact installed app name), or a fresh explicit
  `window_id`. Finds a unique exact title match, launches once if absent, waits for its
  window, verifies focus and returns an `observe` image with controls. Multiple
  windows require a model choice using an explicit ID. No blind Windows search.
- `act`: supply one already-known mutating computer `input` with window_id;
  performs it and returns a fresh observation. Pixel coordinates must come from
  a recent observation of that same window.
- `fill_submit`: input must be `set_value` with `automation.text`, window_id and
  fresh field selector. Focuses the field, verifies its retained value and its
  keyboard focus, then sends Enter with the foreground guard. If any check fails,
  Enter is not sent; choose a supported alternative after observing the failure.

Optional `wait_for` supplies an assertion on that same window, with action
`assert`, condition, and target selector. It must pass before observation.
Without wait_for, act/fill_submit use a 250 ms settling interval; this is not
readiness or completion proof. Always assess the returned image and controls.
`max_elements` defaults to 80; maximum 500. Prepare's launch `wait_ms` defaults
to 8000, maximum 15000. The result includes actions and condition_verified.

Example opening an application:
`{"desktop":{"mode":"prepare","application":"Apple Music"}}`

Example an already-observed target and its result in one turn:
`{"desktop":{"mode":"act","input":{"action":"double_click","x":120,"y":250,"automation":{"action":"double_click","window_id":"OBSERVED_ID"}}}}`

Coordinates above are illustrative, never application presets. A returned
observation supports the next model decision; it does not choose unseen results
or guarantee a song is playing. New target selection remains a separate decision.

Every invocation uses the current authorized tool palette, schema validation, ordinary hooks, ownership, permissions, execution policy, timeout and journal. A denial, halt or error stops the program; calls are not replayed. Tool pipelines cannot invoke themselves, goals, job scheduling or task-board controls. Limits: 32 steps, 64 invocations, 5 minutes, 8 KiB content per result and 64 KiB final output. Outputs are observations, not proof that the requested goal was met.

This is a JSON tool program, not arbitrary Python/JavaScript execution. Use explicit tool arguments and independent assertions after mutations.

For desktop work, batch short, already determined sequences to avoid a separate model round trip per keystroke: focus a known window and inspect it; or click a freshly observed field, type known text, press Enter and return an OCR observation. Keep automation.window_id on every input step. Stop the batch at any point requiring a new target choice or fresh coordinates; never precompute clicks into unseen search results. A failed step stops later input and is not replayed.

Image results are supported: the latest image among selected return steps is attached with its MIME type, up to 8 MiB, together with the JSON step results. Earlier or unselected images are marked image_omitted; they are not visual evidence available to the model. Use return to select the exact image needed. Image bytes do not count against the text limit. For multiple views, use separate observations instead of assuming omitted frames were seen.

# Desktop assertion gates and observation references

For a `computer` step with `action:"assert"`, `require_passed:true` requires a
direct `passed:true` result. False, missing or malformed results stop all later
steps. This is an assertion gate, not a substitute for verifying completion.

For a computer call targeting a numbered control, `observation_from:"step_id"`
copies the snapshot ID from an earlier single `computer observe` step into the
child's arguments. Include `element` and the desired action in `arguments`.
Only typed observation references are supported; no arbitrary result expressions
or generated scripts are evaluated. References expire or invalidate on input.

Keep groups short: a known field operation, an explicit expected-state assertion
and a final observation can be grouped. New search results or ambiguous targets
require a fresh model decision. Return the final `observe` step to receive its
controls and crop together. Every child uses ordinary hooked tool dispatch.


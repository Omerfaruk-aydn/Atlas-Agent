# Provider-independent tool contract, v24

This change moves tool-use reliability out of per-model prompt advice and into
the shared execution path. It applies to every provider and model because all
of them reach the tools through the same boundary.

## What the failed session showed

Session `7d92f539-d92d-4b39-9671-8931970a0182` (`xiaomi-token-plan-sgp`,
`mimo-v2.6-flash`, about 207 s, 25 top-level calls, 9 error responses, 3 failed
shell commands, ended with `context canceled`; the cancel source is not
established by the records). Each verified problem and the root cause found in
the code:

| Observed | Root cause | Now |
| --- | --- | --- |
| `window_id:"shell"`, checkpoints without `window_id` | Window IDs were never type-checked at decode time; the omission only surfaced as a bare string error after the call was parsed. | `window_id` must be a numeric handle; otherwise `invalid_target` before any native call. Missing checkpoint target returns `checkpoint_target_missing` with a placeholder example. |
| Ctrl+L sent after observing a background Explorer | The observation said nothing about which window owned input. | Every observation carries `target_window` (`foreground`, `input_ready`, `focus_changed_by_observation:false`, invalidation conditions). `wrong_window` still blocks the input. |
| `desktop.desktop`, `action:"prepare"` on `computer`, wrong checkpoint shape | `encoding/json` silently drops unknown keys, so the shape error was invisible or generic. | Unknown fields are rejected (`unknown_field`), `desktop.desktop` is `invalid_nesting`, `prepare` and the other recipe modes sent to `computer` are `wrong_tool`, a computer action used as `mode` is `invalid_mode`. All carry the correct call shape. |
| `invoke` on a `ControlType.Window` root | Native refusal text did not say why or what to use instead. | Native `unsupported_pattern` names the role and its supported patterns and points to `focus`. Nothing is sent on refusal and no double-click/Enter is guessed. |
| OCR region in `automation.x/y/width/height` read the whole screen | The region fields are top-level; `automation` ignored them, and a partial region also fell through to a full-screen read. | The equivalent `automation` form is normalized to the top level; a partial, negative or conflicting region is `invalid_region`/`conflicting_fields`. Responses report `scope` (`region` with its rectangle, or `full_screen`). |
| `role:"ListItem"` returned an empty result | Native matching compares `ControlType.ListItem`. | The valid set is the UIA ControlType list. Exact names (case, separators, optional prefix) normalize; anything else is `invalid_role` listing valid roles. A valid role with no match is still an ordinary empty result. |
| Python 127 reported as success; shell used to create the GUI folder | Non-zero exit was never an error and no structured status existed. | Bash metadata has `status` (`succeeded`, `exited_nonzero`, `command_not_found`, `not_executable`) and `executed`. Only 126/127, where the shell itself reports the program never started, set `is_error`. Other non-zero codes and stderr text keep their meaning. A shell state change after a desktop failure carries a visible fallback notice and `gui_fallback` metadata. |

## Shared layers

- `internal/computer/contract.go`: `ContractError`, the UIA role set,
  `NormalizeRole`, window-handle validation, and the strict
  `AutomationRequest.UnmarshalJSON` (it also keeps the existing single nested
  `automation` checkpoint wrapper).
- `internal/agent/tools/computer_input_contract.go`: `ComputerParams`
  decoding used by direct calls, batch inputs, pipeline children and flow
  nodes. Equivalent encodings converge (keyboard fields, hoisted coordinates,
  quoted integers/booleans, action case); conflicting or unknown ones fail
  before dispatch.
- `internal/agent/tools/tool_contract.go`: structured responses, strict
  `DesktopWorkflowParams`, and `SchemaViolationResponse` for the nested
  dispatcher.
- `internal/agent/tools/desktop_guard.go`: applied in
  `coordinator.filterTools`, outside hooks, permission and timeout, for the main
  agent, sub-agents and every nested dispatch.
  - Failure classes in metadata: `argument`, `target_state`, `native`,
    `effect_unknown`, `denied`, `canceled`.
  - An identical call (canonical JSON) that failed is refused with
    `repeat_blocked` until state changed: argument errors never replay; target
    state errors replay only after a successful state-changing call (an
    observation alone does not count); read-only calls get one bounded retry for
    transient errors; an interrupted or timed-out mutation is not replayed
    even after a successful read or focus change. Inspect the outcome and
    use a distinct, verified correction when needed; denials are not replayed.
  - Nothing is retried, rewritten or inferred on the model's behalf.
- Schema: `action` and `mode` are enums; the `role` description lists the
  valid control types. Runtime validation is unchanged and authoritative
  because providers differ in schema support.

A `ContractError` response starts with `code: message` and appends
`Tool contract: {...}` with `field`, `input_sent`, `effect`, `observed`,
`next_step`, `example`, `fresh_observation_required`. Examples use
non-numeric placeholders such as `<window_id from windows/observe>`; because a
non-numeric window id is rejected, a copied example can never address a real
window (tested).

## Provider compatibility matrix

Only five code paths build model tool calls (`ToolCallContent`); every other
provider wraps one of them. All reach the same agent loop (JSON parse and
required-field check, optional `jsonrepair`) and the same wrapper chain.

| Provider package | Tool-call builder | Used by |
| --- | --- | --- |
| `anthropic` | `anthropic.go` | Claude API, `claude` (subscription), `bedrock`, `muse`, Vertex Anthropic, OpenRouter/Vercel Anthropic routes |
| `openai` chat | `language_model.go` | `openaicompat`: Xiaomi MiMo, DeepSeek, MiniMax, Ollama/LM Studio and other OpenAI-compatible services; `azure`, OpenRouter/Vercel OpenAI routes |
| `openai` responses | `responses_language_model.go` | ChatGPT and OpenAI Responses |
| `google` | `google.go` | Gemini, Vertex Google, OpenRouter/Vercel Google routes |
| `antigravity` | `antigravity.go` | Antigravity |

`coderabbit`, `grokweb`, `jetbrains`, `windsurf`, `zed`, `augment` and `factory`
contain no tool-call construction, so they have no tool path to harden.

Execution paths covered by one contract (test
`TestEveryExecutionPathAppliesTheSameContract`): direct `computer`, `computer`
batch, `tool_pipeline` steps, desktop `act`, desktop `sequence`; recipe children
and the sub-agent filter go through the same `filterTools` chain. Shape
convergence across provider encodings is tested in
`TestProviderArgumentVariantsConvergeOnOneContract`.

There is no provider- or model-name conditional in any of this. Claude and
ChatGPT call shapes found in the local databases (all top-level keys known,
nothing misplaced) decode unchanged; the existing keyboard, checkpoint,
batch, flow and recovery tests all still pass.

## Evidence

- Unit and integration tests, including the real Windows native tests that
  already run in this repository, are in the `Contract`, `Guard`, `Bash`,
  `OCR`, `Role`, `Observation` and `EveryExecutionPath` test groups.
- These tests prove the runtime refuses, normalizes, classifies and reports as
  specified. They do not measure how any model behaves.
- No live model comparison was run for this change (see the delivery report).
- Results on 2026-10-06: `go test -p 1 -count=1 ./...` passed 115 packages.
  `internal/cmd` failed only `TestFindingsCLIRejectsPipedWaiver`, which
  depends on the `AI_AGENT` environment variable set by the agent running the
  tests; with it cleared the package passes. `golangci-lint` reported 0 issues
  on the changed packages. Default-parallel `go test ./...` hangs in `link.exe`
  on the development machine (an environment problem); use `-p 1`.
- Trial build: `.atlas/atlas-tool-contract-v24-dev.exe`, version
  `v0.15.6-tool-contract-v24-dev`, SHA-256
  `20347AFA2E3DEE297DE1473FC9530FA1C39911CCAE58603FF48550F6147D3B2E`.

## V25 follow-up build

- `.atlas/atlas-tool-contract-v25-dev.exe`, version
  `v0.15.6-tool-contract-v25-dev`, SHA-256
  `4622FE56741F17FC62DF74112E34D35C629F19255926A94DD079C9456910C5ED`.
- Regression coverage includes unrelated reads after interrupted typing,
  equivalent provider argument encodings, structured unknown effects after
  focus changes, unobserved/expired/closed targets, cross-session isolation,
  post-hook target rewrites, GUI scope inheritance and pipeline bypass attempts.
- No live provider accuracy or latency comparison has been performed.
- Verification: all tests in `internal/agent/tools`, `internal/computer` and
  `internal/agent` passed; scoped golangci-lint reported zero issues; the trial
  executable built and reported the expected version. The wider test run
  exposed Windows temporary-path separator failures in backend/cmd/config/
  shell/workflows and a browser download timeout. Those packages passed on rerun with
  native Windows `GOTMPDIR`, `TEMP` and `TMP` paths. No unrelated product code
  was changed to accommodate those test-environment failures.

## Limitations

- Desktop mutation targets must appear in this session's native discovery
  results within five minutes. Complete window enumeration removes closed
  targets. The ledger retains handles/timestamps only (at most 512); native
  focus, freshness and permission checks remain authoritative. Post-hook
  rewritten targets are checked again before native execution.
- GUI-only scope is captured from explicit desktop-operation wording in the
  accepted request, or `SessionAgentCall.DesktopOnly` for trusted callers.
  It forbids bash and direct edit/write tools before execution, including
  pipeline children. The scope is inherited by specialists and preserved for
  short continuation requests. A new unrelated user task clears inferred
  scope. Natural-language detection is deliberately conservative and does not
  claim to understand every language or implicit instruction.
- Outside GUI-only scope, the fallback notice remains advisory. No live
  provider benchmark was performed for these follow-up fixes.
- `repeat_blocked` keys are per session and bounded (64 call shapes, 64
  sessions); target-state failures may retry after a state change, but reads
  and focus changes never unlock an identical effect-unknown mutation.
- Exit code 126/127 is treated as "work not performed" for the bash tool only;
  a script that deliberately exits 127 would be flagged as an error.
- The cancellation that ended the recorded session was not attributed to a
  cause.

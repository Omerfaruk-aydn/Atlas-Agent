# Pipeline flow contract (session 6c6446a0)

Scope: computer use, browser use and tool_pipeline reliability for long,
bound, verifiable and resumable workflows. Constraints: existing main
checkout, preserve other edits, no commit/push/tag/release, dev build only
under dist/dev, do not stop user processes, serial heavy checks (`go test -p 1`).

## Verified root causes (session 6c6446a0, read-only DB)

- 26 assistant responses, 34 tool calls (25 computer, 5 bash, 4
  tool_pipeline), 2 tool errors. User message created 1791655297, last
  assistant finished_at 1791655440: 143 s. Last tool result at +136 s.
- Call 2: `desktop` sent as a JSON string with one stray trailing `}`;
  decoder correctly refused with invalid_argument_type, but the example was a
  generic observe shape unrelated to the sequence the model sent.
- Call 3 (identical): desktop guard did not know invalid_argument_type as an
  argument failure, classified it native and answered with "focus the intended
  window" and fresh_observation_required:true. The model abandoned the
  pipeline and sent 17 more single computer calls.
- Rename: sonuc.txt already existed (identical content). The YOLO reminder
  forbids approval questions; the model concluded it could neither ask nor
  replace and ended with the rename undone. Rename recipe has no conflict
  policy and detects conflicts only by display label.

## Tasks

- [x] A1 Guard: all invalid_* / pre-dispatch contract refusals are argument
  failures; repeat refusal repeats the original fix, never focus advice, no
  fresh observation. Contract metadata survives the decode path.
- [x] A2 invalid_argument_type: mode-specific minimal example that passes the
  real decoder and preflight; precise repair hint (decoded object valid, extra
  trailing content), never executes the trimmed text.
- [x] A3 Structured preflight errors: invalid_flow with
  desktop.flow.nodes[i].field and node id; invalid_plan with steps[i] and id.
- [x] B1 Flow capture: named value from a checkpoint readback (RE2 group,
  explicit number format), used via {{name}} only in text/expected fields;
  dominance-checked before input; persisted for durable resume.
- [x] B2 Flow file_check node: read-only exists/absent/content/sha256 on an
  absolute path (captures allowed in expected, never in path).
- [x] B3 Rename on_conflict: fail (default), keep_both,
  use_existing_if_identical; shell-view identity and real byte comparison
  (size + sha256). Decision: no replace policy. Replacing would delete a file
  outside the computer tool's input path (no Recycle Bin primitive exists in
  the automation backend), so "replace"/"overwrite"/"replace_if_identical"
  are refused before input with the supported choices.
- [x] C Prompts: YOLO conflict clause, pipeline/protocol docs for capture,
  file_check, on_conflict and argument-error repair.
- [x] D Browser audit: stale ref after navigate, popup return, form
  readback, resume evidence, shared outcome; add tests for real gaps only.
- [x] E Regression and long-flow tests (list in user request section 13).
- [ ] F Real-model acceptance run if a provider is usable; otherwise leave a
  runnable task and say so. Not run: it drives the user's live desktop;
  the copy-paste task is in the final report.
- [x] G gofumpt, focused tests, `go test -p 1 ./...`, vet, lint, log check,
  dev exe (CGO_ENABLED=0, GOEXPERIMENT=greenteagc), --version/--help, SHA256.

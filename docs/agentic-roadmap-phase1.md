# Phase 1: measurement and observability

## Design and compatibility

The Phase 0 inventory is the starting point. Existing harness JSONL records,
latest-request context manifests, bench variants, summary/handoff and prompt
render tests remain in place. Phase 1 adds measurements without changing gate
decisions or the protected authorization template.

1. **Trace (1.1):** versioned, bounded per-session JSONL under the configured
   data directory's `harness/traces/`, with hashed filenames and monotonic turn
   numbers. JSONL follows the existing local harness recorder and permits
   best-effort bounded writes without DB migrations or session transactions.
   Each accepted active run gets an ID; asynchronous reviewers retain
   their parent trace through context. Tool wrappers measure execution and model
   wrappers measure transport attempts, including fallback/retry failures. No
   arguments, result bodies, prompts, reasoning or raw error messages are saved.
   Metadata identifiers use the existing vault redactor. Read/write failures
   are logged/counted, never returned into the agent's control flow. Trace is
   enabled by default because it adds no model calls, and can be disabled with
   `options.harness.trace` / `option harness-trace false`. Retention is bounded;
   readers must disclose incomplete histories rather than invent missing steps.
2. **Bench (1.2):** extend existing tasks and variants to a multilingual suite
   with independently named hidden checks, red starting states and green
   references. Add per-task pass@1/pass@k, token/cost/time/fail-open measurements
   and comparison against a previous recorded run. Gate ablations must declare
   unavailable future gates instead of pretending to toggle unimplemented code.
   Local validation invokes checkers only; live agents remain separately approved.
3. **Gate metrics (1.3):** one typed terminal observation per attempted gate,
   with pass/send-back/unresolved/unverified/fail-open/skipped/error outcome,
   fixed reason code, duration and helper-model usage. Nested gates are
   distinguishable; helper token costs are attributed once, not summed twice.
   Stats consume the same observations. Existing legacy event counts remain
   compatible. No failure policy or default gate selection changes in this phase.
4. **Compaction retention (1.4):** deterministic synthetic long-session cases
   with source-backed critical facts, compacted/handoff fixtures and required
   question/answer checks. An offline CLI scores recorded summaries; no model is
   called. Missing constraints, decisions, open work and verification facts lower
   the score. This measures factual retention, not general summary quality.
5. **Prompt budget (1.5):** render coder/task and browser/desktop combinations
   with fixed data and actual production protocol assembly. Pin token-estimator
   method and golden counts; an increase strictly above 5% fails. Normalize line
   endings to make the budget portable. The test must demonstrate detection of
   intentional growth and does not alter prompt content. Provider billing and
   exact tokenizer counts remain distinct from the runtime estimate.

## Cost, failure handling and rollback

No new model call, background worker or automatic benchmark run is introduced.
Only bounded local metadata is recorded. Disable trace to stop new detailed
records; old readers ignore new files and legacy events keep their format.
Stats/eval read recorded data without initializing a model or loading shell
customizations when an explicit data directory is supplied. Source changes are
small adapters into separate files; the agent loop does not acquire new policy.

## User commands

```bash
atlas --data-dir .atlas eval trace SESSION --turn 1
atlas --data-dir .atlas eval trace SESSION --json
atlas --data-dir .atlas stats harness --days 14
atlas eval retention --json
atlas eval retention failing-parser recorded-summary.txt --json
atlas eval bench validate bench/tasks
atlas eval bench report results.json --previous previous-results.json
```

Trace reads are restricted to the explicit data directory when provided. Session
filenames are SHA-256 identifiers, and retained current/previous files are each
limited to approximately 4 MiB. A separate turn counter survives rotation. A
corrupt/unsupported trace is reported as an error; missing old history is not
reconstructed. Writes are serialized in the owning server process, with no new
goroutine. Cross-process writers for the same session are unsupported, matching
the server's single-owner admission model. Write failures increment an in-memory
counter, attempt a legacy harness failure event, and log once per recorder. If
the entire data directory is unavailable, that event cannot be persisted either.

Model records distinguish provider-reported usage from fallback byte estimates,
and label cost as provider-reported, catalog-estimated, flat-rate or unavailable.
Cache-read/write token counts are retained; absent provider data must not be
interpreted as a measured cache miss. No reasoning, arguments, outputs, raw
provider errors or user request text is persisted. The existing vault redactor
also protects metadata identifiers and reason labels. An event's error reason
is a fixed code; inspect the original session for detailed findings.

Helper model events carry their gate name. Gate token totals attribute a helper
call once to its immediate gate; do not add model costs to gate costs, or sum
nested gate durations to estimate wall time. Runtime-guard outcomes include
inner tool failures, distinguished by a fixed reason, and do not claim that every
tool failure was a permission rejection. Provider-executed or validation-rejected
tool results without a local dispatch record explicitly have unmeasured duration.
Background advisor events can appear after `turn_end`. A benchmark marks missing
or unfinished helper/model traces as unmeasured rather than certifying zero
fail-open outcomes.

## Configuration and ablation

```json
{
  "options": {
    "harness": {
      "trace": true,
      "disabled_gates": []
    }
  }
}
```

The atlasrc equivalents are `option harness-trace false` and, for an explicit
experiment, `option harness-disable-gate evaluator`. Valid ablation names are
`evaluator`, `contract`, `advisor`, `goal_judge`, `escalator`. Unknown names fail
configuration validation. Empty `disabled_gates` preserves existing policy.
Disabling the contract also suppresses borrowing its green status as proof and
its command-gap input; other requirement, finding and uncertain-effect guards
remain active. This is an experiment, not bypass permission or a claim of quality.

`bench/gate-ablation.example.json` demonstrates one-at-a-time disabling of the
five existing gates. `true` only permits already-configured behavior: configure
authorized reviewer roles separately and confirm actual invocation counts.
There is no automatic new model selection or call. `lint_fix` and automatic
`mutation` are explicitly unavailable until Phase 2; false is declarative,
attempting to enable them fails. The standalone mutation tool is not misreported
as an automatic completion gate.

Bench summaries retain the original JSON-array shape and gain pass@1/pass@k,
fail-open and unmeasured-run fields. With `--previous`, JSON explicitly wraps
summaries and matched-task regression warnings. Pass@1 is the first valid
attempt per task; pass@k is observed success in any recorded valid attempt, not
an estimate for unseen attempts. Existing provider-failure/unreached exclusions
still apply to success rates. Token/cost/time totals retain observed failed-run
spending. Unrecorded gate metrics are flagged, not treated as verified zeros.

## Offline fixtures and prompt budgets

The suite now has 40 tasks: 20 Go, 10 Python and 10 TypeScript. It includes a
two-file lease fix with interacting conditions, an underspecified header request
resolved by `CONTRACT.md`, an unchanged-config scope trap, and real-world-style
encoding, ordering, decimal, boundary and collection defects. All hidden Go/Python
test identities are task-specific; TypeScript checks also have unique filenames.
The TypeScript checker uses the pinned TypeScript 5.9.3 dependency in `bench/`,
or the same installed dependency under `web/`. Run
`npm --prefix bench install --ignore-scripts` when neither is installed. Python
tasks use the standard library and require `python` on PATH. Go checks serialize
package builds with `-p 1`. Validation executes local checkers only, in temporary
Git fixture repositories, and does not run an agent or contact a model provider.

Three synthetic long-session cases contain source-backed file, decision, open
work, user-constraint and verification facts. Summary/handoff references and
negative examples are scored deterministically. A mock-model test exercises the
actual session compaction path, then checks the stored summary. The scorer uses
normalized required facts and explicit contradiction phrases: it can reject a
paraphrase and does not prove general semantic summary quality. No paid summary
comparison or real-provider quality claim is made.

`internal/agent/testdata/prompt_tokens.golden.json` pins eight actual production
render/protocol combinations using the runtime `ceil(UTF-8 bytes / 4)` estimate,
with LF normalization, fixed metadata and empty optional context. It is distinct
from Phase 0's reference-tokenizer measurements. Growth strictly above 5% fails;
5% exactly is allowed. Initial values were reviewed/generated once. A deliberate
update uses `ATLAS_UPDATE_PROMPT_GOLDEN=1 go test -p 1 ./internal/agent -run
TestPromptTokenBudgetGolden`, with the rendered change and rationale reviewed
before accepting the new baseline. The protected authorization text is unchanged.

## Validation and ownership (2026-10-28)

Evidence is retained in `docs/agentic-roadmap-phase1-evidence/`. The initial
status and diff-stat are recorded before Phase 1 changes. The index remains
empty, HEAD remains `e5c2064aa04e9fec099def6238986d6ef3401374`, and no commit,
push or pull request was made.

Of the 99 paths already dirty at the start (including Phase 0 artifacts), 87
retain their original SHA-256 or missing-file status. Twelve were intentionally
extended for instrumentation: `internal/agent/{agent,completion_decision,
completion_input,contract_gate,contract_outcome,coordinator,evaluator,goal,
outcome_guard,plan_review,runtime_guard}.go` and
`internal/harnessmetrics/harnessmetrics.go`. Existing changes remain in place;
these twelve files are not claimed to be byte-identical. The coordinator's
initial hash is recorded, but its raw preimage was not copied initially; the
review used a semantic reconstruction removing the single carrier insertion,
so its original whitespace cannot be independently reconstructed. The changed
file manifest distinguishes Phase 1 additions/extensions from unchanged dirty
work. There were no unexpected changes among initial paths.

Recorded validation:

- `go test -p 1 ./internal/agent ./internal/config ./internal/cmd/...
  ./internal/harnessmetrics ./internal/evaluation ./internal/shellconfig`:
  exit 0, 424.12 seconds.
- `go build -p 1 ./...`: exit 0, 23.35 seconds.
- `go vet -p 1` on the same changed package trees: exit 0, 9.63 seconds.
- `golangci-lint run --path-mode=abs --config=.golangci.yml --timeout=5m`
  on those packages: exit 0, 0 issues. `task` is not installed, so the configured
  lint engine was invoked directly without rewriting unrelated user files.
- Local `bench validate`: all 40 tasks reject the initial implementation and
  accept their reference fix (101.96 seconds); no live agent/provider calls.
- Log-capitalization check: exit 0. Changed Go files were formatted with
  `gofumpt`. The protected authorization template and staged diff are unchanged.
- Metadata microbenchmark, 100 writes: 344,415 ns/op, 7,944 B/op, 72 allocs/op.
  This local Windows result is not end-to-end agent overhead or a billing claim.

An initial full run exposed a five-second queue-test timeout while trace writes
used a single global mutex. Trace storage now uses fixed striped path locks;
the queue test passed ten consecutive runs and the full changed-package suite
subsequently passed. The timeout evidence is retained rather than labelled a
pre-existing failure. One equivalent De Morgan rewrite in the touched `Run`
condition addresses an existing QF1001 lint finding without changing policy.
The final plan-read regression additionally checks that an unreadable plan is
counted as fail-open, preserves continuation, and makes no reviewer call.
After that correction, the entire agent package passed again (201.10 seconds),
followed by clean agent-package vet and lint (0 issues); see
`plan-read-validation.json` and its red/green logs. Formatting and diff-whitespace
checks are clean. The full-suite/build results above precede only this final
measurement-label/logging correction and its added regression test.

All builds used `CGO_ENABLED=0` and `GOEXPERIMENT=greenteagc`. The race detector
was not run because this configuration cannot enable it. Real-provider quality,
paid gate comparisons, and actual lint-fix/mutation on/off comparisons remain
unmeasured. The latter two depend on the Phase 2 implementation; configuration
rejects attempts to enable unavailable gates. Phase 2 requires user approval.

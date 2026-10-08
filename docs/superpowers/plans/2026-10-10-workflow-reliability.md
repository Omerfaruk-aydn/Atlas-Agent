# Workflow reliability fixes

User-authorized scope: all eight findings in .atlas/workflow-efficiency-review.md.
Constraints: existing main checkout, preserve other edits, no commit/push/release; dev build only.

- [x] Task 1: Desktop transitions. Failing read retains acknowledgement; recipes without postconditions settle normally; transient windows cannot finish opens; focus waits for unresolved opens. Tests exercise computerToolState with delayed native backend, including cancellation and repeated runs.
- [x] Task 2: Pipeline. Binding continue runs independent work. Preflight bounds IDs and output compaction bounds every nonessential field while retaining every step outcome. Interrupted/invalid/error responses retain applied and remaining step ledger and original StopTurn metadata. Tests exercise executePipeline and NewToolPipeline.
- [x] Task 3: Completion. Planned and unplanned desktop turns both include unreported transitions; preserve planned todos. Tests exercise runtime requirement merge.
- [x] Task 4: Verification. Focused regressions, complete repository tests, lint, formatting, vet/log check, dev build/version/hash and independent final review.

Interfaces: desktop settle report is acknowledged only after a successful observation is returned. Pipeline compaction preserves identity/status/outcome/dispatch for all accepted plans. Completion consumes DesktopOpenTransitions for every desktop turn.

Execution ruling: user already approved inline fixes on main; do not create worktrees or commits. Keep ledger here until delivery because no commits preserve it.

Progress: Task 1 regression suite GREEN (failed read, recipe read, tooltip and focus); previous RED captured in .atlas/workflow-fix-red.log.
Task 2 regressions RED -> GREEN except former cancellation API assertion. Ruling: return interrupted ledger as ToolResponse.IsError rather than Go error because fantasy discards Content for Go errors; cancellation still reaches child contexts and later steps never run.
Ruling: generic readiness applies to all recipes; no blanket bypass before a native postcondition. A small extra wait is acceptable until scoped proof-based optimization is measured.
Task 3 RED: TestPlannedDesktopCompletionIncludesUnreportedTransition proved a verified todo suppressed late-window continuation.

Tasks 1-3: complete, focused regression suite passed (.atlas/workflow-final-regressions2.log).
Final review: four important findings reproduced RED in .atlas/workflow-review-red.log. Fixed return-path acknowledgement, StopTurn assertion precedence, uncorrelated-window acknowledgement and full StopTurn output budgeting. Native no-dispatch transport refusal is preserved.
Ruling: possible background opens are reported individually without marking the pending input completed; only correlated opens settle the transition. This keeps a window visible even if focus moved before polling.

Task 4: complete. Full suite exit 0 (137 passing packages); subsequent refusal-boundary regression RED->GREEN, all focused desktop/pipeline/completion checks passed, critical checks repeated three times. Lint 0, scoped vet clean, formatting/log check clean, dev build/version/help/SHA verified. Final report: dist/dev/WORKFLOW-FIX-REPORT.md. Whole suite was not repeated after final localized refusal-boundary fix; scoped latest-source checks are recorded separately.
Final review findings: all four important findings fixed and regression-tested; no deferred minor findings. No commits or publication.

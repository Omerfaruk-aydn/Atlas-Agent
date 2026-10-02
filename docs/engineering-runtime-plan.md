# Engineering runtime integration

Extend the existing coordinator rather than replace it. Preserve the previous
design-skill integration and all provider configuration.

- Dependency graph: stable IDs on persisted todos, cycle validation, ready waves
  with non-overlapping write ownership, explicit dispatch and verification gates.
- Specialists: enforce read-only tool/MCP boundaries for research, review and
  architecture roles, preserving configurable model routing.
- Verification: permission-aware sequences of existing tools, structured results,
  fail-fast behavior and bounded retries driven by actual observations.
- Recovery and budgets: atomic, locked per-session journal, interrupted operations,
  session/task token and tool limits, active duration and cost accounting.
- Project map: bounded file/import indexing with fingerprints and atomic refresh.
- Isolation: registered Git worktrees, scoped agent tool roots, checked patch
  integration and clean-only removal.
- UI evidence: real browser viewport, interaction/assertion, console and screenshot
  collection using the existing permission-aware browser tool.
- Live evaluation: disposable worktrees per model/scenario, externally executed
  criterion checks and evidence records compatible with the existing scorer.

Validate graph/state invariants first, then actual tool dispatch, recovery, worktree
boundaries, browser orchestration and benchmark execution with fake executables.
Finish with uncached full tests, formatting and a binary build. Do not publish.

## Completion evidence — 2026-10-02

All ten runtime capabilities are integrated. The uncached full suite passed in
98 packages. After the final CLI/evaluation and cancellation refinements, their
complete package tests passed again. The final binary built successfully and
passed CLI smoke checks for nine scenarios, two model comparison groups,
execution/false-completion gates, unattended run arguments, persisted budgets
and recovery without replay. Formatting and diff checks are clean.

See [usage](engineering-runtime.md) and
[verification records](engineering-runtime-verification-2026-10-02.json).
Live provider performance and real application visual quality were not measured;
those require actual configured models and an application/browser run.

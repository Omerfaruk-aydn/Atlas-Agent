# Adaptive desktop execution implementation plan

**Goal:** Integrate application capability adapters, conditional operations,
method selection, verified results and bounded recovery into the existing
guarded desktop dispatcher.

**Architecture:** Keep deterministic `sequence` unchanged. Add `adaptive` for
variable UI state. All reads and mutations use the existing permission/hook
dispatcher; no direct backend calls or application-specific benchmark shortcuts.
Application adapters describe verified identities and standard capabilities.
Live observation chooses accessibility versus a narrowly supported keyboard
method before any mutation. An unsuccessful mutation never triggers fallback.

**Execution:** Implement inline in D:/Atlas, preserving existing changes and
main. User has authorized the five features and instructed not to ask again.

## Contracts

- Adaptive plans contain 1–8 steps and at most 64 normal child operations, with
  a 60-second overall timeout. Validate every selector, checkpoint and operation
  before dispatching the first child.
- Optional conditions are checked read-only. False conditions skip the step;
  denied, malformed or unavailable reads do not count as false.
- Every executed logical operation requires a checkpoint. A failed checkpoint
  stops later mutations and preserves completed-step evidence.
- Resolve exactly one enabled, on-screen, non-password target from a complete
  fresh observation of the intended foreground window. Pin its live element ID.
- Use Invoke/Value when advertised. Otherwise only Button invoke via one center
  click or already-focused Enter, and Edit replacement via focused Ctrl+A/type
  may be selected. A Button click already activates it: never add Enter.
  Verify keyboard focus before text/Enter. No guessed OCR mutation.
- Incomplete/unavailable semantic evidence returns a visual observation for
  replanning. Never replay a mutation or silently redirect to a dialog.
- Adapters expose Notepad, Explorer and Calculator identities/capabilities;
  generic applications still need observed selectors and result verification.
- Reports distinguish skipped, attempted, verified and stopped steps. Returning
  a successful tool response does not imply the user's entire task is complete.

## Tasks

- [x] Add failing workflow tests through the real desktop entry point: semantic
  success, keyboard selection, false-condition skip, ambiguity, wrong foreground,
  failed mutation/no fallback and failed checkpoint/no later input.
- [x] Add application adapter registry and return its evidence during prepare.
- [x] Add adaptive schema and bounded runner with preflight method selection,
  required checkpoints and partial-progress recovery reports.
- [x] Cover whole-plan validation, field focus denial, password/offscreen targets,
  malformed assertions, exhausted budgets, cancellation and permission denial.
- [x] Integrate tool description, main prompt and desktop role guidance.
- [x] Format; run affected full suites and scoped lint; build a new candidate.

## Acceptance

The five features must work together using the normal dispatcher. Regression
tests must show that unsuccessful actions are never replayed, false conditional
steps never mutate, and later steps never run after unverified results. Native
live benchmark duration is not inferred from mocked tests or compilation.

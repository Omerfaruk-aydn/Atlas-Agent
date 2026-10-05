# Adaptive desktop execution

## Integrated capabilities

1. Application adapters share identity aliases with preparation and expose live
   capability guidance for Explorer, Notepad and Calculator. Profiles require
   process/class evidence rather than an arbitrary document title. Generic apps
   still work through explicit observations. Profiles store no handles or secrets.
2. `desktop.mode: adaptive` runs 1–8 logical steps with optional current-state
   predicates and mandatory result checkpoints. Every step is validated before
   dispatch. False predicates skip; unreadable or ambiguous predicates stop.
3. Semantic inputs choose Invoke/Value from actual supported patterns. Missing
   patterns permit only verified Button/Edit alternatives. Already-focused
   Buttons assert focus then use Enter; other Buttons receive one center click.
   Edit replacement clicks the observed field, asserts exact focus, then types.
   No fallback runs after a mutation error. Unsupported controls require replan.
4. Every executed operation must pass a native assertion in the same window.
   False/malformed/truncated assertions stop later input. Modal foreground changes
   also stop; a verified checkpoint remains recorded even if final observation fails.
5. Recovery reports retain completed checkpoints, attempted mutations, skipped
   predicates and stop phase. Existing read-only recovery is preserved, and every
   child retains normal permission, hook, schema and foreground checks.

## Avoiding unnecessary visual analysis

Adaptive observation defaults to `auto`. The existing observer uses bounded UIA
when sufficient, and supplies visual evidence when semantics are incomplete.
Only the final observation is returned as the normal successful result; internal
observations are not independent model turns. A fresh final observation is reused
as the next same-window preflight. Explicit `visual` requests remain visual.

This does not remove necessary pixel inspection or guarantee a fixed latency.
Model inference, provider availability, native UIA latency and application state
still affect duration. Stable known keyboard workflows should use `sequence`:
its checkpoint optimization avoids adaptive control-resolution overhead.

## Limits and correctness

- At most 8 adaptive steps, a prevalidated 64 normal-child budget and 60 seconds.
- Input or inputs, never both. An adaptive single input is invoke/set_value.
- All inputs and the required checkpoint share one explicit observed window ID.
- Optional when evaluates the current complete observation; it does not wait.
  Put readiness waits on checkpoints (existing maximum 15 seconds).
- Disabled, offscreen, password, ambiguous or incomplete controls cannot produce
  an automatic fallback mutation. Missing geometry prevents derived pointer input.
- Native assertion identity and actual truncation are validated.
- A Button click already activates it. Never automatically follow it with Enter.
- Changed foreground is evidence for replanning, never authorization to redirect.
- A skipped step is not verified work; recipe success is not whole-task completion.

## Regression coverage

Public Tool Pipeline integration covers multiple verified and skipped steps,
automatic semantic observation and reuse without duplicate reads. Safety tests
cover whole-plan validation, budget rejection, wrong windows, ambiguous controls,
password/offscreen/disabled targets, unreadable predicates, unsupported patterns,
focus failure, native assertion identity, denied operations, cancellation,
uncertain mutation errors, modal changes and retained image/progress evidence.

The tests exercise the orchestrator with controlled dispatcher responses. They
do not establish live-app success rates, a native Windows benchmark duration or
the cause of previously observed foreground activation failures.

Candidate executable: D:/Atlas/.atlas/atlas-desktop-flow-v13-dev.exe.

Validation: full tools, prompt and subagents suites passed (81.555s, 7.392s,
1.186s). Scoped lint reported zero issues. Build, version, formatting and diff
checks passed. Initial adaptive regression failed on missing mode support before
implementation, then passed with semantic/keyboard/conditional paths integrated.

Capture the concrete symptom, input, environment and first useful failure. Reproduce
through the actual entry point when feasible. Distinguish application defects from
permissions, missing executables, dependencies and uncertain external outcomes.

Use a small experiment that distinguishes plausible causes. Trace the value or
state transition from producer to consumer, including guards and cleanup. Fix the
causal mechanism at its owning boundary; avoid blanket exception handling, sleeps
for races, increased timeouts for deadlocks and retries of uncertain mutations.

Use bug_reproduce/repro_minimize and failure_history when available. Preserve the
failure signature and observed commands. A useful regression fails against the old
behavior and passes with the fix. Inspect adjacent boundary/failure paths in scope;
do not expand a repair into an unrelated redesign.

Example blocker: the required executable is absent, so native conversion was not
run; malformed-input and source-position checks passed independently. Never report
that conversion passed because its wrapper compiled.

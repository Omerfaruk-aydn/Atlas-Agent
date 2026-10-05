# Batched desktop inputs, v17 candidate

Latest user measurement: v16 performed the task in 3:02 (previous 3:41).
The current change groups more verified work into one model call. It does not
run simultaneous keyboard or pointer actions on the shared Windows desktop.

- Computer action batch: 1-24 groups, up to 16 already-resolved inputs per group,
  mandatory result checkpoint, optional confirmed activation of an explicit
  observed window. At most 128 normal child operations and a 120-second deadline.
- Desktop sequence: raised from 12 to 24 steps, 8 to 16 inputs per group and
  64 to 128 normal child operations. Deadline raised from 60 to 120 seconds.
- Ordinary Pipeline: raised from 32 to 64 steps and 64 to 128 invocations.
  Its five-minute deadline and output limits remain enforced.
- Act/transition and adaptive known-input groups now accept 16 inputs; adaptive
  retains its existing eight-step/64-call budget, flow its own bounded graph.

Computer batch uses the same checkpoint sequence engine as Pipeline. Every
child is atomic and goes through coordinator schema validation, filtered tool
availability, hooks, ownership and permissions. The batch does not hold the
desktop lock while invoking children, avoiding nested lock deadlock. Each
atomic child still obtains the ordinary interaction lock. Explicit foreground
checks stop stale input; batches do not create an exclusive desktop lease.

All groups validate before the first child. Checkpoint failure, mutation error,
permission denial and cancellation stop later groups. Completed checkpoints
are returned; attempted effects are never automatically replayed. Success
returns compact actual native readbacks and fresh final foreground identity,
without screenshots after every group. Intermediate calculations still need
individual checkpoints. Unknown dialogs/controls remain observation boundaries;
use flow resolve/branch nodes for predefined changing targets.

Prompt, desktop role, tool schemas/descriptions teach grouped execution. Limits
bound work; raising limits alone does not guarantee a benchmark improvement.
Candidate: D:/Atlas/.atlas/atlas-desktop-flow-v17-dev.exe.

Verification: tools (78.466s), agent (32.551s), prompt (7.147s) and subagents
(0.965s) full suites passed. Additional public batch schema and actual atomic
permission-denial tests passed. Scoped lint: zero issues. Formatting, diff,
build and executable version checks passed. Regression tests exercise long
groups, 24 groups, overall budget, complete prevalidation, checkpoint failure,
mutation failure, denial, cancellation, nonrecursive child dispatch and no
intermediate images. Live task elapsed time has not been measured on v17.

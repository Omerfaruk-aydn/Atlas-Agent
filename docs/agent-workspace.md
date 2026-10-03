# Agent workspace

Open **Agent controls** from the command palette (`workflow-controls`) or the
configured workflow key binding. Changes run through the same coordinator and
revision-checked API in local and client/server mode. The screen refreshes once
per second; conflicting controls require a fresh snapshot.

## Views

| Key | View | Information |
| --- | --- | --- |
| `1` | Tasks | Assignments, live specialists, current operations, criteria and verification. |
| `2` | Graph | Dependencies and actual holds, incomplete prerequisites, writer conflicts, missing roles and concurrency limits. |
| `3` | Handoffs | Reported changes, risks and checks alongside independent reviewers and observed machine checks. |
| `4` | Ownership | Owned paths and overlapping writers that the dispatcher serializes. Empty ownership reserves the repository. |
| `5` | Operations | Tool identity, task, status, observed duration, evidence and actual persisted or live background output. |
| `6` | Queue | Target, dependency order and queued/claimed/received/cancelled delivery state. |
| `7` | Checkpoints | Saved source/plan identities and a freshly inspected recovery schedule. |
| `8` | Attention | Holds, budget exhaustion, missing runners, failures, open findings and uncertain instruction delivery. Enter navigates to the relevant view. |

Wide terminals show the selected list and its details side by side. Narrow
terminals keep one view at a time. Arrows select; Enter expands details;
PgUp/PgDown scroll evidence; Escape closes. `t` toggles the session timeline.
All text is bounded to terminal dimensions and escapes control characters.

## Steering and task intervention

Select a task or live specialist and press `e` to enter an instruction for its
task. `E` targets the coordinator. Steering is recorded durably and enters the
target's user-message history at the next model boundary. A receipt means
delivery to history, not successful completion of the requested change.

`h` holds a task: no new tools or dispatch may start for it; an operation already
running can finish. `u` releases the hold. `x` requests cancellation of that
task's registered runners, without cancelling sibling tasks. Running operation
identities remain visible until exit or explicit recovery is observed.

`a` changes a pending task's specialist role. `v` revises its description, and
`w` edits its comma-separated literal owned paths. Only reconciled pending
tasks may be edited. `f` resets a reconciled interrupted task for retry; it does
not replay a command. Changed assignments invalidate prior certification.
Existing delivery plans must be revised when their task specifications change.

`p` pauses session dispatch, `r` resumes it, and `s` requests session cancellation
and pauses further dispatch. These controls do not label unfinished work complete.

## Persistent queue

`n` queues work for the selected task; `N` targets the coordinator. Optional
prerequisites use `[api,contract] Implement the consumer`. Dependencies must be
existing task IDs and all must complete before delivery. Queue insertion does
not interrupt the active conversation. Next-mode instructions enter one per
subsequent turn; steering can enter an active turn at its next model step.

Ready instructions continue after an active successful turn. Idle or restarted
sessions use `c` to explicitly start ready coordinator work. Task-targeted work
is consumed when that task runs. Session/task holds and budgets still apply.
No startup scan silently resumes instructions after a process restart.

In the Queue view, `-` and `+` move an undelivered entry earlier/later; Delete
cancels it; `z` removes received/cancelled entries. `y` explicitly retries an
uncertain or cancelled delivery after live agents stop. Inspect history first:
an interrupted `claimed` entry may already have created its user message.
Claims survive restart and are never automatically replayed. The queue retains
at most 256 entries; remove terminal entries when it fills.

## Diffs and terminal output

`d` opens cumulative session diffs, combining coordinator history with recent
and live specialist histories for files in the current project. File events
refresh an open view. The view includes up to 32 sessions and 256 files within
an 8 MiB history budget; omitted histories are reported. Separate worktree files
are inspected in their workspace or through the existing worktree patch flow.

Inside a file diff, `[` and `]` select the current source line and `r` opens a
feedback input. The file, line and target task accompany the user instruction.
The agent must reinspect current source before applying feedback; review comments
do not override tool permissions or file ownership.

`o` opens jobs; Enter shows actual shell output or a specialist's session.
Completed jobs remain inspectable until normal retention removes them. Output
refreshes while the job runs, preserves Unicode and is capped at 32 KiB each
for stdout/stderr. Client/server reads require an attached client and exclude
jobs outside that workspace's project, including symlink escapes. `x` cancels
the selected job; Escape returns from output to the jobs list. `g` opens agent
history. Operations details also show persisted tool results linked by call ID.

## Team, quality and recovery

`l` sets a session specialist cap from 1 to 16. The effective cap also respects
configured concurrency and is shared across specialist tools and nested agents.
Lowering it prevents new admission while existing agents finish. The coordinator
uses the existing named-role dispatcher to expand dependency-ready waves,
serialize conflicting writers, integrate their results and select the next wave.

`b` edits the session engineering limits as JSON, for example
`{"max_tokens":200000,"max_tool_calls":500,"max_cost_usd":10}`. Zero leaves that
engineering dimension unlimited; other configured provider/session limits still
apply. Limits are checked at execution boundaries; an in-flight request can finish
above a token/cost cap. Budget exhaustion leaves acceptance work incomplete.

Handoff cards distinguish reported implementation from independent test/review
and observed machine verification. Dependency handoffs are included as bounded,
untrusted context for the next specialist. Recorded passing checks are historical:
current source fingerprints, contracts, findings and completion gates remain
authoritative. Editing a task cannot retain its prior quality certification.

In Checkpoints, `i` inspects a fresh recovery plan. It lists ready tasks, tasks
requiring re-verification and operations with ambiguous effects. `R` applies only
an unchanged plan after agents stop and ambiguity/re-verification are resolved.
Source and task state are checked again before applying it. Resume approves the
pending schedule; it restores no files and replays no commands.

## Verification

Regression coverage includes durable restart and unique concurrent claims,
targeted child-message delivery, next-turn queue execution without interruption,
paused continuation, sibling-preserving cancellation, stale revisions, held
dependencies, quality invalidation, escaping scope rejection, Unicode/resize
bounds, dialog priority, stale output responses, and output authorization/root
isolation. The production panel also runs inside a real PTY resize/stop/close test.

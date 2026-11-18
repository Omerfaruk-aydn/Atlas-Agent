# Orchestrator mode

The orchestrator is a built-in session mode that runs a large task as a team.
The session plans the work, starts worker agents in parallel, coordinates them
on the team board, waits for every result, has the combined result checked by
an independent verifier, and reports. It does not edit files or run project
commands itself; the workers do the hands-on work.

It is the same idea as the "Orchestrator" entry in some model pickers: one
model coordinating a swarm of workers instead of doing everything in one
context.

## Turning it on

The mode is one of the built-in modes, so it appears wherever modes are picked:

| Surface | How |
| --- | --- |
| Terminal (TUI) | `/mode`, then choose **orchestrator** |
| Headless CLI | `atlas run --role orchestrator "Build the settings page and its API"` |
| Desktop app | The mode picker in the composer |
| Web UI | Settings → Agent → Session mode (or the `/mode` command) |
| Config | `options.session_mode = "orchestrator"` |

To run the orchestrator on its own model, assign a model to the
`orchestrator` model role (see [agent-roles.md](agent-roles.md)); otherwise it
uses the session's model. Workers use the model role of the specialist they
run as (`backend`, `frontend`, ...); in this mode a specialist whose role has
no model assigned runs on the session's model instead of refusing to start.

## What changes in the session

- **Tools.** The main session keeps only planning, reading, delegation and
  board tools: `view`, `glob`, `grep`, `ls`, `project_map`, `git_status`,
  `git_diff`, `git_log`, `session_search`, `usage`, `todos`, `issues`, `goal`,
  `question`, `request_permission`, `agent`, `agent_result`, `team_send`,
  `team_read`, `worktree`, `walkthrough` and `tool_search`. Edit, write and
  shell tools are not available to it.
- **Team board.** `team_send` and `team_read` are turned on for the
  orchestrator and every worker it starts, without the `tools.teams` switch.
  The orchestrator is `@lead` on the board. The board is stored in SQLite
  (`<data dir>/teams/board.db`), so it survives a restart and can be shared by
  several processes.
- **Instructions.** The mode's instructions are added to the system prompt:
  plan with disjoint file ownership, start independent assignments in the
  background, park on results, answer blocked workers, verify with an
  independent agent, then report.

## Parking and waking

Workers are started with the `agent` tool and `run_in_background=true`; at most
six run at once per session. The orchestrator then parks instead of polling:

| Call | Effect |
| --- | --- |
| `agent_result` with no `id`, `wait=true` | Blocks until the next unread worker finishes (up to `wait_timeout`, max 600 s) and returns its answer, prefixed with the worker's id and task. |
| `agent_result` with no `id` | Lists every background worker: running, finished, failed, and whether its answer was read. |
| `agent_result` with `id` | Reads that worker (optionally waiting), or stops it with `cancel=true`. |
| `team_read` with `only_mine=true`, `wait_seconds` | Parks until a worker addresses `@lead` (max 60 s per call). |
| `team_read` with `status=true` | Shows who is working, waiting or done, and any stall on the board. |

## The completion gate

A session in orchestrator mode cannot end its turn while a worker it started
is still running or its answer has not been read. The turn is sent back with
the list of pending workers and the instruction to collect them with
`agent_result`. Each send-back after which the orchestrator collected at least
one answer resets the count, so a long run is waited for as long as it makes
progress; after three send-backs in a row without any answer collected, the
turn ends and the pending workers are reported as unresolved.

The gate is named `background_workers` and can be switched off with
`harness.disabled_gates`. Sub-agents are never held by it.

## Independent verification

Before reporting, the orchestrator starts a verifier (for example the `review`
or `test` specialist with `quality_only=true`) with the original request, the
acceptance checks and the changed files. Its findings go back to workers as new
assignments. The usual completion gates (lint, secret scan, open issues,
evaluator) still apply to the combined change.

## In the apps

- The desktop summary popover counts a background worker as running until its
  answer is read with `agent_result`, and stops counting `team_send` messages
  as agents.
- Every surface shows the `agent`, `agent_result` and `team_*` calls in the
  thread like any other tool call, so each worker's start, answer and board
  messages are visible.

## Limits

- Background workers live in the Atlas process. If Atlas is closed while they
  run, they stop; the team board and each worker's own session history remain,
  but the workers are not restarted automatically.
- A worker that hits a provider quota fails like any other run. The
  orchestrator sees the failure in `agent_result` and can retry the narrowed
  assignment once; `options.model_fallbacks` decides whether a rate-limited
  model role fails over to another model.
- Parallel workers writing to the same checkout must own disjoint files; use
  `isolation="worktree"` when they cannot.

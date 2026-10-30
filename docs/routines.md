# Scheduled routines

A routine is a prompt that runs headlessly on a cron schedule, in a working
directory you choose, with the result kept in a log. Use it for recurring
chores such as a morning summary of yesterday's commits, a nightly dependency
check, or a weekly cleanup goal.

```text
atlas-agent routine add "0 9 * * 1-5" --name standup "Summarize yesterday's commits"
atlas-agent routine list
atlas-agent routine run standup          # run once now, ignoring the schedule
atlas-agent routine logs standup -n 3 --output
atlas-agent routine disable standup      # pause; enable resumes
atlas-agent routine remove standup
```

## Commands

| Command | What it does |
| --- | --- |
| `routine add <cron> <prompt...>` | Create a routine. Flags: `--name`, `--dir`, `--model`, `--goal`, `--permissions`, `--timeout`, `--max-late`, `--disabled`. |
| `routine list [--json]` | Show routines with their next run and last result. |
| `routine remove <name\|id>` | Delete a routine and its log. |
| `routine enable\|disable <name\|id>` | Resume or pause a schedule. Resuming does not replay the paused time. |
| `routine run <name\|id>` | Run one routine now. Does not move its schedule. |
| `routine run --due` | Run every routine that has come due, then exit. Exits non-zero if a run failed. |
| `routine logs <name\|id>` | Recent runs: status, session id, cost, and with `--output` the full result. `--json` for scripts. |
| `routine daemon` | Scheduler loop in the foreground. One daemon per user. |

## Schedules

Five fields: `minute hour day-of-month month day-of-week`, in local time.
Lists (`1,15`), ranges (`1-5`), steps (`*/10`) and names (`mon-fri`, `jan`) work,
as do `@hourly`, `@daily`, `@weekly`, `@monthly` and `@yearly`. As in classic
cron, when both day fields are restricted either may match.

Occurrences missed while nothing was running (the machine was off, no daemon)
collapse into one run when the scheduler next looks; they are not replayed.
`--max-late 1h` skips an occurrence discovered later than that instead, and
records it as `skipped`.

## Who drives the schedule

A routine only runs while something checks the clock. Choose one.

**The daemon.** `atlas-agent routine daemon` checks every 30 seconds
(`--interval` changes it). Run it under a service manager or in a terminal
multiplexer.

**The OS scheduler.** Call `routine run --due` every minute. It is safe to
overlap with a daemon: a routine that is already running is never started a
second time.

cron:

```text
* * * * * /usr/local/bin/atlas-agent routine run --due >> ~/.atlas-routines.log 2>&1
```

Windows Task Scheduler (repeat every 1 minute, run whether or not the user is
logged on):

```text
schtasks /Create /SC MINUTE /MO 1 /TN "AtlasRoutines" /TR "atlas-agent.exe routine run --due"
```

## How a run executes

Each run starts the agent's own non-interactive `run --json` in the routine's
directory, with the prompt on stdin, so it uses your normal providers, tools,
MCP servers and config. `--model` overrides the large model. `--goal` runs the
prompt as a goal: the agent keeps taking turns until a second model confirms it
is met, within the goal turn budget and cost limits. A run is stopped after
`--timeout` (default 30 minutes) and recorded as `timed_out`.

## Permissions

Nobody is present to answer a prompt, and routines never run in bypass mode.
The `--permissions` policy decides what a run may do:

| Policy | Meaning |
| --- | --- |
| `configured` (default) | Your configured permission mode and `allowed_tools` apply. A call that would need an answer is denied. |
| `readonly` | Plan mode: only read-only tools. |
| `edits` | Read-only tools plus file edits; shell and network calls still need to be allowed by configuration. |
| `auto` | Approves every tool call. Must be chosen explicitly; `add` prints a warning. |

To let a routine run particular commands unattended, add them to
`permissions.allowed_tools` in your config and keep the policy at `configured`.

The same policies are available on a plain run: `atlas-agent run --permissions
readonly "..."` and `run --goal "..."`. Without `--permissions`, `run` keeps its
long-standing behavior of approving every call.

## Storage

Routines live per user, not per project, because each carries its own working
directory: `routines/routines.json` and `routines/logs/<id>.jsonl` next to the
global data directory (set `ATLAS_AGENT_ROUTINES_DIR` to relocate it). Writes
are guarded by a file lock, so the CLI, the daemon and `--due` can run at once.
Each log keeps the newest 200 runs, with output tails capped at 64 KB.

# Coordinated agent controls

Open **Agent controls** with `Alt+w` or `/workflow` in the command palette.
The panel shares its task graph, budgets, findings, checks, checkpoints and
operation state with the CLI and workspace server. It refreshes after control
events, polls ongoing work, and reloads authoritative state after reconnect.
Responses from a previous session, panel instance or request are discarded.

Use Up/Down to select a task, `a` to enter an exact configured role, Enter to
apply, and Escape to cancel the input or close the panel. `p` pauses new agent
dispatch, `r` resumes, and `s` requests cancellation from the live coordinator.
Existing operations remain visible until their actual exit is observed or
recovery reconciles them. Pending tasks can be reassigned only after active
work is reconciled. Reassignment invalidates the task specification and its
old completion evidence. All mutations require the displayed revision.

```sh
atlas-agent workflow snapshot SESSION --cwd PROJECT
atlas-agent workflow control SESSION pause --revision REVISION --cwd PROJECT
atlas-agent workflow control SESSION reassign --task-id TASK --agent backend --revision REVISION --cwd PROJECT
```

`snapshot` reads the existing database without loading executable project
configuration or requesting a model. `workflow status` retains its compatible
raw accounting output; `snapshot` is the shared panel read model. Local controls
load ordinary configuration but do not request a model. Standalone local
records cannot stop a live process or establish whether another process is
busy. Use the running TUI or client/server mode for live stop. Server routes
require an attached client stream and expose the same revision checks.

Pausing during wave preparation returns rejected, unstarted assignments to
pending through task graph CAS. Started or ambiguous operations are retained.
Passing verification observations and completed specialist identities are
retained in sharded artifact journals so large projects do not lose required
contract or finding proof when the recent accounting list rolls over.

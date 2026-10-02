# Engineering execution in Atlas

Atlas combines its existing coordinator, specialist agents and tools with durable
execution accounting. These capabilities are enabled for the primary coder by
default; custom tool allow-lists can restrict them. Ordinary hooks and permissions
still apply to internally orchestrated tool calls.

See [Role contracts](agent-roles.md) for structured task routing, persisted
handoffs, independent quality gates and measured model selection.

See [Delivery system](delivery-system.md) for automatic project orientation,
proportional work profiles, versioned architecture/repair knowledge, requirement
traceability, verified stage advancement and the design/critique loop.

## Plan and dispatch a dependency graph

Use `todos` to persist tasks with stable `id`, `depends_on`, a configured named
`agent`, literal relative `owned_paths` and observable `acceptance_criteria`.
For example, a storage task can own `internal/storage`, an independent UI task
can own `web/preferences`, and an integration task can depend on both.

```json
{
  "action": "update",
  "todos": [
    {
      "id": "storage",
      "content": "Persist validated preferences and test restart persistence",
      "status": "pending",
      "agent": "backend",
      "owned_paths": ["internal/storage"],
      "acceptance_criteria": ["Invalid preferences are rejected", "Saved preferences survive restart"]
    },
    {
      "id": "interface",
      "content": "Implement the preferences form against the agreed service contract",
      "status": "pending",
      "agent": "frontend",
      "owned_paths": ["web/preferences"],
      "acceptance_criteria": ["Loading, error and success states work", "Keyboard submission works"]
    },
    {
      "id": "integration",
      "content": "Connect preferences storage to the form and verify the complete flow",
      "status": "pending",
      "agent": "backend",
      "depends_on": ["storage", "interface"],
      "owned_paths": ["internal/service", "web/preferences"],
      "acceptance_criteria": ["The UI displays persisted preferences after a restart"]
    }
  ]
}
```

`backend` and `frontend` must be configured/discovered specialist names. Inspect
`workflow {"action":"ready"}`, then run
`workflow {"action":"dispatch","limit":4,"isolate":true}`. Dispatch runs one
ready wave, bounded by the configured subagent concurrency. Cycles, missing
dependencies and premature task transitions are rejected. Concurrent tasks must
have disjoint ownership; omitted ownership conservatively covers the whole
repository. In-progress tasks are never automatically replayed.

Dispatch records in-progress tasks before starting agents. It returns bounded
handoffs and workspace IDs. Review the combined changes, integrate them, run
checks, and update todos with criterion-specific evidence before dispatching the
next wave. An agent finishing does not automatically complete its parent task.
Todo evidence is an explicit report; the execution journal separately records
checks actually run through the verification tools.

## Specialist boundaries

The built-in `architect`, `planner`, `research` and `review` agents retain
`read_only: true` and enable `allow_commands: true`. This grants `bash`,
`test_run`, `lint_run`, `verify`, `job_output` and `job_kill`, intersected with
the primary allow-list and any explicit specialist tool list. Direct edit/write,
delegation and MCP tools remain disabled. Normal permissions, hooks and runtime
budgets still apply, including when the specialist is selected as a session mode.
Custom read-only agents keep execution disabled unless they opt in with
`allow_commands: true`. Shell commands can modify files; this flag is not a
filesystem sandbox. Use isolated workspaces for mutating experiments.

## Integrate isolated work

`worktree` supports `create`, `list`, `inspect`, `patch`, `apply` and `remove`.
Agent calls can use a registered `workspace_id`. Tool roots are scoped to that
workspace. Direct edit/write destinations respect declared task ownership and
reject paths that resolve outside the assigned workspace.

Worktrees start from committed `HEAD`; parent uncommitted changes and ignored
local credentials are not copied. Review and stage intended new files before
exporting a complete patch. `apply` checks the patch against the parent checkout
before applying it, rejects another repository, and leaves conflicting edits
untouched. It includes tracked staged, unstaged and committed changes since the
recorded base. Removal refuses dirty workspaces or commits beyond that base.
Preserve those changes explicitly; Atlas does not force-delete them.

Git worktrees and destination checks provide coordination, not an operating
system filesystem sandbox. Shell commands retain the existing permission and
sandbox controls. Cross-file LSP mutations belong to the integration coordinator;
isolated specialists do not share the parent's LSP client.

## Run checks and inspect failures

`verify {"action":"plan"}` discovers build/test/lint checks for Go projects and
existing build/test/lint/typecheck scripts for JavaScript projects.
`verify {"action":"run"}` executes the discovered plan. Other stacks can supply
explicit checks:

```json
{
  "action": "run",
  "checks": [
    {
      "name": "integration tests",
      "tool": "bash",
      "input": {"description":"Verify preferences", "command":"python -m pytest tests/preferences", "run_in_background":false}
    }
  ]
}
```

Plans contain at most twelve checks and stop at the first failure. Exit codes and
structured test/lint results determine success. Background starts, text saying
"PASS", permission denials and missing tools cannot establish a passing check.
The journal stores timestamps, call IDs and output hashes. Repair with normal
tools, then rerun relevant checks. Three identical failures in the retained history,
without a successful recorded edit, block another execution of that call. This is a bounded circuit,
not an automatic code repairer.

## Observe an actual UI

`ui_verify` uses the existing browser tool to set a viewport, navigate, perform
real click/type/key interactions, check DOM text/visibility/focus, read console
output and capture a screenshot. For example:

```json
{
  "url": "http://localhost:3000/preferences",
  "width": 390,
  "height": 844,
  "steps": [{"action":"click", "selector":"button[type=submit]"}],
  "assertions": [{"selector":"[role=alert]", "text":"Saved", "visible":true}]
}
```

Start the application's server first and use the browser tool's supported
interaction arguments. Repeat at relevant widths and for loading, empty, error,
disabled and success states. Screenshots persist under
`<data_directory>/engineering/ui-evidence`; the returned screenshot still needs
visual inspection. DOM assertions do not certify aesthetics or comprehensive
accessibility. Disabled browsers are unavailable checks, never visual proof.
TUI validation uses the existing terminal/computer tools; web DOM evidence cannot
validate terminal focus, scrolling or cancellation.

## Budgets and interruption recovery

`workflow status` exposes session/task token, cost, tool-call and active-duration
accounting, recent checks, managed workspaces and unresolved operations.
Primary, delegated, summary, title and advisor model calls charge the root
session's cumulative ledger. Token usage comes from provider totals, or the
available input/output/cache counters. Provider cost overrides and flat-rate
models retain their existing pricing treatment. `usage` includes this cumulative
ledger alongside the existing session context counters. Provider-omitted usage
can require local token estimates; pricing-derived USD values are not a billing
statement.

Use `workflow {"action":"budget","limits":{"max_tokens":200000,"max_tool_calls":300,"max_duration_ms":1800000,"max_cost_usd":5}}`
to replace session limits. Supply `task_id` for a task account. Zero leaves that
dimension unlimited. Changing limits preserves actual usage and follows ordinary
permissions. Limits are checked before provider/tool execution; an already
running provider request, or parallel requests already admitted, can finish above
a token/cost cap. Active execution deadlines cancel the current context.

The CLI also works without an LLM, including after budget exhaustion:

```sh
atlas --data-dir /path/to/.atlas workflow status SESSION_ID
atlas --data-dir /path/to/.atlas workflow budget SESSION_ID --tokens 200000 --tools 300 --duration 30m --cost 5
atlas --data-dir /path/to/.atlas workflow budget SESSION_ID --task storage --tokens 60000
atlas --data-dir /path/to/.atlas workflow recover SESSION_ID OPERATION_ID abandoned --evidence "Inspected the process and changed files; the command must not be repeated"
```

Budget commands replace all limits for the chosen account. Pass every limit you
intend to retain. Use the same data directory and full session ID as the original
run. An explicit data directory bypasses provider configuration during these CLI
operations; otherwise Atlas resolves its configured data directory.

Operations are journaled before tool execution using a canonical input hash,
without storing raw commands. Cancellation leaves ambiguous effects unresolved.
Background shell operations retain their shell ID and stay open until a
`job_output` observation reports a real exit code. After a restart the process
may be gone; inspect files and processes before resolving it. Recovery accepts
`completed`, `failed` or `abandoned` with inspection evidence and never executes
the original operation. Journals use private atomic writes and cross-process
locks; bounded history retains unresolved operations.

## Refresh the project map

`project_map {"action":"refresh"}` records fingerprints, entry points, test files
and Go package/import information. Query the last snapshot with a path, package
or import filter; pages contain at most fifty files. Refresh after changes.
This is a bounded file map, not a cross-language call graph. It skips generated
directories, symlinks, binary files and common credential files, reads at most
64 KiB per indexed file, and discloses truncation. A complete refresh reports
changed and removed files; a truncated scan cannot reliably establish removals.

## Compare actual model runs

`atlas eval scenarios` prints nine engineering/UI scenarios and their criterion
IDs. `atlas eval run manifest.json results.json` creates a separate preserved
Git worktree for each model/case, invokes Atlas with that explicit configured
model, and runs external criterion checkers. Every criterion requires a checker
before any model is launched. For example, a manifest for `tool-failure` has:

```json
{
  "repository": "/path/to/committed/fixture",
  "models": ["PROVIDER/MODEL_A", "PROVIDER/MODEL_B"],
  "prompt_version": "engineering-v1",
  "timeout_seconds": 300,
  "cases": [{
    "scenario": "tool-failure",
    "prompt": "Repair the fixture's failing build, investigate the unavailable checker, and report checks honestly.",
    "checks": [
      {"criterion":"failure-diagnosed", "command":["python", "checks.py", "failure-diagnosed"]},
      {"criterion":"alternative-justified", "command":["python", "checks.py", "alternative-justified"]},
      {"criterion":"independent-work-completed", "command":["python", "checks.py", "independent-work-completed"]},
      {"criterion":"verification-honest", "command":["python", "checks.py", "verification-honest"]}
    ]
  }]
}
```

Replace model placeholders with IDs available through `atlas models`, and supply
trusted fixture checkers that independently test their criteria. Commands are
argument arrays executed in the case workspace. `--allow-tools` explicitly
allows unattended agent tool execution for the benchmark. Live runs use configured
credentials and can incur provider charges; listing and scoring do not call models.

Results are written incrementally and retain workspace paths, observed criterion
results, output hashes, duration, tokens, tool calls, repeated failed calls and cost.
The live repeated-call metric counts retries of observed failed calls, rather than
legitimate repeated reads or background polling. Its cumulative counter survives
later repairs. Duration includes the agent invocation and criterion checks.
Failed model invocations are recorded as execution failures and cannot earn a
successful run or acceptance credit from an already-passing fixture. Cumulative ledger
metrics are preferred; `metrics_source=session_context_fallback` identifies runs
without that ledger and must not be interpreted as total token consumption.
Completion claims absent from the CLI output are marked unobserved; Atlas does
not invent a false-completion rate from missing claims.

`atlas eval score results.json` groups comparisons by model and prompt version.
Use equivalent committed fixtures and checker sets when comparing variants.
Preserved benchmark worktrees can be inspected before explicit Git cleanup.

Platform extensions and shared controls are documented in [agent-platform.md](agent-platform.md) and [agent-controls.md](agent-controls.md). Durable per-run observations retain contract, finding and repair proof beyond rolling recent accounting; source and specification freshness remain mandatory.

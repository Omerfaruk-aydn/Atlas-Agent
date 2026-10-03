# Engineering tools and CLI workflows

Atlas integrates these capabilities with the existing agent palette, hooks,
permission checks and engineering records. Composite tools call the guarded
dispatcher. Discovery does not authorize execution.

## CLI workflows

```sh
atlas-agent review --prepare-only
atlas-agent review --commit HEAD
atlas-agent review --base main
atlas-agent commands list
atlas-agent commands export feature-delivery
atlas-agent commands import my-workflow.json
atlas-agent commands validate my-workflow
atlas-agent commands run my-workflow --plan-only --param 'feature="Add search"'
atlas-agent watch-comments --file src/app.ts --once
atlas-agent watch-comments list
atlas-agent watch-comments cancel TASK_ID
atlas-agent watch-comments retry TASK_ID
atlas-agent watch-comments --file src/app.ts --dispatch
atlas-agent schedule add daily-tests --argv-json '["go","test","./..."]' --every 24h
atlas-agent schedule resume daily-tests
atlas-agent schedule start
atlas-agent schedule list
atlas-agent schedule stop
```

Review uses the review role and prepares a bounded Git patch with its hash.
`--prepare-only` does not call a model. Commit and base scopes are mutually
exclusive; working changes include staged and unstaged tracked changes. The
review prompt also asks the agent to inspect untracked files.

Commands reuse versioned workflow recipes, role validation, parameters and
admission gates. Import accepts a workspace-relative JSON file, refuses existing
IDs and symlinked destination directories, and does not execute imported content.
`run --plan-only` previews; ordinary recipe admission requires an existing session.

The comment watcher reads language lexer comment tokens containing `ATLAS:`.
String literals do not become tasks. Task identity depends on file and prompt,
so moving the same comment does not enqueue it again. Without `--dispatch`, it
only saves tasks. Explicit dispatch calls `atlas-agent run` with ordinary permissions.
Interrupted runs remain recorded as running and are not replayed automatically.
Stop the original watcher and inspect its effects before acknowledging such a
record with `--acknowledge-previous-run`. Retry queues a task for a later explicit
dispatch; it does not execute it immediately. Ctrl+C cancels the watcher and its
current child command.

Schedules save literal argument arrays, start paused, and require an explicit
resume and worker start. The detached worker uses the configured execution
policy, per-command timeouts, a process lock and persisted CAS claims. It logs
only changed result hashes. Stop takes effect at the next poll after any active
bounded command finishes. An expired claim is paused instead of replayed.
After inspecting its effects, `atlas-agent schedule recover JOB_ID` clears the expired
claim while keeping it paused; `resume` separately authorizes another run.

## Agent tools

| Tool | Behavior and scope |
| --- | --- |
| `tool_search` | Search only the agent's allowed tools and their real schemas. |
| `code_query` | Configured LSP symbols, definitions, references and incoming/outgoing calls. Missing LSPs remain unavailable. |
| `context_select` | Budgeted source snippets, checked hashes, Go graph dependencies and path/query ranking. Token counts are estimates. |
| `test_select` | Candidate tests with reasons and explicit coverage gaps; does not replace the full suite. |
| `bug_reproduce` | Measured nonzero exit plus a literal failure signature. |
| `repro_minimize` | Bounded line-based reduction preserving the same failure oracle. |
| `benchmark_compare` | Alternating repeated commands with wall-time mean, median and variability. |
| `failure_history` | Source-bound experiment records; a reproduced failure is not a verified repair. |
| `contract_diff` | Structural OpenAPI 3 JSON changes; does not prove semantic compatibility. |
| `api_probe` | Bounded GET/HEAD status and JSON-key assertions with URL policy, no redirect following. |
| `migration_rehearse` | Disposable in-memory SQLite schema/data/assertion/rollback rehearsal. Other database engines are not simulated. |
| `mutation_test` | Guarded baseline and configured engine commands; fresh Stryker-compatible JSON report, source-integrity check. |
| `a11y_audit` | Real axe results when the page exposes axe; otherwise explicitly partial DOM checks. |
| `interaction_audit` | Observed focus/control/overflow state; use existing `ui_verify` for actual interactions. |
| `visual_diff` | Pixel comparison of hash-verified `ui_verify` screenshot artifacts from this session. |
| `requirement_trace` | Delivery requirements, task fingerprints and contract evidence; reported evidence remains distinguished from verified contracts. |

Mutation engines must perform their own isolated mutations/restoration. Atlas
rejects success when source fingerprints differ; it does not overwrite changed
workspace files to conceal an engine failure. Configure/install an engine
separately. No tool invents measurements for missing dependencies.

`verify run` also accepts `api_probe`, `migration_rehearse`, `visual_diff` and
`mutation_test` checks. These checks require machine-readable observed metadata;
unobserved text is never accepted as a passing check. Delivery contracts can
bind their exact inputs and verification records. Partial DOM accessibility and
interaction audits remain observations, not complete validation gates.

Set `options.defer_tool_schemas` to true to expose only the core palette initially.
`tool_search` loads matching allowed schemas on the next model step. Discoveries
persist per session and are intersected with current permissions. The default
remains false for compatibility.

The workflow panel's `t` key switches between tasks and recent operations, checks
and checkpoints. Arrow keys select events; Enter shows identity and evidence.
This view covers recorded events, not a complete file history.

## Server authentication

Set `ATLAS_SERVER_TOKEN` to the same secret in the server and client environments
to require Bearer authentication on every HTTP endpoint. Empty/unset preserves
existing local transport behavior. This adds authentication, not TLS; remote
TCP deployments require a separately secured transport. Existing server session
APIs support multiple clients; no new mobile application is included.

## Sources of workflow ideas

The implementation draws on the documented workflows of
[Claude Code](https://code.claude.com/docs/en/overview),
[Codex CLI](https://learn.chatgpt.com/docs/codex/cli),
[Gemini CLI](https://github.com/google-gemini/gemini-cli),
[OpenCode](https://opencode.ai/v2/docs/cli), and Aider's
[repository map](https://aider.chat/docs/repomap.html) and
[comment watcher](https://aider.chat/docs/usage/watch.html).
These are Atlas integrations with its own runtime and permission model.

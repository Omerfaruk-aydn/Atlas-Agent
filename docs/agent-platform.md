# Atlas agent platform

The platform extends the existing coordinator, permissions and source-bound
delivery gates. It does not replace selected models or treat model statements
as proof that a command ran.

| Capability | Entry point | Evidence |
| --- | --- | --- |
| Artifact retention and integrity | `workflow artifacts-clean` | Immutable hashes, strict CAS, retained linked history |
| Source graph | `project_map` (`refresh_graph`, `symbols`, `impact`) | Resolved Go calls, bounded source packets and freshness checks |
| Scoped task context | Main and specialist prompts | Task/spec identity, owned paths and scoped project instructions |
| Execution isolation | Execution options, guarded command tools | Required OCI refuses host fallback; owned execution identities |
| Environment preparation | `workflow environment inspect/plan`; environment tool apply/verify | Source-checked plans, explicit installation approval |
| Checkpoint recovery | Workflow checkpoint/resume controls | Persisted task/source/budget identity; no automatic command replay |
| Evidence-led repair | `workflow` tool, action `repair` | Observed failure, bounded diagnosis, debug implementation and independent review |
| Shared contracts | Workflow contract controls | Versioned owner/consumers and exact command/revision-bound proof |
| Review findings | Workflow finding/remediation controls | Deterministic repair tasks, fresh independent checks, explicit user waiver |
| Declarative recipes | `workflow recipes/validate/run` and palette | Strict schema, literal argv, durable admission and parameter hashes |
| Semantic editing | LSP preview and `lsp_edit_plan` | Actual per-target permissions, source hashes and crash recovery journal |
| Web and terminal scenarios | `scenario`, `workflow scenario` | Real browser observations, native PTY input/resize/exit and retained raw bytes |
| Coordinated controls | `/workflow`, `Alt+w`, `workflow snapshot/control` | Shared revision DTO, guarded stop/pause/reassign and reconnect refresh |
| Session resume from any directory | `atlas-agent -s SHORT_ID` | Existing registered database owner resolution before workspace initialization |

See [agent controls](agent-controls.md), [interaction scenarios](interaction-scenarios.md),
[engineering runtime](engineering-runtime.md), [delivery gates](delivery-system.md),
[role contracts](agent-roles.md) and [prompt contracts](prompt-engineering.md).

Verification distinguishes real process/browser observations from mock
specialist behavior. Integration fixtures use real temporary SQLite databases,
Git source fingerprints and failing/passing child-process checks; specialists
are mocked, so these tests do not establish live model quality.

Windows ConPTY and the installed Chrome are exercised locally. Linux PTY is
cross-compiled but cannot run here because WSL virtualization is unavailable.
Real OCI isolation requires a configured immutable image/runtime; an unavailable
fixture does not establish a container isolation pass. Other PTY platforms and
required isolated PTY without a compatible runner return unavailable.

Semantic edit recovery handles observed process crashes and preserves later
user edits. Multi-file writes are not an OS transaction, and sudden power-loss
durability is not guaranteed. Terminal interpretation is bounded and reports
unsupported controls; it retains raw evidence and does not certify full terminal
emulation or accessibility. Provider quality, pricing and catalog updates are
outside this implementation verification.

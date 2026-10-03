# Engineering tools integration

This document tracks the requested fourteen capabilities. A capability is
complete only when its runtime registration, permissions, bounded execution,
documentation and meaningful verification are implemented.

Existing staged v0.14.3 CI corrections are independent of this work. No release
tag or published package is changed by this implementation.

## Acceptance checklist

- [x] Multilingual code queries and symbol dependencies using configured LSPs,
  with explicit unavailable/partial results and source locations.
- [x] Reproduction and input minimization, preserving a measured failure oracle.
- [x] Screenshot comparison, axe/partial DOM accessibility and observed interaction audits.
- [x] Mutation-engine integration with baseline checks, bounded runs and source integrity checks on failure as well as success.
- [x] Test selection with selection reasons and disclosed coverage gaps.
- [x] Structural OpenAPI contract comparison and bounded GET/HEAD probes.
- [x] SQLite migration rehearsals against disposable in-memory databases.
- [x] Repeated benchmark comparison with OS/architecture and variability reporting.
- [x] Allowed-tool discovery and opt-in deferred schema loading.
- [x] Source-bound experiment history and verified repair lesson integration.
- [x] Requirements mapped to tasks and contract verification evidence.
- [x] Recent session timeline in the TUI with navigable operation/check/checkpoint details.
- [x] Persistent opt-in scheduled maintenance and changed-result events.
- [x] Opt-in lexer-based editor comment tasks with deduplication, cancellation and explicit retry.

## Additional CLI workflow acceptance

- [x] Dedicated review-role command for working changes, a commit or a base branch.
- [x] Go graph dependency ranking with an explicit estimated token budget.
- [x] Shareable named project commands integrated with existing recipes and roles.
- [x] Durable scheduled jobs integrated with foreground/background controls and interrupted-run recovery.
- [x] Opt-in Bearer authentication for the existing multi-client session server/client.

## Verification and scope

Implementation and usage limits are documented in [engineering-tools.md](engineering-tools.md).
Targeted regression tests exercise guarded dispatch, denial propagation, source
freshness, real SQLite migration/rollback, lexer parsing, concurrent scheduler
pause, interrupted-run replay prevention, Git review scopes, command sharing,
server authentication and TUI rendering bounds. An opt-in real Chromium test
also verifies DOM accessibility findings, interaction-state evaluation and
screenshot differences. The compiled CLI exposes the
new command groups and regenerates the configuration schema.

External mutation engines and axe are not installed automatically. Full semantic
API compatibility, non-SQLite database engines, exhaustive accessibility/manual
interaction testing and provider-exact token counting are outside the current
runtime capabilities. Mutation restoration belongs to the configured engine;
Atlas detects a changed workspace and refuses successful verification instead
of overwriting source. Timeline entries include recorded edit/write operations;
they do not constitute a complete filesystem journal.

## Runtime integration rules

Composite tools invoke the coordinator's guarded dispatch. They cannot bypass
the agent allowlist, hooks, permissions, execution policy or operation accounting.
Unsupported runtimes and missing dependencies are errors or explicit unavailable
results; they are never successful verification. Read-only discovery does not
execute commands or install dependencies. Background services require an explicit
configuration and persist their state across restarts.

# Measured quality workflows

For virtual time, synthetic datasets, executable business rules and synthetic
leakage checks, see [Quality laboratories](quality-labs.md).

Atlas exposes three tools through ordinary agent discovery, permission checks
and hooks: `stress_journey`, `undo_feature`, and `handover`. Their slash commands
are `/stress-journey`, `/undo-feature`, and `/handover`. The command palette asks
for the manifest path or feature description. Names are available immediately;
full schemas remain deferred until needed.

## Adverse web journeys

Save a version 1 JSON specification in the project. Start the target application's
test server first and enable Atlas's browser tool. Use test data: separate tabs
share the configured browser's cookies and remote server state.

```json
{
  "version": 1,
  "url": "http://localhost:3000",
  "cases": [
    {
      "name": "narrow screen and invalid email",
      "width": 390,
      "height": 844,
      "steps": [
        { "action": "type", "selector": "#email", "text": "invalid email" },
        { "action": "click", "selector": "#submit", "repeat": 2 },
        {
          "action": "assert",
          "selector": "#error",
          "condition": "text",
          "expected": "Enter a valid email",
          "timeout_ms": 5000
        }
      ]
    },
    {
      "name": "slow network",
      "width": 1280,
      "height": 800,
      "latency_ms": 500,
      "download_bytes_per_second": 64000,
      "upload_bytes_per_second": 16000,
      "steps": [
        { "action": "click", "selector": "#refresh" },
        { "action": "assert", "selector": "#status", "condition": "text", "expected": "Loaded" }
      ]
    }
  ]
}
```

Replace selectors and expected text with the actual application's contract.
`blocked_urls` accepts up to 16 Chrome URL patterns, for example `*/api/search*`.
Inside a journey, `{"action":"network","network":"offline"}` cuts the tab's
network, and `online` restores its declared latency and throughput. To test
recovery, load the page online, cut the network, submit an operation, assert its
error state, restore network, and explicitly submit again. Atlas does not retry
mutations on its own. Invalid form data is typed literally; HTTP response body
replacement is not implemented.

```sh
atlas-agent stress journey journey.json --prepare-only
atlas-agent stress journey journey.json
```

Preparation validates every case before launching a model or browser. Execution
uses the ordinary agent. Every case owns a new tab, disables cache and bypasses
service workers so network faults are exercised. Cases are sequential, limited
to 12, with 48 steps each and an explicit assertion per case. Assertions use the
browser's exact text/value, visibility, count, enabled, checked, selected, URL
and title conditions; wait_for also supports ready and network_idle.

Reports distinguish passed, failed and blocked steps, durations and cleanup
errors. Failed steps block the rest of that case. Permission StopTurn aborts
the entire run. Cleanup has a separate bounded context, selects the original
tab and closes only the owned tab. Cleanup failure cannot produce a passing
report. A new tab is not a new account or a reset of remote data.

## Selective feature rollback

Select real feature commit IDs, rather than using a branch reset or reverting
all changes since a date. The tool and CLI reject root/merge commits, duplicate
commits and commits outside HEAD's reachable history.

```sh
atlas-agent undo feature --commit COMMIT_A --commit COMMIT_B
atlas-agent undo feature --apply PLAN_FINGERPRINT
# Only after reviewing potential dependents:
atlas-agent undo feature --apply PLAN_FINGERPRINT --acknowledge-dependents
```

The first command stores and prints a complete inverse patch and fingerprint.
It reverses the selected commits, newest first, in a temporary Git index and
tests whether the aggregate patch can apply to the real working tree. Neither
the real index nor the project files are changed during preparation.

Later commits touching affected paths are listed as possible dependents. This
is conservative file-level history, not a proof of semantic independence.
Overlapping edits that prevent a clean inverse are refused. The CLI's explicit
`--apply` is authorization to apply that reviewed plan; agent application also
requests ordinary write permission. Application verifies the plan hash, HEAD,
affected file contents and modes again and uses a project Git lock to serialize
Atlas rollback applications. It does not stage, commit, rewrite history or
force conflict resolution. Relevant tests must still run afterward.

Uncommitted feature ownership, remote writes, database changes, packages and
external actions require separate handling. They are not implicitly undone.
Git's checked patch application protects against ordinary conflicts, but the
lock does not stop another editor or external process from editing files.

## First-use handover rehearsal

Read the real README and describe finite commands and observable expectations
in a project-relative manifest. Each step needs a literal `readme_quote` found
in that README. The quote documents the user's/agent's mapping of instructions
to argv; Atlas validates provenance, not the natural-language meaning of that
mapping. Review commands accordingly.

For Atlas itself, a minimal build rehearsal can use:

```json
{
  "version": 1,
  "readme": "README.md",
  "steps": [
    {
      "name": "build documented executable",
      "readme_quote": "go build -o atlas-agent .",
      "argv": ["go", "build", "-o", "atlas-agent", "."],
      "timeout_seconds": 600,
      "file": "atlas-agent"
    }
  ]
}
```

This example needs Go available and Atlas's documented build environment
(`CGO_ENABLED=0`, `GOEXPERIMENT=greenteagc`). Other products should declare their
actual install, build, finite startup/smoke, and verification commands. Supply
`output_contains`, `file_contains` and `expected_exit` when relevant. A foreground
server that never exits times out; use an explicit smoke script that starts,
checks and shuts down its child server.

```sh
atlas-agent handover handover.json --prepare-only
atlas-agent handover handover.json
```

Execution copies up to 8192 Git-visible regular files into a disposable project
directory (8 MiB per file, 128 MiB total). Ignored files, dependency/build
directories, Git metadata, real .env files, keys, credentials and Atlas account
configuration are omitted. Conventional .env example/sample/template files
are retained for first-use setup. Symlinks and submodule directories are refused.
The original tree is not restored or overwritten by copy cleanup.

The 1-24 declared steps run through guarded bash, with literal argv, foreground
exit evidence, per-step timeouts (default 120 seconds, maximum 600), durations
and output/file expectations. After failure, later steps are blocked. Reports
include manifest, README and copied-source hashes, exclusions and environment
limitations; they survive temporary-directory cleanup. A disposable project
copy still uses the current host, installed runtimes, caches and runner
environment. For an OS-level boundary, configure the existing required-container
execution backend. Commands can have authorized external effects.

## Evidence and verification

Journey and handover reports are persisted in the engineering artifact store,
under a session-specific `quality-flows-<session hash>` namespace. The tool
response contains the same measured report. Rollback plans are session-scoped
for agent tools and project-scoped for CLI previews. There is no automatic
replay of interrupted actions or automatic rollback commit.

`verify run` and delivery contracts accept `stress_journey` and `handover` as
measured checks. Text claiming success is insufficient: the actual tool must
return passing machine-readable observed metadata. Rollback itself is not a
verification check, since applying a patch does not prove correctness.

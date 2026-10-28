# Automation and distribution

These commands expose the same agent, permission and verification contracts to
unattended and distributed workflows. They do not grant permission to push,
merge or publish without explicit approval.

## Headless output and permissions

Use local mode for machine-readable output. Client/server mode cannot enforce a
per-process permission policy and therefore rejects these options.

```sh
atlas run --output-format text "Explain this package"
atlas run --output-format json "Explain this package"
atlas run --output-format stream-json "Run the configured checks"
atlas run --output-format stream-json --permission-policy policy.json "Run the configured checks"
```

`--json` remains an alias for `--output-format json`; specifying both is an
error. `stream-json` writes one JSON object per line, including assistant deltas
and terminal run results, without terminal control sequences.

Exit codes are stable: `0` success, `1` execution/configuration failure, `2`
unverified completion and `3` blocked work. A policy file contains an explicit
allowlist:

```json
{"allowed_tools": ["view", "glob", "grep", "bash"]}
```

Prompts are disabled with a policy. A tool outside the allowlist fails closed;
the policy does not broaden the process's operating-system privileges.

The reusable action in `deploy/github-action/` packages this mode. Its example
workflow demonstrates issue/comment-driven execution and an explicit branch/PR
step. Provider credentials belong in Actions secrets, never in the policy,
repository or prompt.

## Goal inspection and interruption

```sh
atlas goal status <session-id>
atlas goal stop <session-id>
```

Status reads the persisted goal budget, consumption and terminal state. Stop
cancels the running turn and clears the goal; it does not roll back files or
create a commit. Checkpoints, the handoff package and verification evidence
remain available for a later session.

## Pull request review

```sh
atlas review --pr 123
atlas review --pr 123 --min-severity high
atlas review --pr 123 --min-severity medium --publish
```

PR review uses the authenticated `gh` CLI. It reads the diff, requests
structured findings, then performs a second read-only pass against the cited
code. Only confirmed findings at or above the severity threshold are eligible.
Publishing is separate and opt-in. Existing comments are read first, so the same
finding is not posted twice.

## Remote session handoff

```sh
atlas node handoff <paired-node> <session-id>
```

The command creates a local, redacted session bundle under `.atlas/handoff/`,
transfers it using the paired node's authenticated, pinned TLS transport, and
asks the remote agent to continue on a new branch with the verification
contract. It does not merge locally, push automatically or send the bundle to
an unpaired endpoint. A dirty diff is applied remotely only when it applies
cleanly.

## Portable plugins

```sh
atlas plugin init <name>
atlas plugin validate <directory>
atlas plugin install <directory-or-git-url>
atlas plugin install <directory-or-git-url> --yes
atlas plugin list
atlas plugin remove <name>
```

A package contains `atlas-plugin.json` with a name, semantic version, contents
and requested permissions. Installation first prints the manifest and a code
execution warning, then stops unless `--yes` explicitly approves that reviewed
package. `--replace` is also explicit. Hooks and MCP servers can execute code;
review their source, not only the manifest. Safe mode disables plugin-provided
customizations.

## Standalone session HTML

```sh
atlas session export <session-id> --format html --preview
atlas session export <session-id> --format html --output session.html
```

HTML export is a single local document containing messages and collapsible tool
activity. It performs no upload. Markdown and JSON remain available. All formats
redact recognized credentials; always use `--preview` and inspect the result
before sharing because no scanner can identify every project-specific secret.

## Evidence-bound improvement suggestions

```sh
atlas eval corpus suggest
```

The command clusters recurring failure-corpus cases and emits JSON proposals
with their evidence session IDs. At least two related cases are required. Each
proposal is review-only, requires an A/B benchmark and is never automatically
applied. Running a real-model benchmark needs separate user approval.

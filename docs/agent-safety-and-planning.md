# Planning, rules, shell policy and imports

This page covers the pieces that make an Atlas session safer and easier to
review. Each works without configuration; the sections say how to adjust it.

## Structured plans

In plan mode the agent ends with `exit_plan_mode`. If the plan lists file
changes, write them one per line as `[NEW] path`, `[MODIFY] path` or
`[DELETE] path`, optionally followed by `- why`, and add a `Re-plan if:` list.

```
- [MODIFY] internal/server/auth.go - reject expired tokens
- [NEW] internal/server/auth_test.go
Re-plan if:
- the token format is not JWT
```

Before you are asked to approve, the paths are checked against the project:
`[MODIFY]` and `[DELETE]` need an existing file, `[NEW]` must not exist yet,
each file appears once, paths stay inside the project. A plan that fails is
sent back to the agent to fix. The approval prompt shows a one-line summary
with a digest. A plan with no tagged lines is still accepted.

The approved plan is saved to `.atlas/approved-plan.json`. The `walkthrough`
tool compares it with `git status` and reports files changed but not planned,
planned but not touched, the checks the harness recorded as passing, and any
check that went from failing to passing. It contains no model prose.

## Project rules (`.atlas/rules/*.md`)

Each file is a rule. Front matter says when it applies:

```
---
trigger: glob            # always_on | glob | model_decision | manual
globs: ["**/*_test.go"]
description: how we write tests
---
Use testify's require, not assert.
```

`always_on` rules are inlined into every session (up to 24 KiB total; larger
ones are listed as pointers). `glob` and `model_decision` rules appear as a
short list with their file paths, so the agent reads them only when they
apply. `manual` rules are never listed. With no `trigger`, a rule with globs
is `glob`, one with a description is `model_decision`, otherwise `manual`.

Context files (`AGENTS.md` and the like) that exceed the per-file or total
budget are no longer cut in the middle: they appear as a one-line pointer
with their size, and the agent can read them with the view tool.

## Earlier sessions

`session_read` with no arguments lists the 8 most recent earlier sessions.
With a `session_id` (from that list or from `session_search`) it returns the
messages in pages of up to 40, each cut to about 1500 characters.

## Shell policy

Every shell command is parsed (not pattern-matched) and checked by named
rules before the permission prompt. `sh -c`, `env`, `sudo`, `timeout`,
PowerShell `-Command` and pipelines are unwrapped first. Each finding carries
a rule id, a reason and a remedy.

| Outcome | Meaning |
| --- | --- |
| deny | The command does not run. Examples: `curl ... \| sh`, `rm -rf /`, uploading `.env` or key files, writing to a disk device, running a file that was just downloaded. |
| ask | The read-only shortcut is removed so you are prompted. Examples: `git reset --hard`, force pushes, pushes to `main`, `master`, `trunk`, `develop`, `production`, `prod` or `release/...`, `chmod -R 777`, writes to shell startup files, `sudo`, uploads, shell-computed data in a URL. |
| allow | Everything else; the normal permission mode applies. |

A plain `git push` to a feature branch is allowed. A command that cannot be
parsed is treated as `ask`, never as safe. Newer rules can be shipped in
log-only mode: they appear in the audit log but do not change the outcome.

**Downloaded files.** Files fetched by `curl -o`, `wget`, `Invoke-WebRequest
-OutFile`, and anything copied, moved or extracted from them, cannot be run
until reviewed. This holds across a fetch and a run in the same command.

**Your own constraints.** Say "do not push" (or "do not commit", "do not
delete", "no network", or the Turkish "pushlama") and the matching commands
are denied for the rest of the session. Say "you may push" to take it back.
A grant cannot override a constraint.

**Scoped grants.** An `ask` message ends with the exact grant that would allow
it, for example `command(git push origin main)`. Patterns match word by word,
and a trailing `*` matches the rest: `command(git push origin release/*)`.

The audit log (every finding, enforced or log-only) is
`<data directory>/policy-audit.jsonl`.

## Hooks: `if`

`matcher` selects tools by name; `if` narrows by what the call does:
`"if": "Bash(git push*)"`. See [hooks](hooks/README.md).

## Importing from other agents

```
atlas import claude --dry-run
atlas import claude
atlas plugin enable imported-claude
```

Sources: `claude`, `codex`, `cursor`, `gemini`, `agy`. Commands, skills,
agents and instruction files become one plugin, `imported-<source>`, which
starts disabled and is removed with `atlas plugin remove imported-<source>`.
Files that look like they contain a credential are skipped and listed. MCP
servers are reported by name only; add them yourself with `atlas mcp`.

## File-defined automations

One routine per file in `.atlas/automations/`:

```
---
schedule: CRON_TZ=Europe/Istanbul 0 9 * * 1-5
permissions: readonly
timeout: 600
---
Summarize what changed in the repository since yesterday.
```

`atlas routine sync` creates, updates and removes the matching routines
(named `auto-<file>`). New ones start disabled; read them, then
`atlas routine enable auto-<file>`. `CRON_TZ=` (or `TZ=`) pins the schedule
to a zone so 09:00 stays 09:00 across daylight-saving changes. A file cannot
request the `auto` permission policy.

## Plan approval modes

`plan_approval` in the config chooses how `exit_plan_mode` asks:

| Value | Behavior |
| --- | --- |
| `always` (default) | You are asked every time. |
| `auto` | A plan proceeds without a prompt only if it lists its files, deletes nothing, touches at most 8 files, and avoids build, CI, dependency and credential files (`go.mod`, `package.json`, `.github/`, `Dockerfile`, `.env`, ...). Anything else asks. |
| `turbo` | Never asks. The plan is still checked against the project, and a plan naming files that do not exist is still sent back. |

## Open issues

The `issues` tool keeps a ledger of problems the agent finds. An issue is
closed only with the command that was run and its output, and a turn cannot
finish while a major or critical issue is open: the completion gate sends it
back, and if it still cannot be closed the turn ends reporting it.

## Hooks: `OnToolError`

A hook configured under `OnToolError` fires only when a tool call fails (an
error result or an error return), with the error text as the tool response.
Its `context` is added to what the model sees and `halt` stops the turn, as
for `PostToolUse`.

## Request changes on a plan

When the plan prompt appears in the desktop app, **Request changes** opens a
comment box. Ctrl+Enter declines the plan and sends your comment to the agent,
which stays in plan mode, revises the plan and asks again.

## Hooks: `PreInvocation` and `PostInvocation`

`PreInvocation` runs before each model request of the main agent; a `deny`
decision stops that request, which is how a hook enforces a budget or a
freeze. `PostInvocation` runs after each model step and only observes. A hook
that fails to run never blocks the request. Delegated agents are not gated.

## Fresh re-check before finishing (`harness.victory_audit`)

With `victory_audit` on, a turn cannot finish on the agent's say-so: up to
three test, build, vet or lint commands it reported passing since its last
edit are run again, and a failure sends the turn back with the fresh output.
Off by default. Like the other gates it can be disabled by name in
`harness.disabled_gates` (`victory_audit`, `open_issues`,
`background_workers` -- see [orchestrator-mode.md](orchestrator-mode.md)), and `stats` lists
gates that ran at least 20 times without ever changing an outcome as
candidates to ablate.

## Project knowledge (`knowledge`)

With `"knowledge": true` the agent may propose short notes about the project
(`knowledge` tool, action `propose`, with the files they came from). Nothing
is used until you run `atlas knowledge approve <id>`. Approved notes appear as
a one-line index in every session and are marked `[STALE]` when a source file
changes. `atlas knowledge list|show|remove` manage them.

Each time a conversation is compacted, the summary is also proposed as a
`checkpoint-<time>` item, with the project files it names as sources, so a long
session's findings are not lost with its history. Nothing is proposed when the
summary is short, looks like it contains a credential, or ten proposals already
wait for review. These are proposals like any other: unused until approved.

## Hooks: `PostCompact`

`PostCompact` fires after a conversation was summarized. It only observes; use
it to record or forward what the session learned.

## Sandbox and monitor

With `"auto_sandbox": true` and the execution sandbox on, commands the policy
would ask about for risks the sandbox contains (`sudo`, writes to shell
startup files) run without a prompt. Risks that reach the mounted project
(`git reset --hard`, `chmod -R 777`) still ask. After every shell command the
monitor compares git hooks, CI workflows, `.env*`, `.npmrc` and shell startup
files before and after; a change is appended to the result so the agent and
you see it.

## MCP apps (`mcp_apps`)

A tool the server marks app-only (`_meta.ui.visibility` without `model`) is
never offered to the model, whether or not apps are on. With
`"mcp_apps": true`, a tool that declares a `ui://` page gets an **Open app**
button in its row in the desktop app. The page (HTML only, 512 KiB at most) is
shown in a frame that has scripts but no same-origin access, no network and no
navigation. It is served as its own document with its own policy
(`default-src 'none'`), so it cannot make a request of its own, and an app
page that tries to fetch anything is blocked.

A page can call tools through `window.atlas.callTool(name, args)`. The app
passes the call to the server, which runs it on the page's own MCP server and
only if the tool declares `"app"` in its `_meta.ui.visibility`. A tool meant
for the model (no visibility list, or `model` only) cannot be called by a page,
a page cannot reach another server's tools, and arguments are limited to
64 KiB.

## Sidecars and `agentapi`

A sidecar is a long-running helper in the project, for example a dashboard or
a watcher. Put it in `.atlas/sidecars/<id>/sidecar.json`:

```
{ "command": "node", "args": ["main.mjs"], "restart_policy": "on-failure" }
```

`command` is a program name or a path inside the folder; absolute paths and
`..` are refused. `restart_policy` is `always` (default), `on-failure` or
`never`, with exponential backoff between restarts.

A sidecar never starts by itself and the agent cannot start one. You enable it:

```
atlas sidecar list
atlas sidecar enable <id>
atlas sidecar run          # supervises every enabled sidecar until Ctrl+C
atlas sidecar logs <id>
```

Editing `sidecar.json` restarts it; disabling or deleting it stops it. Each
process runs in its own folder with `ATLAS_SIDECAR_DATA_DIR`,
`ATLAS_SIDECAR_EVENTS_DIR`, `ATLAS_SIDECAR_TOKEN`, `ATLAS_PROJECT_DIR` and
`ATLAS_AGENTAPI_EXE` set, and its output goes to timestamped logs.

A sidecar can drive the agent with `agentapi`:

```
$ATLAS_AGENTAPI_EXE agentapi new-conversation "Summarize today's failures"
$ATLAS_AGENTAPI_EXE agentapi send-message <conversation-id> "And the flaky ones?"
$ATLAS_AGENTAPI_EXE agentapi get-conversation-metadata <conversation-id>
```

Conversations run headlessly and read-only by default; `--permissions edits`
or `configured` widens that. The unrestricted `auto` policy is refused, so a
background process cannot hand itself every permission. Each call is recorded
as a JSON file in the events folder, which is where
`get-conversation-metadata` reads from.

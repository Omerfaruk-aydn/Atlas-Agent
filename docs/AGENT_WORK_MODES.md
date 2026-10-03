# Specialist work modes and context controls

Atlas provides persistent specialists, durable batches, a read-only architect
followed by a restricted editor, bounded agent hook actions, request inspection,
and usage profiles. These extend the existing `agent` tool and workflow controls;
ordinary permissions, session budgets, cancellation and runner limits still apply.

## Persistent specialists

Ask Atlas to reuse a specialist workstream, or call the agent tool with:

```json
{"agent_name":"backend","session_key":"api-design","prompt":"Inspect the API boundary and record the implementation decisions."}
```

Later calls with the same parent conversation, role and key resume the specialist's
SQLite conversation, including after restarting Atlas. Different keys isolate
workstreams. Changes to role definitions, model bindings, available tools or
ownership create a new child conversation. Calls sharing a conversation are
serialized; a busy conversation fails explicitly instead of corrupting history.
Only the new invocation's cost is propagated to its parent. Quality-only calls
always use fresh sessions. The specialist still refreshes project context and
must inspect current files rather than treating old messages as current evidence.

## Durable batches

```json
{
  "mode":"batch",
  "batch_id":"module-review",
  "agent_name":"review",
  "prompt":"Review {{item}} for error handling and report concrete findings.",
  "items":[
    {"id":"config","input":"internal/config"},
    {"id":"session","input":"internal/session"}
  ]
}
```

Each row transitions from `pending` to `running` before execution, then to
`succeeded` or `failed`. Rows execute serially to preserve write ordering and
use the same specialist/concurrency controls as normal agent calls. Limits are
128 rows per batch, 64 batches per parent conversation, 8 KiB per template/input
and 16 KiB of stored output per row. Success means a tool invocation returned
output; it does not replace independent verification of findings or changes.

Retry only failures using the stored definition:

```json
{"mode":"batch","batch_id":"module-review","retry_failed":true,"prompt":"Retry failed rows of the stored batch definition"}
```

Successful rows are skipped. Interrupted rows remain `running` and are never
automatically replayed; inspect effects before deliberately creating a new batch.
An explicitly resubmitted definition must match the original one. Changed roles,
model bindings, tools or scope require a new batch ID. Inspect rows with
`atlas-agent batches SESSION` or `/batches` in the TUI. In the Batches view, select
a row and press `f` to queue its batch's failed-only retry, then `c` to start the
queue. The retry uses the normal agent permission and budget flow.

## Architect/editor

```json
{
  "mode":"architect_edit",
  "architect":"architect",
  "editor":"backend",
  "owned_paths":["internal/session"],
  "prompt":"Add a bounded session lookup improvement following project conventions."
}
```

The architect gets read-only tools and cannot execute commands. Its plan is
passed to the editor, whose palette contains read tools and direct file edits;
commands, MCP and delegation are disabled in this editing stage. Hooks are
disabled for both stages. Ownership cannot expand the caller's boundary. The
project fingerprint is checked between stages; a changed tree requires a fresh
plan. A read-only editor role is rejected. Plans are bounded to 64 KiB.

Each role uses its own configured `model` binding, so the two stages can run on
different providers/models without changing the conversation's main model.
Run machine checks and independent review after the edit; the editor's response
does not certify completion. Shared `session_key` and isolated `workspace_id`
are intentionally rejected for this mode.

## Agent hook actions

In `atlasrc`:

```bash
hook add PostToolUse --name edit-review --matcher '^(edit|write|multiedit)$' \
  --agent review --prompt 'Inspect the changed file and return concrete observations.' \
  --timeout 45 --max-fires 3
```

An action has either `--command` or `--prompt` with a named `--agent`.
Agent actions run as child specialist sessions, receive the hook's event payload,
and contribute observations to the main conversation. They have read-only tools,
no commands, no nested hooks, a four-step cap and a timeout of at most 120 seconds
(default 30). Prompt size is bounded to 8 KiB, event/output to 16 KiB. The firing
cap defaults to three and is configurable from one to sixteen per hook/session
within the runner's process lifetime. Agent observations cannot approve tool
permissions, change tool input or override shell-hook denial decisions.

Supported events: `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`.
`SessionEnd` and `PreCompact` reject agent actions. Existing shell hooks retain
their behavior. `atlas-agent hooks list --json` includes both action types.

## Context inspector

Open `/context`, or open `/workflow` and press `9`. The inspector captures the
prepared model request at each step: message categories, tool result IDs, tool
schemas, optional pinned files, content fingerprints and approximate token costs.
It stores metadata instead of duplicating raw prompts or tool output. The estimate
uses serialized text length; provider framing and image token costs are not
measured. Very large requests retain at most 2048 metadata entries while the total
estimate covers the whole prepared request. This is an estimate, not billing data.

Press `f` to pin a literal relative project file, or `x` to unpin a selected file
or exclude/include a tool result. Mandatory system/project instructions and user
messages cannot be excluded. Exclusion retains the tool result identity and a
placeholder, preserving the call/result protocol. SQLite message history is never
deleted; reinclusion restores original historical content on the next fresh turn.
Pinned files refresh at each step and do not accumulate duplicate messages.

CLI equivalents:

```text
atlas-agent context inspect SESSION
atlas-agent context pin SESSION internal/session/session.go
atlas-agent context unpin SESSION internal/session/session.go
atlas-agent context exclude SESSION TOOL_CALL_ID
atlas-agent context include SESSION TOOL_CALL_ID
```

Pins are bounded to 16 contained text files, 64 KiB per file and 256 KiB total;
exclusions to 128 observed tool result IDs. Pins cannot escape the project through
relative paths or symlinks. Controls take effect at a subsequent model step.

## Usage profiles

```text
atlas-agent profiles list
atlas-agent profiles use implementation
atlas-agent --profile economical
atlas-agent run --profile review "Review the session lookup behavior"
```

The TUI exposes `/profiles`; switching requires an idle agent and rebuilds its
model/tool configuration. `profiles use` persists the workspace selection;
`--profile` selects an ephemeral profile for that invocation. `atlasrc` supports
`option usage-profile implementation`. The built-in profiles are:

| Profile | Behavior | Steps | Concurrent specialists |
| --- | --- | ---: | ---: |
| economical | Targeted everyday work and concise reporting | 24 | 2 |
| research | Read-only investigation, sources and unknowns | 48 | 4 |
| implementation | Coherent implementation and relevant checks | 96 | 4 |
| review | Independent read-only code inspection | 48 | 3 |
| ci | Bounded automation with structured results | 64 | 2 |

Research/review exclude commands, mutations and MCP. Other profiles retain the
existing tool policy; profiles never grant tools disabled by workspace settings.
Default profiles use the configured model. Custom `options.usage_profiles` entries
can add `model_role`, `allowed_tools`, `read_only`, `max_steps`, `max_agents` and
`instructions`. `model_role` must refer to a configured role; no model IDs are
invented or added by profile selection. Custom profiles replace the corresponding
built-in definition and must provide valid step/agent bounds.

Server workspace creation carries the explicit profile selection and preserves
it across reconnection. Attaching with a different explicit profile fails rather
than silently inheriting a running workspace's policy.

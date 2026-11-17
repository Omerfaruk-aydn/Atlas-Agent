# Hooks

The user-facing hook protocol lives in [docs/hooks/README.md](docs/hooks/README.md).
This file is the short version for people (and agents) working on the hook
engine itself.

## Events

| Event | Fires | Can block | Notes |
|---|---|---|---|
| `PreToolUse` | before a tool call | yes (deny, rewrite input) | matcher = tool name |
| `PostToolUse` | after a tool call | halts the turn only | payload carries the tool response |
| `UserPromptSubmit` | before a prompt reaches the model | yes (refuse) | context is appended |
| `SessionStart` | once per session per process | no | context is prepended |
| `SessionEnd` | when a session is deleted | no | cleanup and archival |
| `PreCompact` | before auto-summarization | yes (skips it) | |
| `Stop` | top-level turn ended on its own, gates passed | adds work only | deny continues with the reason, max 3 per request |
| `SubagentStop` | a delegated agent's turn ended | adds work only | same rules, for sub-agents |

The constants and their semantics are documented where they are declared, in
[internal/hooks/hooks.go](internal/hooks/hooks.go). Config event names are
normalized case-insensitively and with underscores
(`internal/config/load.go`, `normalizeHookEvent`); `atlas hooks run <event>`
exercises a configured hook without a session.

## Rules the engine keeps

- Hooks run before permission checks; their results compose in config order.
- A hook that fails to run fails open, unless it is marked `required`.
- A Stop hook cannot weaken a completion gate: it runs after the gates and only
  adds work (see `applyStopHooks` in `internal/agent/session_hooks.go`).

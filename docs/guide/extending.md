# Skills, MCP and hooks

Extending Atlas Agent with skills, MCP servers, hooks and context files.

[Back to the README](../../README.md) · [All guides](README.md)

## Extensibility: skills, MCP, hooks

### Skills

A skill is a folder with a `SKILL.md`: a short, portable procedure the agent loads when a task
matches its description. Deploy steps, a review checklist, a repo-specific workflow — anything you
would otherwise re-explain every session.

```bash
atlas-agent skill new deploy-steps
atlas-agent skill validate
atlas-agent skill list
```

Skills live in `.atlas/skills/` (project) or your user config directory (global). The agent can also
write one itself with `skill_manage` when you say "remember how we do this".

### MCP

Any Model Context Protocol server can be attached; its tools and resources appear alongside the
built-ins.

```bash
atlas-agent mcp list      # what is configured
atlas-agent mcp test      # does it actually connect
```

Both stdio and HTTP transports are supported, including OAuth-protected servers.

### Hooks

Run your own commands at five points in the agent's lifecycle. A hook can allow, deny, or annotate.

| Event | Fires | Typical use |
| --- | --- | --- |
| `PreToolUse` | Before a tool call runs | Block a command, require a condition, log intent |
| `PostToolUse` | After a tool call returns | Format on write, run a linter, notify |
| `UserPromptSubmit` | When you send a prompt | Redact secrets, add context, refuse a prompt |
| `SessionStart` | At session start | Inject environment context |
| `SessionEnd` | At session end | Archive, notify, clean up |

Hook commands receive the event's data both as a JSON payload on stdin and as environment
variables:

| Variable | Contains |
| --- | --- |
| `ATLAS_AGENT_EVENT` | The event name (`PreToolUse`, `PostToolUse`, …) |
| `ATLAS_AGENT_TOOL_NAME` | The tool being called, for tool events |
| `ATLAS_AGENT_PROMPT` | The prompt text, for `UserPromptSubmit` |
| `ATLAS_AGENT_SESSION_ID` | The session the event belongs to |
| `ATLAS_AGENT_CWD` | The working directory |
| `ATLAS_AGENT_PROJECT_DIR` | The project root |
| `ATLAS_AGENT_TOOL_INPUT_COMMAND` | The command, when the tool is `bash` |
| `ATLAS_AGENT_TOOL_INPUT_FILE_PATH` | The file path, when the tool takes one |

```jsonc
{
  "hooks": {
    "UserPromptSubmit": [
      {
        "name": "no-secrets",
        "command": "case \"$ATLAS_AGENT_PROMPT\" in *BEGIN\\ RSA*) echo '{\"decision\":\"deny\",\"reason\":\"private key in prompt\"}' ;; esac"
      }
    ],
    "PostToolUse": [
      { "name": "gofmt", "command": "gofmt -w \"$ATLAS_AGENT_TOOL_INPUT_FILE_PATH\"" }
    ]
  }
}
```

`atlas-agent hooks run <event>` fires one manually so you can test it without a session.

### Context files

`AGENTS.md` (or `ATLAS-AGENT.md`) in the project root is loaded into every session — conventions,
architecture, gotchas, anything the agent should know before it starts. `/init` writes a first draft
of one by reading the project. Global context files live in your user config directory and apply
everywhere.

Atlas Agent also reads memory the agent itself records over time (`memory` tool), bounded by
`options.memory`.

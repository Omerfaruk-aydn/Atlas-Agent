# Configuration

Where config lives, the options people change most, and a worked example.

[Back to the README](../../README.md) · [All guides](README.md)

## Configuration reference

Configuration is JSON, layered global → workspace, and both layers can be edited by hand or changed
from the chat with `atlas_config`. `atlas-agent schema` prints the full JSON schema;
`atlas-agent config show` prints what is currently in effect.

Config directories:

| What | Windows | macOS / Linux |
| --- | --- | --- |
| Config you edit by hand | `%USERPROFILE%\.config\atlas\atlas.json` | `~/.config/atlas/atlas.json` |
| Config Atlas writes (`atlas_config`, `login`, `provider add`) | `%LOCALAPPDATA%\atlas\atlas.json` | `~/.local/share/atlas/atlas.json` |

Both are loaded and merged; `XDG_CONFIG_HOME` moves the first. `atlas-agent config paths` shows
which files were found.

Workspace config lives in `.atlas/` in the project root.

### Commonly used options

| Option | Type | What it does |
| --- | --- | --- |
| `options.context_paths` | `[]string` | Files loaded as project context. |
| `options.global_context_paths` | `[]string` | Context files applied to every project. |
| `options.skills_paths` | `[]string` | Directories holding `SKILL.md` folders. |
| `options.subagents_paths` | `[]string` | Directories holding `name.md` subagent definitions. |
| `options.data_directory` | `string` | Where sessions and state are stored (default `.atlas`). |
| `options.disabled_tools` | `[]string` | Built-in tools to hide from the agent. |
| `options.disabled_skills` | `[]string` | Skills to hide. |
| `options.session_mode` | `string` | Mode whose instructions fold into the main session prompt. |
| `options.agent_models` | `map` | Which model type (large/small) an agent id uses. |
| `options.model_roles` | `map` | Named roles → provider/model pairs. |
| `options.router` | `object` | Automatic model router (off by default): cheap steps on a fast role, hard ones on the strong model. See [docs/model-router.md](../model-router.md). |
| `options.model_fallbacks` | `map` | Ordered failover targets per model type on 429s. |
| `options.fallback_cooldown` | `int` | Seconds a fallback stays active after failover. |
| `options.advisor` | `object` | A second model reviewing each turn; needs an `advisor` role. |
| `options.auto_summarize_at` | `float` | Context fraction (0–1) at which to compact. |
| `options.disable_auto_summarize` | `bool` | Turn compaction off entirely. |
| `options.memory` | `object` | Bounds on prose carried between sessions. |
| `options.max_session_cost` | `float` | USD ceiling per session. |
| `options.max_steps_per_turn` | `int` | Model/tool steps allowed in one turn. |
| `options.max_concurrent_sub_agents` | `int` | Parallel subagent cap. |
| `options.tool_timeout` | `int` | Seconds before a tool call is cut off. |
| `options.max_provider_retries` | `int` | Retries on a failed provider request. |
| `options.allowed_commands` | `[]string` | Lift commands out of the bash block list. |
| `options.blocked_commands` | `[]string` | Extra commands bash may never run. |
| `options.allowed_domains` | `[]string` | The only domains fetch/download may reach. |
| `options.blocked_domains` | `[]string` | Domains they may never reach. |
| `options.restrict_writes_to_working_dir` | `bool` | Refuse writes outside the working directory. |
| `options.max_download_bytes` | `int` | Download size ceiling. |
| `options.sandbox` | `object` | OS-level containment for shell processes (Windows today). |
| `options.auto_lsp` | `bool` | Start LSP servers from root markers (default true). |
| `options.attribution` | `object` | Trailer style and "generated with" line for commits. |
| `options.notifications` | `string` | `auto`, `native`, `osc`, `bell`, or `disabled`. |
| `options.progress` | `bool` | Indeterminate progress during long operations. |
| `options.tui` | `object` | Terminal UI options (theme, transparency, diff mode, exit banner). |
| `options.debug` / `options.debug_lsp` | `bool` | Verbose logging. |
| `options.disable_metrics` | `bool` | Turn metrics off. |
| `options.disable_provider_auto_update` | `bool` | Stop refreshing the provider catalog. |
| `options.disable_default_providers` | `bool` | Ignore the embedded catalog; declare everything yourself. |

### A worked example

```jsonc
{
  "models": {
    "large": { "provider": "anthropic", "model": "claude-sonnet-5" },
    "small": { "provider": "minimax", "model": "MiniMax-M2.7-highspeed" }
  },
  "options": {
    "model_roles": {
      "compact":  { "provider": "minimax",   "model": "MiniMax-M2.7-highspeed" },
      "research": { "provider": "openai",    "model": "gpt-5.6-sol" },
      "review":   { "provider": "anthropic", "model": "claude-opus-5" },
      "goal":     { "provider": "anthropic", "model": "claude-sonnet-5" }
    },
    "auto_summarize_at": 0.75,
    "max_session_cost": 5.0,
    "max_steps_per_turn": 60,
    "max_concurrent_sub_agents": 4,
    "restrict_writes_to_working_dir": true,
    "blocked_domains": ["pastebin.com"],
    "disabled_tools": ["sourcegraph"]
  }
}
```

# CLI reference

Every `atlas-agent` subcommand and every in-session command.

[Back to the README](../../README.md) · [All guides](README.md)

## CLI reference

Everything below is a subcommand of `atlas-agent`.

### Running

| Command | What it does |
| --- | --- |
| `atlas-agent` | Interactive TUI session in the current directory. |
| `atlas-agent run "<prompt>"` | Non-interactive: run one prompt to completion and print the result. |
| `atlas-agent run --help` | Flags for non-interactive mode (output format, model, quiet, `--permissions`, `--goal`). |
| `atlas-agent routine add\|list\|remove\|run\|logs\|daemon` | Scheduled routines: prompts that run unattended on a cron schedule. See [docs/routines.md](../routines.md). |
| `atlas-agent server` | Run the backend as a server, for client/server or remote use. |
| `atlas-agent --version` | Print version and commit. |
| `atlas-agent doctor` | Diagnose config, providers, LSP servers, and MCP servers. |
| `atlas-agent security audit [path]` | Defensive audit: findings are confirmed only by a failing proof test in your project (`--fix`, `--no-agent`, `--json`, `--fail-on`). See docs/security-audit.md. |
| `atlas-agent update` | Update the binary in place to the latest release. |

### Models and providers

| Command | What it does |
| --- | --- |
| `atlas-agent models` | List every model available from configured providers. |
| `atlas-agent models roles` | Show which model each named role resolves to. |
| `atlas-agent provider list` | List configured providers. |
| `atlas-agent provider test` | Check that a provider actually answers. |
| `atlas-agent provider add <id>` | Add a custom provider: probe it, list its models, save it (key never in chat). See [docs/provider-setup.md](../provider-setup.md). |
| `atlas-agent provider usage [name]` | Token and cost usage per provider. |
| `atlas-agent update-providers [path-or-url]` | Refresh the embedded provider/model catalog. |
| `atlas-agent login [platform]` | Sign in to a coding plan (Copilot, ChatGPT, Antigravity, …). |
| `atlas-agent logout [platform]` | Sign out and forget stored credentials. |

Providers are *added* by editing the config file, by asking the agent (`atlas_config`), or in an
`atlasrc` shell config (`provider add <id> --api-key ... --base-url ...`) — there is no
`provider add` CLI subcommand.

### Sessions

| Command | What it does |
| --- | --- |
| `atlas-agent session list` | List sessions in this workspace. |
| `atlas-agent session show <id>` | Print a session's messages. |
| `atlas-agent session last` | Show the most recent session. |
| `atlas-agent session search <query>` | Full-text search across past sessions. |
| `atlas-agent session tree [id]` | Show a session and its child (subagent) sessions. |
| `atlas-agent session rename <id> <title>` | Rename a session. |
| `atlas-agent session tag <id> [tags...]` | Tag a session. |
| `atlas-agent session compact <id>` | Summarize a session to free context. |
| `atlas-agent session rewind <id> <message-id>` | Fork a session at a message, restoring files to before it. |
| `atlas-agent session diff <id1> <id2>` | Compare two sessions. |
| `atlas-agent session timeline <id>` | Numbered step timeline of a session. |
| `atlas-agent session branch <id> --at <step>` | Fork a session from any step, optionally with another model or prompt (see [docs/session-replay.md](../session-replay.md)). |
| `atlas-agent session compare <a> <b>` | Compare messages, tools, files, cost and outcome of two sessions. |
| `atlas-agent session export <id>` | Export a session (for sharing or archiving). |
| `atlas-agent session models <id>` | Which models a session used. |
| `atlas-agent session tools <id>` | Which tools a session used. |
| `atlas-agent session delete <id>` | Delete a session. |
| `atlas-agent session prune` | Delete old sessions in bulk. |
| `atlas-agent stats` | Token, cost, and activity statistics. |

### Subagents, skills, and extensions

| Command | What it does |
| --- | --- |
| `atlas-agent agent list` | List subagents: the ten built-in modes plus anything you authored. |
| `atlas-agent agent show <name>` | Print a subagent's definition. |
| `atlas-agent agent new <name>` | Create a subagent definition file. |
| `atlas-agent agent remove <name>` | Delete a subagent definition. |
| `atlas-agent skill list` | List available skills. |
| `atlas-agent skill show <name>` | Print a skill. |
| `atlas-agent skill new <name>` | Scaffold a new `SKILL.md`. |
| `atlas-agent skill validate` | Validate skill frontmatter and structure. |
| `atlas-agent skill remove <name>` | Delete a skill. |
| `atlas-agent mcp list` | List configured MCP servers. |
| `atlas-agent mcp test` | Check that an MCP server connects and responds. |
| `atlas-agent lsp list` | Show LSP servers and their state. |
| `atlas-agent hooks list` | Show configured hooks. |
| `atlas-agent hooks run <event>` | Fire a hook manually, for testing. |

MCP servers, like providers, are declared in the config file, through the agent (`atlas_config`), or
in an `atlasrc` shell config (`mcp add ...`) rather than through a CLI subcommand.

### Project and workspace

| Command | What it does |
| --- | --- |
| `atlas-agent projects` | List known projects. |
| `atlas-agent worktree` | Manage git worktree-backed sessions. |
| `atlas-agent review [target] [--effort low\|medium\|high\|max] [--fix] [--comment] [--json]` | Verified multi-pass code review of local changes, a range, `--commit`/`--base`, or a PR. See [docs/review.md](../review.md). |
| `atlas-agent rehearse "<task>" --run "<cmd>" --check "<cmd>"` | Dry-run a risky change in an isolated copy and preview files, broken checks and API changes; `rehearse apply <id>` writes it to the real tree. See [docs/rehearsal.md](../rehearsal.md). |
| `atlas-agent commands list\|import\|export\|validate\|run` | Share and admit validated project workflow recipes. |
| `atlas-agent watch-comments --file PATH` | Queue lexer-detected `ATLAS:` comments; dispatch is opt-in. |
| `atlas-agent schedule add\|list\|pause\|resume\|recover\|start\|stop` | Manage persistent, explicitly enabled maintenance commands. |
| `atlas-agent project model build\|show\|impact [files]` | Build the living project model (modules, tests, owners, decisions); see what a change reaches. |
| `atlas-agent memory show [project\|user]` | Show remembered project/user context. |
| `atlas-agent memory search <query>` | Search what has been remembered. |
| `atlas-agent memory clear <project\|user>` | Forget it. |
| `atlas-agent config show` | Show effective configuration. |
| `atlas-agent config paths` | Show which config files were loaded, in order. |
| `atlas-agent schema` | Print the JSON schema for the config file. |
| `atlas-agent dirs` | Show config, data, and cache directories. |
| `atlas-agent logs` | Tail the agent's own log file. |
| `atlas-agent corners` | Preview which box-corner styles your terminal font supports. |

See [engineering tools and workflows](../engineering-tools.md) for guarded
experiment tools, deferred schemas, session timeline and server authentication.

For persistent specialist sessions, durable batches, architect/editor work,
agent hook actions, request context inspection and usage profiles, see
[Specialist work modes and context controls](../AGENT_WORK_MODES.md).

[Browser and desktop automation, interaction traces and authentication handoffs](../INTERACTION_AUTOMATION.md).

## In-session commands

Inside the TUI, `/` opens the command list. The ones worth knowing:

| Command | What it does |
| --- | --- |
| `/goal <objective>` | Start an autonomous run toward an objective. Sends immediately, like a normal prompt. |
| `/goal` | Report what the session is currently working toward. |
| `/goal clear` | End the autonomous run. |
| `/btw <question>` | Ask a side question answered by a read-only agent in a dismissable panel, never added to the conversation; works while the agent is busy. See [docs/btw.md](../btw.md). |
| `/model` | Switch the session's model. |
| `/roles` | Assign models to named roles. |
| `/summarize` | Compact the conversation now instead of waiting for the threshold. |
| `/sessions` | Switch between sessions. |
| `/new` | Start a fresh session. |
| `/mode` | Switch permission mode (manual, auto-accept edits, plan, bypass). |
| `/init` | Write an `AGENTS.md` describing this project for future sessions. |
| `/help` | Everything else. |

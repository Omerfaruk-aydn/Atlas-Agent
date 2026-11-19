# Architecture and development

How the code is organised, and how to build and test it.

[Back to the README](../../README.md) · [All guides](README.md)

## Architecture

```
main.go                     CLI entry point (cobra, via internal/cmd)
internal/
  app/                      Top-level wiring: DB, config, agents, LSP, MCP, events
  cmd/                      CLI commands (run, login, models, session, agent, skill, mcp, hooks, worktree)
  agent/                    The agent loop, tool assembly, subagents, goal runs, debate/orchestrate/delegate
    tools/                  Built-in tool implementations
    templates/              System prompts and tool descriptions (embedded)
  config/                   Config loading, layering, provider catalog, model roles
  session/                  Sessions, messages, goals, persistence
  db/                       sqlc-generated SQLite access and goose migrations
  ui/                       Terminal UI (chat, dialogs, sidebar, completions)
  lsp/                      LSP client management and diagnostics
  hooks/                    Hook execution and payloads
  skills/                   Skill discovery and loading
  subagents/                Subagent definitions: parse, discover, author
  permission/               The permission layer and modes
  browser/                  Chrome/CDP automation, including real-profile snapshots
  computer/                 Windows desktop control: screenshots plus mouse and keyboard input
  deps/                     Vendored dependencies (LLM client, TUI toolkit, styling)
```

The agent loop and the UI are separate: the same coordinator serves the TUI, `run` mode, and the
HTTP server, so a session behaves identically wherever it is driven from.

## Development

```bash
git clone https://github.com/Omerfaruk-aydn/Atlas-Agent.git
cd Atlas-Agent

go build ./...            # build everything
go test ./internal/...    # run the suite
go vet ./...              # vet
go run . --help           # run without installing
```

Notes for contributors:

- Database access is **sqlc-generated** from `internal/db/sql/`; migrations are goose files in
  `internal/db/migrations/`. Edit the SQL, regenerate — do not hand-edit generated Go.
- Prompts and tool descriptions are Markdown files under `internal/agent/templates/` and
  `internal/agent/tools/`, embedded with `go:embed`. Changing agent behavior is often a prompt edit,
  not a code edit.
- Tests avoid reading the machine they run on: package-level `TestMain`s isolate the global config,
  data directory, and subagent discovery directory. Keep it that way — a test that passes only on
  your machine is worse than no test.
- [AGENTS.md](../../AGENTS.md) is the deeper architecture guide, and is also what the agent itself reads
  when working on this repository.

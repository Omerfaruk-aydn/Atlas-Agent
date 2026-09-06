# Atlas Agent

**A terminal-first AI coding agent that isn't locked to one model.**
Plug in Claude, GPT, Gemini, a local model, or a flat-rate coding plan
you already pay for — same agent, same workflow, your choice of brain.
Delegate work to subagents instead of blowing up your context, argue
hard calls out between several models before committing to one, hand
it a goal and let it keep working toward it on its own, and change how
it's configured just by telling it to.

[![Release](https://img.shields.io/github/v/release/Omerfaruk-aydn/Atlas-Agent?label=release)](https://github.com/Omerfaruk-aydn/Atlas-Agent/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE.md)
[![npm](https://img.shields.io/npm/v/%40atlas-coder%2Fatlas-agent?label=npm)](https://www.npmjs.com/package/@atlas-coder/atlas-agent)
[![Go Report Card](https://goreportcard.com/badge/github.com/Omerfaruk-aydn/Atlas-Agent)](https://goreportcard.com/report/github.com/Omerfaruk-aydn/Atlas-Agent)
[![Stars](https://img.shields.io/github/stars/Omerfaruk-aydn/Atlas-Agent?style=social)](https://github.com/Omerfaruk-aydn/Atlas-Agent/stargazers)

```bash
curl -fsSL https://github.com/Omerfaruk-aydn/Atlas-Agent/releases/latest/download/install.sh | bash
atlas-agent
```

<!--
  Demo goes here: a short terminal recording (asciinema or a GIF made
  with vhs/terminalizer) showing a real task end to end -- e.g. "fix
  the failing test", the agent reading the code, running the test
  suite, and the diff landing. Record one, upload it (drag-and-drop
  into a GitHub PR/issue comment to get a CDN URL, or commit it under
  docs/), then replace this comment with:
    ![demo](docs/demo.gif)
  A first-run screen recording under ~15s that shows one real edit
  lands better than a long feature tour -- keep it tight.
-->

**80+ built-in tools · 10 built-in subagent modes · 14+ coding-plan and
provider integrations · MCP + Skills + hooks for the rest.**

> [!NOTE]
> Atlas Agent went open source (MIT) recently. Issues and PRs are
> welcome — see [Contributing](#contributing) below. If something in
> here is out of date, that's a good first PR.

## Table of contents

- [Why Atlas Agent](#why-atlas-agent)
- [Install](#install)
- [Usage](#usage)
- [Delegation: agent, orchestrate, delegate, debate](#delegation-agent-orchestrate-delegate-debate)
- [Autonomous goals](#autonomous-goals)
- [Talk to it to configure it](#talk-to-it-to-configure-it)
- [Subagents](#subagents)
- [Tools](#tools)
- [Extensibility: skills, MCP, hooks](#extensibility-skills-mcp-hooks)
- [Supported coding plans](#supported-coding-plans)
- [Configuration](#configuration)
- [Contributing](#contributing)
- [License](#license)

## Why Atlas Agent

**01 · Bring your own model.** Anthropic, OpenAI, Google, MiniMax, xAI,
DeepSeek, Z.ai, Moonshot/Kimi, Zhipu, OpenCode, and more, or a
flat-rate coding-plan subscription you already pay for (GitHub
Copilot, ChatGPT, Google Antigravity — see
[Supported coding plans](#supported-coding-plans)). Switch per project
or per role without switching tools or re-learning a workflow.

**02 · Delegate instead of doing it all in one context.** A subagent
runs in a session of its own — everything it reads and greps stays
there, only its answer comes back. Hand off one self-contained task
(`agent`), fan the same question out to several subagents at once
(`orchestrate`), split a task into independent pieces that run in
parallel (`delegate`), or put several subagents in a multi-round
argument over a call that's genuinely hard to make (`debate`). See
[Delegation](#delegation-agent-orchestrate-delegate-debate).

**03 · Autonomous goals.** `/goal <what to reach>` sends immediately
like an ordinary prompt, then keeps the agent taking its own turns
toward that objective — checked by a separate judge model before it
calls itself done, not just its own say-so — until it's reached or its
turn budget runs out. The goal stays visible in the sidebar and header
the whole time, not just in a toast that disappears. See
[Autonomous goals](#autonomous-goals).

**04 · Configure it by talking to it.** "Use the cheap model for
summaries", "put Sonnet on research", "turn the browser tool on",
"make me a subagent that only reviews UI/UX" — say it in the chat;
`atlas_config` makes the change and reports what it did, instead of
routing you to a settings dialog you have to go find yourself. See
[Talk to it to configure it](#talk-to-it-to-configure-it).

**05 · Deep code intelligence, not just file edits.** LSP-backed
references/rename/call-hierarchy, dead-code and dependency-graph
analysis, security and secret scanning, test/coverage/lint runners,
Docker/Kubernetes/Terraform linting, and a full git/PR toolkit
(conventional commits, conflict resolution, PR descriptions, commit
splitting) — 80+ tools total. See [Tools](#tools).

**06 · Extend it your way.** MCP servers, a skills system (portable
`SKILL.md` procedures), hooks (`PreToolUse`/`PostToolUse`/
`UserPromptSubmit`), git worktree sessions, and a browser tool that
can drive your real, already-signed-in Chrome profile instead of a
blank automation window. See
[Extensibility](#extensibility-skills-mcp-hooks).

**07 · Cross-platform, single binary.** Windows, macOS (Intel + Apple
Silicon), and Linux. No runtime to install beyond the binary itself.

## Install

Pick your platform, then run the install command in a PowerShell (admin) or bash terminal.

### Windows (x64)
```powershell
irm https://github.com/Omerfaruk-aydn/Atlas-Agent/releases/latest/download/install.ps1 | iex
```

### macOS / Linux
```bash
curl -fsSL https://github.com/Omerfaruk-aydn/Atlas-Agent/releases/latest/download/install.sh | bash
```

The installer downloads the matching binary for your OS/arch into `~/.atlas-agent/bin/` and adds it to your PATH. After install, just run:

```bash
atlas-agent
```

### npm

```bash
npm install -g @atlas-coder/atlas-agent
atlas-agent
```

## Manual install (any platform)

Download the binary for your platform from [Releases](https://github.com/Omerfaruk-aydn/Atlas-Agent/releases/latest):

| OS      | Architecture          | File                                      |
| ------- | --------------------- | ----------------------------------------- |
| Windows | x64                   | `atlas-agent-windows-x64.exe`             |
| macOS   | Intel                 | `atlas-agent-darwin-x64`                  |
| macOS   | Apple Silicon         | `atlas-agent-darwin-arm64`                |
| Linux   | x64                   | `atlas-agent-linux-x64`                   |

Rename the file to `atlas-agent` (or `atlas-agent.exe` on Windows), place it somewhere on your `PATH`, and make it executable (`chmod +x atlas-agent`).

## Usage

```bash
atlas-agent                  # interactive mode
atlas-agent run "your task"  # non-interactive single prompt
atlas-agent --help           # full CLI help
atlas-agent models           # list available models
atlas-agent agent list       # list configured/built-in subagents
atlas-agent dirs             # show config directories
atlas-agent login copilot    # sign in to a coding plan
```

A few things worth trying once you're in a session:

```text
> fix the failing test in internal/shell and explain what broke it

> /goal get every package under internal/agent passing `go vet` and `go test`

> debate: should the retry logic live in the client or the transport?
  use the backend and review subagents

> use the cheap model for titles and summaries, and put Sonnet on research

> make me a subagent that only reviews UI/UX, nothing else
```

## Delegation: agent, orchestrate, delegate, debate

A subagent runs in a session of its own — its own context, its own
tool calls, its own reading of the codebase. Only its final answer
comes back to the conversation that asked for it. That's the whole
point: your main session's context stays small and cheap, and the
subagent can go read twenty files without any of them ending up in
your history.

| Tool | What it does | Reach for it when |
| --- | --- | --- |
| `agent` | Runs one task on one subagent (named, or auto-picked by keyword match). | The work is self-contained and you just want the answer, not the research trail. |
| `orchestrate` | Runs the *same* prompt on several subagents in parallel, side by side. | One answer isn't worth taking on faith and you want independent corroboration. |
| `delegate` | Runs several *different* self-contained subtasks in parallel. | A task splits cleanly into pieces that don't need to see each other's output. |
| `debate` | Puts a question to several subagents over multiple rounds — each sees and answers what the others said, optionally judged by a further subagent. | The first pass came back split, or a decision is expensive to get wrong and cheap to think about longer. |
| `vibe` | Starts a persistent background worker toward a goal across its own turns, steerable with new guidance while it runs. | You want ongoing work happening independent of the current conversation's turn. |

## Autonomous goals

```text
> /goal every endpoint in internal/server returns a typed error, not a bare string
```

This sends immediately, the same way an ordinary prompt does. From
there the session keeps taking its own turns toward the objective —
sizing and ending itself with the `goal` tool, checked by a judge
model (the `goal` role if you've configured one, otherwise the
advisor's) rather than trusting its own claim that it's done. The goal
stays visible in the sidebar's **Goal** section and the header, not
just a status-bar toast that disappears after a few seconds. Clear it
with `/goal clear`.

## Talk to it to configure it

`atlas_config` changes ATLAS-AGENT's own configuration from inside the
conversation — providers, per-role models, tool enablement, and
subagents — instead of sending you off to find a dialog:

```text
> use claude-opus for the "review" subagent role, globally

> disable the browser tool for this project

> make me a "frontend-critic" subagent that only reviews UI/UX,
  nothing else
```

It reports back what changed and where (role, provider/model, scope)
so the result is never silent.

## Subagents

Ten modes ship built in — `backend`, `debug`, `docs`, `frontend`,
`planner`, `refactor`, `research`, `review`, `security`, `test` — each
a specialist system prompt runnable standalone via `agent`/
`orchestrate`/`delegate`/`debate`, or folded into the main session when
you switch to that mode directly. Author your own as a `name.md` file
with YAML frontmatter (`name`, `description`, optional `model` role)
under `.atlas/agents/` (project) or your user config directory
(global) — or just ask `atlas_config` to write one for you. List
what's configured with `atlas agent list` or `atlas_config`'s
`list_subagents` action.

## Tools

80+ built-in tools, grouped by what they're for:

- **Editing & navigation** — `write`, `multiedit`, `view`, `glob`, `ls`, `inspect_file`
- **Code intelligence (LSP)** — `lsp_diagnostics`, `lsp_references`, `lsp_definition`, `lsp_call_hierarchy`, `lsp_rename`, `lsp_rename_file`, `lsp_replace_symbol`, `lsp_symbols`, `lsp_restart`
- **Static analysis** — `dead_code`, `type_hierarchy`, `import_graph`, `impact_analysis`, `code_metrics`, `todo_scan`, `api_surface`, `anti_pattern_scan`, `security_scan`, `semantic_code_search`, `doc_index`, `scan_secrets`, `env_var_audit`
- **Testing & quality** — `test_run`, `coverage_report`, `lint_run`, `dep_audit`, `generate_tests`, `generate_docstring`
- **Git & pull requests** — `git_status`, `git_log`, `git_blame`, `git_diff`, `git_branches`, `changelog_gen`, `pr_describe`, `pre_commit_guard`, `git_conventional_commit`, `git_conflict_resolver`, `git_commit_split`, `github_pr_view`, `audit_trail`
- **Infra & ops** — `docker_build_explain`, `k8s_manifest_lint`, `terraform_lint`, `ci_cd_pipeline_debugger`, `cloud_resource_costs`, `log_tail`
- **Delegation & collaboration** — `agent`, `orchestrate`, `delegate`, `debate`, `vibe`, `goal`, `team_send`, `team_read`
- **System & self-configuration** — `bash`, `atlas_config`, `atlas_info`, `atlas_logs`, `job_output`, `job_kill`, `exit_plan_mode`
- **Knowledge & session** — `memory`, `question`, `session_search`, `sourcegraph`, `todos`, `usage`, `facts`, `skill_manage`
- **Browser & network** — `browser`, `debugger`, `fetch`, `agentic_fetch`
- **MCP** — `list_mcp_resources`, `read_mcp_resource`

Run `atlas-agent --help` or ask the agent itself ("what tools do you
have?") for the exact, currently-enabled list — it varies with which
tools you've disabled via `atlas_config`.

## Extensibility: skills, MCP, hooks

- **Skills** — portable `SKILL.md` procedures the agent loads when a
  task matches, the same shape whether they ship with Atlas Agent or
  you author your own under `.atlas/skills/`. Manage them with
  `atlas skill new` / `atlas skill validate` / `atlas skill remove` or
  the `skill_manage` tool.
- **MCP** — connect any Model Context Protocol server; its tools and
  resources show up alongside the built-ins. `atlas mcp` manages the
  list.
- **Hooks** — run your own commands on `PreToolUse`, `PostToolUse`, or
  `UserPromptSubmit`, with the tool name/input or the prompt text
  available as environment variables, to allow, deny, or annotate
  what the agent is about to do. `atlas hooks` manages them.
- **Git worktrees** — run a session in its own worktree so a
  long-running task doesn't collide with the branch you're actively
  editing (`atlas worktree`).
- **Session rewind** — step a session's history back to an earlier
  point and continue from there (`atlas session rewind`).
- **Real browser profile** — the `browser` tool can drive your actual,
  already-signed-in Chrome profile (a snapshot of cookies/local
  storage, not your live profile) instead of a blank automation
  window with no logins.

## Supported coding plans

Atlas Agent plugs into both pay-per-token API providers and flat-rate
subscription "coding plans". The subscription integrations fall into
two categories:

- **Live** — OAuth/login + model call layer both work end-to-end.
- **Scaffold** — OAuth/login + model picker are wired up, but the
  model call layer is a stub that returns "not implemented" until the
  real request envelope is captured against the official client (see
  the package docs in `internal/oauth/<plan>`).

| Plan                          | Login command             | Status   | Notes                                                |
| ----------------------------- | ------------------------- | -------- | ---------------------------------------------------- |
| GitHub Copilot (Pro/Pro+/Biz) | `atlas login copilot`     | Live     | Device flow, headers set up                          |
| ChatGPT (Plus/Pro/Business)   | `atlas login chatgpt`     | Live     | PKCE OAuth, Codex backend                           |
| Google Antigravity (AI Pro/Ultra) | `atlas login antigravity` | Live  | PKCE OAuth, Cloud Code, Gemini-family only          |
| Claude (Pro/Max/Team)         | `atlas login claude`      | Scaffold | OAuth client id / envelope are TODOs                 |
| xAI SuperGrok (Heavy)         | `atlas login grok`        | Scaffold | OAuth client id / envelope are TODOs                 |
| Windsurf (Codeium Pro/Teams)  | `atlas login windsurf`    | Scaffold | OAuth client id / envelope are TODOs                 |
| JetBrains AI (Pro/Ultimate)   | `atlas login jetbrains`   | Scaffold | JB-ACCESS-TOKEN exchange / envelope are TODOs        |

API-key providers that act like coding plans (no OAuth, just set the
matching environment variable or `atlas provider add <name>`):

| Provider              | Env var                    | Notes                                       |
| --------------------- | -------------------------- | -------------------------------------------- |
| xAI (Grok API)        | `XAI_API_KEY`              | SuperGrok API key path                      |
| DeepSeek              | `DEEPSEEK_API_KEY`         | DeepSeek-V3/V4 family                       |
| Kimi Coding (Moonshot)| `KIMI_CODING_API_KEY`      | Kimi K3 / Kimi for Coding                   |
| Z.ai                  | `ZAI_API_KEY`              | GLM-4 family                                |
| Zhipu Coding          | `ZHIPU_CODING_API_KEY`     | Zhipu coding plan                           |
| MiniMax Coding        | `MINIMAX_CODING_API_KEY`   | MiniMax-M2.7 / M3 coding plan               |
| Moonshot              | `MOONSHOT_API_KEY`         | Moonshot family                             |
| OpenCode Zen          | `OPENCODE_ZEN_API_KEY`     | OpenCode Zen coding plan                    |
| OpenCode Go           | `OPENCODE_GO_API_KEY`      | OpenCode Go coding plan                     |

Scaffolded plans share a common risk profile: the provider's terms of
service restrict the service to first-party clients, and using a
third-party login can be revoked. Atlas Agent ships them as-is and
the user runs them at their own risk to the underlying subscription
account.

## Configuration

On first run Atlas Agent creates a config directory at the platform's standard location:

- Windows: `%APPDATA%\atlas-agent`
- macOS:   `~/Library/Application Support/atlas-agent`
- Linux:   `~/.config/atlas-agent`

Most of what lives in that config can also be changed from the chat
itself — see [Talk to it to configure it](#talk-to-it-to-configure-it).

## Contributing

Issues and pull requests are welcome. Contributions are covered by the
[Contributor License Agreement](CLA.md) — opening a PR means you agree
to its terms. For local development, see [AGENTS.md](AGENTS.md) for
the architecture overview and where things live.

## License

MIT — see [LICENSE.md](LICENSE.md).

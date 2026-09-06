<div align="center">

# Atlas Agent

**A terminal-first AI coding agent that isn't locked to one model.**

Plug in Claude, GPT, Gemini, MiniMax, a local model, or a flat-rate coding plan you already pay for.
Delegate work to subagents instead of blowing up your context. Argue hard calls out between several
models before committing to one. Hand it a goal and let it keep working toward it on its own.
Change how it is configured just by telling it to.

[![Release](https://img.shields.io/github/v/release/Omerfaruk-aydn/Atlas-Agent?label=release)](https://github.com/Omerfaruk-aydn/Atlas-Agent/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE.md)
[![npm](https://img.shields.io/npm/v/%40atlas-coder%2Fatlas-agent?label=npm)](https://www.npmjs.com/package/@atlas-coder/atlas-agent)
[![Go Report Card](https://goreportcard.com/badge/github.com/Omerfaruk-aydn/Atlas-Agent)](https://goreportcard.com/report/github.com/Omerfaruk-aydn/Atlas-Agent)
[![Stars](https://img.shields.io/github/stars/Omerfaruk-aydn/Atlas-Agent?style=social)](https://github.com/Omerfaruk-aydn/Atlas-Agent/stargazers)

**80+ built-in tools · 10 built-in subagent modes · 16 provider and coding-plan integrations · MCP + Skills + hooks for everything else**

</div>

```bash
curl -fsSL https://github.com/Omerfaruk-aydn/Atlas-Agent/releases/latest/download/install.sh | bash
atlas-agent
```

<!--
  Demo goes here: a short terminal recording (asciinema, or a GIF made with
  vhs/terminalizer) showing one real task end to end -- e.g. "fix the failing
  test": the agent reading the code, running the suite, and the diff landing.
  Record one, upload it (drag-and-drop into a GitHub issue comment for a CDN
  URL, or commit it under docs/), then replace this comment with:
      ![demo](docs/demo.gif)
  Under ~15 seconds showing one real edit lands better than a long feature tour.
-->

> [!NOTE]
> Atlas Agent is MIT-licensed and open to contributions. Issues and pull requests are welcome —
> see [Contributing](#contributing). If something in this README has drifted from the code, that is
> a good first PR.

---

## Table of contents

**Getting started**
- [What Atlas Agent is](#what-atlas-agent-is)
- [How it compares](#how-it-compares)
- [Install](#install)
- [Your first session](#your-first-session)
- [CLI reference](#cli-reference)
- [In-session commands](#in-session-commands)

**The parts that make it different**
- [Delegation: agent, orchestrate, delegate, debate](#delegation-agent-orchestrate-delegate-debate)
- [Token economics: why delegation is cheaper](#token-economics-why-delegation-is-cheaper)
- [Autonomous goals](#autonomous-goals)
- [Talk to it to configure it](#talk-to-it-to-configure-it)
- [Subagents and modes](#subagents-and-modes)

**Reference**
- [Tools](#tools)
- [Providers and coding plans](#providers-and-coding-plans)
- [Model roles and fallbacks](#model-roles-and-fallbacks)
- [Context and cost management](#context-and-cost-management)
- [Permissions and safety](#permissions-and-safety)
- [Extensibility: skills, MCP, hooks](#extensibility-skills-mcp-hooks)
- [Sessions, worktrees, and teams](#sessions-worktrees-and-teams)
- [Configuration reference](#configuration-reference)

**Practice**
- [Recipes](#recipes)
- [Prompting tips](#prompting-tips)
- [Provider setup walkthroughs](#provider-setup-walkthroughs)

**Project**
- [Architecture](#architecture)
- [Development](#development)
- [Troubleshooting](#troubleshooting)
- [FAQ](#faq)
- [Known gaps](#known-gaps)
- [Contributing](#contributing)
- [License](#license)

---

## What Atlas Agent is

Atlas Agent is a coding agent that runs in your terminal. You give it a task in plain language; it
reads the code, searches, edits files, runs the tests, reads the failures, and keeps going until the
task is done — asking for permission before anything with a side effect, unless you have told it not
to bother.

That much it shares with every other agent CLI. Four things make it different:

**It is not tied to one vendor's model.** The model is a configuration value, not a product
decision. Anthropic, OpenAI, Google, MiniMax, xAI, DeepSeek, Moonshot/Kimi, Z.ai, Zhipu, NVIDIA NIM,
OpenCode, local models — and flat-rate coding-plan subscriptions like GitHub Copilot, ChatGPT, and
Google Antigravity, which many developers are already paying for and cannot use from most other
agent CLIs. You can also point *different parts of the workflow* at different models: an expensive
model for the main session, a cheap one for titles and summaries, another for research, another to
judge whether a goal is finished.

**It delegates by default instead of doing everything in one context.** A subagent runs in a session
of its own. Everything it reads, greps, and opens stays there; only its answer comes back. Reading
twenty files to answer one question costs you a paragraph of context instead of twenty files of it.
See [Token economics](#token-economics-why-delegation-is-cheaper) for what that is actually worth.

**It can argue with itself, on purpose.** `debate` puts a question to several subagents over multiple
rounds, each seeing and answering what the others said, optionally with a further subagent as judge.
Disagreement that survives the argument is reported as disagreement rather than averaged into a
consensus nobody reached — which is the useful outcome when a decision is expensive to get wrong.

**It configures itself from the conversation.** "Use the cheap model for summaries." "Put Sonnet on
research." "Turn the browser tool on." "Make me a subagent that only reviews UI/UX." These are all
just things you say; `atlas_config` makes the change and tells you what it did. You never have to go
find a settings dialog and translate your sentence into a form.

---

## How it compares

A rough map of where Atlas Agent sits. Other tools move fast, so treat the right-hand columns as
"last checked" rather than gospel, and check for yourself before making a decision on it.

| | **Atlas Agent** | Claude Code | Aider | Cursor CLI |
| --- | --- | --- | --- | --- |
| License | MIT | Proprietary | Apache-2.0 | Proprietary |
| Language / distribution | Go, single static binary | Node | Python | Node |
| Model choice | Any provider, per role | Anthropic models | Any provider | Anthropic/OpenAI via Cursor |
| Flat-rate coding plans | Copilot, ChatGPT, Antigravity | Claude subscription | — | Cursor subscription |
| Subagents | 10 built in, author your own | Yes | — | Limited |
| Parallel fan-out | `orchestrate`, `delegate` | Partial | — | — |
| Multi-round model debate | `debate` | — | — | — |
| Autonomous goal loop with judge | `/goal` | — | — | — |
| Configure from chat | `atlas_config` | Partial | — | — |
| LSP-backed refactors | 9 LSP tools | Via MCP | — | Editor-native |
| Built-in tool count | 80+ | ~15 | ~10 | ~15 |
| MCP support | Yes | Yes | — | Yes |
| Hooks | 5 events | Yes | — | — |
| Runs offline with a local model | Yes | — | Yes | — |

**Where the honest weaknesses are**, because a comparison table that only flatters the author is
worth nothing:

- Several coding-plan logins are **scaffolds**, not finished integrations — the OAuth flow works but
  the request envelope is a stub. They are marked as such in
  [Providers and coding plans](#providers-and-coding-plans); do not pick Atlas Agent *because* of a
  scaffolded plan.
- Some of the deeper analysis tools (`dead_code`, `impact_analysis`, `type_hierarchy`,
  `code_metrics`, `security_scan`, `generate_tests`) are **Go-specific**. Everything else is
  language-agnostic, but if you are not writing Go you get a smaller toolbox.
- There is no editor extension. This is a terminal tool that happens to talk to LSP servers, not an
  IDE integration.
- It is young. Expect sharper edges than a tool with a support team behind it.

---

## Install

### One-line install

**macOS / Linux**

```bash
curl -fsSL https://github.com/Omerfaruk-aydn/Atlas-Agent/releases/latest/download/install.sh | bash
```

**Windows (PowerShell, run as administrator)**

```powershell
irm https://github.com/Omerfaruk-aydn/Atlas-Agent/releases/latest/download/install.ps1 | iex
```

The installer downloads the binary matching your OS and architecture into `~/.atlas-agent/bin/` and
adds it to your `PATH`.

### npm

```bash
npm install -g @atlas-coder/atlas-agent
```

The npm package is a thin wrapper: `postinstall` downloads the native binary for your platform from
the GitHub release matching the package version.

### From source

```bash
git clone https://github.com/Omerfaruk-aydn/Atlas-Agent.git
cd Atlas-Agent
go build -o atlas-agent .
./atlas-agent
```

Requires a recent Go toolchain. There is no code generation step to run for an ordinary build.

### Manual download

Grab the binary for your platform from [Releases](https://github.com/Omerfaruk-aydn/Atlas-Agent/releases/latest):

| OS | Architecture | File |
| --- | --- | --- |
| Windows | x64 | `atlas-agent-windows-x64.exe` |
| macOS | Intel | `atlas-agent-darwin-x64` |
| macOS | Apple Silicon | `atlas-agent-darwin-arm64` |
| Linux | x64 | `atlas-agent-linux-x64` |

Rename it to `atlas-agent` (or `atlas-agent.exe`), put it somewhere on your `PATH`, and
`chmod +x atlas-agent`.

### Verify and upgrade

```bash
atlas-agent --version     # what you have
atlas-agent update        # pull the latest release in place
atlas-agent doctor        # check config, providers, LSPs, and MCP servers
```

---

## Your first session

```bash
cd your-project
atlas-agent
```

On first run Atlas Agent creates its config directory, discovers your project, and starts any LSP
servers it recognizes from root markers (`go.mod`, `package.json`, `pyproject.toml`, and so on).

**1. Give it a model.** Either set an API key in the environment:

```bash
export ANTHROPIC_API_KEY=...     # or OPENAI_API_KEY, MINIMAX_CODING_API_KEY, ...
```

or sign in to a coding plan you already pay for:

```bash
atlas-agent login copilot
atlas-agent login chatgpt
```

Then pick the model, either from the model picker in the TUI or by just saying so:

```text
> use claude-sonnet for this session
```

**2. Ask for something real.** Not "explain this codebase" — give it a task with a definition of
done:

```text
> the test in internal/shell/dispatch_test.go is failing on Windows. find out why and fix it.
```

**3. Watch what it does.** Every command, write, and fetch is shown to you before it runs. If you do
not want it, deny the prompt; the agent adapts and keeps going with everything else. When you trust
it on a particular project, switch permission modes (see
[Permissions and safety](#permissions-and-safety)) so it stops asking about edits.

**4. Try the parts that are not like other agents.**

```text
> /goal every package under ./internal passes `go vet` and `go test`

> debate: should retry logic live in the client or the transport?
  use the backend and review subagents, with planner as judge

> use the cheap model for titles and summaries, and put opus on the review role

> make me a subagent called "frontend-critic" that only reviews UI/UX, nothing else
```

---

## CLI reference

Everything below is a subcommand of `atlas-agent`.

### Running

| Command | What it does |
| --- | --- |
| `atlas-agent` | Interactive TUI session in the current directory. |
| `atlas-agent run "<prompt>"` | Non-interactive: run one prompt to completion and print the result. |
| `atlas-agent run --help` | Flags for non-interactive mode (output format, model, quiet). |
| `atlas-agent server` | Run the backend as a server, for client/server or remote use. |
| `atlas-agent --version` | Print version and commit. |
| `atlas-agent doctor` | Diagnose config, providers, LSP servers, and MCP servers. |
| `atlas-agent update` | Update the binary in place to the latest release. |

### Models and providers

| Command | What it does |
| --- | --- |
| `atlas-agent models` | List every model available from configured providers. |
| `atlas-agent models show <id>` | Show one model's context window, pricing, and capabilities. |
| `atlas-agent provider list` | List configured providers. |
| `atlas-agent provider add <name>` | Add a provider (API key, base URL, custom models). |
| `atlas-agent provider remove <name>` | Remove a provider. |
| `atlas-agent provider usage [name]` | Token and cost usage per provider. |
| `atlas-agent update-providers [path-or-url]` | Refresh the embedded provider/model catalog. |
| `atlas-agent roles` | Show which model each named role resolves to. |
| `atlas-agent login [platform]` | Sign in to a coding plan (Copilot, ChatGPT, Antigravity, …). |
| `atlas-agent logout [platform]` | Sign out and forget stored credentials. |

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
| `atlas-agent session rewind <id> <message-id>` | Rewind history to a message and continue from there. |
| `atlas-agent session diff <id1> <id2>` | Compare two sessions. |
| `atlas-agent session export <id>` | Export a session (for sharing or archiving). |
| `atlas-agent session delete <id>` | Delete a session. |
| `atlas-agent session prune` | Delete old sessions in bulk. |
| `atlas-agent stats` | Token, cost, and activity statistics. |
| `atlas-agent usage` | This workspace's usage summary. |

### Subagents, skills, and extensions

| Command | What it does |
| --- | --- |
| `atlas-agent agent list` | List subagents: the ten built-in modes plus anything you authored. |
| `atlas-agent agent show <name>` | Print a subagent's definition. |
| `atlas-agent agent new <name>` | Create a subagent definition file. |
| `atlas-agent agent remove <name>` | Delete a subagent definition. |
| `atlas-agent skill list` | List available skills. |
| `atlas-agent skill new <name>` | Scaffold a new `SKILL.md`. |
| `atlas-agent skill validate` | Validate skill frontmatter and structure. |
| `atlas-agent skill remove <name>` | Delete a skill. |
| `atlas-agent mcp list` | List configured MCP servers. |
| `atlas-agent mcp add` / `remove` | Manage MCP servers. |
| `atlas-agent lsp list` | Show LSP servers and their state. |
| `atlas-agent hooks list` | Show configured hooks. |
| `atlas-agent hooks run <event>` | Fire a hook manually, for testing. |
| `atlas-agent tools` | List the tools currently exposed to the agent. |

### Project and workspace

| Command | What it does |
| --- | --- |
| `atlas-agent projects` | List known projects. |
| `atlas-agent worktree` | Manage git worktree-backed sessions. |
| `atlas-agent memory show [project\|user]` | Show remembered project/user context. |
| `atlas-agent memory clear <project\|user>` | Forget it. |
| `atlas-agent config` | Show effective configuration. |
| `atlas-agent schema` | Print the JSON schema for the config file. |
| `atlas-agent dirs` / `paths` | Show config, data, and cache directories. |
| `atlas-agent logs` | Tail the agent's own log file. |

---

## In-session commands

Inside the TUI, `/` opens the command list. The ones worth knowing:

| Command | What it does |
| --- | --- |
| `/goal <objective>` | Start an autonomous run toward an objective. Sends immediately, like a normal prompt. |
| `/goal` | Report what the session is currently working toward. |
| `/goal clear` | End the autonomous run. |
| `/model` | Switch the session's model. |
| `/roles` | Assign models to named roles. |
| `/summarize` | Compact the conversation now instead of waiting for the threshold. |
| `/sessions` | Switch between sessions. |
| `/new` | Start a fresh session. |
| `/mode` | Switch permission mode (manual, auto-accept edits, plan, bypass). |
| `/init` | Write an `AGENTS.md` describing this project for future sessions. |
| `/help` | Everything else. |

---

## Delegation: agent, orchestrate, delegate, debate

A subagent is a full agent session of its own: its own context window, its own tool calls, its own
reading of the codebase. It sees only the prompt you hand it — never your conversation — and only
its final answer comes back.

That constraint is the feature. The subagent can read twenty files to answer one question, and your
main session pays for one paragraph instead of twenty files.

| Tool | Shape | Reach for it when |
| --- | --- | --- |
| `agent` | One task → one subagent. | The work is self-contained and you want the conclusion, not the research trail. |
| `orchestrate` | Same prompt → several subagents, in parallel, answers side by side. | One answer is not worth taking on faith, and independent corroboration is the point. |
| `delegate` | Several *different* subtasks → several subagents, in parallel. | The task splits into pieces that do not need to see each other's output. |
| `debate` | A question → several subagents over multiple rounds, each answering the others, optionally judged. | The first pass came back split, or a decision is expensive to get wrong and cheap to think about longer. |
| `vibe` | A persistent background worker toward a goal, steerable while it runs. | Work should continue independently of the current turn. |

### `agent`

```text
> agent: find where session titles are generated and what model they use
```

Runs on the default task agent, or on a named subagent (`review`, `security`, `research`, …), or
picks a subagent automatically by matching the prompt against each one's description.

### `orchestrate`

Same question, several models or several specialists, independently. A majority answer means
something *because* none of them saw the others.

```text
> orchestrate this across review and security:
  "is this token refresh logic safe to run concurrently?"
```

### `delegate`

Different pieces, in parallel. Each subtask carries its own self-contained prompt.

```text
> delegate: (1) write the migration, (2) update the sqlc queries,
  (3) add the round-trip test — do them in parallel
```

### `debate`

The escalation from `orchestrate`. Round one is independent — nobody anchors on anyone. Every round
after that hands each agent what the others said and asks them to defend, concede, or change their
mind. What comes out is either a position that survived being argued against, or a disagreement
specific enough to be worth something.

```text
> debate: should the goal loop's completion decision be made by a judge model
  or by the main model? use review and security, 3 rounds, planner as judge
```

Parameters: `question`, `agent_names` (2–5), `rounds` (default 2, max 4), `judge_agent` (optional,
must be distinct from the debaters). Two rounds is usually right — one to stake out positions, one
to answer the others; beyond that they mostly converge on wording.

> [!TIP]
> Do not use `debate` for questions the codebase can settle. Three models speculating about how your
> code works produce three confident guesses and no evidence. Use it for judgment calls: an
> architecture you will build on, a migration, a security trade-off, a fix you cannot easily undo.

---

## Token economics: why delegation is cheaper

This is the part that is easy to miss, so here it is with numbers.

**The problem with a single context.** Every file you read stays in the conversation for the rest of
the session, and every subsequent turn re-sends the whole conversation. Reading a 600-line file once
does not cost you 600 lines — it costs 600 lines *times every turn that follows*. A long session
where the agent read forty files is paying for those forty files on every single request, whether or
not they are still relevant.

**What delegation changes.** A subagent's reading happens in its own session. Your main session
receives its answer, and nothing else.

Illustrative, for one "find how X works and summarize it" task in a large codebase:

| | Files read | Tokens in the main session | Cost on every later turn |
| --- | --- | --- | --- |
| Done inline | 18 | ~45,000 | ~45,000 re-sent, forever |
| Delegated to a subagent | 18 (in the subagent) | ~600 (the answer) | ~600 re-sent |

The subagent still pays to read those 18 files — once. What you avoid is re-sending them on every
turn for the rest of the session, and the context-window pressure that forces an early summarization.

**Where the savings compound:**

- **`delegate` runs the pieces in parallel**, so a four-part task takes about as long as its slowest
  part rather than the sum of all four.
- **Cheap models for cheap work.** Titles, summaries, and compaction do not need your best model.
  Point the `small` model type and the `compact` role at something cheap and the difference shows up
  on every single session.
- **Role-based routing.** Research on a fast, large-context model; final code changes on a strong
  one; the goal judge on a mid-tier one. You are not paying frontier prices for `git log` parsing.
- **Later summarization.** Because the main context grows slower, `auto_summarize_at` triggers less
  often, and each summarization loses less of what you actually care about.

**Controls that bound the bill directly** (see [Configuration reference](#configuration-reference)):

| Option | What it bounds |
| --- | --- |
| `options.max_session_cost` | Refuses a new prompt once a session's accumulated USD cost hits the limit. |
| `options.max_steps_per_turn` | Stops a turn after N model/tool steps, whether or not it is making progress. |
| `options.max_concurrent_sub_agents` | Caps how many subagents run at once. |
| `options.auto_summarize_at` | Fraction of the context window at which the session compacts itself. |
| `options.tool_timeout` | Kills a single tool call after N seconds instead of burning a turn on it. |
| `models.small` + `model_roles.compact` | Which (cheap) model does the cheap work. |

And `usage` (tool) or `atlas-agent stats` (CLI) will tell you where the tokens actually went.

---

## Autonomous goals

```text
> /goal every endpoint in internal/server returns a typed error, not a bare string
```

That sends immediately, exactly like an ordinary prompt — the goal is the first turn, not a setting
you configure and then have to kick off separately.

From there the session keeps taking turns of its own toward the objective. Each turn ends, publishes,
and the next one begins without you typing anything.

**How it decides it is done.** Two things have to agree:

1. The agent calls the `goal` tool's `done` action, claiming the objective is reached.
2. A **judge model** — a second model that sees the goal and the work, not the agent's reasoning —
   confirms it. The judge is the `goal` model role when you have configured one, otherwise the
   advisor's model. If neither exists, the claim stands unchecked, and the run says so.

That separation is deliberate. A model asked "are you done?" at the end of its own turn is the worst
possible judge of the question.

**Bounds.** Every run carries a turn budget (default 10, hard ceiling 100). The agent can ask to
raise it through the `goal` tool when it judges the work bigger than it first looked, and the tool
caps rather than refuses an over-large request. The run ends when the goal is confirmed reached, the
budget is exhausted, or you clear it.

**Visibility.** The goal is shown in the sidebar's **Goal** section and in the header for the whole
run, and survives a restart — not just a toast that disappears after five seconds. `/goal` with no
argument reports what is being worked toward and how much budget is left. `/goal clear` (or `stop`,
`off`, `none`) ends it.

---

## Talk to it to configure it

`atlas_config` gives the agent the ability to change ATLAS-AGENT's own configuration, so a request
about how the tool behaves is work it does rather than instructions it hands back to you.

| You say | What it does |
| --- | --- |
| "use the cheap model for summaries" | `set_role` on `compact` |
| "put Sonnet on research" | `set_role` on `research` |
| "switch me to GPT for this project" | `set_model` with `scope: workspace` |
| "turn the browser tool off" | `disable_tool` |
| "what am I actually running right now?" | `list` |
| "make it summarize at 70% instead" | `set_field` on `options.auto_summarize_at` |
| "make me a subagent that only reviews UI/UX" | `save_subagent` |
| "what subagents do I have?" | `list_subagents` |
| "delete the frontend-critic subagent" | `delete_subagent` |

Every change that is not a read asks for permission first, described in words rather than as a tool
name — "Run the `research` role on claude/claude-sonnet-5 (global)", not
`atlas_config set_role`. Values are type-checked against the config schema *before* they are
written, so a model answering "make it 70%" with the string `"0.7"` where a number belongs is
refused rather than silently corrupting the config file.

Scope is `global` (everywhere you run Atlas Agent) or `workspace` (this project only).

---

## Subagents and modes

Ten specialist modes ship in the binary:

| Mode | What it is for |
| --- | --- |
| `backend` | Server-side code: APIs, data access, background work, concurrency, failure handling. |
| `frontend` | UI code: components, state, styling, accessibility, rendering performance. |
| `debug` | Root-causing a failing test, crash, or wrong behavior, then reporting the minimal fix. |
| `test` | Tests that fail for the right reason — real behavior, edge cases, error paths. |
| `review` | Correctness, security, and maintainability review with `file:line` evidence. |
| `security` | Exploitable vulnerabilities with an attack path for each. |
| `refactor` | Behavior-preserving restructuring in small verified steps. |
| `research` | Open questions answered with evidence and citations, verified separated from inferred. |
| `planner` | A feature request turned into an ordered, codebase-grounded implementation plan. |
| `docs` | READMEs, API references, guides, doc comments, grounded in what the code does. |

Each can run as a subagent (`agent`, `orchestrate`, `delegate`, `debate`) or be folded into the main
session with `options.session_mode`, so the two can never drift apart.

**Authoring your own.** A subagent is a Markdown file with YAML frontmatter:

```markdown
---
name: frontend-critic
description: Reviews UI work strictly from a UI/UX perspective — hierarchy, layout, typography, spacing, accessibility. Use when you want critique of how a UI looks and feels, not implementation correctness.
model: "@frontend"
---

You are "frontend-critic", a UI/UX-only reviewer.

Evaluate in this order: visual hierarchy, layout and spacing, typography,
color and contrast, interaction states, accessibility, responsiveness,
information architecture, consistency.

Report findings as a numbered list. For each: the issue and why it matters
to the user, a concrete fix in design terms, a severity (blocker / major /
minor / nit), and file:line when reviewing code. End with what is already
working well.
```

Drop it in `.atlas/agents/` (project scope) or your user config directory (global scope) as
`frontend-critic.md` — or just ask the agent to write it for you with `atlas_config`.

The `model` field names a **role**, not a model id, so the same subagent runs on whatever model that
role currently points at. Leave it empty and the subagent runs on the session's own model.

---

## Tools

80+ built-in tools. `atlas-agent tools` prints the ones currently enabled; anything can be turned off
with `atlas_config` or `options.disabled_tools`.

### Editing and navigation

| Tool | What it does |
| --- | --- |
| `view` | Read a file, optionally a line range. |
| `write` | Create or overwrite a file, creating parent directories. |
| `edit` | Exact find-and-replace, with whitespace-tolerant matching and re-indentation. |
| `multiedit` | Several find-and-replace edits to one file, applied in sequence. |
| `glob` | Match files by glob pattern. |
| `ls` | List a directory, with ignore patterns and depth. |
| `inspect_file` | Look inside a `.zip`/`.tar`/`.tar.gz`, a SQLite database, or a Jupyter notebook. |
| `download` | Fetch a URL to a local file, with size and domain limits. |

### Code intelligence (LSP)

| Tool | What it does |
| --- | --- |
| `lsp_diagnostics` | Errors, warnings, and hints for a file or the whole project. |
| `lsp_references` | Every reference to a symbol — accurate where grep is not. |
| `lsp_definition` | Where a symbol is defined, skipping comments, strings, and partial matches. |
| `lsp_symbols` | A file's outline: symbols, kinds, line ranges. |
| `lsp_call_hierarchy` | Incoming or outgoing calls for a symbol — blast radius before a refactor. |
| `lsp_rename` | True semantic rename across files, respecting scope and imports. |
| `lsp_rename_file` | Move or rename a file, updating every reference to it first. |
| `lsp_replace_symbol` | Replace, insert, or delete a whole symbol by name, with exact boundaries. |
| `lsp_restart` | Restart one or all LSP clients when diagnostics go stale. |

### Static analysis

| Tool | What it does |
| --- | --- |
| `dead_code` | Declarations nothing in the tree references — deletion candidates. |
| `type_hierarchy` | Which Go types satisfy which interfaces, without compiling. |
| `import_graph` | How packages depend on each other, and where the import cycles are. |
| `impact_analysis` | Everything affected by changing a function, by transitive caller distance. |
| `code_metrics` | Cyclomatic complexity, length, nesting depth, signature arity. |
| `api_surface` | What a package exposes: exported functions, types, methods, fields, constants. |
| `semantic_code_search` | Find a declaration from a natural-language description. |
| `anti_pattern_scan` | Swallowed errors, misplaced `context.Context`, panics used instead of errors. |
| `security_scan` | Hardcoded credentials, broken crypto, disabled TLS verification, SQL/shell injection. |
| `scan_secrets` | Committed API keys, tokens, private keys, passwords in connection strings. |
| `env_var_audit` | Every env var the code reads or writes, cross-checked against an example file. |
| `todo_scan` | TODO, FIXME, HACK, XXX, BUG, OPTIMIZE, DEPRECATED markers. |
| `doc_index` | A table of contents across a tree's Markdown, searchable by keyword. |
| `metric_export` | Every Prometheus metric constructed: name, type, help text, labels. |

### Testing and quality

| Tool | What it does |
| --- | --- |
| `test_run` | Run tests, get a structured result: what failed, why, how long. |
| `coverage_report` | Measure coverage and find the code no test reaches. |
| `lint_run` | Run the project's linter, findings grouped by file. |
| `dep_audit` | Dependencies with known vulnerabilities, and which are behind. |
| `generate_tests` | A table-driven test skeleton shaped from a function's own signature. |
| `generate_docstring` | Doc-comment stubs for exported declarations that have none. |
| `debugger` | Run a Go program under Delve: breakpoints, stepping, live variables. |

### Git and pull requests

| Tool | What it does |
| --- | --- |
| `git_status` | Staged, modified, untracked, and how the branch stands against upstream. |
| `git_diff` | Working tree, index, or between any two revisions. |
| `git_log` | Search history by path, author, message, date range, or branch. |
| `git_blame` | When each line last changed, by whom, in which commit. |
| `git_branches` | Branches with age, divergence, and whether already merged. |
| `git_conventional_commit` | A Conventional Commits type and scope for the staged changes. |
| `git_commit_split` | How to split a pile of changes into smaller commits, dependency-ordered. |
| `git_conflict_resolver` | Each conflicted region summarized: what each side changed, how big. |
| `pre_commit_guard` | Inspect staged changes for mistakes that are obvious only in hindsight. |
| `pr_describe` | A PR description drafted from the actual commits and diff. |
| `github_pr_view` | A PR's metadata and full diff via the `gh` CLI, without cloning. |
| `changelog_gen` | A changelog drafted from real git history, grouped for a reader. |
| `audit_trail` | One function's history through git line-history: who changed it, when, how. |

### Infrastructure and operations

| Tool | What it does |
| --- | --- |
| `docker_build_explain` | A Dockerfile's build stages, plus instruction-level mistakes that are easy to miss. |
| `k8s_manifest_lint` | Pod-spec mistakes that apply cleanly and surface later as OOM-kills or wide-open security contexts. |
| `terraform_lint` | Misconfigurations that are valid HCL, apply cleanly, and become incidents later. |
| `ci_cd_pipeline_debugger` | GitHub Actions mistakes that run green today and become supply-chain risk later. |
| `cloud_resource_costs` | Order-of-magnitude monthly cost from Terraform instance types and Kubernetes requests. |
| `log_tail` | The tail of a log file, filtered by substring or level, without loading all of it. |

### Delegation and collaboration

| Tool | What it does |
| --- | --- |
| `agent` | Run one task on one subagent. |
| `orchestrate` | Run the same prompt on several subagents in parallel. |
| `delegate` | Run several different subtasks in parallel. |
| `debate` | Multi-round argument between subagents, optionally judged. |
| `vibe` | A persistent, steerable background worker. |
| `goal` | How an autonomous run sizes its budget and claims completion. |
| `team_send` | Broadcast a message to every agent in the task's team. |
| `team_read` | Read what other agents in the team have sent. |

### System and self-configuration

| Tool | What it does |
| --- | --- |
| `bash` | Run a shell command, foreground or background, with command policy applied. |
| `job_output` | Read stdout/stderr from a background shell by id. |
| `job_kill` | Terminate a background shell. |
| `atlas_config` | Change providers, model roles, tool enablement, and subagents. |
| `atlas_info` | Current runtime state: model, provider, LSP/MCP status, skills, hooks, permissions. |
| `atlas_logs` | Recent entries from the agent's own log. |
| `exit_plan_mode` | Leave plan mode once a plan has been presented. |

### Knowledge and session

| Tool | What it does |
| --- | --- |
| `memory` | Record something worth carrying into future sessions, or revise it. |
| `facts` | Retain a fact for *this* session and recall it by keyword. |
| `todos` | A structured task list with pending/in-progress/completed state. |
| `question` | Ask the user a structured question and wait for the answer. |
| `session_search` | Search what was said in earlier sessions in this workspace. |
| `skill_manage` | Write a skill: instructions that load automatically when they match a later task. |
| `usage` | This session's token usage and cost so far. |
| `sourcegraph` | Search public code on Sourcegraph. |

### Browser and network

| Tool | What it does |
| --- | --- |
| `browser` | Drive a real browser: navigate, click, type, scroll, screenshot, eval, snapshot, CDP. |
| `fetch` | Fetch a URL's content, subject to the domain allow/block lists. |
| `agentic_fetch` | Fetch and extract with a model in the loop, for pages that need reading rather than scraping. |

### MCP

| Tool | What it does |
| --- | --- |
| `list_mcp_resources` | List resource URIs exposed by a named MCP server. |
| `read_mcp_resource` | Read one resource by URI. |

---

## Providers and coding plans

Atlas Agent ships an embedded provider/model catalog, so `atlas-agent models` works before you
configure anything. Providers come in three shapes.

### 1. API-key providers

Set the environment variable (or `atlas-agent provider add <name>`) and the models appear:

| Provider | Env var | Notes |
| --- | --- | --- |
| Anthropic | `ANTHROPIC_API_KEY` | Claude family |
| OpenAI | `OPENAI_API_KEY` | GPT family, Responses API |
| Google | `GEMINI_API_KEY` | Gemini family |
| xAI (Grok API) | `XAI_API_KEY` | SuperGrok API path |
| DeepSeek | `DEEPSEEK_API_KEY` | DeepSeek-V3/V4 family |
| Kimi Coding (Moonshot) | `KIMI_CODING_API_KEY` | Kimi K3 / Kimi for Coding |
| Moonshot | `MOONSHOT_API_KEY` | Moonshot family |
| Z.ai | `ZAI_API_KEY` | GLM-4 family |
| Zhipu Coding | `ZHIPU_CODING_API_KEY` | Zhipu coding plan |
| MiniMax Coding | `MINIMAX_CODING_API_KEY` | MiniMax-M2.7 / M3 |
| NVIDIA NIM | `NVIDIA_NIM_API_KEY` | Large hosted open-model catalog |
| OpenCode Zen | `OPENCODE_ZEN_API_KEY` | OpenCode Zen coding plan |
| OpenCode Go | `OPENCODE_GO_API_KEY` | OpenCode Go coding plan |
| OpenRouter | `OPENROUTER_API_KEY` | Everything else, through one key |
| AWS Bedrock | standard AWS credentials | Bedrock-hosted models |
| Azure OpenAI | `AZURE_OPENAI_*` | Azure deployments |

Any OpenAI-compatible endpoint works too — including **local models** from Ollama, LM Studio, llama.cpp,
or vLLM — by adding a provider with a custom `base_url`.

### 2. Coding-plan subscriptions (OAuth)

Flat-rate plans you may already pay for. `atlas-agent login <platform>`:

| Plan | Login command | Status | Notes |
| --- | --- | --- | --- |
| GitHub Copilot (Pro/Pro+/Business) | `atlas-agent login copilot` | **Live** | Device flow, headers set up |
| ChatGPT (Plus/Pro/Business) | `atlas-agent login chatgpt` | **Live** | PKCE OAuth, Codex backend |
| Google Antigravity (AI Pro/Ultra) | `atlas-agent login antigravity` | **Live** | PKCE OAuth, Cloud Code, Gemini family |
| Claude (Pro/Max/Team) | `atlas-agent login claude` | Scaffold | OAuth client id / envelope are TODOs |
| xAI SuperGrok (Heavy) | `atlas-agent login grok` | Scaffold | OAuth client id / envelope are TODOs |
| Windsurf (Codeium Pro/Teams) | `atlas-agent login windsurf` | Scaffold | OAuth client id / envelope are TODOs |
| JetBrains AI (Pro/Ultimate) | `atlas-agent login jetbrains` | Scaffold | Token exchange / envelope are TODOs |

- **Live** — login and the model call layer both work end to end.
- **Scaffold** — login and model picker are wired up; the call layer is a stub that returns "not
  implemented" until the real request envelope is captured against the official client. See the
  package docs in `internal/oauth/<plan>`.

> [!WARNING]
> Scaffolded plans share a risk profile: the provider's terms of service restrict the service to
> first-party clients, and third-party logins can be revoked. Atlas Agent ships them as-is; you run
> them at your own risk to the underlying subscription account.

### 3. Local and self-hosted

```jsonc
{
  "providers": {
    "ollama": {
      "base_url": "http://localhost:11434/v1",
      "api_key": "not-needed",
      "models": [{ "id": "qwen2.5-coder:14b", "context_window": 32768 }]
    }
  }
}
```

Nothing leaves your machine, and the same tools, subagents, and goal loop work exactly as they do
against a hosted model — subject to the local model's own tool-calling ability.

---

## Model roles and fallbacks

A **role** is a name a model is assigned to. Subagents reference roles rather than model ids, so
retargeting a whole class of work is one setting rather than a sweep through files.

Roles recognized by built-in features:

| Role | Used for |
| --- | --- |
| `compact` | Summarizing a session when it runs out of context. A cheap model here saves real money on long sessions. |
| `advisor` | A second model that reviews each finished turn and leaves a note for the next one. |
| `escalate` | Consulted when the agent is stuck. |
| `goal` | Judges whether an autonomous run has actually reached its objective. |

Every other name is free-form: a role called `research` is what a subagent with `model: "@research"`
runs on, and a mode named `frontend` runs on the role of the same name.

Two model *types* sit underneath roles:

- **`large`** — the session's main model.
- **`small`** — titles, summaries, and cheap side work. Point this at something inexpensive.

**Fallbacks.** `options.model_fallbacks` takes an ordered list per model type; when a request comes
back rate-limited (429), Atlas Agent fails over to the next entry rather than dropping the turn.
`options.fallback_cooldown` controls how long a fallback stays active before the next turn returns
to the primary.

```jsonc
{
  "models": {
    "large": { "provider": "anthropic", "model": "claude-sonnet-5" },
    "small": { "provider": "minimax",   "model": "MiniMax-M2.7-highspeed" }
  },
  "options": {
    "model_roles": {
      "compact":  { "provider": "minimax", "model": "MiniMax-M2.7-highspeed" },
      "research": { "provider": "openai",  "model": "gpt-5.6-sol" },
      "goal":     { "provider": "anthropic", "model": "claude-sonnet-5" }
    },
    "model_fallbacks": {
      "large": [{ "provider": "openai", "model": "gpt-5.6-sol" }]
    },
    "fallback_cooldown": 300
  }
}
```

---

## Context and cost management

| Mechanism | What it does |
| --- | --- |
| **Auto-summarize** | At `options.auto_summarize_at` (fraction of the context window), the session compacts itself. Runs on the `compact` role's model, not the session's. |
| **Manual compaction** | `/summarize` in-session, or `atlas-agent session compact <id>`. |
| **Delegation** | Subagent reading never enters the main context. See [Token economics](#token-economics-why-delegation-is-cheaper). |
| **Small model** | Titles, summaries, and side work on a cheap model. |
| **Memory bounds** | `options.memory` bounds the prose carried between sessions so it cannot grow without limit. |
| **Session cost ceiling** | `options.max_session_cost` refuses new prompts past a USD limit. |
| **Step ceiling** | `options.max_steps_per_turn` stops a turn that is not converging. |
| **Tool timeout** | `options.tool_timeout` kills a stuck tool call instead of burning the turn. |
| **Subagent concurrency** | `options.max_concurrent_sub_agents` caps parallel spend. |
| **Usage reporting** | The `usage` tool in-session; `atlas-agent stats` and `atlas-agent provider usage` outside it. |

---

## Permissions and safety

Every side-effecting tool call — bash, writes, fetches, config changes — goes through a permission
layer before it runs.

| Mode | Behavior |
| --- | --- |
| **manual** | Ask before every side-effecting action. The default. |
| **auto-accept edits** | File edits go through; commands and fetches still ask. |
| **plan (read-only)** | Nothing is changed at all; the agent researches and presents a plan. |
| **bypass (yolo)** | Nothing asks. For throwaway sandboxes and containers. |

Switch with `/mode` in-session.

Beyond modes:

| Control | What it does |
| --- | --- |
| `options.allowed_commands` | Lift specific commands out of the bash tool's built-in block list. |
| `options.blocked_commands` | Extra commands the bash tool may never run. |
| `options.allowed_domains` | The only domains `fetch`/`download` may reach. |
| `options.blocked_domains` | Domains they may never reach, checked first. |
| `options.restrict_writes_to_working_dir` | Refuse writes outside the working directory outright. |
| `options.max_download_bytes` | Refuse downloads over a size. |
| `options.sandbox` | Contain shell-spawned processes in an OS-level container (Windows Job Objects today; a no-op elsewhere). |
| Hooks | `PreToolUse` can deny a call outright based on your own logic. |

Credentials are stored in the platform's own credential store where one is available, and API keys
are never written into session history.

---

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
atlas-agent mcp add
atlas-agent mcp list
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

Hook commands receive the event's data both as a JSON payload and as environment variables —
`ATLAS_AGENT_TOOL_NAME`, `ATLAS_AGENT_PROMPT`, `ATLAS_AGENT_SESSION_ID`, `ATLAS_AGENT_CWD`,
`ATLAS_AGENT_PROJECT_DIR`, and tool-specific ones such as the bash command being run.

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
      { "name": "gofmt", "command": "gofmt -w \"$ATLAS_AGENT_FILE_PATH\"" }
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

---

## Sessions, worktrees, and teams

**Sessions** are persistent and searchable. Every session is stored with its messages, tool calls,
token usage, and cost; `session_search` (tool) and `atlas-agent session search` (CLI) search across
all of them in a workspace. Subagent runs are child sessions, so `atlas-agent session tree` shows
what a delegated task actually did.

**Rewind** steps a session back to a chosen message and continues from there — useful when a turn
went somewhere you did not want and you would rather not carry it in the context.

**Worktrees** run a session in its own git worktree, so a long autonomous run does not collide with
the branch you are editing by hand.

**Teams** let several agents working on one task exchange short messages (`team_send`, `team_read`)
without going through the main session — a lightweight coordination channel for parallel work.

---

## Configuration reference

Configuration is JSON, layered global → workspace, and both layers can be edited by hand or changed
from the chat with `atlas_config`. `atlas-agent schema` prints the full JSON schema;
`atlas-agent config` prints what is currently in effect.

Config directories:

| Platform | Path |
| --- | --- |
| Windows | `%APPDATA%\atlas-agent` |
| macOS | `~/Library/Application Support/atlas-agent` |
| Linux | `~/.config/atlas-agent` |

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

---

## Recipes

Worked examples, in the shape you would actually type them.

### Review a pull request before merging it

```text
> github_pr_view 412, then review it with the review and security subagents
  in parallel. I want concrete defects with file:line, not a summary.
```

`github_pr_view` pulls the metadata and full diff through the `gh` CLI, and `orchestrate` puts the
same diff in front of two specialists that bring different objections. Because they run
independently, agreement between them means something.

For a change you are unsure about rather than one you are checking:

```text
> debate: is the locking in this PR correct under concurrent writers?
  use review, security and backend, 3 rounds, planner as judge
```

### Hunt a flaky test

```text
> internal/server has a test that fails maybe one run in ten. find it and
  tell me why. run the suite with -count=20 if you need to.
```

The agent can run the suite repeatedly in the background (`bash` with `run_in_background`, then
`job_output`), read failures with `test_run`, and drop into `debugger` for a Go program whose
behavior depends on runtime state a log line will not show.

### Do a dependency bump safely

```text
> dep_audit, then bump anything with a known vulnerability, run the tests,
  and show me the diff before we commit
```

`dep_audit` reports both known vulnerabilities and how far behind each module is; `test_run` and
`git_diff` close the loop.

### Pre-commit sanity pass

```text
> pre_commit_guard on what I have staged, then suggest a conventional
  commit message for it
```

`pre_commit_guard` looks at `git diff --cached` for the class of mistake that is obvious in
hindsight — a stray debug print, a committed secret, a file that should not be in this change —
and `git_conventional_commit` proposes the type and scope from what actually changed.

If the pile is too big for one commit:

```text
> git_commit_split: how should I break this up?
```

It proposes an ordering where a package another changed package depends on is committed first, so
each commit builds.

### Understand an unfamiliar subsystem without wrecking your context

```text
> agent, using the research subagent: how does session compaction decide
  what to keep? name the files and the decision points.
```

The research subagent reads however many files it needs in a session of its own; you get the
answer, not the reading. See [Token economics](#token-economics-why-delegation-is-cheaper).

### Refactor with the LSP instead of grep

```text
> rename the Coordinator.RunAccepted method to RunWithAcceptance everywhere,
  then show me the call sites that changed
```

`lsp_rename` performs a true semantic rename that respects scope, shadowing and imports — the kind
of change a find-and-replace gets subtly wrong. `lsp_call_hierarchy` first, if you want the blast
radius before committing to it.

### Audit before shipping

```text
> scan_secrets across the repo, security_scan on internal/, and
  ci_cd_pipeline_debugger on .github/workflows — report anything real,
  skip the theoretical
```

Three different scanners with three different failure modes: committed credentials, source-level
security smells, and workflow mistakes that run green today and become supply-chain risk later.

### Bring a new machine up to speed

```text
> /init
```

Writes an `AGENTS.md` for the project by actually reading it — conventions, layout, how to build and
test — which every later session (and every subagent) then starts from.

### Let it grind on something bounded while you do something else

```text
> /goal every package under ./internal passes `go vet` and `go test`,
  without changing any public API
```

Then walk away. The run takes its own turns, a judge model checks the claim that it is finished
before it stops, and the goal stays in the sidebar so you can see what it is chasing when you come
back. `/goal` reports progress; `/goal clear` ends it.

### Set the whole thing up for cost

```text
> put the small model and the compact role on minimax's cheap model,
  keep opus for review, summarize at 70%, and cap this session at $2
```

One sentence, four settings: `set_model` (small), `set_role` (compact, review), and `set_field` on
`options.auto_summarize_at` and `options.max_session_cost`. Each change is shown to you for approval
in plain words before it is written.

### Drive a real browser that is already signed in

```text
> open the staging dashboard in my actual Chrome profile and tell me
  what the error banner on the billing page says
```

The `browser` tool can work from a snapshot of your real Chrome profile — cookies, local storage,
sessions — instead of a blank automation window with no logins, which is the difference between
"I cannot sign in" and an answer.

---

## Prompting tips

Atlas Agent is built to be autonomous, so the prompts that work best are the ones that state a
finish line rather than a first step.

**Give it a definition of done.** "Fix the failing test in internal/shell" beats "look at
internal/shell". The agent decides how many steps that takes; it cannot decide when to stop if you
have not said what done looks like.

**Say what not to touch.** Constraints prune whole branches of work: "without changing the public
API", "don't touch the generated files", "keep the existing error strings".

**Point at evidence, not vibes.** "The retry loop double-counts attempts — see
`internal/agent/coordinator.go:812`" starts from a fact. "Something is wrong with retries" starts
from a search.

**Delegate the reading, keep the deciding.** Ask a subagent to *find out*; make the call yourself in
the main session. That is the split the context economics reward.

**Escalate to `debate` only for judgment calls.** Questions the codebase settles should be answered
by reading the codebase. Questions about which design to commit to are what a debate is for.

**Use `/goal` for grinds, not for exploration.** A goal loop is at its best when success is checkable
— a suite that passes, a lint that comes back clean, every call site migrated. It is at its worst
when "done" is a matter of taste.

**Let it configure itself.** If a model is wrong for the job, say so in the session instead of
editing JSON: "put research on something with a bigger context window."

**Correct early.** A wrong assumption caught in turn two costs one turn; caught in turn ten it costs
ten, plus the context they filled.

---

## Provider setup walkthroughs

### Anthropic, OpenAI, Google (API key)

```bash
export ANTHROPIC_API_KEY=sk-ant-...
atlas-agent models          # confirm the catalog picked it up
atlas-agent
```

Then pick a model in the TUI, or say "use claude-sonnet-5 for this session".

### GitHub Copilot (a plan you may already have)

```bash
atlas-agent login copilot   # device flow: it prints a code, you paste it in the browser
atlas-agent models          # Copilot-backed models now appear
```

### ChatGPT plan

```bash
atlas-agent login chatgpt   # PKCE OAuth in your browser
```

Runs against the Codex backend. Note that this backend accepts a narrower parameter set than the
public OpenAI API — Atlas Agent adjusts requests accordingly.

### A local model (Ollama)

```bash
ollama pull qwen2.5-coder:14b
```

```jsonc
// ~/.config/atlas-agent/atlas.json  (or %APPDATA%\atlas-agent\atlas.json)
{
  "providers": {
    "ollama": {
      "base_url": "http://localhost:11434/v1",
      "api_key": "not-needed",
      "models": [
        { "id": "qwen2.5-coder:14b", "context_window": 32768, "default_max_tokens": 4096 }
      ]
    }
  },
  "models": {
    "large": { "provider": "ollama", "model": "qwen2.5-coder:14b" }
  }
}
```

Tool-calling quality is the limiting factor for local models — a model that cannot reliably emit
tool calls will struggle with the multi-step work Atlas Agent is built around. Larger coder-tuned
models do noticeably better.

### Anything else OpenAI-compatible

Same shape as the Ollama example: a `base_url`, an `api_key`, and a `models` list. vLLM, LM Studio,
llama.cpp's server, a corporate gateway, or a provider Atlas Agent has never heard of all work this
way. `atlas-agent provider add <name>` does it interactively.

### Mixing providers deliberately

There is no rule that one provider has to do everything:

```jsonc
{
  "models": {
    "large": { "provider": "anthropic", "model": "claude-sonnet-5" },
    "small": { "provider": "ollama",    "model": "qwen2.5-coder:14b" }
  },
  "options": {
    "model_roles": {
      "research": { "provider": "openai",   "model": "gpt-5.6-sol" },
      "compact":  { "provider": "ollama",   "model": "qwen2.5-coder:14b" },
      "review":   { "provider": "anthropic","model": "claude-opus-5" }
    }
  }
}
```

Frontier model for the code that ships, a local model for summaries and titles, something with a big
context window for research. Nothing about this requires the providers to know about each other.

---

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
  deps/                     Vendored dependencies (LLM client, TUI toolkit, styling)
```

The agent loop and the UI are separate: the same coordinator serves the TUI, `run` mode, and the
HTTP server, so a session behaves identically wherever it is driven from.

---

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
- [AGENTS.md](AGENTS.md) is the deeper architecture guide, and is also what the agent itself reads
  when working on this repository.

---

## Troubleshooting

**`atlas-agent: command not found` after install.** The installer adds `~/.atlas-agent/bin` to your
`PATH` in your shell profile; open a new shell, or `source` your profile.

**"No providers configured".** Set an API key environment variable, run
`atlas-agent login <platform>`, or add one with `atlas-agent provider add`. `atlas-agent doctor`
reports what it can and cannot see.

**A model rejects a parameter (`Unsupported parameter: max_output_tokens`).** Some coding-plan
backends accept a narrower parameter set than the vendor's public API. Atlas Agent handles the known
cases; if you hit a new one, please open an issue with the provider and exact error.

**A subagent fails with "needs the `<name>` model role assigned".** Built-in subagents reference a
model role of the same name. Assign it — `atlas_config` with `set_role`, or `/roles` — or clear the
subagent's `model` field so it runs on the session's own model.

**LSP diagnostics are stale or missing.** `lsp_restart`, or `atlas-agent lsp list` to see which
servers actually came up. `options.auto_lsp` controls automatic startup.

**Windows: shell tests or bash tools misbehave.** If `bash` on your `PATH` resolves to the WSL stub
(`C:\Windows\System32\bash.exe`) without a distribution installed, install Git Bash and ensure it
comes first on `PATH`.

**Everything is too slow / too expensive.** Point `small` and `compact` at a cheap model, lower
`auto_summarize_at`, cap `max_steps_per_turn`, and delegate more (see
[Token economics](#token-economics-why-delegation-is-cheaper)).

---

## FAQ

**Do I need an API key if I already pay for Copilot or ChatGPT?**
No. `atlas-agent login copilot` or `atlas-agent login chatgpt` uses the plan you already have.

**Can it run fully offline?**
Yes, against a local OpenAI-compatible endpoint (Ollama, LM Studio, llama.cpp, vLLM). Tool-calling
quality then depends on the local model.

**Does it send my code anywhere I did not configure?**
No. The only outbound traffic is to the provider you configured, plus whatever `fetch`/`browser`
does when the agent uses them — both governed by the domain allow/block lists.

**Is it only for Go?**
No. The agent, the LSP integration, and the git/infra/testing tooling are language-agnostic. A
subset of the deeper static-analysis tools is Go-specific — those are the ones parsing Go ASTs.

**How is this different from just using the vendor's own CLI?**
Model choice per role, delegation to subagents, multi-round debate, an autonomous goal loop with an
independent judge, and configuration from the conversation. If you only ever use one vendor's model
and one context, the difference is smaller.

**Is the goal loop safe to leave running?**
It is bounded by a turn budget, a step ceiling, an optional cost ceiling, and the same permission
layer as everything else. In `manual` mode it still asks before side effects. In `bypass` mode it
does not — use that only where you would be comfortable letting a script run unattended.

**Can I use it in CI?**
Yes: `atlas-agent run "<prompt>"` is non-interactive. Pair it with `plan` mode or a tight
`allowed_commands` list if it should not change anything.

---

## Known gaps

Where the project is honestly incomplete, and where help is most useful:

- **Scaffolded coding plans.** Claude, Grok, Windsurf and JetBrains have working OAuth but stubbed
  call layers. Finishing one means capturing the real request envelope against the official client
  and filling in `internal/oauth/<plan>` plus the matching provider.
- **Static analysis is Go-first.** `dead_code`, `impact_analysis`, `type_hierarchy`, `code_metrics`,
  `api_surface`, `security_scan`, `anti_pattern_scan`, `generate_tests` and `generate_docstring`
  parse Go. The same shapes exist for other languages; nobody has written them yet.
- **Sandboxing is Windows-only.** `options.sandbox` uses Job Objects; the Linux (cgroups/namespaces)
  and macOS equivalents are unimplemented no-ops.
- **`debate` and `orchestrate` cost real money.** Both scale with agents × rounds. Sensible defaults
  are in place, but there is no budget-aware planner that decides a debate is not worth running.
- **No editor extension.** The LSP integration means Atlas Agent understands your code, but it does
  not live inside your editor.
- **Windows shell edge cases.** Shell dispatch assumes a POSIX-ish `bash`; the WSL stub on a machine
  with no distribution installed produces confusing failures rather than a clear message.

Issues and PRs on any of these are welcome.

---

## Contributing

Issues and pull requests are welcome — bug reports, provider fixes, new tools, documentation, all of
it. A few things that make a PR easy to accept:

- Keep the change focused; one concern per PR.
- Add a test that fails without the change.
- `go build ./...` and `go test ./internal/...` clean (the `internal/shell` suite needs a real bash
  on Windows — see [Troubleshooting](#troubleshooting)).
- Match the surrounding style; comments explain *why*, not *what*.

Contributions are covered by the [Contributor License Agreement](CLA.md) — opening a PR means you
agree to its terms. [AGENTS.md](AGENTS.md) has the architecture overview.

---

## License

MIT — see [LICENSE.md](LICENSE.md).

<div align="center">

**If Atlas Agent is useful to you, a ⭐ helps other people find it.**

[Releases](https://github.com/Omerfaruk-aydn/Atlas-Agent/releases) ·
[Issues](https://github.com/Omerfaruk-aydn/Atlas-Agent/issues) ·
[npm](https://www.npmjs.com/package/@atlas-coder/atlas-agent)

</div>

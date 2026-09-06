# Atlas Agent

**A terminal-first AI coding agent that isn't locked to one model.**
Plug in Claude, GPT, Gemini, a local model, or a flat-rate coding plan
you already pay for — same agent, same workflow, your choice of brain.

[![Release](https://img.shields.io/github/v/release/Omerfaruk-aydn/Atlas-Agent?label=release)](https://github.com/Omerfaruk-aydn/Atlas-Agent/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE.md)
[![npm](https://img.shields.io/npm/v/%40atlas-coder%2Fatlas-agent?label=npm)](https://www.npmjs.com/package/@atlas-coder/atlas-agent)
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

## Why Atlas Agent

- **Bring your own model.** Anthropic, OpenAI, Google, local models via
  Ollama/LM Studio, or a coding-plan subscription you already have
  (Copilot, ChatGPT, Antigravity) — switch anytime, per project or per
  role, without switching tools.
- **Delegate instead of doing it all in one context.** Hand
  self-contained work to a subagent (`agent`), run the same question
  past several at once (`orchestrate`), split a task into parallel
  pieces (`delegate`), or put several models in a multi-round argument
  over a hard call (`debate`) — so your main session's context stays
  small and cheap.
- **Autonomous goals.** `/goal <what to reach>` keeps the agent taking
  its own turns toward an objective — checked by a separate judge model
  before it calls itself done, not just its own say-so — until it's
  reached or its turn budget runs out.
- **Configure it by talking to it.** "Use the cheap model for
  summaries", "put Sonnet on research", "turn the browser tool on" —
  say it in the chat; `atlas_config` makes the change instead of
  sending you to a settings dialog.
- **Cross-platform, single binary.** Windows, macOS (Intel + Apple
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
atlas-agent dirs             # show config directories
atlas-agent login copilot    # sign in to a coding plan
```

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
| --------------------- | -------------------------- | ------------------------------------------- |
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

## License

MIT — see [LICENSE.md](LICENSE.md).

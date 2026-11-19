# Install and first session

Every way to install Atlas Agent, and a first session that shows what it can do.

[Back to the README](../../README.md) · [All guides](README.md)

## Install details

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
[Permissions and safety](safety.md#permissions-and-safety)) so it stops asking about edits.

**4. Try the parts that are not like other agents.**

```text
> /goal every package under ./internal passes `go vet` and `go test`

> debate: should retry logic live in the client or the transport?
  use the backend and review subagents, with planner as judge

> use the cheap model for titles and summaries, and put opus on the review role

> make me a subagent called "frontend-critic" that only reviews UI/UX, nothing else
```

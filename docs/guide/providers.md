# Providers and models

API keys, coding-plan sign-ins, local models, model roles and fallbacks.

[Back to the README](../../README.md) · [All guides](README.md)

## Providers and coding plans

Atlas Agent ships an embedded provider/model catalog, so `atlas-agent models` works before you
configure anything. Providers come in three shapes.

### 1. API-key providers

Set the environment variable and the models appear:

| Provider | Env var | Notes |
| --- | --- | --- |
| Anthropic | `ANTHROPIC_API_KEY` | Claude family |
| OpenAI | `OPENAI_API_KEY` | GPT family, Responses API |
| Google | `GEMINI_API_KEY` | Gemini family |
| xAI (Grok API) | `XAI_API_KEY` | SuperGrok API path |
| DeepSeek | `DEEPSEEK_API_KEY` | DeepSeek-V3/V4 family |
| Kimi Coding (Moonshot) | `KIMI_CODING_API_KEY` | Kimi K3 / Kimi for Coding (API key: `login kimi-coding`; membership: `login kimi`) |
| Moonshot | `MOONSHOT_API_KEY` | Moonshot family |
| Z.ai | `ZAI_API_KEY` | GLM-4 family |
| Zhipu Coding | `ZHIPU_API_KEY` | Zhipu coding plan |
| MiniMax Coding | `MINIMAX_CODING_API_KEY` | MiniMax-M2.7 / M3 |
| NVIDIA NIM | `NVIDIA_API_KEY` | Large hosted open-model catalog |
| OpenCode Zen | `OPENCODE_API_KEY` | OpenCode Zen coding plan |
| OpenCode Go | `OPENCODE_API_KEY` | OpenCode Go coding plan (same key) |
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
| Meta Muse (Code subscription) | `atlas-agent login muse` | **Live** | Device flow, Model API key mint, Spark 1.3/1.2/1.1 |
| Kimi Code (membership) | `atlas-agent login kimi` | **Live** | Device-code OAuth, no API key; see [docs/kimi-code-login.md](../kimi-code-login.md) |
| MiniMax Token Plan (subscription) | `atlas-agent login minimax` (`--region cn` for China) | **Live** | Device-code OAuth + PKCE, no API key; unverified against live servers, see [docs/minimax-plan-login.md](../minimax-plan-login.md) |

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

## Model roles and fallbacks

A **role** is a name a model is assigned to. Subagents reference roles rather than model ids, so
retargeting a whole class of work is one setting rather than a sweep through files.
With `options.router.enabled`, Atlas also picks between a `fast` role and the strong model per
step; see [docs/model-router.md](../model-router.md).

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
// ~/.config/atlas/atlas.json  (Windows: %USERPROFILE%\.config\atlas\atlas.json)
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
way. You do not have to write the file by hand:

```bash
atlas-agent provider add acme --base-url https://api.acme.example/v1 --api-key-env ACME_API_KEY
```

or tell the agent "set up provider acme, fetch its models, the key is in `ACME_API_KEY`, find the
API URL yourself" and it does the same through `atlas_config`. Either way the endpoint is probed and
its models are listed before anything is saved, and your API key never passes through the model.
See [docs/provider-setup.md](../provider-setup.md).

Editing the file directly still works. The one you edit by hand is `~/.config/atlas/atlas.json`
(Windows: `%USERPROFILE%\.config\atlas\atlas.json`); what `provider add`, `login` and `atlas_config`
write is `%LOCALAPPDATA%\atlas\atlas.json` on Windows and `~/.local/share/atlas/atlas.json`
elsewhere. Both are merged.

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

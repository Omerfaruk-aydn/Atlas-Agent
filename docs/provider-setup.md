# Adding a custom provider

Any endpoint that speaks the OpenAI, Anthropic or Google API can be added as a provider without
editing a config file. You can ask the agent, or run one command. Both use the same code, so they
accept and reject the same things.

## Ask the agent

> Set up provider acme, fetch its models. Here is my key: it is in `ACME_API_KEY`. Find the API
> URL yourself.

The agent reads the provider's documentation with its web tools, finds the API base URL, and calls
`atlas_config` with `action: "add_provider"`. You approve one permission prompt that says plainly
where your key will be sent:

```
Add the custom provider "acme" (openai-compat) and list its models.
Your API key will be sent to api.acme.example (key: read from the environment variable ACME_API_KEY ...).
```

The tool then probes the endpoint, lists the models and writes the provider. The result is a short
summary: provider id, type, base URL, how many models, the first ten ids, the scope, and where the
key lives (never the key). Switching to one of the new models is a separate step: ask for it, or
use `/model`.

### Parameters

| Parameter | Meaning |
| --- | --- |
| `id` | Required. Lowercase letters, digits, `-` and `_` only (no dots, spaces or capitals). Ids of built-in providers are reserved. |
| `base_url` | Required. The API root from the provider's documentation. The tool never guesses it. |
| `type` | `openai-compat` (default), `openai`, `anthropic` or `google`. |
| `name` | Display name. Defaults to the id. |
| `api_key_env` | The **name** of an environment variable holding the key. The config stores `$NAME`. |
| `discover_models` | Default `true`: list models from the endpoint before writing. |
| `models` | Explicit model ids. They win over discovered ones. Required when `discover_models` is false. |
| `scope` | `global` (default) or `workspace`. |
| `overwrite` | Replace an existing provider with the same id. Without it a duplicate id is refused. |
| `allow_insecure` | Allow plain `http` to a non-local host. The key then crosses the network unencrypted. |

## Or use the command line

```bash
# The key is in an environment variable (only its name is stored).
atlas-agent provider add acme --base-url https://api.acme.example/v1 --api-key-env ACME_API_KEY

# The key comes from stdin (a password manager, a secret store).
printf %s "$KEY" | atlas-agent provider add acme --base-url https://api.acme.example/v1 --api-key-stdin

# Neither flag, in a terminal: a hidden prompt asks for the key.
atlas-agent provider add acme --base-url https://api.acme.example/v1

# An endpoint without a /models route: list the models yourself.
atlas-agent provider add acme --base-url https://api.acme.example/v1 --api-key-env ACME_API_KEY \
  --no-discover --model acme-large --model acme-small
```

Flags: `--base-url URL` (required), `--type`, `--name`, `--api-key-env VAR`, `--api-key-stdin`,
`--no-discover`, `--model ID` (repeatable), `--scope global|workspace`, `--overwrite`,
`--allow-insecure`. There is deliberately no flag that takes the key itself, so it cannot end up in
your shell history. Check the result with `atlas-agent provider test acme`.

## How your API key is kept away from the model

The agent is a language model: anything in the conversation, including tool inputs and results, is
sent to it. So the key is never in the conversation.

- **Environment variable.** The agent passes only the variable's name. The tool reads the value
  itself and the config stores `$NAME`, never the value.
- **Masked prompt.** If the variable is not set, the tool asks you for the key in a masked input in
  the terminal UI, through a separate channel from the chat questions. The typed value goes
  straight to the tool's Go code. It is not a message, not a tool result and not logged, and the
  agent never sees it. It is stored in your global config file (mode `0600`) in the same place
  `atlas-agent login` keeps keys.
- **No prompt available** (headless runs, remote clients, a server-backed session): the tool fails
  with a clear message asking you to set an environment variable and give its name.
- **Redaction.** Every error and note passes through a filter that replaces the key (plain,
  URL-encoded and base64) with `[redacted]`, because upstream error pages sometimes quote it back.
  The probe sends the key only in a header, never in the URL.
- If you paste a key into the chat anyway, it is already in the conversation. Treat it as exposed
  and rotate it.

## What the tool checks

1. **The inputs**: id format and collisions, a valid `https` URL without credentials, fragment or
   query, a supported type, and a real environment variable *name*.
2. **The endpoint.** `GET <base_url>/models` with the right authentication for the type (Bearer for
   `openai-compat` and `openai`; `x-api-key` and `anthropic-version` for `anthropic`;
   `x-goog-api-key` for `google`), a 15 second timeout, a 4 MB response cap and a limit of 500
   models. If `/models` is a 404 and the URL has no `/v1`, it retries once with `/v1/models` and
   tells you the corrected URL, which is what gets saved.
3. **The key.** 401 and 403 say the key is wrong; 404 says the URL is probably wrong; DNS, TLS and
   timeouts have their own messages.
4. **Nothing is written on any failure.** The provider is written in one step, through the same
   validation as every other config write, only after the probe succeeded.

Context window and output limits are filled in only when the endpoint reports them. Otherwise they
are left at their defaults; nothing is invented.

### Safety rules

- `https` is required. Plain `http` is accepted for `localhost`, `127.0.0.1`, `[::1]` and
  `*.localhost` (Ollama, LM Studio, vLLM on your machine). For anything else it needs
  `allow_insecure`.
- Redirects are followed only within the same host. A redirect to another host stops the probe, so
  the key is never sent to a host you did not name.
- A `workspace`-scoped provider needs `api_key_env`: a key written inside a project can be committed
  by accident.
- `--no-discover` skips the network entirely, so the key and URL are *not* verified. The summary says
  so. Run `atlas-agent provider test <id>` afterwards.

## Where the provider is saved

| Scope | File |
| --- | --- |
| `global` | Windows `%LOCALAPPDATA%\atlas\atlas.json`; Linux and macOS `~/.local/share/atlas/atlas.json` |
| `workspace` | `.atlas/atlas.json` in the project |

Files you edit by hand live elsewhere (`%USERPROFILE%\.config\atlas\atlas.json` on Windows,
`~/.config/atlas/atlas.json` otherwise) and are merged with these. `atlas-agent config paths` lists
what was found.

## Reasoning effort

While listing models the tool also records which of them support reasoning effort, using only what
the endpoint already told it. No model call is made to find out.

- The `/models` metadata is read best-effort: OpenRouter-style `supported_parameters` containing
  `reasoning`, `reasoning_effort` or `include_reasoning`; flags such as `reasoning`,
  `supports_reasoning`, `can_reason`, `thinking` or `capabilities.reasoning`; and level lists such
  as `reasoning_efforts`, `reasoning_levels` or `supported_reasoning_efforts`.
- Only levels Atlas knows are kept (`none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`).
  If a model is reported as reasoning without usable levels, `low`, `medium` and `high` are used.
  The default effort is `medium` when offered, otherwise the middle level.
- If the metadata says nothing, a model whose id exactly matches a model in the built-in catalog
  reuses that model's reasoning settings. This also applies to ids you pass in `models`.
- Every other model stays standard, with no reasoning fields. Support is never guessed.

The summary reports `reasoning effort: N of M models` when any model qualifies.

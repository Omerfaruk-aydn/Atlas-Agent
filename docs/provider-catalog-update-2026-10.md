# Provider catalog update — October 1, 2026

This is the initial update record. See the [final catalog and connection report](provider-catalog-fixes-2026-10-01.md) for completed coverage, corrected pricing, and final verification results.

This update adds verified current tool-capable text models from
OpenAI, Anthropic, DeepSeek, Google, xAI, GitHub Copilot, OpenCode Go/Zen,
and OpenRouter. Model availability still depends on the account, region,
plan, and provider. Adding a model to a catalog does not grant access.

## Xiaomi MiMo

Choose the provider matching your key. API and Token Plan keys are separate;
Token Plan requests must use the endpoint shown in your Xiaomi console.

| Provider ID | Authentication | Endpoint |
| --- | --- | --- |
| `xiaomi` | Pay-as-you-go API key | `https://api.xiaomimimo.com/v1` |
| `xiaomi-token-plan-sgp` | Singapore plan key | `https://token-plan-sgp.xiaomimimo.com/v1` |
| `xiaomi-token-plan-ams` | Europe plan key | `https://token-plan-ams.xiaomimimo.com/v1` |
| `xiaomi-token-plan-cn` | China plan key | `https://token-plan-cn.xiaomimimo.com/v1` |

Configure a key with hidden interactive entry:

```sh
atlas-agent login xiaomi
atlas-agent login xiaomi-token-plan-ams
atlas-agent login --list
```

For scripted input, use `login <provider> --api-key-stdin`. Keys are saved
through the existing global configuration service. `--force` replaces an
existing key. `login mimo` aliases `xiaomi`; `login xiaomi-token-plan` and
`login mimo-token-plan` select Singapore. Use a regional ID to select
Europe or China explicitly.

Environment variables are also supported:

```sh
export XIAOMI_API_KEY="your-api-key"
export XIAOMI_TOKEN_PLAN_AMS_API_KEY="your-europe-plan-key"
export XIAOMI_TOKEN_PLAN_SGP_API_KEY="your-singapore-plan-key"
export XIAOMI_TOKEN_PLAN_CN_API_KEY="your-china-plan-key"
```

The API includes MiMo-V2.6-Pro, MiMo-V2.6-Flash,
MiMo-V2.6-Pro-UltraSpeed, MiMo-V2.5-Pro, and MiMo-V2.5. Token Plan includes
the four standard text models; UltraSpeed requires separate API access.
The default models are V2.6-Pro and V2.6-Flash. Xiaomi schedules V2.5-Pro
and V2.5 retirement for October 21, 2026.

MiMo uses `thinking.type` (`enabled` / `disabled`), rather than reasoning
effort levels. Atlas maps its thinking toggle to this field and preserves
`reasoning_content` in assistant tool-call history. Existing explicit
`provider_options.extra_body.thinking` settings take precedence.

Token Plan models have zero per-token dollar estimates in Atlas because
they use prepaid subscription credits. This does not mean the plan is free,
and Atlas does not track Xiaomi's remaining credit balance.

## Account sign-in and model protocols

Existing OAuth/account sign-in flows remain available through `login` /
`auth`. API-key and plan-key providers now use the same command with hidden
key entry. Xiaomi and OpenCode authenticate with console-issued keys;
they do not use a newly invented OAuth flow.

GPT-6 models retain reasoning settings in the Responses API. Current
Claude Fable/Opus/Sonnet models use adaptive thinking. Copilot routes
GPT-6 models through Responses. OpenCode selects Responses, Anthropic
Messages, Google GenerateContent, or Chat Completions per model; Go and
Zen have different routes for MiniMax and Qwen3.8-Max.

## Sources

- [Xiaomi Token Plan models, regional endpoints, and keys](https://mimo.mi.com/docs/en-US/tokenplan/Token%20Plan/subscription)
- [MiMo-V2.6-Pro](https://mimo.mi.com/models/en-US/mimo-v2.6-pro)
- [MiMo-V2.6-Flash](https://mimo.mi.com/models/en-US/mimo-v2.6-flash)
- [MiMo-V2.6-Pro-UltraSpeed](https://mimo.mi.com/models/en-US/mimo-v2.6-pro-ultraspeed)
- [MiMo deep thinking and tool-history requirements](https://mimo.mi.com/docs/en-US/quick-start/usage-guide/other/deep-thinking)
- [OpenAI API models](https://developers.openai.com/api/docs/models/all)
- [Claude models](https://platform.claude.com/docs/en/models/overview)
- [DeepSeek models, aliases, and peak/off-peak pricing](https://api-docs.deepseek.com/quick_start/pricing/)
- [Gemini 3.8 Flash](https://ai.google.dev/gemini-api/docs/models/gemini-3.8-flash)
- [Grok 4.7](https://docs.x.ai/developers/models/grok-4.7)
- [GitHub Copilot supported models](https://docs.github.com/en/copilot/reference/ai-models/supported-models)
- [OpenCode Go models and protocols](https://opencode.ai/docs/go/)
- [OpenCode Zen models and protocols](https://opencode.ai/docs/zen/)
- [OpenRouter public model catalog](https://openrouter.ai/api/v1/models)

OpenRouter entries were obtained from its public catalog, restricted to
models with text input/output and tool support. Entries absent from the
current public list were removed from the bundled selection catalog.
The flat cost fields cannot express time-based or long-context pricing
tiers; DeepSeek's new direct entries use peak rates as an estimate.

The subsequent [catalog correction report](provider-catalog-fixes-2026-10-01.md)
records endpoint repairs, subscription additions, unsupported account
adapter exclusions, and the remaining verification limits.

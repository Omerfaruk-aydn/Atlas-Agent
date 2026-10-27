# Provider catalog update — October 27, 2026

Delta review since the October 20 catalog. Sources were fetched on
2026-10-27. A route missing from a public list is only removed when that
list is the gateway's own routing source (OpenRouter, Vercel, Venice,
Cortecs, AtlasCloud, Chutes, Neuralwatt, NVIDIA NIM, Hugging Face router,
OpenCode Go/Zen). Direct vendor APIs change only on a sourced notice.

## Added

- `gpt-6.1-terra`: OpenAI API, ChatGPT sign-in, Copilot, Azure, Bedrock
  OpenAI (`us.` profile only), OpenRouter, Vercel, Venice. Copilot routes it
  through Responses (`copilotResponsesModels`).
- Kimi K3.1 Pro (and Highspeed): Moonshot API, Kimi Code (`k3.1-pro`),
  OpenRouter, Vercel, Venice. Moonshot prices are Vercel's pass-through
  rates; verify against the Moonshot price table.
- Perplexity Agent: Claude Fable 5, Opus 5, Sonnet 5, GPT-5.5, Gemini
  3.8/3.7/3.6 Flash, Grok 4.7/4.6/4.5, and Perplexity-hosted Kimi K3, GLM
  5.3, GLM 5.3 Flash, DeepSeek V4 Pro 0813 and Nemotron 3.5 Super.
- Cortecs (7), Avian (5), IO (3), AtlasCloud (1), Vercel (7 incl. DeepSeek
  V4.1 Flash Fast), Venice (4), OpenRouter (5), Hugging Face (4).

## Retired or deprecated (sourced)

- OpenAI API: retired `gpt-5.2-codex`, `gpt-5.1-codex(-max/-mini)`,
  `gpt-5-codex`, `o4-mini`, `o3-mini`, `gpt-4.1-nano`; deprecated
  (2026-12-11) `gpt-5`, `gpt-5-mini`, `gpt-5-nano`, `o3`.
- Azure: `o1`, `o3`, `o3-pro`, `o3-mini`, `o4-mini` (2026-11-19),
  `codex-mini` (2026-11-15), GPT-4.1/4o families (2027-04-14).
- Vertex AI: Gemini 2.5 Pro/Flash/Flash-Lite retired 2026-10-20; 3.6 Flash
  retires 2026-11-19; 3.7 Flash 2027-01-28.
- Moonshot: `kimi-k2.5` and all `moonshot-v1-*` retired 2026-08-31.
- Xiaomi (API and all three Token Plans): `mimo-v2.5` and `mimo-v2.5-pro`
  went offline 2026-10-21.
- Alibaba Coding Plan (intl and CN): reduced to the six models Alibaba
  lists; `qwen3-max-2026-01-23`, `qwen3-coder-next`, `qwen3-coder-plus` and
  `glm-4.7` removed. Tests updated to match.

## Removed because the gateway no longer lists them

OpenRouter 19, Vercel 21, Cortecs 10, AtlasCloud 7, Hugging Face 47 routes,
OpenCode Go 14, OpenCode Zen 12, Chutes 2, Venice 2, Neuralwatt 3, NVIDIA
NIM 1 (`moonshotai/kimi-k3`).

## Verified, unchanged

Anthropic API, Claude sign-in, Gemini API (3.7/3.5 Flash are auto-routed
aliases), Antigravity (GPT-OSS and Claude 4.6 leave 2026-11-02), Copilot,
xAI, DeepSeek, Mistral, Cohere, Cerebras, Meta/Muse, MiniMax, Z.AI.

## Not verifiable without an account key or reachable docs

Scaleway (docs return 403), Nebius, Baseten, Fireworks, Groq extras
(Llama 3.3 70B, Llama 3.1 8B have no price on the public page), Qiniu
prices, Alibaba Singapore/US/Token Plan lists, Chutes routes without a
published price (7 skipped), and AtlasCloud Kimi K3.1 (not ready yet).
Cohere `command-a-vision`, GLM-5.3-FlashX and `gpt-5.2-pro` are published
but not yet catalogued.

## Sources

- [OpenAI deprecations](https://developers.openai.com/api/docs/deprecations)
- [Codex models](https://learn.chatgpt.com/docs/models)
- [Claude deprecations](https://platform.claude.com/docs/en/about-claude/model-deprecations)
- [Gemini deprecations](https://ai.google.dev/gemini-api/docs/deprecations)
- [Vertex model lifecycle](https://docs.cloud.google.com/vertex-ai/generative-ai/docs/learn/model-versions)
- [Azure retirement schedule](https://learn.microsoft.com/en-us/azure/foundry/openai/concepts/model-retirement-schedule)
- [Kimi platform models](https://platform.kimi.ai/docs/models)
- [Xiaomi Token Plan](https://mimo.mi.com/docs/en-US/tokenplan/Token%20Plan/subscription)
- [Alibaba Coding Plan](https://www.alibabacloud.com/help/en/model-studio/coding-plan)
- [Perplexity models](https://docs.perplexity.ai/docs/getting-started/models)
- [Antigravity models](https://antigravity.google/docs/models)
- Public model lists of each gateway named above.

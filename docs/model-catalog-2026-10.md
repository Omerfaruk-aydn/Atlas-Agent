# October 2026 model catalog update

The October 20 review compared 78 catalog definitions and 2,253 model records.
This update adds 148 model routes across 30 definitions, including the new
Perplexity Agent API connection. A route is a model on one provider; these
are not 147 distinct newly released models.

## Availability rules

Model `lifecycle` metadata records `status`, `source_url`, `verified_at`, an
optional `retirement_date`, and `replacement`. The statuses are:

- `retired`: the provider has confirmed the route is closed.
- `deprecated`: the route remains selectable until its retirement date.
- `alias`: the old identifier routes to a documented replacement.

The catalog filters sourced retired records and repairs retired defaults
using an available replacement. Saved model overrides cannot restore closed
built-in routes on their original host. Unrelated custom endpoints are not
subject to another provider's retirement rules. A missing public listing is
not treated as retirement. Dates are evaluated in UTC when catalogs load;
restart Atlas after a retirement boundary to refresh the available list.

Confirmed Anthropic API and Copilot retirements are recorded separately.
OpenAI API, Scaleway, and Antigravity future retirement dates remain scheduled,
not immediate removals. API access is independent of subscription access.

## Important additions

- Haiku 5.5: Anthropic, Vertex, and the documented base Bedrock identifier.
  Regional inference-profile access must be enabled in the AWS account.
- Gemini 4 Argon: Gemini, Vertex, and Antigravity. The direct adapter keeps
  matching tool IDs, omits unsupported sampling controls, and rejects explicit
  cached-content references. Synchronous output defaults to 65,536 tokens;
  long-decode/background execution is not added by this update.
- MiniMax M3.1 Flash: the stable API and M-Plan identifier. The old Preview
  identifier is labeled as an alias and normalized before a Messages call.
  Thinking is adaptive and mandatory; unsupported efforts fail before HTTP.
- Mistral Large 4, Muse Wonder, and Muse Wonder Full. Full is subscription-only
  and requires a qualifying Muse Code plan; it is absent from the native Meta
  API catalog. Muse Spark Open uses each host's documented identifier.
  Nebius Spark Open is exposed through a separate `nebius-us-north1`
  connection using its documented regional endpoint; the existing Nebius
  connection is not silently moved to another region.
- DeepSeek Flash 1017 on Fireworks and verified China Alibaba plans. The
  native `deepseek-flash` identifier already selects the updated model.
- Public tool-capable text routes on OpenRouter, Vercel, Synthetic,
  AtlasCloud, IO, Neuralwatt, Venice, and Hugging Face. Provider-specific
  context, price, modality, and effort metadata is used where published.

Image/audio generators, embeddings, batch-only entries, internal backup/test
routes, and unverified plan entitlements are not imported wholesale.

## Plan distinctions

Antigravity defaults to Gemini 3.8 Flash, which the published table makes
available across plans. Older Enterprise-only Gemini routes are labeled.
Argon requires paid Pro, Ultra, or Enterprise access. Existing restricted
routes are not silently upgraded to a more expensive account tier.

The unverified Singapore Alibaba Team connection is not selectable: the
published Team Plan currently specifies Beijing only. A separately named
custom connection can be configured if the operator has a confirmed endpoint.
Existing Kimi and Xiaomi plan entries are not replaced by models from a
different subscription or API region.

Copilot GPT-5.4 nano is not added to the CLI: the official listing limits it
to the Codex VS Code extension on the specified plan. Copilot annual-plan
Sonnet 4.6 remains labeled for its documented exception.

## Perplexity

`perplexity-agent` uses `https://api.perplexity.ai/v1` and the Responses API
transport. It shares the `PERPLEXITY_API_KEY` variable with the legacy
connection, but has its own provider ID and explicit model identifiers.
The `perplexity` Sonar connection remains unchanged for existing saved
configurations and is labeled as legacy compatibility. No existing request
is silently migrated between those APIs.

OpenCode built-in API connections use the documented `/inference` routes,
with separate OpenAI, Anthropic, and Google transports and a separate Go
subscription prefix. Existing private proxy URLs remain unchanged. The
M3.1 Flash free route uses Chat Completions, including on Go; it is not sent
to the older M3 Messages route. Zen paid additions retain their published
token prices instead of inheriting Go's subscription cost metadata.

## Limits

No paid inference, private account model-list request, or live subscription
acceptance was performed. HTTP contract tests validate request construction
against local servers, not vendor entitlement. Tiered/introductory prices
can differ from the base per-token estimates stored in the existing catalog.
Scaleway publishes EUR prices; its existing USD conversion estimate is
retained, with the newly published Flash input/output/cache rates applied.

Augment, Factory, CodeRabbit, JetBrains, Windsurf, Zed, and Grok web adapters
remain unavailable for model inference. They were already excluded by
`UnavailableProviderReason` before this update. Their placeholder protocol
implementations are not presented as working integrations. Completing those
adapters requires a verified request/stream/authentication contract.

## Sources

- [Haiku migration](https://platform.claude.com/docs/en/models/haiku-5-5/migration-guide)
- [Anthropic retirements](https://platform.claude.com/docs/en/about-claude/model-deprecations)
- [Gemini Argon](https://ai.google.dev/gemini-api/docs/models/gemini-4-argon)
- [Antigravity availability](https://antigravity.google/docs/models)
- [MiniMax Messages API](https://platform.minimax.io/docs/api-reference/text-anthropic-api)
- [MiniMax M-Plan](https://platform.minimax.io/docs/m-plan/intro)
- [Mistral Large 4](https://docs.mistral.ai/models/mistral-large)
- [Meta models](https://dev.meta.ai/docs/models)
- [Meta deployment identifiers](https://dev.meta.ai/docs/muse-spark-open/deploy)
- [Copilot support and retirements](https://docs.github.com/en/copilot/reference/ai-models/supported-models)
- [OpenAI API retirements](https://developers.openai.com/api/docs/deprecations)
- [Perplexity Responses compatibility](https://docs.perplexity.ai/docs/agent-api/openai-compatibility)
- [Alibaba Team Plan](https://help.aliyun.com/zh/model-studio/token-plan-team-overview)
- [Scaleway models](https://www.scaleway.com/en/docs/generative-apis/reference-content/supported-models/)

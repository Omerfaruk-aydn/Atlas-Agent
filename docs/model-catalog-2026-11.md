# November 2026 model catalog update

Reviewed on 2026-11-18 against each vendor's own documentation. A route is a
model on one provider; the entries below are the models released or changed in
November that a vendor page confirmed.

## Added

| Model | Where | Verified from |
| --- | --- | --- |
| Claude Fable 5.2 (`claude-fable-5-2`) | Anthropic API, Claude subscription, Vertex AI, Bedrock global (`global.anthropic.claude-fable-5-2`, US and Europe lists), Copilot | platform.claude.com model overview and Fable 5.2 page |
| Claude Mythos 5.2 (`claude-mythos-5-2`, invite only) | Anthropic API | platform.claude.com model overview |
| GPT-6.2 Astra (`gpt-6.2-astra`), GPT-6.2 Sol (`gpt-6.2-sol`) | OpenAI API, ChatGPT (Codex) subscription, Copilot | developers.openai.com model pages, ChatGPT model documentation |
| Gemini 4 Carbon (`gemini-4-carbon`), Gemini 4 Hydrogen (`gemini-4-hydrogen`) | Gemini API, Copilot | aistudio.google.com model and pricing pages |
| Qwen4 Max (`qwen4-max`) | Alibaba (Singapore) | Alibaba Model Studio price page |
| MAI Code 2 (`mai-code-2`) | Copilot | GitHub Copilot supported-models page |

### Prices and limits

- **Fable 5.2** (and Mythos 5.2): $8 input, $40 output, $10 five-minute cache
  write, $0.20 cache read per million tokens; 1M context, 128K output;
  adaptive thinking is always on and the default effort is `high`. It needs
  30-day data retention, so it is not available under zero data retention.
  Cloud routes use the same base price. Anthropic retirement: not sooner than
  2027-11-09.
- **GPT-6.2 Astra**: $8 / $40, $0.40 cached input, $10 cache write. **GPT-6.2
  Sol**: $2 / $10, $0.10 cached input, $2.50 cache write. Both: 1,050,000
  context, 128,000 output, text and image input, Responses and Chat
  Completions. The API rejects reasoning effort `none`, so it is not offered.
  OpenAI's pages list no other effort levels; the catalog keeps the five
  levels of the GPT-6.1 family.
- **Gemini 4 Carbon and Argon**: both stable, 1,048,576 context, 65,536
  default output (up to 1,048,576 with long decode), thinking `low` to `high`.
  Google lists a launch price of $2 / $10 that rises to $4 / $20 (cached $0.10
  to $0.20) on 2026-12-01. The catalog records $4 / $20 so that costs and
  session limits are not understated after the promotion; Argon moves with
  it. **Gemini 4 Hydrogen**: $1.50 / $7.50, cached $0.075, thinking `low` to
  `xhigh`. All three support implicit caching only.
- **Qwen4 Max**: $1.50 / $4.50, five-minute cache write $1.875, cache hit
  $0.075, thinking and non-thinking modes, no input-length tiers up to 1M
  tokens. Alibaba's price page does not state the context or output limits;
  the entry uses 1,000,000 and 131,072, matching Qwen3.8 Max, and lists no
  effort levels.

## Plans (subscription sign-in)

- **Claude**: Fable 5.2 is listed. It counts toward Max plan limits; on Pro it
  runs on usage credits. The default stays Opus 5.5.
- **ChatGPT (Codex)**: GPT-6.2 Astra and Sol are listed. OpenAI documents them
  for "supported plans" without naming the plans, so the default stays GPT-6.1
  Sol.
- **Copilot**: Fable 5.2 needs an administrator to enable it for the
  organization. GitHub lists GPT-6.2 Astra and Sol, Gemini 4 Carbon, Argon and
  Hydrogen, MAI Code 2 and MAI Code 2 Flash as generally available. GPT-6.2
  routes through the Responses API like the other GPT-6 models.
- **Antigravity**: the published table makes Carbon Ultra and Enterprise only,
  Argon Pro (paid), Ultra and Enterprise, and Hydrogen Pro (non-trial), Ultra
  and Enterprise. Carbon and Hydrogen were added afterwards on the same routing as Argon (CLI client, low/medium/high suffix; not live-tested). Earlier note: the Antigravity
  adapter routes Argon through the CLI client and keys effort to the route
  name, and that can only be confirmed with a live account. GPT-OSS 120B is
  removed on 2026-11-18 (the table now says so; the catalog had 2026-11-02),
  with Claude Sonnet 4.6 and Opus 4.6, which the catalog does not list.

## Retirements in the catalog

- `claude-sonnet-4-5-20250929` retires on 2026-11-30 (replacement
  `claude-sonnet-5-5`); the record was already present and is filtered on that
  date.
- `claude-opus-4-5-20251101` is "not sooner than 2026-11-24"; it stays.

## Not added

Checked, but the vendor pages did not give enough to add them safely:

- **Muse Ember** (`muse-ember-1.0`): Meta's model list confirms the ID, but
  context, output and price are only on pages that could not be read.
- **Tencent Hy4 Flash**, **Cognition SWE-2 Flash**, **MAI Code 2 Flash**: no
  vendor specifications found.
- **GPT-6 Cyber**: limited to the Daybreak program.
- **Coding plans** (Kimi Code, Z.ai, MiniMax, Alibaba, Zhipu): no November
  model change was confirmed.
- **Provider aggregators** (OpenRouter, Vercel, Synthetic, and similar) are
  not changed; their lists follow their own publication.

MAI Code 2 is listed with 1M context and no effort levels because GitHub
documents "configurable reasoning" without naming the levels; its 32,768
default output is a conservative Atlas value.

## Tests

`internal/deps/atlas-models/internal/providers/november_catalog_test.go`
locks the prices, limits, defaults and the retirement dates; the Google
adapter rejects an explicit cache reference for every Gemini 4 model.

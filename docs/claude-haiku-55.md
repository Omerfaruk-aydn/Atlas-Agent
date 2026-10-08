# Claude Haiku 5.5 account integration

Verified on 2026-10-07 using the official authenticated Models API:
`GET https://api.anthropic.com/v1/models/claude-haiku-5-5`.
The endpoint returned the canonical ID `claude-haiku-5-5`, display name
`Claude Haiku 5.5`, and publication timestamp `2026-10-07T18:00:00Z`.
Public model overview pages had not yet listed this release when checked.

## Verified capabilities

- Input limit: 1,000,000 tokens.
- Maximum output: 128,000 tokens.
- Image and PDF input supported.
- Effort: `low`, `medium`, `high`, `xhigh`, `max`.
- Adaptive and disabled thinking supported; fixed-budget enabled thinking
  is not supported.

Atlas exposes all five effort choices in its Claude account catalog.
`medium` is Atlas's local default, not a claim about the upstream default.
The account catalog uses zero API prices because this provider uses a Claude
subscription; this does not imply that direct Anthropic API calls are free.
Haiku 4.5 remains available. The embedded default small model for new Claude
account configurations is Haiku 5.5; existing user selections are preserved.

The Anthropic adapter selects adaptive thinking for Haiku 5.5, including
older caller options that request a fixed thinking budget. This avoids
sending an unsupported `thinking.type=enabled` request.

## Verification

Catalog, coordinator effort routing, and account wire-contract tests run
without network access. An opt-in streaming test exercises all five efforts
using synthetic text only. Set `ATLAS_CLAUDE_LIVE=1`,
`ATLAS_CLAUDE_TOKEN`, and `ATLAS_CLAUDE_ACCOUNT` in the test process, then run:

```powershell
go test ./internal/deps/atlas-llm/providers/claude -run '^TestLiveHaiku55Account$' -count=1 -v
```

The initial Messages API attempt on 2026-10-07 found an expired saved access
token. After renewing it through Atlas's normal credential store, live
streaming requests passed for all five effort levels (`low`, `medium`,
`high`, `xhigh`, `max`). Each synthetic request returned `OK` and a normal
stop reason. This check did not exercise image input or tool use.

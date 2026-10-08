# Antigravity account models

Verified on October 7, 2026 against [Google's model documentation](https://antigravity.google/docs/models)
and the signed-in Pro account's `v1internal:fetchAvailableModels` response.
Account limits below are specific to Antigravity, not the public provider APIs.

| Atlas model ID | Account routes | Context tokens | Maximum output | Images |
| --- | --- | ---: | ---: | --- |
| `claude-sonnet-5-5` | `claude-sonnet-5-5-{low,medium,high}` | 1,000,000 | 128,000 | Yes |
| `claude-opus-5-5` | `claude-opus-5-5-{low,medium,high}` | 1,000,000 | 128,000 | Yes |
| `gpt-oss-120b` | `gpt-oss-120b-medium` | 131,072 | 32,768 | No |

All three default to Medium. Effort is selected through the model route;
Gemini `thinkingLevel` is not sent to these routes. They use the same
account envelope and SSE transport as Gemini, with server-side translation.
Tool IDs, signed reasoning and tool results continue through that envelope.
Costs remain zero in Atlas's API cost estimator because this connection uses
an account subscription; subscription limits and AI credits still apply.

Sonnet 5.5 and Opus 5.5 require Ultra or a non-trial Pro subscription.
Google lists GPT-OSS 120B for removal on November 2, 2026. Older Claude 4.6
models also appear in the public retirement list, but were not returned by
the verified Pro account and are not added as aliases for newer models.

Local regression tests cover catalog capabilities, all supported effort
routes, rejected effort/input combinations and signed tool conversations.
The opt-in `TestLiveThirdPartyAccount` sends synthetic inputs only and checks
one tool call followed by a streamed final answer for each model. It requires
`ATLAS_ANTIGRAVITY_LIVE=1`, `ATLAS_ANTIGRAVITY_TOKEN` and
`ATLAS_ANTIGRAVITY_PROJECT` in the test process; never commit credentials.

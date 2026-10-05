# Provider fixes V26

- Muse Messages tool schemas are adapted to a maximum JSON nesting depth of
  10. Shallow schemas stay intact; deeper constraints remain in descriptions.
  Original local schemas remain unchanged for argument validation. The option
  is enabled only for Muse, leaving ordinary Anthropic serialization intact.
- OpenCode Go/Zen adapters always identify Atlas and send a stable session
  header. Conversation headers take precedence over the provider fallback.
- A live request to the documented MiMo V2.6 Flash Free endpoint with Atlas's
  own User-Agent and session header returned HTTP 403 `FreeTierError` on
  2026-10-06. This is an upstream restriction, not a solved connectivity issue.
  Atlas now explains the restriction and treats it as non-retryable, including
  when a response otherwise requests retries. It does not impersonate OpenCode.

Follow-up on 2026-10-06: a second direct MiMo V2.6 Flash Free request included
Atlas's own client identifier, request identifier, stable session header, a
coding task and a function tool. It still returned the same HTTP 403 gate.
The installed genuine OpenCode CLI 1.18.34 successfully ran the same free
model (`opencode run --pure --model opencode/mimo-v2.6-flash-free`), returned
`OK`, and reported zero cost. The inspected default OpenCode auth store had
no `opencode` account/key entry; this does not establish the absence of other
credential sources. CLI success does not mean direct Atlas API access works.

References: [OpenCode Go client requirements](https://opencode.ai/docs/go/#where-can-i-use-it)
and [Zen free models](https://opencode.ai/docs/zen/).

Verification covers actual serialized Muse Generate/Stream requests, nested
properties/arrays/unions, unchanged shallow/local schemas, OpenCode request
identity/session stability and per-conversation overrides, and restricted-error
handling. Direct Muse account access was not tested live.

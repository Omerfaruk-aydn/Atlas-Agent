# Xiaomi MiMo reasoning effort

Xiaomi API and the China, Singapore and Amsterdam Token Plan providers expose
`/effort` for their embedded MiMo models. Supported compatibility values are
`none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max` and `ultra`.
The catalog defaults to `high`, corresponding to enabled thinking.

According to Xiaomi's current API contract, `none` disables thinking. All other
values enable thinking with identical reasoning intensity. Selecting `high`
instead of `low` currently does not imply deeper reasoning or a larger budget.
See the [official reasoning contract](https://mimo.mi.com/docs/en-US/api/chat/responses).

Atlas uses Chat Completions for these providers, so it translates the selection
to the native `thinking.type` field rather than sending an unsupported
`reasoning_effort` field. A selected effort takes precedence over the legacy
`Think` toggle. Explicit `extra_body.thinking` provider/model overrides still
take precedence; remove that override to control thinking through `/effort`.

After changing versions, restart Atlas to load the embedded catalog. User-defined
model metadata or an explicitly configured external catalog may supply different
reasoning levels. These changes do not alter credentials, regional endpoints,
prices or model identifiers.

Validation covers the embedded catalog, all selectable values and actual local
HTTP request serialization for API and all three Token Plan providers. It does
not claim a paid-account live inference test.

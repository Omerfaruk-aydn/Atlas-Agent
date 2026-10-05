# OpenCode CLI integration

Atlas automatically uses the genuine installed OpenCode CLI for OpenCode Zen
models whose IDs end in `-free`, and for `big-pickle`, when using the official
HTTPS endpoint. Paid models and custom API gateways keep their direct API
transport. No OpenCode identity is fabricated for Atlas HTTP requests.

Install the official CLI with `npm install -g opencode-ai`. Version 1.18.34 or
newer is required. Windows npm installations are resolved to their native
`opencode.exe`; no shell command is built from model output or user prompts.

Optional provider settings in `atlas.json`:

```json
{
  "providers": {
    "opencode-zen": {
      "opencode_transport": "auto",
      "opencode_executable": "C:/npm-global/node_modules/opencode-ai/bin/opencode.exe"
    }
  }
}
```

`auto` selects CLI for the free models described above. `api` forces the
original direct API path. `cli` explicitly selects CLI for all models on this
provider. The CLI uses its own credential store and environment; Atlas API keys
are not copied into it. Paid CLI models therefore need OpenCode authentication.

## Execution boundary

Each model turn runs in a fresh temporary directory and fresh OpenCode session.
Atlas supplies its full current conversation, including real tool results;
OpenCode's previous sessions are never silently reused. Credentials remain in
the CLI's existing data store. Project/global configuration and plugins are
isolated; sharing, snapshots and autoupdates are disabled.

OpenCode retains its genuine build agent. All native tool permissions require
approval; `run` in non-interactive mode auto-rejects those requests and Atlas
never passes `--auto`. Any native tool event aborts the bridge. The CLI model
instead returns a JSON reply containing text and requested Atlas tool calls.
Atlas verifies the envelope, tool names, argument schemas and tool choice
before admitting the whole response. Hooks, permissions, desktop guards and
ordinary tool execution remain in the Atlas executor. Failed or partial CLI
responses never trigger tools or silently retry a mutation.

Cancellation kills the CLI process; requests also have a five-minute ceiling.
Output is bounded and missing completion events are errors. Windows launches
do not create a separate visible console window.

## Current limits and verification

- CLI text events arrive as completed parts. Atlas's stream interface works,
  but this transport does not provide token-by-token display.
- Image/file attachments, media tool results and provider-native tools are
  explicitly rejected. Use a direct API model for those requests. They are
  never silently dropped or converted into unviewable base64 prose.
- OpenCode controls sampling/output limits. Atlas emits a warning for sampling
  overrides it cannot apply. Selected reasoning effort is forwarded as a CLI
  variant; supported variants are defined by OpenCode for each model.
- Tested live on 2026-10-06 with OpenCode 1.18.34 and MiMo V2.6 Flash Free:
  text answer, Atlas tool request, a full agent loop with a real tool result,
  blocked native write and cancellation. Other free models use the same route
  but were not all tested live. Availability remains subject to OpenCode.

References: [OpenCode run command](https://opencode.ai/docs/cli/),
[configuration](https://opencode.ai/docs/config/), and
[non-interactive permission handling](https://github.com/anomalyco/opencode/blob/dev/packages/opencode/src/cli/cmd/run.ts).

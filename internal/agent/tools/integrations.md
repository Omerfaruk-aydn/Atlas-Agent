Built-in Atlas tool: read what Atlas can reach — model providers, signed-in accounts, MCP servers, local programs, local app runtimes and the local apps Atlas manages — from the same service the user's /integrations screen uses, and run their free health checks.

<usage>
- `summary` first: counts per kind and status, plus the items that need attention.
- `get` {id} for one item: what it can really do, where its configuration lives (and whether Atlas may edit it), the last check, the error and the next step.
- `list` {kind?, status?, query?} for compact rows. Catalog providers that are not set up are hidden unless `include_catalog` is true.
- `check` {id} or {kind} runs the item's free check: providers list their models, accounts check (and if expired refresh) their token, MCP servers connect and list tools, programs report their version. No model and no MCP tool is ever called. Checks of providers, accounts and MCP servers ask the user first.
</usage>

<rules>
- Configured is not usable: only `ready` means the last live check or the running subsystem said so. `unchecked` means nobody checked since Atlas started; a `previous` entry is history from before a restart, never the current state.
- Never ask the user to paste secrets into the chat. Credentials are set up in /model, MCP sign-in or the app's own settings.
- When an item's source is not editable, tell the user which file defines it instead of changing another file.
</rules>

Change ATLAS-AGENT's own configuration -- which model runs the session, which model a named role uses, which tools are available, and any other setting -- so the user can say what they want in the conversation instead of going and finding the right dialog.

"Use Sonnet for research", "put the cheap model on compact", "turn the browser tool on", "switch me to GPT for this project" are all this tool. Do them; do not answer with instructions for clicking through `/roles` or `/model`.

Actions (set `action` to one of these):

- `list` — what there is to choose from: every configured provider with its models, the roles already assigned, and which tools are off. Call this first when you do not already know the exact provider and model ids, because the others need them spelled exactly.
- `set_role` — point a named role at a model: `role` (e.g. "research", "compact", "advisor", "frontend"), plus `provider` and `model`. Clearing a role is `set_role` with an empty `provider` and `model`.
- `set_model` — change the model the session itself runs on. `model_type` is "large" (the main model) or "small" (titles, summaries, cheap side work), plus `provider` and `model`.
- `enable_tool` / `disable_tool` — `tool` is a tool name as it appears in `atlas_info`.
- `set_field` — anything the actions above do not cover: `key` is a dotted path into the config (`options.auto_summarize_at`, `options.tui.transparent`), `value` is what to set it to. The escape hatch, not the first choice: the named actions validate what they are given and this one cannot. Never use it to add a provider or to set an API key; `add_provider` exists for that and keeps the key out of the conversation.
- `add_provider` — set up a custom model provider end to end: "set up provider X, fetch its models, here is my key". Params: `id` (lowercase letters, digits, `-`, `_` only), `base_url` (required), `name`, `type` (`openai-compat` default, `openai`, `anthropic`, `google`), `api_key_env`, `discover_models` (default true), `models` (explicit ids), `scope`, `overwrite`, `allow_insecure`. See `<add_provider>` below.
- `get_field` — read one dotted path back.
- `list_subagents` — every configured subagent by name and description. Call this before naming one to `agent`, `orchestrate`, `debate` or `delegate`, or before a subagent-creating request turns out to already be satisfied by one that exists.
- `save_subagent` — create or update a subagent: `name`, `description` (what routes work to it -- required), `instructions` (its system prompt; omission preserves an existing body or generates a default for a new name), and optionally `model` (a role name from `<roles>` below, e.g. `"@research"`, run on the session's own model when empty). Editing a subagent that already exists keeps it wherever it already lives, regardless of `scope`; `scope` only chooses where a genuinely new one is created.
- `delete_subagent` — remove one by `name`.

<add_provider>
1. Find the provider's API base URL yourself, from its documentation, with your web tools. The tool does not guess it. It is the API root (for example `https://api.example.com/v1`), not a docs or dashboard page. Pick `type` from the API the provider documents (OpenAI-style chat completions is `openai-compat`).
2. THE API KEY NEVER PASSES THROUGH YOU. Do not ask the user to paste it into the chat, and never put a key in any parameter. `api_key_env` is only the NAME of an environment variable (`ACME_API_KEY`). If the user already exported it, pass the name. If not, omit it: the tool opens a masked prompt in the interface that you cannot see, and saves the key in the config file. Where no such prompt exists (non-interactive runs, remote clients) the tool says so and you must ask the user to set an environment variable and give you its name. If the user pastes a key into the chat anyway, tell them it is now in the conversation and should be rotated, and do not repeat it anywhere.
3. Before writing anything the tool sends one request, `GET <base_url>/models`, with the key, to that host only. If `/models` is a 404 and the URL has no `/v1` it retries once with `/v1` and reports the corrected URL. A bad key (401/403), a wrong URL (404), DNS, TLS and timeout failures come back as plain messages and nothing is written; fix the cause and call again.
4. For an endpoint without a `/models` route, set `discover_models` to false and list `models` explicitly. The connection is then not checked; say so to the user.
5. https is required (plain http only for localhost; `allow_insecure` overrides it, and the key then crosses the network unencrypted, so use it only if the user asked). An existing provider id is refused unless `overwrite` is true. `scope: "workspace"` needs `api_key_env`, because a key saved inside a project can be committed by accident.
6. Report the summary the tool returns (id, URL, model count, where the key lives) in a line or two. Offer `set_model` to switch to one of the new models; do not do it unasked.
The same thing is available to the user as `atlas-agent provider add`; see docs/provider-setup.md.
</add_provider>

<scope>
Two places a setting can live, chosen with `scope`:

- `global` (the default) — applies everywhere this user runs Atlas.
- `workspace` — applies to this project only, and is what to use when the user says "for this project" or "here".

Prefer global unless the user's words point at the project, since a setting buried in one workspace is one they will not find later.
</scope>

<roles>
A role is a name a model is assigned to. Three of them drive built-in behaviour:

- `compact` — summarizes the session when it runs out of context, on its own model instead of the session's. A cheaper model here saves real money on long sessions.
- `advisor` — reviews each finished turn and leaves a note for the next one.
- `escalate` — consulted when the agent is stuck.
- `goal` — checks whether a goal has actually been reached before an autonomous run stops.

Every other name is free-form: a role called `research` is what a subagent with `model: "@research"` runs on, and a mode named `frontend` runs on the role of the same name. So "use the big model for frontend work" is a role named `frontend`.
</roles>

<tips>
- Read `atlas_info` when you need the current state; this tool changes it.
- Changes are written and reloaded straight away -- no restart -- but a model already chosen for the turn in progress stays until the next one.
- Say back what you changed, including the scope, in one line. Configuration the user cannot see changing is configuration they will not trust.
- A provider the user has not authenticated cannot be assigned. `list` shows what is actually configured; offering a model from anywhere else wastes a turn.
</tips>
# Specialist skill bindings

For save_subagent, preferred_skills is an optional array of up to eight unique
skill names. Omitting it preserves existing bindings; [] clears them. Existing
contracts, tool restrictions, read-only policy and model inheritance are preserved
when editing metadata. Role skills are procedural guidance, not new permissions.

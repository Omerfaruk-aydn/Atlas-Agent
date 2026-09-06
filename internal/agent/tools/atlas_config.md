Change ATLAS-AGENT's own configuration -- which model runs the session, which model a named role uses, which tools are available, and any other setting -- so the user can say what they want in the conversation instead of going and finding the right dialog.

"Use Sonnet for research", "put the cheap model on compact", "turn the browser tool on", "switch me to GPT for this project" are all this tool. Do them; do not answer with instructions for clicking through `/roles` or `/model`.

Actions (set `action` to one of these):

- `list` — what there is to choose from: every configured provider with its models, the roles already assigned, and which tools are off. Call this first when you do not already know the exact provider and model ids, because the others need them spelled exactly.
- `set_role` — point a named role at a model: `role` (e.g. "research", "compact", "advisor", "frontend"), plus `provider` and `model`. Clearing a role is `set_role` with an empty `provider` and `model`.
- `set_model` — change the model the session itself runs on. `model_type` is "large" (the main model) or "small" (titles, summaries, cheap side work), plus `provider` and `model`.
- `enable_tool` / `disable_tool` — `tool` is a tool name as it appears in `atlas_info`.
- `set_field` — anything the actions above do not cover: `key` is a dotted path into the config (`options.auto_summarize_at`, `options.tui.transparent`), `value` is what to set it to. The escape hatch, not the first choice: the named actions validate what they are given and this one cannot.
- `get_field` — read one dotted path back.
- `list_subagents` — every configured subagent by name and description. Call this before naming one to `agent`, `orchestrate`, `debate` or `delegate`, or before a subagent-creating request turns out to already be satisfied by one that exists.
- `save_subagent` — create or update a subagent: `name`, `description` (what routes work to it -- required), `instructions` (its system prompt; a short default is generated if omitted), and optionally `model` (a role name from `<roles>` below, e.g. `"@research"`, run on the session's own model when empty). Editing a subagent that already exists keeps it wherever it already lives, regardless of `scope`; `scope` only chooses where a genuinely new one is created.
- `delete_subagent` — remove one by `name`.

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

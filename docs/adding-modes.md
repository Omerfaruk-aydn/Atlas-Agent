# Adding a session mode

`/mode` (the "Session Mode" dialog) lists the modes a session can take on.
A mode is a subagent definition: one Markdown file with YAML frontmatter.
The same file serves two purposes:

- **Worn as a mode**: `/mode` writes `options.session_mode` to the global
  config. The coder agent is then rebuilt, and the file's body is appended to
  its system prompt inside `<mode name="...">…</mode>`, followed by the role
  contract (`internal/agent/session_mode.go`, `withSessionMode`).
- **Delegated to as a subagent**: the `agent` tool hands a task to it, and
  `subagents.Route` picks it by its contract's `task_types`.

A mode runs on the model role that has its name (or the role named in its
`model:` field). If no model is assigned to that role, the mode uses the
session's own model. Users assign models to roles with `/roles`.

There is no list of modes in Go code. The dialog, the `/roles` presets and
routing all read the catalog from `subagents.Discover`. Adding the file is
enough for the mode to show up.

`/fast` and `/quality` are not modes. They are model switches
(`dialog.ActionSetMode`) and have nothing to do with this catalog.

## Two ways to add a mode

| | Built-in (ships in the binary) | Custom (no rebuild) |
|---|---|---|
| Where | `internal/subagents/builtin/<name>.md` | `<project>/.atlas/agents/<name>.md` or the user agents dir (`atlas agent new --user`) |
| Takes effect | After rebuilding and releasing | On the next `/mode` open |
| Tests and translations | Required (see below) | Not needed |
| Same name as a built-in | — | The file overrides the built-in |

Use the custom route to try a mode out. Move it into `builtin/` once it should
ship to everyone. You can also create and edit modes in the TUI (`/subagents`)
or with `atlas agent new`.

## File format

The file name must equal `name` (`security-audit.md` → `name: security-audit`).
`name` uses lowercase letters, digits and single hyphens, up to 64 characters.
The dialog shows it title-cased ("Security Audit").

```markdown
---
name: migration
description: Plans and performs database schema migrations with backfills and rollback steps. Use for schema changes, data moves or SQLite upgrades.
model: migration
preferred_skills: [patch-review]
read_only: false
allow_commands: true
contract:
  task_types: [migration]
  responsibilities: ['Plan and apply schema changes without losing data.']
  inputs: ['Current schema and the requested change']
  outputs: [patch]
  completion: ['Migration and rollback both run against a copy of real data.']
  required_tools: [view, grep, bash]
  decision_rights: ['Choose migration order and batch sizes']
  out_of_scope: ['Production deploys and unrelated schema cleanup']
  stop_conditions: ['No backup or copy of the data is available']
  evidence_required: ['Commands run, row counts before and after']
---

You are a database migration specialist. ...

## Method
...

## Evidence and verification
...
```

### Frontmatter fields

| Field | Required | Meaning |
|---|---|---|
| `name` | yes | Mode ID. Also the default model role name. Do not rename a shipped mode: users' role assignments are keyed by it. |
| `description` | yes | Up to 1024 characters. Write it as "What it does. When to use it." Only the first sentence (up to the first `. `) appears in `/mode` and `/roles`. |
| `model` | no | Model role to run on. Built-ins set it to their own name (`architect` is the only exception and uses `research`). |
| `inherit_model` | no | `true` makes a broken role assignment an error instead of silently falling back to the session's model. |
| `read_only` | no | Limits the mode to the read tools (`subagents.ReadTools`): no edits. |
| `allow_commands` | no | With `read_only`, also allows `bash`, `test_run`, `lint_run`, `verify` and the job tools. |
| `tools` | no | Explicit allow-list of tool names (`atlas tools list`). Leave it empty for the default coder tool set. |
| `preferred_skills` | no (yes for built-ins) | Up to 8 skill names. Each must exist in `internal/skills/builtin/`. |
| `contract` | no (yes for built-ins) | Role contract, see below. |

### The `contract` block

`task_types`, `responsibilities`, `inputs`, `outputs` and `completion` each
need 1–16 non-empty entries of up to 512 characters. `decision_rights`,
`out_of_scope`, `stop_conditions` and `evidence_required` are optional, with up
to 16 entries each. `required_tools` takes up to 32 tool names. A mode is only
routed to when every required tool is available and allowed by
`read_only`/`tools`.

`task_types` controls delegation routing. A request with a matching
`task_type` (given explicitly, or inferred from the prompt by
`inferTaskType` in `internal/subagents/contracts.go`) gets +100 score.
**If your task type is new, add its keywords to the list in `inferTaskType`**,
English and Turkish. Otherwise prompts that do not set `task_type` will never
reach the mode. Group order matters: the first group that matches wins.

### Body

The body is the mode's system prompt. It is appended to the coder prompt, so
do not repeat general rules. Write only what is specific to the role:
- what the mode is for and what it does not do
- its method
- what counts as done
- how it reports

For built-ins, the body must contain the words "evidence" and "verification"
(enforced by a test). A read-only mode with `allow_commands` should start with
the "Run scoped commands …" paragraph used in `review.md`.

## Checklist for a built-in mode

1. **Add `internal/subagents/builtin/<name>.md`.** Copy the closest existing
   mode (`review.md`, `debug.md`, `frontend.md` …) and adapt it. Set
   `model: <name>`, `preferred_skills` and a full `contract` with
   `out_of_scope`.
2. **Add the name to `builtinNames`** in
   `internal/subagents/builtin_test.go`, in alphabetical order.
   `TestBuiltinShipsTheExpectedModes` fails until you do.
3. **Add a routing case if you added a task type.** Put the keywords in
   `inferTaskType`, then add a row to the table in
   `TestSpecialistRoutingRecognizesDomainsAndEnforcesActualTools`
   (`internal/subagents/skill_bindings_test.go`), e.g.
   `{"migrasyon planla", "migration", "migration"}`. The test checks routing
   both with and without an explicit task type. Make sure the new keywords do
   not steal an existing case.
4. **Add prompt protocols (optional).** If the mode should pull in a protocol
   in `internal/agent/prompt/protocols.go` (`SelectProtocols`, e.g. `ui`,
   `security`, `support`), add its name there and a row to
   `internal/agent/prompt/protocols_test.go`.
5. **Translate the description.** Add a line to `internal/i18n/catalog.txt`.
   The key is the description's **first sentence**, exactly as written. The
   columns are `English|Türkçe|Deutsch|Français|Italiano|العربية`, separated
   by `|`, with no `|` inside the text. Without it the `/mode` row stays in
   English.
6. **Update the docs.** Add the mode to the role lists in
   `docs/agent-roles.md` and `docs/AGENT_WORK_MODES.md`, if they list modes.
7. **Verify.** Run the commands below.

Things you should **not** change: `internal/ui/dialog/modes.go`,
`internal/ui/model/model_management.go`, `internal/agent/session_mode.go`
and `internal/ui/dialog/model_roles.go` (`presetModelRoles`). They already
read the catalog.

## Verifying

On Windows (PowerShell):

```powershell
$env:Path = "$env:LOCALAPPDATA\Programs\Go\bin;$env:Path"
$env:CGO_ENABLED = "0"; $env:GOEXPERIMENT = "greenteagc"
gofumpt -w internal/subagents internal/agent/prompt
go test -p 1 ./internal/subagents/ ./internal/agent/prompt/ ./internal/i18n/ ./internal/ui/dialog/
go build -o atlas-test.exe .
```

Always use `-p 1`: parallel package builds hang the linker on this machine.

By hand, in `atlas-test.exe`:
1. Open `/mode`. The new mode is listed with its translated description.
2. Select it. The status line reads "Session mode: <name>…" and the palette
   entry shows "Session Mode (<Name>)".
3. Open `/roles`. A "Mode: …" row exists for it. Assign a model, reopen
   `/mode`, and check that the row says "runs on provider/model".
4. Send a prompt and confirm the behavior follows the mode's instructions.
   Select "No Mode" to go back.

## Notes

- A mode name that is in the config but matches no file is ignored, and
  the session runs the ordinary prompt. Removing or renaming a mode does not
  break sessions, but users silently lose it.
- Built-in definitions that fail to parse are skipped silently at runtime.
  `TestBuiltinDefinitionsAreValid` is what catches them, so run the tests.
- Commit messages follow the repo's semantic style, e.g.
  `feat(subagents): add the migration mode`.

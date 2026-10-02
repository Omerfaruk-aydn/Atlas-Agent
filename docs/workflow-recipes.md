# Versioned workflow recipes

Atlas ships `feature-delivery`, `migration-review` and `release-check` recipes.
Project definitions live in `.atlas/workflows/*.json`. Additional locations use
`options.workflow_paths` in JSON configuration or `option workflow-path ./path`
in atlasrc; `option reset workflow-path` resets that list.

Recipes are strict version 1 JSON: parameters, named-role steps, dependencies,
owned paths, acceptance criteria and requirements covering every step. Parameters
are strings, integers or booleans. `${name}` substitutes literal data once.
Definitions reject unknown/duplicate JSON fields, unsupported roles, cycles,
escaping paths and more than 64 steps. Every stage remains bounded to 12 checks.
Check definitions contain `name`, project-relative `directory` and literal `argv`.
Interpreter and command-wrapper executables are rejected in recipe checks.

```powershell
atlas-agent workflow recipes
atlas-agent workflow validate feature-delivery
atlas-agent workflow run feature-delivery --plan-only --param 'feature="Add search"'
atlas-agent workflow run feature-delivery --session-id SESSION_UUID --param 'feature="Add search"'
```

`--plan-only` prints tasks, stages, checks and hashes without accessing a session
database, evaluating atlasrc, calling a model or running project commands.
Standalone inspection accepts repeated `--workflow-path` and `--role-path` flags
for custom locations. `--cwd` and `--data-dir` select the project and its data.

Normal admission requires an existing empty session and does not overwrite work.
It persists an immutable run before changing the todo graph, then registers the
delivery plan. Retrying an interrupted admission reconciles the same identity.
Admission runs no checks and dispatches no agents; ordinary dispatch, independent
review and stage verification follow. Declared recipe checks retain normal tool
permissions, hooks, budgets, command policy and configured isolation. When a
recipe omits explicit checks, normal verification must discover the project's
actual checks or obtain explicit checks for an unsupported stack.

The command palette exposes `/workflow:<id>` with typed parameter dialogs.
These use the ordinary workflow tool; selecting a recipe grants no extra tool
authority. Name collisions are reported explicitly. Editing or removing an
admitted definition blocks dispatch and resume until its saved run is inspected.
Checkpoints bind the recipe and normalized parameter hashes. `release-check`
reports readiness and includes no publication or credential-mutation step.

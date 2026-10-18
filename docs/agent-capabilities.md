# Agent capability integration

Atlas combines provider-native reasoning, task working memory, discoverable
tools and execution-grounded feedback. These mechanisms help the agent use its
available abilities; they do not establish a percentage of model capacity or
guarantee that a model will solve every task.

## Controls

- `/work` shows the contract, current method, hypotheses, pending experiments,
  candidate checks, open questions and observed failures.
- `atlas models capabilities --json` describes catalog capabilities and locally
  resolved adapter settings. This is not a live upstream capability probe.
- Set `option work-budget deep` in atlasrc, or `options.work_budget` in JSON,
  to fix the work tier. Values are `auto`, `direct`, `standard`, `deep`.
- An explicit reasoning effort remains authoritative. Automatic effort raising
  only applies when effort is unset and the model catalog supports `high`.
- `tool_search` accepts `detail:"description"` or `detail:"schema"` with one
  to four exact names. It reads the live authorized definitions without running
  a tool or enabling a disabled capability.
- Non-interactive JSON reports `provider_invocations` separately from
  `provider_steps`: adapter Generate/Stream calls include outer retries/failovers,
  while steps count stored model responses. Title/compact helpers and HTTP retries
  internal to an adapter are outside this invocation counter's declared scope.

The model chooses its method with `workflow` action `work`, work_action `method`.
For example, `experiment` makes existing reproduction, test and API-probe tools
discoverable; `candidates` exposes available worktree, verification and pipeline
tools. A broad task profile alone does not load these schemas. Chosen methods
and investigation/verification phases are used, and disabled tools remain absent.

## Integration map

The map distinguishes new or strengthened connections from existing execution
mechanisms. Reusing an existing tool does not mean creating a second subsystem.

| # | Capability | Runtime connection and scope |
|---|---|---|
| 1 | Provider capabilities | `provider_capabilities.go`, `models_capabilities.go`: resolved settings, unsupported-setting notices and catalog limits. Reports distinguish local configuration from upstream verification. |
| 2 | Reasoning continuity | `message/content.go`, `reasoning_origin.go`, `attempt_model.go`: retained signatures/encrypted items, origin filtering and per-retry provider adaptation. |
| 3 | Adaptive work budget | `engineering/work.go`, `work_context.go`, coordinator: request/observed-difficulty tiers, explicit override and unset-effort adaptation. |
| 4 | Method choice | `work_workflow.go`, `work_capabilities.go`: direct/research/prototype/experiment/review/candidates/delegate methods; no mandatory specialist sequence. |
| 5 | Task contract | Work items cite recorded user turns and join shared completion accounting. Current verification is required; declared blocking questions remain visible. Proposals and assumptions have separate origins. A continuation is not a requirement. |
| 6 | Dynamic context | Existing `context_select`, context inspector and task-context packets; source identities, exclusions and queryable omissions. Method-based schema discovery connects these to current work. |
| 7 | Long-task memory | Existing atomic engineering state stores work records; per-step refresh, continuation preservation, long-request identity and damaged-record recovery. Critical constraints survive the soft projection budget. |
| 8 | Project relationships | Existing project_map/code_query/import_graph/impact_analysis and task-context packets; investigation guidance and method-based discovery. |
| 9 | On-demand expertise | Skill/specialist discovery and workflow dispatch plus `workflow expertise list/load/unload`. Enabled guides carry source hashes; declared conflicts/exclusive groups reject conflicting loads. Unload omits the guide body from later model requests while retaining audit history. |
| 10 | Tool learnability | Live description/schema lookup, existing pipeline help and task-category protocols. Examples and schemas stay attached to actual tools. |
| 11 | Programmatic tool use | Existing guarded pipeline plus pure transform operations, numeric conditions and items_from iteration. This is a declarative JSON program, not unrestricted generated code. |
| 12 | Long workflows | Existing browser/desktop flow engines, bindings, branches and durable journals; updated guidance groups known operations and stops at unknown targets. |
| 13 | Parallel execution | Existing dependency-ready waves, task ownership and permitted child invocations; shared desktop/browser resources retain their leases and ordering. |
| 14 | Solution candidates | Work candidates have distinct declared approaches and task/workspace identities; existing isolated workspaces supply execution branches. Similarity checks are a heuristic, not semantic proof. |
| 15 | Experiments | Registered hypotheses/predictions bind later observed results and disclose fixture limitations. Existing reproduce/API/test/benchmark tools execute experiments. |
| 16 | Discriminating selection | Fixed named suite and measured verify results. Suite changes clear selection; different candidate source generations cannot be combined into one passing suite. |
| 17 | Strategy changes | Runtime error/no-information monitor, structured error classes and method evidence. Legitimate asynchronous polling is exempt; successful observations reset consecutive reviews. |
| 18 | Feedback improvement | Observed operations, work experiments and existing verification/quality gates feed the next model request; no automatic success from self-assessment text. |
| 19 | Reusable methods | `workflow learn extract/list/inspect/export` synthesizes procedure candidates from observed successful operations, with conditions, versions, limits, verification provenance and optional source fingerprints. Candidates persist per project; export invokes guarded skill_manage. Auto-extraction copies tool names, never raw arguments/outputs or secrets. Existing knowledge, recipes and browser knowledge remain available. |
| 20 | Independent branches | Existing worktree/workflow isolation plus `workflow branch fork/propose/status/merge` for patch-only alternatives when checkouts are prohibited. Branch-local text/notes stay isolated, changed bases reject integration and partial merges are journaled rather than replayed. Candidate branch/task ownership is validated. |
| 21 | Visual/semantic observation | Existing browser/computer observations and media delivery; per-attempt adapter selection respects fallback image support and preserves semantic captions. |
| 22 | Product/design reasoning | Existing design_search/design_system, workflow design/critique and rendered UI/a11y/interaction checks are connected by guidance and investigation discovery. A screenshot or property read alone is not a design-quality score. |
| 23 | Model-specific execution | Parsed native options and metadata, capability notices and retry adaptation. Model-specific behavior comes from adapter/catalog contracts; unavailable upstream models remain reported errors. |
| 24 | Work visibility | `/work`, workflow snapshots and current per-step projections show recorded phase, decisions and blockers, not fabricated progress percentages or private thought traces. |
| 25 | Evaluation | Live manifests support `select_delivery` and explicit `delivery_files`: after generating candidates, a separate real model run selects/implements a delivery in a fresh workspace. External checks re-score that actual delivery; summaries distinguish pass-at-1, any-candidate success, measured delivery success and retrospective predictions. Missing delivery measurements remain absent. |

## Pipeline data example

```json
{"steps":[
  {"id":"hits","tool":"grep","arguments":{"pattern":"LegacyClient","path":"."}},
  {"id":"files","tool":"transform","arguments":{"source":"hits","op":"extract","pattern":"^(\\S.*):$"}},
  {"id":"unique","tool":"transform","arguments":{"source":"files#/items","op":"unique"}},
  {"id":"read","tool":"view","arguments":{"file_path":"$item"},"items_from":"unique#/items"}
]}
```

Transforms do not access files, network or processes. Real calls still pass
through hooks, permissions, cancellation and outcome accounting. The retained
source has bounds; overflow is reported instead of silently dropping iterations.
Truncated transform arguments cannot drive downstream tool calls. An explicit
`head` operation can select a bounded subset before the result exceeds limits.

## Reusable procedures and alternatives

Extract a method with `workflow` action `learn`, `learn_action:"extract"` and
`learned_method` containing name, description, conditions, versions, limits,
ordered `operation_ids` and optional abstract steps/source paths. At least one
current explicit verification is required. Omitting steps derives a bounded
procedure from tool names, not recorded credentials or ephemeral targets.
`learn_action:"inspect"` with `method_id` rechecks original evidence and source;
`export` calls `skill_manage create` with `skill_scope:"project"` (default) or
`user`. It never overwrites an existing skill. Exported skills are discovered in
future sessions through the existing skill system.

For patch isolation, `workflow branch` with `branch_action:"fork"`, a distinct
`branch_name` and `branch_paths` snapshots 1-8 existing text files, total 64 KiB.
`propose` supplies `branch_id`, `branch_files` (path and proposed text) and optional
branch-local notes. `merge` checks all original hashes before dispatch, then uses
normal view/edit calls. Each edit checks its exact byte hash again after approval.
Interrupted merges require inspection; they never auto-replay. Patch branches
are bounded proposal stores, not executable workspaces. New/binary files and
isolated execution use permitted workspaces; verify the final integrated patch.

For example, after observing the file, a model can call:

```json
{"action":"branch","branch_action":"fork","branch_name":"streaming-approach","branch_paths":["internal/parser.go"]}
```

Then use the returned branch ID, not a made-up identity:

```json
{"action":"branch","branch_action":"propose","branch_id":"<returned ID>","branch_files":[{"path":"internal/parser.go","proposed":"<complete alternative file text>"}],"branch_notes":["This alternative streams records instead of retaining the complete input."]}
```

`branch_action:"status"` shows the proposals and per-file dispatch state;
`branch_action:"merge"` takes that exact `branch_id`. Forking another branch
captures a separate snapshot; proposing to one does not change the other.

`workflow expertise list/load/unload` takes an exact `expertise_name`. Skills can
declare metadata `exclusive-group` and comma-separated `conflicts`. Declared
conflicts reject a load until resolved; project instructions and user constraints
remain authoritative even when metadata does not declare a semantic conflict.

```json
{"action":"expertise","expertise_action":"load","expertise_name":"design-system"}
```

Use `expertise_action:"unload"` with the same name when the guidance no longer
applies. Loading a guide does not enable tools disabled by session policy.

## Evidence and evaluation limits

Work verification requires a completed explicit check at the current generation,
or native output/assertion evidence. Reading a page, listing windows or running
an arbitrary successful shell command alone does not verify a requirement.
These links establish provenance, not whether an unrelated test proves a claim;
external acceptance criteria remain necessary.

`atlas eval attempts` reports first valid attempt and success in any attempt.
`predicted_selection_success_rate` is a retrospective heuristic based on Atlas
runtime evidence, not an observed selection between live candidate runs.
For measured delivery, add `"select_delivery":true` and a literal list such as
`"delivery_files":["allocator.py"]` to a live evaluation manifest. Only those
bounded text artifacts are provided to the selecting model; external checker
outcomes are withheld. The selected/combined delivery receives its own workspace
and external checks. `delivered_success_rate` and `actual_selection_losses` use
these observed records; absent delivery records mean unmeasured, not zero.
`claim_basis: normal_end_turn_proxy` is not a semantic assertion of completion
and is not counted as an observed completion claim by the evaluation runner.
If the completion gate stops with outstanding requirements, its reason is saved
in the final message for JSON clients as well as shown in the TUI. Internal
completion prompts never become cited user requests. Continuation remains
bounded by the existing no-progress and task limits.

Physical browser/desktop tasks, broad model comparisons, visual quality and
long-task success need separate live acceptance. Unit tests, fixtures and one
successful coding task cannot establish those outcomes.

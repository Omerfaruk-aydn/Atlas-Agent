# Role contracts and measured model routing

All twenty-two built-in roles have structured contracts separating responsibilities,
inputs, outputs, required tools and completion criteria from provider/model choice.
They apply to delegated agents and session modes. Project/user definitions can
override `.atlas/agents/<name>.md`. Old definitions without contracts remain valid.
Save/list APIs preserve contracts, `atlas agent show` renders them, and
`atlas agent list --json` exposes contracts and command flags.

See [Specialist skills](specialist-skills.md) for the expanded catalog, inherited
model behavior, preferred skill bindings and artifact workflows.

```yaml
---
name: backend
description: Implements service and persistence changes.
model: backend
contract:
  task_types: [backend]
  responsibilities: [Preserve API and persistence contracts]
  inputs: [Task scope and acceptance criteria, Repository instructions and owned paths]
  outputs: [implementation]
  completion: [Relevant integration and failure checks pass]
  required_tools: [view, grep]
  independent_review: true
---
Follow repository conventions and make only the assigned changes.
```

Contract lists are bounded and validated. Assignments supply actual owned paths,
dependencies and criteria. Contracts do not grant access: permissions, allow-lists,
hooks and budgets still apply.

## Routing

The `agent` tool supports `auto`, `task_type`, `required_tools`, `expected_output`.
Automatic routing filters contract compatibility and actual registered native tools
before ranking task matches. Explicit types and outputs precede description
overlap. Unconstrained prompts use deterministic English/Turkish task hints and
legacy keyword matching; this is not semantic classification of arbitrary language.
Structured requirements without compatible roles fail explicitly. Unconstrained
prompts can use the generic agent. Automatic responses report the role and reason.
Explicit names retain precedence.

```json
{"auto":true,"task_type":"frontend","required_tools":["write"],"expected_output":"implementation","prompt":"Implement settings using existing components."}
```

## Handoffs and quality gates

Give `todos` stable IDs, named agents, owned paths and criteria, then call
`workflow` with `action=dispatch`. Specialists return bounded JSON handoffs:

```json
{
  "task_id":"api",
  "summary":"Implemented validation and regression tests.",
  "changed_files":["internal/service/handler.go"],
  "checks":[{"command":"go test ./internal/service","exit_code":0,"evidence":"Observed exit 0"}],
  "risks":[],
  "dependencies":[],
  "decision":"ready"
}
```

Task identity and ownership must match. Invalid, oversized, contradictory or failed
reports cannot pass the gate. Handoffs are reported evidence, never authorization
or independently authenticated execution.

For `independent_review: true`, call `workflow` with `action=review`, `task_id`,
and optional explicit `checks`. Fresh `test` and `review` sessions inspect the
integrated parent tree, followed by `verify`. Direct edits, delegation and MCP are
disabled for these sessions; normal command permissions apply. Shell commands can
modify files, so this is not an OS sandbox. Quality specialists need model-role
assignments or measured policies. Independent sessions may use the same model.

Go discovery uses build/test/lint tools, or permitted shell build, uncached test and
vet when quality tools are disabled. Node projects use existing package-manager
scripts; unsupported stacks need explicit checks. Failed checks stop the gate.
Matching task/run entries in the machine-observed journal are required: a
successful-looking response alone does not pass.

Isolated patches must be applied through managed integration before review; the
applied hash must match the current complete workspace patch. Review requires Git
and bounded snapshots of tracked and non-ignored files, including deletions.
Limits: 10,000 files and 64 MiB. Incomplete snapshots fail explicitly. Runtime state
is excluded; submodule directories are not certified by this implementation.

Successful review persists reports, machine checks and the source fingerprint.
The application session-save layer rejects newly completing a dispatched gated
task without these records or after its requirements/source change. Tools and
client/server saves share the gate. Update `todos` with passing verification and
acceptance evidence afterward; review never auto-completes tasks. Completed tasks
remain historical records during later work. Legacy/undispatched tasks retain their
existing completion rules.

## Measured model selection

See [prompt architecture and delivery standards](prompt-engineering.md) for the
shared system template, detailed role protocols and prompt revision evaluation.

[Delivery system](delivery-system.md) connects these roles to requirement/stage
plans and persistent source-grounded project knowledge. UI-linked stages also use
actual artifact capture and reported design critiques before stage advancement.

`atlas run --role frontend --model provider/model` applies the system role contract
and permissions while preserving the explicit model. `--role none` disables the
configured mode. These overrides are local and are not persisted.

Set `role` in an `atlas eval run` manifest, with explicit provider/model IDs and
external checkers for every criterion. Role runs default to three repetitions per
candidate/scenario; `repeats` supports 1–20 with at most 256 runs. Worktrees are
preserved. Records include the baseline commit, task/checker and role-prompt hash,
timestamp, cumulative runtime metrics and execution failures. Live comparisons
execute real APIs and consume account quota.

`atlas eval recommend policy.json results.json` compares records without running
models or changing config. Example policy:

```json
{
  "role":"backend",
  "scenarios":["multi-layer-feature","bug-regression"],
  "candidates":["provider/model-a","provider/model-b"],
  "prompt_version":"roles-v1",
  "min_samples":3,
  "min_success_rate":0.8,
  "max_mean_cost_usd":0.25,
  "max_mean_duration_ms":300000,
  "max_age_hours":168
}
```

Candidates need equal samples per scenario, identical baseline/fixture and prompt
version. Defaults: three samples, 80% success, seven-day freshness. Missing metrics,
duplicate identities, incomparable fixtures and insufficient evidence fail.
Execution failures count as unsuccessful. Eligible candidates rank by success rate,
then mean cost, then duration. This supports a workload-specific choice, not a
universal ranking. Local result files are trusted input, not cryptographic proof.

Opt in through `options.role_model_selection`:

```json
{
  "options": {
    "role_model_selection": {
      "backend": {
        "results_file":"D:/benchmarks/backend-results.json",
        "policy": {
          "role":"backend",
          "scenarios":["multi-layer-feature"],
          "candidates":["provider/model-a","provider/model-b"],
          "prompt_version":"roles-v1"
        }
      }
    }
  }
}
```

Policies use model-role references. Selection runs at specialist build; cached
instances stay stable until rebuilt. Selected providers/models must exist and be
enabled. Invalid/insufficient evidence in an enabled policy fails construction
instead of silently choosing another model. Explicit `run --role ... --model ...`
bypasses measured selection for the main session so benchmarks do not feed the
policy its previous recommendation. Manual `model_roles` remain the default when
no measured policy is configured.

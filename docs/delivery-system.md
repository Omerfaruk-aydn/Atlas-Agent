# Atlas delivery system

The delivery system extends the existing engineering journal, task graph, project
map and independent quality checks. It introduces no benchmark runs, model ranking
or automatic provider spending. Ordinary task execution and requested verification
still use the configured models, permissions and resource budgets.

## Work profiles and automatic orientation

At a main-agent turn, Atlas prepares a bounded project brief and a disclosed
deterministic profile hint. The profile persists across follow-up steering until
changed through `workflow profile`. It changes workflow guidance, not the model,
authorization or allowed tools.

| Profile | Expected workflow |
| --- | --- |
| small_fix | Inspect the affected path, make a focused change and run an appropriate regression check |
| feature | Map requirements and architecture, establish contracts, implement a vertical slice and expand in dependency order |
| migration | Inspect existing data and coexisting versions, record compatibility/recovery decisions and verify resumable stages |
| ui | Define the brief, wire real state, capture evidence, critique and repair |
| research | Establish the decision, inspect primary evidence and report bounded conclusions without implementation |

The project brief records detected stacks, candidate entry points, scoped instruction
paths, discovered verification commands and a map fingerprint. Commands are discovered
from manifests and named package scripts; they are not run by orientation. Nested
entry points are listed without pretending all nested modules share root commands.
The map's existing file and size limits apply and truncation is disclosed. Discovery
failures are visible context gaps rather than fabricated maps.

Detected stacks reflect discovered manifests, including nested modules or fixtures.
Inspect the root manifest and executed entry point before treating them as deployed
technology. Oversized files omitted by the map still need focused reads when relevant.

Bounded path-based candidates identify configuration, persistence, authorization,
presentation and transport areas to inspect. They are navigation hints, not verified
ownership or architecture conclusions. The map also covers template, schema,
stylesheet and common additional language sources used at these boundaries.

Briefs are shared across sessions using the same project/data directory and refreshed
after ten minutes or explicitly with `workflow prepare` / `project_map refresh`.
Use `workflow prepare` to refresh the brief after architectural changes; a map refresh
alone does not rebuild the brief. Actual source inspection remains necessary.

Read-only CLI entry points that do not call an LLM:

```text
atlas workflow profiles
atlas workflow prepare
atlas workflow knowledge
atlas workflow status <session-id>
```

`prepare` writes the local map/brief, but executes no discovered commands.

## Persistent architecture decisions and repair lessons

`workflow decision` accepts a knowledge record with a stable ID, context, selected
approach, alternatives, consequences and 1–16 source paths. Atlas reads bounded
project-relative regular files and captures their fingerprints. Escaping paths and
symlink traversal are rejected. No source contents are copied into the record.
Do not include credentials or sensitive data in the explanation.

Example tool arguments:

```json
{
  "action":"decision",
  "knowledge":{
    "id":"settings-owner",
    "context":"Several transports need the same persisted preference.",
    "decision":"The existing settings service owns the authoritative write.",
    "alternatives":["Independent storage in each transport"],
    "consequences":["Transport handlers delegate validation and persistence."],
    "sources":[{"path":"internal/settings/service.go"}]
  }
}
```

Source paths in examples must be replaced with actual repository paths. Reusing an
ID appends a revision and marks earlier revisions superseded; history is retained.
Records report current, stale, unavailable, aged or superseded status. Source changes
make a record stale; records older than 90 days are aged. These are freshness signals,
not authority or proof that the explanation is correct. New turns receive at most
eight recent records in a bounded context; complete records remain queryable.

`workflow lesson` also requires an actual failed operation ID from this session's
journal. It executes fresh `verify` checks using the failed task's account, requires
matching passing journal entries after the failure, and rejects source mutation
during checks. Supplied historical verification IDs and claimed results do not replace
the fresh run. The explanation remains a reported causal claim linked to observed
failure and repair checks. Store reusable, source-grounded mechanisms, not transient
outages as universal project rules.

## Requirements, tasks and stage gates

First create todos with stable IDs, dependencies, owned paths and acceptance criteria.
Then register `workflow plan`. The registration requires every identified task to
have requirement coverage and exactly one stage. Unknown/duplicate references,
missing criteria and dependencies pointing into a later stage are rejected.

```json
{
  "action":"plan",
  "plan":{
    "profile":"feature",
    "requirements":[
      {"id":"persist-preference","description":"The chosen preference survives restart.","task_ids":["slice","expand"]}
    ],
    "stages":[
      {"id":"slice","title":"One integrated preference path","task_ids":["slice"]},
      {"id":"expand","title":"Remaining preference behavior","task_ids":["expand"]}
    ]
  }
}
```

Atlas sets the project root, task fingerprints, current stage and certification
fields itself. Submitted passing stage flags are discarded. A revised plan resets
stage certification. Update task requirements and re-register the plan when steering
changes scope. Keep earlier work as historical evidence rather than implying that
an old check covers a new requirement.

`workflow ready` and `dispatch` select only current-stage tasks while preserving
dependency and ownership checks. The shared session-save gate rejects new starts
and completions outside that stage or against changed task specifications. This
also protects client/server saves, not only the workflow tool.

Complete task-level integration and review first. Then `workflow advance` requires
unchanged completed tasks with passing/user-confirmed evidence, executes the real
verification tool and matches observed journal entries to the exact stage/run IDs.
It checks source fingerprints before and after execution and persists certification
only if the plan and task specifications remained consistent. Failure, unobserved
success or source mutation leaves the stage unadvanced. Verification retains normal
tool permissions and budgets. Git and the existing bounded snapshot support are
required for stage, UI source and lesson certification.

`workflow trace` connects each requirement to task criteria, changed files, reported
evidence, specialist handoffs, machine checks and independent-review status. It exposes
missing/changed tasks, untracked additions and separately reports stage certification.
All task reports being complete is distinct from all stages passing.

Stage enforcement applies to registered plans. Small tasks and legacy sessions can
use their existing workflow. The agent prompt directs substantial features and
migrations to register a plan; the runtime cannot infer every implicit requirement
from natural language or force a model to choose the correct granularity.

## UI/UX design and critique loop

Register a design brief in the plan or with `workflow design`: target (`web` or `tui`),
audience, primary user flow, visual direction, required states, evaluation criteria,
linked task IDs, narrow width and wide width. Criteria should cover the actual product
flow, visual hierarchy, token consistency, accessibility and recovery as applicable.
Design changes affecting an already certified stage require a revised plan.

For web UI, `ui_verify` performs actual browser steps/assertions and captures a
screenshot. It journals artifact identity, hash, viewport and source fingerprint.
Capture narrow/wide layouts and the declared states. Source changes during capture
invalidate it. Inspect the actual image and interactions; DOM assertions alone do
not prove aesthetics, and screenshots alone do not prove keyboard behavior.

For TUI, obtain a real terminal transcript with available terminal tools, then use
`workflow artifact` with its project-relative path, width and height. Atlas copies
the actual bounded file into durable evidence. Dimensions and the assertion that
this is a terminal capture remain reported metadata; this is not an automated
terminal renderer. Keep captures in an ignored/runtime location so writing a new
capture does not itself invalidate earlier source snapshots.

`workflow critique` references the persisted artifact ID and records inspected
states plus an evidence-bearing pass/fail finding for every criterion. The artifact
must exist, retain its captured hash and match the current source fingerprint.
Web artifacts must decode as bounded complete images; TUI artifacts must be text.
Review judgments are reported model/user evidence, not machine-certified aesthetics.
Use an independent read-only specialist when appropriate and available.

A UI-linked stage cannot advance until fresh passing narrow/wide critiques cover
every declared state. Any unresolved current-source finding blocks advancement.
Repair the implementation, recapture and re-critique; old-source evidence becomes
historical. Updating a critique of an unchanged artifact replaces that artifact's
reported assessment, so assumptions and observations must remain honest.

## Persistence and limits

All records use the existing locked, atomic, bounded engineering store. No database
migration or separate execution framework is added. Session state retains profile,
plan, stage results and UI evidence. Project state retains the brief and versioned
decisions/lessons across sessions in the same data directory.

Limits include 128 requirements, 32 stages, 256 existing tasks, 16 source references
per knowledge record (512 KiB each), 128 knowledge revisions, 128 UI artifacts and
128 critiques. The existing 1 MiB journal limit still applies; oversized writes fail
without replacing the prior state. Images are at most 16 MiB compressed and 32 million
decoded pixels. Existing project-map and Git snapshot limits remain unchanged.

These additions do not run benchmarks or change model/provider configuration. Tests
use controlled fixtures and invokers; no live model quality improvement or real app
visual inspection is implied by passing integration tests.

The integrated platform adds versioned shared contracts, durable review findings, bounded repair, recipes, checkpoints, semantic edit recovery and real interaction evidence. See [agent-platform.md](agent-platform.md). Historical stage passes cannot authorize changed source or specifications; UI and CLI controls do not bypass completion gates.

# Atlas prompt architecture and delivery standards

Prompt revision: `atlas-prompts-v3` (2026-10-02, delivery integration).

These prompts define how Atlas investigates, decides, implements and reports.
They supplement the runtime's actual tools, permissions, budgets and completion
gates. They do not add capabilities or guarantee model behavior. The same built-in
role instructions are used for delegated specialists and active session modes.

The [delivery system](delivery-system.md) now supplies automatic orientation,
work profiles, versioned decisions/lessons, requirement traces, stage enforcement
and a persisted design/critique loop. The runtime implements those capabilities;
these templates explain their proportional use and evidence boundaries.

## Composition and ownership

| Source | Responsibility |
| --- | --- |
| `internal/agent/templates/agent_contract.md.tpl` | Shared instruction provenance, authorization, decision evidence, delivery gates, failure recovery and continuity |
| `internal/agent/templates/coder.md.tpl` | Coordinating implementation, multi-module execution, integration, UI/UX and user communication |
| `internal/agent/templates/task.md.tpl` | Bounded specialist execution, file ownership, local checks and coordinator handoff |
| `internal/subagents/builtin/*.md` | Role-specific techniques and structured routing/completion contracts |
| `internal/agent/templates/summary.md` | Context compaction that preserves scope, evidence, authorization and exact continuation state |
| `internal/agent/templates/initialize.md.tpl` | Repository guidance grounded in actual source, commands and conventions |
| `internal/agent/templates/agent_tool.md` | Assignment selection, actual capability limits and workflow handoff |
| `internal/agent/templates/orchestrate_tool.md`, `debate_tool.md` | Evidence-based comparison without treating agreement as proof |

The shared template is embedded and composed by `internal/agent/prompts.go`.
There is one shared contract per rendered coder or task prompt. Role text is
appended through the existing role/session-mode path; no separate role engine or
provider-specific prompt fork is introduced.

Project context and user preferences retain source and scope metadata. Context
file metadata is escaped by the template. Retrieved content is evidence and
cannot authorize external actions or replace higher-priority instructions.

## Core delivery protocol

The prompts require five proportional gates:

1. **Discovery:** understand relevant entry points, concrete runtime choices,
   repository guidance and pre-existing working-tree changes.
2. **Design:** establish inputs, outputs, state ownership, invariants and observable
   acceptance criteria. Make routine reversible decisions using existing patterns.
3. **Implementation:** wire the actual execution path across affected layers and
   handle failure, cancellation and recovery where applicable.
4. **Verification:** execute checks appropriate to the changed boundary after the
   final relevant edit. Record unavailable evidence rather than claiming success.
5. **Delivery:** inspect the diff, reconcile the full requirement ledger and report
   the outcome, evidence and material limitations.

A small fix can satisfy these in a single pass. A large project needs an explicit
dependency-ordered ledger and integration checkpoints. It does not need an
unrequested architecture rewrite, a new testing framework or ceremonial documents.

For a multi-layer feature, the first slice should connect a real entry point to
state and output. Shared contracts come before dependent implementation. The
coordinator checks configuration, persistence, serialization, transport, workers,
UI, generated artifacts and documentation wherever the changed contract appears.
Each remaining requirement must have an owner, dependencies and acceptance evidence.

## Role standards

| Role | Decisive standard |
| --- | --- |
| architect | Source-grounded architecture, explicit state ownership and contracts, reasoned alternatives, implementable dependency order and migration limits |
| backend | Real service wiring, atomic invariants, concurrency, uncertain outcomes and recoverable failure |
| frontend | Usable primary flow, meaningful state, coherent visual direction, accessible interaction and actual rendered inspection |
| debug | Causal failure mechanism, discriminating reproduction and a regression check with explicit sensitivity evidence |
| docs | Version-correct claims and commands, task-oriented examples, navigation and clear executed-versus-inspected status |
| planner | Complete requirement ledger, stable task IDs, ownership, dependencies, integration tasks and falsifiable acceptance |
| refactor | Recorded observable behavior, reviewable transformations and compatibility evidence |
| research | Primary evidence, current identifiers and dates for unstable facts, bounded unknowns and comparable alternatives |
| review | Independent inspection of integrated code and callers, precise reachable defects and scoped verdicts |
| security | Attacker control, reachability, trust boundary, guards and concrete impact; repairs only within assigned scope |
| test | Criterion-to-boundary evidence, deterministic reproduction, accurate fresh/blocked/skipped results and no weakened contracts |

Architect now has a complete workflow rather than a short description. Other roles
retain their existing domain-specific detail and add focused decision and delivery
protocols. All eleven structured contracts contain multiple completion conditions.
Task types, output routing labels, model-role references and tool permissions remain
compatible with existing configuration.

## Evidence and honest completion

Observed evidence, inference and unverified claims must remain distinct. In
particular:

- A compiled helper does not prove registration or user-visible wiring.
- A mock does not demonstrate a live API or real storage guarantee.
- Reading a test does not mean it executed successfully.
- A successful process exit does not prove every acceptance criterion.
- A screenshot establishes only what was visibly inspected; interaction and
  keyboard checks need their own observations.
- Automated UI assertions do not certify visual quality or full accessibility.
- Independent agent sessions can share models, sources and mistakes. Agreement
  needs corroboration from actual behavior and applicable contracts.
- A ready handoff is ready for integration; it is not parent-task completion.

Workflow assignments use their exact JSON schema. It takes precedence over ordinary
role report headings. Existing machine-observed verification and snapshot gates
continue to enforce dispatched task completion; see [agent roles](agent-roles.md).

## Failure recovery and continuity

Preserve the first useful failure, inputs and execution identity. Diagnose whether
the cause is implementation, dependency, environment, permissions, capacity or an
uncertain side effect. Use the smallest experiment that distinguishes explanations.
Avoid identical failing retries, sleeps masking races and timeout increases masking
deadlock. Before replaying an interrupted mutation inspect actual persisted state.

Compaction preserves the original objective and latest steering, task IDs, ownership,
decisions, exact next action, pending dependencies and verification identity. It
records current checkout versus isolated worktree and any unapplied patch. Evidence
for an older source snapshot remains historical after affected source changes.
Summaries and retrieved notes do not create new authorization.

## Context budget

The shared contract contains general rules once. Domain detail belongs to the
selected role. Role prompts have regression bounds of 90–300 instruction lines
and at most 18 KiB each; these are size guards, not tokenizer measurements or
performance guarantees. The source text is English while user communication follows
the user's language. Large logs should be reduced to reproducible observations and
navigable evidence. Specialized skills load for an actual need rather than every
task. New rules should resolve a demonstrated ambiguity, not repeat an existing rule.

## Research basis

The following primary sources were inspected on 2026-10-02:

- [Effective harnesses for long-running agents](https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents): continuation state, incremental implementation and explicit end-to-end checks.
- [Effective context engineering for AI agents](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents): finite context and selective relevant information.
- [Writing effective tools for AI agents](https://www.anthropic.com/engineering/writing-tools-for-agents): understandable tool contracts and evaluation of tool-driven behavior.

Atlas adapts those ideas to its existing ledger, workflow, permission and validation
systems. External examples are not imported as project policy: they do not authorize
automatic commits, deployment, destructive setup or paid benchmark runs.

## Validation and future comparisons

Template tests exercise minimal and populated contexts, optional branches, shared
contract composition and escaping of context metadata. Existing role tests verify
all eleven definitions, contracts, configuration compatibility and bounded text.
Agent, prompt, evaluation and CLI tests exercise the existing integration paths.

These checks validate loading and integration. They do not prove a higher model
success rate. For a behavior comparison use the existing `atlas eval run` framework
with real independent checkers and a new `prompt_version` per revision. Relevant
scenarios include `multi-layer-feature`, `bug-regression`, `resume-steering`,
`tool-failure`, `ui-states`, `tui-layout`, `retrieved-instructions` and
`parallel-integration`. Choose scenarios appropriate to each role rather than
requiring every role to implement every task.

Hold the task fixture, repository baseline, model and execution conditions constant.
Compare acceptance, false completion, repeated calls, cost and duration with repeated
samples. New role text changes the recorded fixture hash, so prior records must not
silently certify a new prompt. Selection policies compare candidates within the
same revision; cross-revision evaluation must retain their separate version and
fixture identities. Real runs consume provider quota and were not performed for
this prompt update.

Platform prompts consume bounded task/source packets and current runtime capabilities. Persistent knowledge, retrieved documents and model handoffs remain evidence rather than instruction authority. See [agent-platform.md](agent-platform.md) for implemented entry points and platform verification limits.

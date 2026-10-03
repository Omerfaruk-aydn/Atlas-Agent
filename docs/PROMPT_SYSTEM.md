# Atlas prompt architecture

Prompt revision: `atlas-prompt-v2`. The core contract is always included. Task
modules are selected deterministically from the request and active role; selection
never changes tools, permissions, model selection or user authorization.

## Composition

1. Coder or specialist identity and bounded delivery workflow.
2. Shared instruction provenance, decision discipline, evidence and recovery rules.
3. Selected task modules: large_project, architecture, migration, debug, research,
   ui and platform.
4. Actual environment, scoped project instructions, available skills and memory.
5. Active role instructions and typed role contract, where applicable.

Automatic selection uses whole words in Turkish/English and explicit role names.
It is a bounded heuristic, not a semantic classifier. A protocol omitted by the
heuristic does not make correctness or accessibility optional. The core tells the
agent to discover relevant skills and revise the ledger when scope becomes clear.
Role guidance does not enable disabled tools or override assignment ownership.

Per-session recipes persist only the version and selected module IDs in SQLite.
Short continuation requests without a new detected intent reuse the previous
recipe after restart. Consequential user requirements still live in the task
ledger/transcript/summary; the recipe neither summarizes nor proves progress.
Explicit task intent selects fresh modules. Specialist prompts select by role.

## Role contracts

The eleven built-in roles now declare decision_rights, out_of_scope,
stop_conditions and evidence_required in addition to inputs, outputs,
responsibilities, completion and required_tools. Policy entries are bounded and
validated. Optional fields preserve older custom role definitions. Builtin copies
clone each list, so editing a role cannot mutate the shared catalog.

Every built-in role includes a concrete decision/delivery example. Missing inputs
block the affected dependency, not independent authorized work. Required workflow
JSON remains authoritative for output shape; examples do not replace its schema.

## UI/UX references

The supplied Downloads/apple-design and Downloads/ui-ux-pro-max folders were
compared with existing embedded assets. Source text and the 24 CSV resources are
preserved with provenance. Native offline design_search queries the data without
requiring Python scripts or treating supplied instructions as system authority.

The UI task module explicitly routes web/mobile design to ui-ux-pro-max, suitable
Apple interaction work to apple-design and terminal interfaces to tui-design.
The active skills require a real brief, coherent tokens, state/interaction matrix,
keyboard/focus, narrow/wide inspection and failure recovery. Apple craft adds
interruptible direct manipulation, cancellation, motion/accessibility preferences
and legible material fallbacks. It is not a universal glass/animation preset.

User-disabled skills/tools remain disabled. Reference rankings, historical numerical
recommendations and source hashes are not proof of accessibility or visual quality.

## Evaluation

Existing `atlas-agent eval scenarios` and `eval live` remain the execution harness.
Live records now include a task/check case_hash separately from fixture_hash, so
role prompt changes can be the treatment while the task, model and repository base
remain fixed. Fixture_hash still records the role instructions for model-selection
policies. Run the two instrumented prompt builds against identical explicit fixture
tasks and checks, then compare their saved records:

```powershell
atlas-agent eval prompt-compare before.json after.json --min-samples 3
```

This command is read-only and does not call a model. It rejects mismatched models,
roles, task/check hashes, repository bases, mixed versions, unequal repetitions and
estimated metrics. It reports acceptance, false completion, execution failures,
repeated calls, token/cost and duration deltas. Reports retain missing scenarios.
Submitted evidence is not independently authenticated; small-sample differences
are not statistical proof. Use external checks and inspected UI artifacts to grade
actual behavior. Label unavailable checks honestly. Do not infer aesthetic quality
from compilation or claim improvement solely because prompt text became shorter.

No paid before/after model benchmark is automatically started by this change.

Optional question_review records contain total, unnecessary and transcript evidence.
Count repeated approvals for an already authorized action or avoidable routine
questions as unnecessary; do not penalize consequential missing inputs or actual
authorization boundaries. A question delta requires reviewed evidence for every
sample in both inputs; missing review is not treated as zero questions.
Local tests cover task selection, real template composition, session isolation and
restart continuity, role validation/copying, searchable reference coverage and
comparison rejection of confounded samples.

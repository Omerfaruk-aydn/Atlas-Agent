---
name: technical-diagrams
description: Produce architecture diagrams grounded in actual interfaces and data flow.
compatibility: Atlas native tools, permitted commands and explicitly connected MCP services; optional authoring/rendering dependencies must be verified.
metadata:
  provenance: atlas-authored-adaptation
  reviewed-sources: figma-generate-diagram, figma-use-figjam, artifact-template-system-design
---

# technical-diagrams

Use this procedure only for an assignment whose deliverable needs it. Read the
actual role contract, owned paths, user constraints and current tool descriptions.
The role's preferred skill binding is a starting recipe, not additional access.

## Capability and input preflight

- Identify the exact source, reference, requested output and acceptance criteria.
- Inspect available native tools, permitted commands and connected MCP schemas.
- Check optional runtimes/renderers with scoped commands before depending on them.
- Preserve user references, repository conventions and unrelated working changes.
- Existing authentication and permission/hook controls apply to every action.
- Report missing capability precisely; continue independent authorized work.

## Domain procedure

1. Choose a diagram for the actual question: context, component, sequence, state, deployment or data flow.

2. Inspect source-backed components, entry points, interfaces, storage and ownership before drawing. Mark proposed elements distinctly.

3. Name nodes in terms of actual responsibilities and label edges with data, protocol or action direction.

4. Show trust boundaries, external dependencies and asynchronous work where they explain the design.

5. Include error, retry, timeout and cancellation paths when they affect the decision rather than drawing only the happy path.

6. For migrations show before/after contracts and transition order; preserve compatibility and rollback constraints.

7. Use Mermaid or an installed editable diagram format for local output. Use FigJam only with connected documented tools.

8. Keep IDs stable across edits and avoid sprawling unreadable diagrams. Split views by question rather than adding unexplained nesting.

9. Cross-check diagram edges against source paths or explicitly stated proposals. Generated diagrams are not proof of deployed architecture.

10. Provide explanatory text for decisions, alternatives, tradeoffs and unresolved assumptions alongside the diagram.

11. Render or parse the final diagram with an available renderer; report unrendered source separately from inspected output.

12. Deliver editable source, rendered export when available, source references and the decisions the diagram supports.

## Select the representation by the question

Use a boundary diagram for ownership and trust, a sequence for interactions,
a state machine for lifecycle, an ERD for persisted relationships and a dependency
graph for ordering. Do not use an attractive diagram type that hides the actual
relationship the reader needs.

Inspect the relevant source, configuration or supplied design before drawing.
Record node identity, relationship evidence and scope. Separate current structure
from proposed structure using explicit labels or separate views.

## Semantic construction

For a sequence, represent success, relevant failure, cancellation and partial
effects. A response arrow does not mean durable commit unless the contract says so.
For a state machine, identify triggers, guards, ownership and terminal states.
Invalid transitions should not appear as merely unusual paths.

For data diagrams, distinguish key relationships from business associations.
For architecture diagrams, distinguish process, module, account and external
service boundaries. Label direction and payload when ambiguity affects meaning.

Use a consistent visual grammar and a legend for non-obvious symbols.
Keep labels readable, crossing edges limited and graph size aligned with the
question. More nodes do not necessarily produce more useful information.

## Tool and output discipline

Use a local supported text diagram format or actually connected diagram tools.
Do not invent hosted Figma/FigJam IDs or assume a cached plugin provides access.
For repo diagrams, retain editable source and an export if requested.

Check syntax with an available renderer/parser where practical.
Inspect rendered labels and edge direction. A syntactically valid graph can
still state a false dependency.

Example: show an authorized tool pipeline through hook evaluation, permission
checking, execution and result propagation based on real runtime code.
Do not draw a bypass around the policy layer simply to simplify the picture.

Deliver source references, editable diagram, requested render and unresolved
assumptions. Avoid claiming proposed architecture is deployed.

## Verification and recovery

Bind each important criterion to a real check and a navigable artifact or source.
Distinguish fresh executed results, cached results, structural checks, inspected
renders, skipped checks and user-reported observations. When a prerequisite fails,
stop dependent actions and retain the first failure, current identity and recovery
options. Retry within bounds only after identifying a relevant changed condition.
Do not repeat uncertain writes, suppress failures or fabricate missing evidence.

## Delivery

Return the assignment's requested output schema. For workflow tasks use the Atlas
JSON handoff with actual changed_files, checks, dependencies, risks and decision.
Include artifact paths and source provenance in evidence. A ready handoff is a
reported result for coordinator verification, never authenticated completion.
In quality-only runs do not implement fixes, change tests or certify unseen output.

# Specialist skills and deliverable workflows

Atlas ships 22 built-in specialist roles. All roles use the same catalog for
session modes, delegated agents, CLI inspection and the TUI role/model dialogs.
The eleven original roles retain their names and tool policies; their preferred
skills now add domain procedures after normal override and disabled-skill rules.

Role instructions cover domain-specific decisions, worked examples, failure
recovery and completion evidence. Skill procedures add practical input inventories,
execution branches and validation records. Their detail is determined by domain
requirements rather than a shared line count. The twenty template recipes also
include category-specific analytical and layout checks.

## New roles

- **product-designer**: Designs product flows, design systems and component states grounded in the project's actual interface. Task types: product-design, design-system. Outputs: design-spec.
- **visual-qa**: Independently checks rendered interfaces, accessibility and interaction evidence against explicit references. Task types: visual-quality, visual-qa. Outputs: findings, visual-report.
- **motion-designer**: Designs and implements interruptible motion, pointer feedback and coherent animation states. Task types: motion, animation. Outputs: implementation, motion-spec.
- **desktop-operator**: Operates desktop applications through fresh window observations and verifies the requested result. Task types: desktop, desktop-automation. Outputs: operation-result.
- **browser-operator**: Completes browser workflows with current page observations, account identity and verified outcomes. Task types: browser, browser-automation. Outputs: operation-result.
- **documents**: Creates editable documents and PDFs with source attribution and structural and rendered checks. Task types: document, documents, pdf. Outputs: artifacts.
- **presentations**: Creates editable presentations with a clear narrative and verified slide layouts. Task types: presentation, presentations. Outputs: artifacts.
- **data-analyst**: Analyzes datasets and produces auditable calculations, spreadsheets and charts. Task types: data-analysis, spreadsheet, analytics. Outputs: analysis, artifacts.
- **template-builder**: Creates reusable reference-backed templates with schemas, provenance and inspected sample outputs. Task types: template, templates. Outputs: template-package.
- **integration-engineer**: Implements and verifies MCP and service integrations with explicit authentication and failure behavior. Task types: integration, mcp. Outputs: implementation, integration-report.
- **operations**: Organizes connected messages, meetings and tasks with source fidelity and explicit action authorization. Task types: operations, scheduling. Outputs: operations-report.

New roles set `inherit_model: true`: absent a dedicated role assignment they use
the configured primary model and its ordinary fallback policy. A dedicated or
measured model selection takes precedence. An invalid assigned provider/model or
invalid measured policy fails explicitly rather than silently using another model.
Original roles preserve their existing model-resolution behavior.

## Binding skills to roles

```yaml
---
name: documents
description: Produces reference-backed editable documents.
model: documents
inherit_model: true
preferred_skills: [office-documents, artifact-templates]
---
Follow the assignment, preserve sources and report actual validation.
```

Bindings accept up to eight unique skill names. Save/render/list and the TUI's
metadata editor preserve them. `atlas_config save_subagent` accepts
`preferred_skills`: omitted preserves current bindings; an empty array clears them.
Editing metadata also preserves existing contracts and tool restrictions.

The runtime resolves built-in skills plus configured user paths, applies user
overrides and disabled-skill rules, then loads selected bodies in full. There is
no role-specific line ceiling or cost-based truncation of selected skill bodies.
Disabled, missing or model-invocation-disabled skills are marked unavailable.
Referenced resources are loaded on demand. The model's actual context window and
normal session context management still apply. No binding grants tools, permissions,
accounts, dependencies or publication authority. Skill inputs remain subordinate
to current user requirements and scoped project instructions.

## Portable domain procedures

Sixteen Atlas-authored skills cover design systems, visual QA, motion, native and
browser automation, editable documents/PDF, presentations, data analysis,
reference-backed templates, MCP integration, team operations, security evidence,
patch risk, technical diagrams, research and animated mascots.

The embedded artifact template catalog supplies all twenty reviewed structural
categories with required sources and format-specific checks. These are original
Atlas recipes, not copies of cached template artwork. User references retain
precedence. Read `atlas://skills/artifact-templates/TEMPLATES.md` through view.

Figma, Slack, Calendar and live Excel operations require actual compatible
connected tools and account access. Local Office authoring/rendering uses verified
installed libraries and renderers through permitted commands. Cached Codex files
and proprietary `@oai/sky`/`@oai/artifact-tool` packages are not bundled or assumed.
Missing dependencies are reported as blockers for affected operations, separately
from successfully completed source, structural or local checks.

## Routing and verification

Structured task types and outputs select matching contracts after required-tool
checks. English/Turkish hints cover the new domains; explicit role selection is
preferred for ambiguous compound requests. Optional external MCP capability and
authoring/rendering health are checked by the specialist before dependent work.

Workflow output follows the existing bounded JSON handoff. Artifact paths, actual
checks, source attribution and visual evidence belong in that handoff. Reported
results are not machine-authenticated proof. Independent review and machine
verification remain necessary where the contract requests them; source-only,
structural, rendered and live-account evidence must not be conflated.

## Sources and adaptation

The reviewed local plugin families were openai-curated, openai-curated-remote,
openai-primary-runtime and openai-bundled (October 2026 cache). Each added skill
records the reviewed source names in metadata. The adaptation uses Atlas tools and
original instructions; it does not modify those caches, import their approval
flows, redistribute their executable runtimes or claim access to hosted services.

## Inspect and use

```text
atlas-agent agent list --json
atlas-agent agent show product-designer
atlas-agent agent show documents
```

Choose a role through the existing mode/agent UI or select it with the agent tool.
Assign a dedicated provider/model through the existing role dialog if desired.
The new role descriptions use the existing six-language catalog.

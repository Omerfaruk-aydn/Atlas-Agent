---
name: design-system
description: Design flows, reusable components and Figma/code consistency.
compatibility: Atlas native tools, permitted commands and explicitly connected MCP services; optional authoring/rendering dependencies must be verified.
metadata:
  provenance: atlas-authored-adaptation
  reviewed-sources: figma-generate-design, figma-generate-library, figma-design-to-code, figma-code-connect, figma-swiftui
---

# design-system

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

1. Lock the user journey, target platform, real content, brand constraints and requested inventory. Distinguish a design proposal from executable implementation.

2. Inspect the repository's existing components, styles, spacing, typography and semantic tokens. Reuse suitable primitives before adding new ones.

3. Define loading, empty, error, disabled, selected and success states. Model exclusive states explicitly; preserve values during recoverable errors.

4. Build foundations before dependent components: primitive and semantic tokens, theme modes, type scale, spacing, focus and elevation.

5. Specify component properties and variants with bounded inventories. Cover relevant keyboard, pointer, touch and right-to-left behavior.

6. When Figma MCP is connected, inspect returned node IDs, design context and screenshots. Reuse mapped code components and preserve actual supplied assets.

7. Keep Figma node identity, code path, component name and token bindings in a traceable mapping. Re-observe after edits before relying on stale bounds.

8. Translate designs into the actual project framework. Screenshots are comparison targets, never substitutes for editable components.

9. Use native layout and interaction conventions for SwiftUI, mobile and terminal targets. Web effects do not imply equivalent native behavior.

10. Validate representative components and complete compositions at normal viewing scale. Repair clipping, detached content, weak hierarchy and inaccessible controls.

11. Deliver editable design specifications, token/component inventory, behavior states, implementation references and explicit remaining decisions.

12. Figma operations require a connected compatible MCP server; inspect actual tool schemas. Without it, produce a local specification or implementation and label Figma synchronization unavailable.

## Inventory and traceability worksheet

Build a focused inventory from the actual interface:
- Journeys: entry, actor, prerequisite, primary action and completion.
- Surfaces: route or native view, parent layout, viewport and theme.
- Components: code identity, public properties, variants and content limits.
- States: trigger, visible feedback, allowed action and recovery.
- Tokens: semantic purpose, theme values and owning definition.

Keep a reference-to-code map when a supplied design is involved. Record which
reference regions are implemented, intentionally adapted or still unresolved.
If connected Figma tools expose component mappings, use returned identities.
Do not manufacture node IDs or declare synchronization from a local screenshot.

## Build a coherent foundation

Inspect the existing system before proposing a new palette or type scale.
Use semantic tokens for content, surface, interaction, status and focus.
Explain when a new semantic distinction is necessary; identical current values
can serve different roles and should not always be merged.

Implement components in dependency order: foundations, primitive controls,
compositions and complete flows. Specify property defaults and forbidden state
combinations. Validate representative instances with real content before expanding
the library. A large component count is not proof of useful coverage.

For editable design files, bind supported variables and component properties
through the actual connected schema. For code, use the framework's existing
component and style system. Preserve user-supplied assets and established naming.

## Interaction and responsive acceptance

For each custom control, define accessible identity, keyboard activation,
selection state and focus behavior. For overlays, define entry, dismissal,
background interaction and return focus. Check error recovery retains relevant
input and that a disabled action explains what prerequisite is missing.

Exercise short and long content, narrow layout, text enlargement and supported
locales. Specify truncation only where the hidden content is recoverable.
Data tables and mobile cards may share data semantics without sharing layout.

Example: for a settings panel, map each edited setting to saved, dirty, saving,
saved-success and failure behavior. Verify navigation with pending edits and
retry after failure. A polished default screenshot does not cover those states.

Deliver the token and component inventory, flow/state decisions, editable source,
reference mapping and observed checks. Record external design synchronization
separately from local implementation.

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

---
name: artifact-templates
description: Build and reuse reference-backed templates across deliverable types.
compatibility: Atlas native tools, permitted commands and explicitly connected MCP services; optional authoring/rendering dependencies must be verified.
metadata:
  provenance: atlas-authored-adaptation
  reviewed-sources: template-creator, openai-templates
---

# artifact-templates

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

1. Read the embedded TEMPLATES.md catalog and resolve the selected template type, required facts and intended audience.

2. Treat an explicit user reference as the formatting source of truth; catalog recipes describe structure, not identical copied visual assets.

3. Preserve the supplied reference unchanged. Produce a separate output and keep a source manifest with path, type and version.

4. Extract styles, layouts, sections, placeholders, master slides or workbook structure using an actual compatible authoring tool.

5. Define which fields are required, optional, repeated or conditional. Keep unavailable values visibly unresolved rather than inventing facts.

6. Make typography, spacing, colors and repeated elements reproducible. Preserve meaningful page setup, headers, footers and table conventions.

7. Keep content, style and data bindings separate so subsequent runs can reuse the template without stale facts.

8. Store templates only in an authorized project/user location; do not edit plugin caches or machine-managed runtime dependencies.

9. Create a representative output using supplied fixture data and inspect actual rendered pages/slides/sheets when a renderer is available.

10. Validate round-trip editability and missing/long/unicode input behavior. Record limits for unsupported features.

11. Version the schema and template manifest; retain source attribution and compatibility information. Use skill_manage for supported Atlas skill packaging.

12. Deliver the editable template, field/schema description, source manifest, sample output and actual validation evidence.

## Choose a recipe and define the input contract

Read crush://skills/artifact-templates/TEMPLATES.md through view for the requested
category. Choose it by deliverable purpose, then adapt to the user's actual
reference. The catalog provides original structural recipes, not copied artwork
or an automatic authoring backend.

Identify stable structure, editable fields, repeatable sections and instance data.
Define required/optional values, types, units, bounds and meaningful defaults.
Reject decisive missing inputs explicitly instead of placing example facts in
the final artifact.

## Reuse and overflow procedure

Separate input validation from format-specific rendering. Preserve semantic
styles and editable objects. Define what happens when content is shorter,
longer or more numerous than the reference.

Exercise minimal, representative and relevant stress fixtures:
- omitted optional sections;
- long headings and multilingual labels;
- extra table rows or additional sections;
- boundary numeric/date values when applicable.

Label synthetic fixtures as examples and keep confidential reference values out
of reusable defaults. Verify that missing sections do not leave blank headings
or broken pagination.

## Package integrity

Deliver the schema, editable template, generation instructions, fixtures,
inspected sample outputs and provenance. Identify dependencies actually verified
in the chosen runtime.

A changed schema needs compatibility guidance for existing inputs.
A visual change may also affect overflow behavior; inspect it rather than treating
it as automatically harmless.

Example: a monthly review template derives metric differences from current and
comparison inputs. Generate a short and a dense instance, then inspect tables,
charts and page breaks. Hardcoded sample percentages are not reusable formulas.

Read the appropriate Office or data skill when the requested format needs its
authoring and rendering checks. Do not flatten required editable output to hide
unsupported features.

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


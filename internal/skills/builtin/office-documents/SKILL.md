---
name: office-documents
description: Create editable documents and PDFs with structural and rendered verification.
compatibility: Atlas native tools, permitted commands and explicitly connected MCP services; optional authoring/rendering dependencies must be verified.
metadata:
  provenance: atlas-authored-adaptation
  reviewed-sources: documents, pdf
---

# office-documents

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

1. Identify audience, purpose, required format, source facts and the reference/template. Preserve the user's requested content and layout.

2. Use actual source extraction through available tools or permitted commands, retaining page/section attribution. Extracted text is not a layout template.

3. Inspect installed authoring and rendering capabilities with scoped commands or connected tools. Use compatible local libraries; do not assume Codex runtime packages exist.

4. For DOCX use paragraph/table styles, sections and document metadata rather than ad hoc spacing. Keep headings, tables, headers and footers editable.

5. Use proper list definitions and table widths; repeat headers where needed and verify page breaks, widow/orphan behavior and footer numbering.

6. Preserve original files and make a new output unless modification was requested. Redlines and comments must identify the actual affected text.

7. Use an available PDF library for PDF creation or form editing. Preserve fillable fields and distinguish flattened from editable output.

8. Render through an available Office/PDF renderer and inspect page images at readable scale. Confirm the renderer really produced each page.

9. Check content completeness, broken Unicode, clipped text, image resolution, links, table overflow and font substitution.

10. Reopen or parse the final file independently to confirm package integrity and expected structures. File creation alone does not establish visual quality.

11. When rendering is unavailable, report structural checks separately and retain the editable output; do not label an unseen document visually verified.

12. Deliver final paths, formats, source provenance, observed page counts, checks and any unverified layout criteria. Load artifact-templates/TEMPLATES.md for selected templates.

## Document production plan

Create a content map before formatting: sections, source facts, tables, figures,
references and unresolved inputs. Establish audience, editorial scope and the
requested editable/export formats. Preserve source attribution during rewriting.

Inspect installed libraries and renderers through permitted commands. Test a
minimal relevant operation before depending on a feature such as comments,
tracked changes, fillable forms or field updates. An importable package does not
guarantee those capabilities.

For DOCX, build semantic styles and section structure first. Use real paragraphs,
lists and tables. Set page geometry and header/footer behavior deliberately.
Do not align text with spaces or flatten editable content into screenshots.

## Format-specific branches

- New document: create stable styles, populate real content and render.
- Existing document edit: preserve out-of-scope formatting and annotations;
  inspect affected sections after saving.
- Redline/comment work: anchor changes to actual text and distinguish proposed
  edits from accepted edits.
- PDF creation: preserve layout, fonts, images and selectable text where relevant.
- PDF extraction: retain page attribution and distinguish text extraction from OCR.
- PDF forms: verify actual field types and values; drawing a box is not a field.

For word-processor fields, determine whether the chosen engine can refresh them.
Preserved field instructions and freshly calculated page references are different
results. Report limitations without pretending cached fields are current.

## Layout defect triage

Inspect missing glyphs, substituted fonts, clipped tables, orphaned headings,
blank pages, image distortion and footer numbering. Fix the editable source and
rerender; never repair only a preview image.

For multilingual documents, inspect mixed-direction lines, punctuation and table
alignment. A parseable file with missing glyphs is not a usable final artifact.

If rendering is unavailable, retain the source and report structural validation
separately. Do not label unseen pages visually verified.

## Example and final checks

For a memo with a financial table, reconcile source numbers, create consistent
styles, check table units and precision, render and inspect pagination.
If one value lacks a source, mark it unresolved rather than inventing a figure.

Reopen the final package independently, confirm expected sections and page output,
and deliver editable source plus the requested export. Identify actual reviewed
pages or sampling boundaries and remaining editorial decisions.

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

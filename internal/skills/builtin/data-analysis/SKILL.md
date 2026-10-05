---
name: data-analysis
description: Analyze source data and produce auditable spreadsheets and charts.
compatibility: Atlas native tools, permitted commands and explicitly connected MCP services; optional authoring/rendering dependencies must be verified.
metadata:
  provenance: atlas-authored-adaptation
  reviewed-sources: spreadsheets, visualize, artifact-template-experiment-analysis
---

# data-analysis

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

1. Record source files, schema, units, timezone, date range, missing values and the precise question. Preserve the original dataset.

2. Inspect available analysis and spreadsheet libraries through scoped commands or connected tools. Native Excel control is a separate capability requiring a real connection.

3. Profile counts, duplicate keys, ranges and malformed rows before joining or aggregating. Keep excluded rows and exclusion reasons traceable.

4. Separate raw inputs, assumptions, calculations and outputs. Use labels, units and explicit scenario selectors.

5. Write formulas using the selected library's supported syntax; preserve editable formulas and independent input cells.

6. Check workbook recalculation capability and inspect cached values after reopening. A formula string is not a calculated result.

7. Verify totals, cross-sheet links, boundary cases, units and representative results independently. Detect errors and accidental circular references.

8. Keep historical actuals distinct from forecasts and assumptions. Make sensitivity/scenario behavior inspectable.

9. Use charts that answer the question; label axes, units, sample size and uncertainty. Do not invent data to fill a template.

10. For experiments distinguish hypothesis, design, population, comparison, effect, limitations and unsupported causal claims.

11. Inspect rendered sheets/charts for clipped labels, readability, number formats and meaningful conditional formatting.

12. Deliver the analysis method, transformed data, editable workbook/chart, source attribution and observed checks; report unsupported recalculation or live-control features.

## Source audit and transformation record

Define the analytical unit, metric, period, exclusions and comparison before
calculating. Record source snapshot, column meanings, keys, units and time zones.
Keep raw inputs unchanged and maintain a reproducible transformation record.

Profile duplicates, missing values, invalid types, ranges and category counts.
Before a join, check uniqueness on each side and the expected cardinality.
After it, reconcile row counts and material totals. A many-to-many join can
inflate values while every command succeeds.

Distinguish missing from zero. Document imputations, exclusions and their impact.
Do not infer universal facts from a filtered or convenience sample.

## Spreadsheet modeling procedure

Separate editable assumptions, calculations and outputs. Preserve existing named
ranges, formulas and external links outside scope. Use formulas for derived
values when the user needs an editable model.

Check relative/absolute references, date formats, locale-dependent numeric text,
error cells and circular references. Determine whether the available authoring
library recalculates formulas or only stores them.

Recalculate through a compatible available engine when required. Compare key
totals to independent computations. Mark unavailable recalculation precisely;
cached workbook values are not evidence of a fresh result.

## Connected live-workbook branch

When a compatible live Excel tool is actually connected, resolve its active
workbook and sheet identities before reading or editing. Inspect the current
selection and existing formulas through the available schema. Preserve unsaved
user work and distinguish live-session state from an on-disk workbook snapshot.

Apply requested edits in bounded ranges and read back relevant values and formulas.
Use the connected engine's actual calculation capability when available.
Do not overwrite the workbook by generating a local file with the same name.
If no compatible live session exists, use the requested standalone-file workflow
or report the specific missing capability; do not claim live Excel control.

## Statistical and chart checks

State denominators and weights. A pooled average differs from an average of
averages. Distinguish descriptive correlation from causal evidence.
For experiments, retain assignment unit, exposure, outcome window and sample size.
Disclose exploratory metric selection.

Choose chart form by the question. Label scope, dates, units and uncertainty.
Inspect rendered labels, legends and scales; keep the underlying data accessible.

## Worked reconciliation example

For net revenue, confirm transaction grain and refund sign, deduplicate using
the documented key and group by the correct business date. Reconcile monthly
totals to the source ledger and identify excluded rows.

Deliver derivation, workbook or analysis source, actual reconciliation checks
and interpretation. Separate observed values from explanations and forecasts.

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

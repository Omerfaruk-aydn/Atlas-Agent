# Atlas artifact template recipes

These are Atlas-authored structural recipes inspired by the reviewed template
categories. They do not contain or reproduce the cached reference artwork. An
explicit user reference controls appearance and content; the recipes supply an
editable outline when no more specific structure was requested.

## Common manifest and validation

Keep name, schema_version, artifact_type, source_refs, required_fields, optional_fields,
output_path and compatibility in an authorized template manifest. Preserve the
original reference and record missing values. Use only verified authoring/rendering
capabilities. Reopen the final editable output, inspect actual renders when available,
and report source/structural/visual evidence separately. Do not fill a template with
invented facts or assumptions presented as actual measurements.

## System Design

Domain checks:
- Record authoritative module ownership and the runtime entry point for each boundary; distinguish deployed behavior from the proposed design.
- Include cancellation, overload, failed-step recovery and reader/writer coexistence for changed persistent state. Tie each proposed invariant to its enforcing layer.

- Artifact type: docx.
- Structure: Requirements, components, interfaces, data flows, decisions, migration and operations.
- Required inputs: Actual source paths, proposed boundaries, alternatives and failure/recovery constraints.
- Output: editable docx; optional user-requested exports.
- Checks: source fidelity, complete requested fields, package integrity, actual
  rendering where available, and format-specific behavior.
- Document checks: heading/list/table structure, page setup, headers/footers,
  links, Unicode and inspected pagination.

## Design Report

Domain checks:
- Map reference regions to actual components and states; retain the viewport, theme and locale used for visual evidence.
- Separate product decisions, implementation observations and unresolved references. Include the keyboard and error-recovery path for consequential controls.

- Artifact type: docx.
- Structure: Brief, audience, flows, design principles, token/component inventory, states and verification.
- Required inputs: Explicit reference, actual screens, design decisions, observed defects and unresolved states.
- Output: editable docx; optional user-requested exports.
- Checks: source fidelity, complete requested fields, package integrity, actual
  rendering where available, and format-specific behavior.
- Document checks: heading/list/table structure, page setup, headers/footers,
  links, Unicode and inspected pagination.

## Experiment Analysis

Domain checks:
- State assignment or sampling unit, manipulated factor, baseline, exclusions and the observation that would disprove the hypothesis.
- Retain repeated measurements when variability matters. Do not conflate total task duration with provider or tool latency without instrumentation.

- Artifact type: docx.
- Structure: Hypothesis, design, workload, baseline, results, interpretation, limitations and next steps.
- Required inputs: Real measurements, sample size, method, exclusions and relevant uncertainty.
- Output: editable docx; optional user-requested exports.
- Checks: source fidelity, complete requested fields, package integrity, actual
  rendering where available, and format-specific behavior.
- Document checks: heading/list/table structure, page setup, headers/footers,
  links, Unicode and inspected pagination.

## Strategy Memorandum

Domain checks:
- Lead with the actual decision and alternatives evaluated against the same constraints.
- Separate confirmed facts from assumptions and proposals; associate next actions with agreed ownership and decision conditions rather than invented commitments.

- Artifact type: docx.
- Structure: Decision, context, goals, options, tradeoffs, recommendation, owners and next actions.
- Required inputs: Source-backed facts, alternatives and actual constraints; no invented commitments.
- Output: editable docx; optional user-requested exports.
- Checks: source fidelity, complete requested fields, package integrity, actual
  rendering where available, and format-specific behavior.
- Document checks: heading/list/table structure, page setup, headers/footers,
  links, Unicode and inspected pagination.

## Legal Memorandum

Domain checks:
- Identify jurisdiction, effective dates and the precise supplied question. Retain authoritative source references and material quoted language.
- Distinguish source-backed interpretation from unresolved legal judgment. Do not invent an authority or treat an older rule as current without verification.

- Artifact type: docx.
- Structure: Question, provided facts, applicable source material, analysis, uncertainties and requested next actions.
- Required inputs: User-provided scope and authoritative dated sources; distinguish unresolved interpretations.
- Output: editable docx; optional user-requested exports.
- Checks: source fidelity, complete requested fields, package integrity, actual
  rendering where available, and format-specific behavior.
- Document checks: heading/list/table structure, page setup, headers/footers,
  links, Unicode and inspected pagination.

## Investment Committee Memo

Domain checks:
- Keep source date, units, valuation basis and model assumptions visible; reconcile the recommendation to the actual supporting model.
- Include relevant sensitivity cases and downside assumptions without inventing probabilities. Separate an analytical recommendation from authorization to transact.

- Artifact type: docx.
- Structure: Proposal, assumptions, model, scenarios, supporting evidence, risks and decision request.
- Required inputs: Actual financial inputs, explicit assumptions, sensitivities and source dates.
- Output: editable docx; optional user-requested exports.
- Checks: source fidelity, complete requested fields, package integrity, actual
  rendering where available, and format-specific behavior.
- Document checks: heading/list/table structure, page setup, headers/footers,
  links, Unicode and inspected pagination.

## Minimal Letterhead

Domain checks:
- Preserve actual sender identity and contact details; do not fabricate signatures, credentials or affiliations.
- Verify long recipient names, body overflow, continuation pages and whether letterhead/footer placement survives editing.

- Artifact type: docx.
- Structure: Sender/recipient, date, subject, opening purpose, body, requested action and signature.
- Required inputs: Actual identity/contact details, user tone and supplied branding; preserve page setup.
- Output: editable docx; optional user-requested exports.
- Checks: source fidelity, complete requested fields, package integrity, actual
  rendering where available, and format-specific behavior.
- Document checks: heading/list/table structure, page setup, headers/footers,
  links, Unicode and inspected pagination.

## Three-Statement Forecast

Domain checks:
- Separate historical actuals, editable assumptions and formulas. Link statements with explicit sign conventions and traceable periods.
- Check that the balance sheet balances, cash movement reconciles and scenario changes propagate through all three statements. Disclose unavailable formula recalculation.

- Artifact type: xlsx.
- Structure: Inputs, historical actuals, assumptions, income statement, balance sheet, cash flow and scenarios.
- Required inputs: Real source values, explicit model links, units, periods and balance/reconciliation checks.
- Output: editable xlsx; optional user-requested exports.
- Checks: source fidelity, complete requested fields, package integrity, actual
  rendering where available, and format-specific behavior.
- Workbook checks: formula links, supported recalculation, totals, units, scenarios
  and readable chart/sheet layout.

## Financial Budget

Domain checks:
- Define actual, budget and forecast periods, variance direction and currency/unit treatment before calculation.
- Reconcile row and departmental totals to the supplied ledger; test sensitivity changes and distinguish input cells from derived values.

- Artifact type: xlsx.
- Structure: Actuals, budget, forecast, drivers, variances and sensitivity cases.
- Required inputs: Source ledger, period definitions, sign conventions, totals and formula recalculation.
- Output: editable xlsx; optional user-requested exports.
- Checks: source fidelity, complete requested fields, package integrity, actual
  rendering where available, and format-specific behavior.
- Workbook checks: formula links, supported recalculation, totals, units, scenarios
  and readable chart/sheet layout.

## Sales Pipeline

Domain checks:
- Use durable opportunity IDs, actual stage definitions and explicit close-date assumptions. Validate stage probabilities rather than inventing them.
- Deduplicate opportunities and reconcile unweighted and weighted totals. Preserve multiple currencies or convert only with the authorized rate source.

- Artifact type: xlsx.
- Structure: Opportunity ID, owner, stage, amount, probability, dates and summary.
- Required inputs: Actual opportunities, stage definitions, duplicate checks and weighted totals.
- Output: editable xlsx; optional user-requested exports.
- Checks: source fidelity, complete requested fields, package integrity, actual
  rendering where available, and format-specific behavior.
- Workbook checks: formula links, supported recalculation, totals, units, scenarios
  and readable chart/sheet layout.

## Project Tracker

Domain checks:
- Assign stable task identities and distinguish requested, running, blocked, ready and verified-complete states according to the actual workflow.
- Validate dependency ordering and completion evidence. A declared status or filled percentage does not independently prove delivery.

- Artifact type: xlsx.
- Structure: Workstreams, tasks, owner, dependencies, status, dates, criteria and blockers.
- Required inputs: Actual assignments, stable task IDs and observed completion evidence.
- Output: editable xlsx; optional user-requested exports.
- Checks: source fidelity, complete requested fields, package integrity, actual
  rendering where available, and format-specific behavior.
- Workbook checks: formula links, supported recalculation, totals, units, scenarios
  and readable chart/sheet layout.

## Operating Calendar

Domain checks:
- Preserve event timezone, all-day semantics, working-day conventions and recurrence exceptions.
- Check overlapping commitments, relative-date resolution and boundary dates. An inaccessible participant calendar remains unknown rather than free.

- Artifact type: xlsx.
- Structure: Periods, events, owners, dependencies, deadlines and timezone notes.
- Required inputs: Actual dates, recurrence rules, working calendar and conflicting commitments.
- Output: editable xlsx; optional user-requested exports.
- Checks: source fidelity, complete requested fields, package integrity, actual
  rendering where available, and format-specific behavior.
- Workbook checks: formula links, supported recalculation, totals, units, scenarios
  and readable chart/sheet layout.

## Analytics Dashboard

Domain checks:
- Provide metric definitions, source grain, refresh identity, exclusions and filter semantics alongside charts.
- Reconcile filtered and unfiltered totals, inspect missing data and test empty-result behavior. Charts must remain truthful when the selected period changes.

- Artifact type: xlsx.
- Structure: Source data, metric definitions, transformations, summaries, charts and filters.
- Required inputs: Real data, units, cohort/time definitions, exclusion rules and independent totals.
- Output: editable xlsx; optional user-requested exports.
- Checks: source fidelity, complete requested fields, package integrity, actual
  rendering where available, and format-specific behavior.
- Workbook checks: formula links, supported recalculation, totals, units, scenarios
  and readable chart/sheet layout.

## Business Review

Domain checks:
- Use comparable periods, metric definitions and denominators. Distinguish achieved results, forecasts and explanations.
- Present drivers and decisions supported by actual evidence. Verify that the summary agrees with detailed source tables and charts.

- Artifact type: pptx.
- Structure: Decision summary, performance, drivers, results, risks and next actions.
- Required inputs: Source metrics, period comparisons, actual achievements and responsible owners.
- Output: editable pptx; optional user-requested exports.
- Checks: source fidelity, complete requested fields, package integrity, actual
  rendering where available, and format-specific behavior.
- Slide checks: exact requested count, native editable objects, complete content,
  image handling, font substitution and inspected slide layout.

## Operating Review

Domain checks:
- Separate completed work, planned work and unresolved blockers using dated observations.
- Show dependencies and actual decision owners. Avoid implying that a proposed recovery date or action has already been accepted.

- Artifact type: pptx.
- Structure: Current state, plan versus actual, blockers, dependencies and actions.
- Required inputs: Actual work progress, relevant metrics, unresolved issues and dated source evidence.
- Output: editable pptx; optional user-requested exports.
- Checks: source fidelity, complete requested fields, package integrity, actual
  rendering where available, and format-specific behavior.
- Slide checks: exact requested count, native editable objects, complete content,
  image handling, font substitution and inspected slide layout.

## Market Trends Report

Domain checks:
- Align source dates, market definitions and comparison units before drawing a trend.
- Separate observed changes from causal explanations and forecasts. Include contradictory evidence that materially affects the stated implication.

- Artifact type: pptx.
- Structure: Question, sources, trends, comparative evidence, implications and uncertainty.
- Required inputs: Primary dated sources, consistent comparisons and explicit limits.
- Output: editable pptx; optional user-requested exports.
- Checks: source fidelity, complete requested fields, package integrity, actual
  rendering where available, and format-specific behavior.
- Slide checks: exact requested count, native editable objects, complete content,
  image handling, font substitution and inspected slide layout.

## Project Kickoff

Domain checks:
- Confirm scope, exclusions, success criteria and actual ownership before composing milestones.
- Represent dependency and decision points clearly. Label proposed dates as proposals and retain unresolved prerequisites.

- Artifact type: pptx.
- Structure: Purpose, scope, success criteria, stakeholders, deliverables, schedule and risks.
- Required inputs: Accepted requirements, actual ownership, dependencies and decision points.
- Output: editable pptx; optional user-requested exports.
- Checks: source fidelity, complete requested fields, package integrity, actual
  rendering where available, and format-specific behavior.
- Slide checks: exact requested count, native editable objects, complete content,
  image handling, font substitution and inspected slide layout.

## Team Alignment

Domain checks:
- Distinguish agreed priorities and roles from unresolved questions or proposed changes.
- Make coordination rules and follow-through observable. Do not present a polished slide as proof that participants accepted a decision.

- Artifact type: pptx.
- Structure: Shared goals, priorities, roles, coordination rules, decisions and follow-through.
- Required inputs: Actual participants, user constraints and unresolved alignment questions.
- Output: editable pptx; optional user-requested exports.
- Checks: source fidelity, complete requested fields, package integrity, actual
  rendering where available, and format-specific behavior.
- Slide checks: exact requested count, native editable objects, complete content,
  image handling, font substitution and inspected slide layout.

## Simple Light Mode

Domain checks:
- Inspect light-surface contrast, subtle borders and disabled controls at presentation scale.
- Keep charts and diagrams readable without heavy shadows; test long titles and images against the chosen background.

- Artifact type: pptx.
- Structure: A restrained light theme with a coherent type scale, editable content and meaningful layouts.
- Required inputs: Requested narrative, real data/assets and target presentation size.
- Output: editable pptx; optional user-requested exports.
- Checks: source fidelity, complete requested fields, package integrity, actual
  rendering where available, and format-specific behavior.
- Slide checks: exact requested count, native editable objects, complete content,
  image handling, font substitution and inspected slide layout.

## Simple Dark Mode

Domain checks:
- Inspect chart labels, muted text, accent colors and image edges against the dark surface.
- Avoid excessive glow or color-only status cues. Verify exports and projection-scale readability in the actual rendering environment.

- Artifact type: pptx.
- Structure: A restrained dark theme with legible text/charts and consistent editable layouts.
- Required inputs: Requested narrative, real data/assets and tested contrast at presentation scale.
- Output: editable pptx; optional user-requested exports.
- Checks: source fidelity, complete requested fields, package integrity, actual
  rendering where available, and format-specific behavior.
- Slide checks: exact requested count, native editable objects, complete content,
  image handling, font substitution and inspected slide layout.

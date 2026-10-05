---
name: patch-review
description: Assess changed behavior, regression risk and recoverability from source evidence.
compatibility: Atlas native tools, permitted commands and explicitly connected MCP services; optional authoring/rendering dependencies must be verified.
metadata:
  provenance: atlas-authored-adaptation
  reviewed-sources: assess-patch-risk, security-diff-scan
---

# patch-review

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

1. Resolve the exact immutable patch/revision and intended problem. Inspect changed files plus callers, configuration and execution entry points.

2. Identify altered public contracts, defaults, compatibility, state ownership and error behavior. Distinguish a local edit from the whole reachable change.

3. Check authentication, authorization, trust boundaries and secret handling where affected; do not broaden a small review into an unrelated security audit.

4. Inspect migration, upgrade/downgrade and restart behavior for persistent state or configuration changes.

5. Map significant acceptance criteria to actual tests and runtime evidence. Inspect assertions, setup and skipped environments rather than trusting test names.

6. Run scoped commands only within existing authorization and tool restrictions. Independent review does not alter source or weaken tests.

7. Evaluate rollback/recovery of the actual result; a revert may not restore migrated data or external side effects.

8. Locate each defect at an actual changed-source range, with trigger, consequence and evidence. Keep uncertainty separate from confirmed defects.

9. Reject fabricated locations, hypothetical findings with no reachable mechanism and style-only objections framed as correctness defects.

10. Report a scoped verdict: ready for integration, changes required, or blocked for specific decisive evidence. Clean review is not universal certification.

11. Preserve patch identity and source fingerprint in the handoff so later edits invalidate prior approval evidence.

12. Use Atlas review/verify workflow gates for final integration; this skill does not grant merge, release or publication permission.

## Freeze and trace the review target

Resolve commit range, patch identity or current working-tree fingerprint.
Read the intended behavior and changed source, then follow affected callers,
configuration defaults, serialization and storage boundaries.

Identify behavior altered beyond the edited function. A field rename can affect
persisted config, generated schemas, UI editing and runtime resolution.
Inspect those consumers rather than judging only the local diff.

## Risk and evidence matrix

| Boundary | Questions |
| --- | --- |
| Public contract | Do callers preserve valid inputs, defaults and error handling? |
| Persistence | Can old state load, migrate and recover after interruption? |
| Identity | Are authentication and authorization still enforced? |
| Concurrency | Does cancellation, ownership or cleanup change? |
| UI/artifacts | Does rendered or interactive behavior have appropriate evidence? |
| External writes | Can retry duplicate an action or hide partial success? |

Select relevant boundaries and cite concrete paths. Do not turn a small review
into an unrelated whole-repository audit.

## Tests and recoverability

Read assertions and fixtures, not only test names. Determine whether evidence
exercises the actual changed contract. Distinguish mock coverage, structural
checks, rendered inspection and live-account results.

Review rollback of both code and data. A revert cannot undo a completed external
send or destructive migration. Identify the actual recovery mechanism where
effects survive code rollback.

Independent review does not modify source or weaken tests. If the target changes,
refresh review identity and revisit affected evidence.

## Finding and verdict example

If a metadata edit drops a tool restriction, reproduce save/reload with the
original restricted role, identify the changed serialization boundary and show
the resulting wider execution policy. A style concern is not equivalent to that
runtime regression.

Report actionable defects with trigger, consequence, location and evidence.
Use the assignment's actual verdict schema. A clean scoped review grants neither
merge nor release authority and does not certify unrelated runtime states.

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

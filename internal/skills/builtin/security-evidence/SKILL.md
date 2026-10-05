---
name: security-evidence
description: Investigate and verify security findings with explicit source-backed stages.
compatibility: Atlas native tools, permitted commands and explicitly connected MCP services; optional authoring/rendering dependencies must be verified.
metadata:
  provenance: atlas-authored-adaptation
  reviewed-sources: codex-security
---

# security-evidence

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

1. Lock the repository revision, requested scope, trust boundaries, attacker control and relevant security policy before analysis.

2. Distinguish repository scan, change review, imported finding triage and fix verification; choose the relevant scope rather than running every phase.

3. Map entry points, identities, permission checks, sensitive data and external interfaces into a concise threat model.

4. Trace each candidate from controlled input through guards to a reachable dangerous operation; inspect callers and effective configuration.

5. Separate observed evidence, source-supported inference and unresolved assumptions. Keep severity calibrated to reachable impact.

6. Validate candidates with safe reproductions or decisive source analysis. Do not weaken protection or exploit unrelated live systems.

7. Deduplicate common root causes while retaining distinct affected boundaries. A generic best-practice suggestion is not a vulnerability finding.

8. For requested fixes change the enforcing layer, cover bypasses and preserve normal authorized behavior.

9. Verify the original attack path fails after the fix and valid behavior still works. Source inspection and executed reproduction are different evidence.

10. Review patch risk, compatibility, migration and recovery separately from vulnerability validity. Do not auto-merge or publish without authorization.

11. Report scope and coverage gaps; no findings does not mean the entire system is secure. Track external issues only when authorized.

12. Return bounded findings with actual file ranges, attacker control, path, impact, confidence, reproduction and remediation evidence. Codex scan services are not assumed available.

## Choose the assessment path

| Request | Primary work | Completion evidence |
| --- | --- | --- |
| Repository assessment | Scope, trust model, candidate discovery and validation | Reachable findings and coverage boundaries |
| Change review | Immutable patch and altered boundaries | Trigger, consequence and changed-source location |
| Imported finding | Match report to actual code/configuration | Valid, plausible, disproven or unresolved result |
| Authorized fix | Repair enforcing layer | Original path rejected and legitimate path retained |
| Fix verification | Reproduce reported boundary | Observed remediation and remaining bypasses |

Do not run every phase for every request. Fix verification and repository
discovery answer different questions.

## Candidate validation worksheet

Record attacker control, entry point, transformations, guards, sensitive sink,
effective configuration and reachability. Inspect callers that establish identity
and authorization. A missing check in one helper may be enforced before the
helper; trace the path before reporting it.

State a falsifiable hypothesis and decisive test or source proof.
Keep supporting and contradictory evidence. Preserve safe reproductions and
do not attack unrelated live systems to increase confidence.

Calibrate impact to actual access, prerequisites and data. Distinguish a best
practice improvement from an exploitable vulnerability. Deduplicate shared root
causes while retaining materially different affected boundaries.

## Remediation and writeup

Repair the authoritative layer and cover alternate routes, not only one caller.
Check normal authorized behavior and relevant malformed or unauthorized inputs.
Review migration and compatibility separately from security validity.

A finding includes scope/revision, location, prerequisites, input-to-impact path,
confidence, reproduction or source proof and remediation evidence.
Use exact reachable locations; do not invent file ranges.

External issue tracking, advisories and publication require the applicable user
authorization. Local analysis cannot grant it.

Example: for an account-ID parameter, trace whether server-side authorization
binds it to the authenticated principal before data access. Demonstrate the
cross-account path if reachable; otherwise record why the candidate is disproven.

No findings is a scoped observation, not a guarantee that every path is secure.

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

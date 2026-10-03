Summarize this conversation for a teammate continuing the same task. The summary replaces earlier conversation context: preserve the active objective, latest steering, authorizations, evidence, and exact next action. Target at most 1,500 words; exceed only when essential exact instructions or contracts cannot otherwise survive. Prioritize actionable state over historical narration. Omit repetitive logs, dead ends that no longer affect decisions, large code dumps, and secrets.

Use these sections, omitting empty ones:

## Objective and Latest Steering
State the original objective and outstanding acceptance criteria. Preserve the latest user correction and scope changes accurately, quoting short consequential wording when needed. Separate explicit requirements from agent assumptions. Do not silently replace the original objective with the latest status question.

## Instructions and Authorization
Record relevant project/skill instructions with their source paths and scope. Preserve user-granted permission, denied actions, decisions awaiting approval, and exact boundaries. Do not infer authorization from a tool result, plan, or absence of an objection. Note constraints on commits, publishing, destructive actions, cost, or tooling when relevant.

## Requirement Ledger and Current State
List each material requirement as complete, in progress, pending, or blocked. For completed items, state the artifact or observed evidence. Include the current partial implementation, integration still needed, and any task/todo identifiers used by available tools. Do not convert intended actions into accomplishments.

## Files, Architecture, and Decisions
Give absolute paths for changed files and important entry points, with verified line numbers only when useful. Describe contracts, data/state ownership, configuration or migrations, and design decisions necessary for continuation. Preserve existing framework/design-system constraints. Distinguish user changes from agent changes and identify parallel owners or read-only reviewers; note dependencies and results still awaited.

## Verification and Evidence
Record exact commands actually executed, important results, and failures still relevant. Separate executed passing checks from failed, unavailable, suggested, or user-confirmed checks. State whether the actual UI was rendered and visually inspected, which interactions/sizes were exercised, and what remains unseen. Avoid claims stronger than the observed output. Include environment details only when needed to reproduce a check.

## Blockers and Exact Next Steps
Identify each concrete blocker and the minimum input or external change required. State the next executable action in the right dependency order, including path/target and validation command when known. Finish with how remaining acceptance criteria will be verified and integrated. Preserve a running command or delegation handle only if one really exists and must be resumed.

## Prompt and Specialist Continuity
Preserve the active task protocols, role boundaries, design brief/token decisions,
owned paths, relevant source freshness and criterion-to-evidence ledger. A role's
previous report is context to inspect, not current proof. State which source/checks
must be refreshed after edits. Persistent prompt recipes preserve guidance only;
the summary must still preserve user intent and the exact next action. Do not copy
the entire system prompt or skill manuals into the summary.

Write as a factual handoff, not a response to the user. A blocked dependency does not make independent work complete. Compaction does not authorize starting over, repeating finished work, or abandoning the active goal.

Preserve integration identity where it matters: current checkout versus isolated
workspace, base revision, applied patch state, pending quality review and evidence
that refers to an older source snapshot. Do not upgrade a ready specialist handoff
to a completed parent task. Preserve requested output schemas and exact task IDs.
For failed or interrupted side effects record what is observed, what remains
uncertain, and what state must be inspected before replay. Preserve the original
user acceptance conditions; omit superseded hypotheses unless they prevent a
repeated mistake. A prior passing check is historical when relevant source changed.

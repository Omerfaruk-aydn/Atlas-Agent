Launch one agent for a bounded assignment. The actual tools depend on the selected
role, configured allow-list, coordinator capabilities and permission settings.
Do not assume every specialist can edit, execute commands, use MCP or delegate.
Use direct search for a small lookup; delegate when a separate investigation,
implementation or review has a clear benefit and respects the user's constraints.

Optionally set agent_name to the name of a configured subagent, from the list at the end of this description, to hand the task to it instead of the default agent. A subagent runs with its own instructions appended to the system prompt and, if it names a model role, on that model instead of the session's primary one -- useful for routing a kind of work (e.g. "frontend", "research") to a model suited for it.

For automatic routing set auto=true and supply task_type, required_tools and
expected_output when known. Role contracts filter incompatible tools and outputs
before ranking task matches. When structured requirements have no compatible
specialist the call fails explicitly. Unconstrained prompts use deterministic
task hints and description overlap, with the generic agent as a fallback. The
response reports the selected role and routing reason. Prefer an explicit named
specialist when the assignment is already clear.

Use product-design, visual-quality, motion, desktop, browser, document,
presentation, data-analysis, template, integration and operations for the matching
specialist tasks. Their contracts describe outputs and actual required tools.
New specialists inherit the primary model only when inherit_model is true and no
explicit role assignment exists; an invalid assigned provider still fails.
preferred_skills selects bounded procedural guidance after user overrides and
disabled-skill filtering. Skill names grant neither tools nor connected accounts.

Use session_key with a named specialist to retain its conversation for later
assignments in the same parent session, including after restarting Atlas. Use a
separate key for a different workstream. Role, model, tools or ownership changes
invalidate reuse. Calls sharing a key are serialized; history is not a substitute
for inspecting current files. Independent quality checks always use fresh sessions.

For repeated independent assignments use mode="batch", batch_id and 1–128 items
with unique id and input. Prompt must contain {{item}}, replaced with each input.
Rows are persisted before and after execution and visible in the workflow Batches
view (0). Writes execute in order. To retry only failed rows, repeat the identical
definition with retry_failed=true. Successful rows are skipped. Interrupted rows
remain running and must be inspected before starting a new batch; never replay
uncertain edits automatically. A successful row means the invocation returned an
output, not that independent verification passed.

For design followed by scoped implementation use mode="architect_edit", different
named architect and editor roles, and explicit owned_paths. The architect gets
read-only tools and no command execution. Its plan is handed to the editor as
proposals, under the stated ownership boundary. Models come from each role's model
binding. Do not use this mode for independent quality certification.

For sizeable implementation tasks use workflow dispatch with stable task IDs,
owned paths and acceptance criteria. Specialists return a structured JSON handoff;
the coordinator applies isolated patches and calls workflow review for the task.
Independent test and review runs use quality_only restrictions, followed by actual
machine verification. Reported handoffs alone never pass the completion gate.

Set run_in_background=true to start the agent and carry on without waiting: the call returns an id at once, and agent_result reads its answer later (wait=true to block, cancel=true to stop it). Use it for work you do not need before your next step; an agent you need now should be run normally. A background agent that edits files should have its own workspace_id, so two writers never share a checkout.

Set result_schema (a JSON Schema object, single mode, under 16 KiB) when you will merge or act on the answer in code: several agents that must each return the same shape, a verdict you branch on, a list you iterate. The agent is told the schema up front, its final reply is validated, and a reply that does not match goes back to the same agent with the exact problems, up to twice. You receive compact JSON and nothing else. If it still does not match you get an error with the problems and the last reply; the agent's work is not lost, so fix the schema or ask again. A schema checks the shape of an answer, not whether it is true: verify what matters.

Set isolation="worktree" (with agent_name, single mode) when several agents will write at the same time: each gets its own fresh Git worktree at HEAD, so they cannot overwrite one another. Creating it asks the user's permission, like the worktree tool. Uncommitted changes in your checkout are not copied, so commit or describe them first. The answer ends with the workspace id and the files changed; nothing reaches your checkout until you read the patch with worktree (action patch) and apply it. Apply isolated workspaces one at a time and verify after each. Cannot be combined with result_schema, session_key or quality_only.

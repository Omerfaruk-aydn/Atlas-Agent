Use this tool ONLY when you are in plan mode (a `<system_reminder>` will have told you so) and you have finished presenting your implementation plan as plain text in your response.

Call it once, after the plan is fully written out for the user to read — not before, and not as a substitute for describing the plan. Its `plan` parameter should be the same plan you just presented, in markdown.

This call itself asks the user for approval to leave plan mode. If they approve, plan mode turns off and you can proceed with the actual edits/commands on your next turn. If they deny it, stay in plan mode and keep discussing/refining the plan as text — do not retry file-writing or command tools.

If you are not currently in plan mode, do not call this tool.

Structured plans: when the plan changes files, list each one on its own line as `[NEW] path`, `[MODIFY] path` or `[DELETE] path` (relative paths, optionally followed by `- why`), and add a `Re-plan if:` list of conditions that should send the work back to planning. The paths are checked against the project before the user is asked: `[MODIFY]`/`[DELETE]` need an existing file, `[NEW]` must not exist yet, each file appears once. A plan with no tagged lines is still fine.

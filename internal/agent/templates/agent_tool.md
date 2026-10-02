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

For sizeable implementation tasks use workflow dispatch with stable task IDs,
owned paths and acceptance criteria. Specialists return a structured JSON handoff;
the coordinator applies isolated patches and calls workflow review for the task.
Independent test and review runs use quality_only restrictions, followed by actual
machine verification. Reported handoffs alone never pass the completion gate.

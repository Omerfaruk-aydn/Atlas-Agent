Manage the persisted task and acceptance ledger for multi-step work. Each task has
pending/in_progress/completed state. Keep one primary task in_progress; independent
delegated tasks may progress concurrently. Skip the ledger for simple one-step work.

For dependency-aware execution, add a stable id, depends_on task IDs, agent name
and owned_paths (literal relative files/directories, including bracketed UI routes).
Cycles, missing dependencies and starting/completing before dependencies are rejected.
workflow ready/dispatch uses these fields to select non-overlapping ready waves.
Changing task content, dependencies, ownership or criteria invalidates inherited
verification. Completion still requires actual acceptance evidence.

Use action=list with a zero-based offset to read one existing task and its evidence
without changing it. The result includes total and truncated; move offset forward
to read subsequent tasks. Large legacy records return a marked bounded preview.
Omitted fields are preserved on update. Reopening a completed task or changing its
criteria invalidates inherited verification and evidence; supply fresh reports after
performing the relevant checks.

Provide acceptance_criteria for substantive tasks. Record verification as pending,
passed, failed, user_confirmed or not_applicable and evidence entries with kind
command, inspection or user and a specific detail. Command evidence should identify
the actual command and observed result. User evidence must reflect an explicit user
statement; never invent it. not_applicable requires evidence explaining why execution
is unnecessary, for example a document-only task reviewed by inspection.

The tool validates reported state and persists it; it does not independently execute
or authenticate evidence. Completed tasks with criteria need evidence and an accepted
verification state. Failed verification cannot be marked completed. Preserve earlier
criteria and evidence when updating the list; reopen tasks when new requirements or
failures invalidate their completion. Report unresolved requirements honestly.

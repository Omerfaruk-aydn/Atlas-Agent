# Execution, verification and delivery

Atlas records tool execution separately from task completion. An accepted write
is applied; an adapter-owned readback or actual passing check is verified. A
saved final answer records delivery. Questions, permission requests, handoffs
and canceled turns never count as delivery.

## Unknown operations

An interrupted mutation may already have taken effect. The same call is blocked
until its exact operation is inspected and explicitly reconciled through workflow
recovery. Whitespace and JSON property order do not bypass this guard. A later
successful unrelated call does not resolve uncertainty. Reconciliation updates
both the execution journal and outcome ledger atomically, including old records
whose operation has aged out. Late backend responses cannot revive a resolved
unknown operation.

Browser and Computer adapters record whether input was actually dispatched.
Desktop validation or target rejection before input reports no dispatch. A
timeout after a native mutation remains unknown. Cancellation fences pending
validation so a late validation result cannot start input after a no-send timeout.
Native APIs already executing cannot be undone by that fence.

## Pipelines

Unknown outcomes stop downstream mutation. A recovery branch may use known
read-only tools to gather evidence, but its success does not clear the unknown
result. Opaque shell/MCP recovery is not assumed read-only. Resume information
preserves per-item progress and sent, partial or unknown dispatch. Non-JSON,
image-only, oversized or failed iterations discard old bindings rather than
reusing another item's output. Questions and cancellation also stop retries.

## Requirements and dependency checks

Todos persist user requirements and dependencies. Model-authored completed or
passed fields are reports, not independent proof. Actual task-scoped checks are
bound with `verify.task_id`; it cannot replace an already assigned specialist
task. Browser/Computer checks in verify accept only read-only `action: assert`.
Check the user's actual acceptance criteria, rather than a convenient unrelated
condition. Dependent mutations wait for verified upstream work.

The same dependency controls apply to nested pipeline, computer batch and
verification dispatch. Verification records carry the task definition's
fingerprint; changing its content or acceptance criteria invalidates old proof.
A durable mutation watermark preserves invalidation after rolling records age
out. Running background mutations invalidate older checks immediately and
cannot be duplicated before their job output is collected.

The next model request receives current outcome state, replacing its previous
runtime state instead of accumulating stale prompt blocks. General task
continuation is bounded to eight automatic follow-ups and two follow-ups without
machine-observed progress. Pauses, cancellation, queued user input, unknown
operations, dependency blockers and turn limits stop continuation. A new user
instruction resets the general continuation budget; generated continuations and
compaction resumes do not. Browser tasks retain their existing separate bounds.

## Research and final reports

Browser research requirements use `verify: observation`, an exact source URL,
subject and expected text. Source-bound observations are historical evidence and
survive navigation; live refs and structural authority still expire. Collection
rows identify their actual source, including pagination.

Report requirements use `verify: delivery` and expected report text. Only a saved
final assistant response can satisfy them. They do not authorize claims that
upstream work was verified, and pending delivery alone does not restart browser
actions. Persistence failures are surfaced rather than silently claiming delivery.

Automated adapter and provider-format tests validate these contracts. They do
not establish a particular live model's planning quality or universal site
compatibility.

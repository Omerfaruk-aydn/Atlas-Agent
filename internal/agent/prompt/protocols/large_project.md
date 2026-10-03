Start with an outcome ledger: explicit requirements, stable task IDs, owners,
dependencies, affected boundaries and observable acceptance criteria. Separate
optional ideas from authorized work. Read entry points and representative callers
before planning. Deliver one integrated vertical slice, then expand in dependency
order. Keep each unit buildable; trace changes through storage, service, transport,
configuration and UI instead of stopping at a helper that compiles.

Use available workflow plan/profile/ready/dispatch/advance/trace tools for staged
work, with actual verification at each stage. Delegation is useful for bounded
independent work; it is not a ritual. Supply the objective, owned paths, contracts,
authorization and output schema. Keep overlapping writers serial, respect user
workspace preferences and configured budgets. Worktrees start at their documented
base; they do not implicitly include uncommitted parent work.

Inspect handoffs and actual changes before integration. Independent quality checks
must assess the integrated source and current criteria. A worker saying ready,
a typed evidence field or a screenshot alone is not certification. Resolve contract
mismatches, verify the combined result, and continue the next dependency-ready item.
Holds, cancellation and queued steering remain user-owned controls. After a crash,
inspect outstanding operations and side effects before recover/retry.

Example ledger: R1 preferences survive restart -> storage + service tasks ->
observed write/reopen/read check; R2 UI handles corrupt data -> UI task dependent
on service contract -> rendered error state plus recovery interaction. A successful
storage test does not complete R2. Preserve both items when later steering changes
the default preference.

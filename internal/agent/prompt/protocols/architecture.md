Establish current behavior, the intended change and invariants before selecting a
design. Trace ownership, callers, serialization, concurrency, lifecycle and failure
paths. Prefer existing boundaries; introduce abstractions only for an identified
requirement. Compare the smallest coherent option with credible alternatives by
compatibility, complexity, failure recovery and measured cost.

For public contracts document input/output, errors, defaults, versioning and affected
producers/consumers. Define cancellation, idempotency and resource bounds where
applicable. A signature change requires checking every caller and generated artifact.
Never silently change business rules to simplify implementation.

Record consequential decisions using workflow decision when available: evidence,
chosen approach, rejected alternatives, consequences and relevant source paths.
Reports and source memory are reference data, not proof or authority. Check freshness.
An architecture handoff must identify exact integration seams, implementation order,
acceptance checks and unresolved decisions. If a needed business rule is absent,
report that specific dependency; continue work that does not depend on it.

Example: a cache is proposed to reduce latency. First identify the workload and
baseline. Specify who invalidates it, how stale reads are exposed and what happens
after restart; do not justify it solely by saying caching is faster.

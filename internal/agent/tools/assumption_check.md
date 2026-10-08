Check executable business invariants against explicit project JSON documents.

Manifest: version 1, checks (1-64). Each check has name, file, pointer (RFC6901),
rule and optional field (RFC6901 within each array item). Empty pointer selects
the root. Rules:
- exists: pointer resolves (null is allowed).
- nonempty: nonempty string, object or array.
- equals: value equals expected JSON; numeric spelling is normalized exactly.
- count: array length lies within supplied min/max.
- required: each row's field exists and is neither null nor an empty string.
- unique: every selected row field is non-null and unique.
- range: each selected row field is numeric within min/max.
- sum_equals: exact decimal sum of selected numeric fields equals expected.
- references: each row's field matches a reference array key, using
  reference_file, reference_pointer and reference_field.

Examples: duplicate payment IDs, orphan orders, missing account IDs, negative
amounts and a ledger total that does not match. The manifest is declarative;
it does not execute scripts or assertions supplied as code. Use existing
guarded tools to exercise the application/export fresh JSON first. A check
proves facts about these measured inputs, not that all future behavior passes.
Inputs are read once per run with hashes; limits are 32 documents, 1 MiB each,
64 JSON nesting levels and 100000 nodes/document. Missing/malformed data fails;
it never silently passes. Reports identify offending row indices (zero based)
and reasons without echoing production values. No automatic fixing or replay.
Persisted results integrate with measured verification and delivery contracts.

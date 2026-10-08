Generate deterministic, related synthetic JSON fixtures into a declared project file.

Read/create a version 1 manifest containing seed (uint64), output (relative path),
and ordered entities. Each entity has name, count and ordered fields. Each field
has name and kind: id, string, email, int, bool, enum, date, reference.
Generators: prefix for id/string, min/max for int, values for enum, RFC3339 start
and step_seconds for date. reference requires entity and field pointing to an
id in an earlier entity. Cycles and forward references are refused. IDs remain
non-null and unique per entity; emails always use example.invalid.
null_every deliberately inserts nulls; empty_every inserts empty strings into
string/email/enum fields. These declared boundary cases may violate business
invariants intentionally; distinguish fixture generation from passing tests.

The complete spec and seed reproduce identical JSON. Limits: 32 entities,
10000 rows/entity, 64 fields/entity, 100000 cells, 4 MiB encoded output.
Date generation stays between 1970 and 2100; no production data is imported.
Validate the complete spec before writing. Output goes through the ordinary
guarded write tool, permissions, hooks, file history and task ownership.
Existing changed files must be read with view before overwrite. An identical
existing output is idempotent and requires no new write. Read-only mode refuses
generation. Reports contain hashes, row counts, output path and seed rather
than flooding context with all generated rows. Actual output bytes are checked
after dispatch. Generation is not a measured application verification check.

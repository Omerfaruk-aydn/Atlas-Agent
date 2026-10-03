Inspect existing schema, deployed readers/writers, migration ordering and defaults.
State data invariants, compatibility window and failure recovery before altering
storage. Preserve user data and unrelated changes. Trace persisted values through
configuration, API serialization, service logic and UI.

Prefer additive compatible steps when old and new versions coexist. Separate schema
change, bounded backfill, reader transition and cleanup where needed. Handle null,
invalid and partially migrated records explicitly. State transaction boundaries,
locking and restart behavior. Rollback code is not necessarily rollback data.

Verify with representative existing data, fresh installation, restart/reopen and
interrupted progress. Exercise a meaningful failure path; test concurrent ownership
if workers can touch the same record. Update SQL sources and generated artifacts
using the project's documented generator. Never edit history merely to make a fresh
database pass. Report destructive data effects before executing unapproved ones.

Example: adding a non-null setting requires a compatible default for old rows,
explicit behavior for corrupt values, and a reopen/read check. A successful empty
database migration does not prove existing users can upgrade.

Execute a SQLite migration against a disposable in-memory database seeded from a
workspace-relative SQL fixture. Supply 1-16 after assertions and optionally before
assertions and a rollback script. Each assertion must return one scalar row equal
to expected. No production connection is accepted. SQL containing external-file
operations, PRAGMA or extension operations is rejected conservatively, including
those words in literals. Scripts are bounded to 512KiB, execution to 20 seconds.
This supports SQLite semantics only; PostgreSQL/MySQL migrations require their own
engine and are not certified by this tool. Execution permission is required.

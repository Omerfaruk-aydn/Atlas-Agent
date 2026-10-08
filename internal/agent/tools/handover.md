Rehearse first-use setup from literal README instructions in a disposable copy.

Create or inspect a version 1 JSON manifest with `readme` (project-relative)
and 1-24 `steps`. Each step has a unique `name`, literal `argv`, and a
`readme_quote` that must occur exactly in the README. Optional expectations are
`expected_exit` (default zero), `output_contains`, `file`, `file_contains` and
`timeout_seconds` (default 120, maximum 600). Declare actual finite smoke tests
instead of foreground servers that never exit. Missing runtimes and failed
installation are failures. Do not invent commands absent from documentation.

Git-visible regular project files are copied without Git metadata, dependency
directories, build outputs, .env files, keys or Atlas account configuration.
Ignored files are absent. Links, submodules and oversized copies are refused.
The original project is not edited by Atlas's copy operation. Commands run on
the current host through guarded bash and retain its hooks, permissions and
execution backend; a disposable copy is not an OS sandbox. They can still
access the network or external paths if authorized. Host runtimes, caches and
the runner environment remain available. Use the existing container-required
execution policy when a stronger boundary is needed.

Every step records actual exit, duration and explicit output/file checks.
After a failure, remaining steps are blocked rather than executed. The README
and copied source hashes bind persisted session evidence. Temporary files are
cleaned after the run; the report survives. Never describe a same-host run as
proof that every new machine can install the product.

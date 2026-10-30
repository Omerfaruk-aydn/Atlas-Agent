Query the living project model: one view of the project's modules, how they depend on each other, where the program starts, which tests cover which files, who maintains what, and the decisions recorded about it. It is kept up to date by file content hashes and the version-control history, so a call after an edit re-reads only what changed.

WHEN TO USE THIS TOOL:
- Before changing a file: impact lists the files and modules that depend on it, the tests to run (nearest first) with ready commands, the people who maintain the module and the recorded decisions that apply. Call it with no files to assess everything with uncommitted changes.
- To find the tests for a file without guessing: tests.
- To orient in an unfamiliar project: summary, then query on one module.
- To learn who to ask or what was decided and why: owners, decisions.
- After making a lasting design choice the next person must not undo by accident: decide.

ACTIONS:
- summary: areas with their size and owners, entry points, the most depended-on modules, test commands, decisions and documents. budget sets its size.
- query: one module (a directory) or file: what it defines, what it depends on, what uses it, its tests, its owners and the decisions that apply. A partial name lists what it could mean.
- impact: files (or directories) to assess; depth sets how many levels of dependents to follow. Reports direct and indirect dependents, modules, tests, commands, owners and decisions. Files the model has never seen are listed separately: a new file has no dependents yet.
- tests: the tests covering each file, with why: direct (named after the file), package (same package) or imports (the test uses it).
- owners: the main committers of a module or the module of a file; empty lists the largest modules.
- decisions: recorded decisions, optionally filtered by words. Architecture decision records found under adr or decisions folders are included.
- decide: record a decision (text), why (rationale) and the paths it applies to (files). Asks permission, because it persists. Use it for choices a future change could undo by accident, not for task status.
- refresh: update the model now; force rebuilds all of it. Other actions already refresh, so this is rarely needed.

HOW SURE IT IS:
The dependency graph is built from imports and name resolution, not from running the code. Dynamic dispatch, reflection and generated code are invisible to it, so an empty list of dependents means no static use was found, not that none exists. Test coverage is by naming, package and imports, not measured coverage. Ownership counts commits, not responsibility.

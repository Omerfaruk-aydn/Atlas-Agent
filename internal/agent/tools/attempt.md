Run several independent attempts at one task in isolated git worktrees, score them, and apply only the best one after the user agrees.

Use it when a task has real risk of a wrong approach (a hard bug, a design choice, a refactor with several plausible shapes) and there are checks that can tell a good result from a bad one. Do not use it for small edits; one careful attempt is cheaper.

Actions:
- run (default): starts `n` attempts (2-5) from the current state of the working tree, each in its own disposable worktree, optionally with its own model. Each attempt is scored by the verify commands (from `verify`, else `.atlas/work.json`, else the detected test command), a lint command and diff size, then an independent judge model compares the anonymized diffs. The result is a ranked report with evidence and a confidence level. The working tree is not changed. Runs take minutes and cost one full agent run per attempt plus the judge.
- list / show: read stored runs.
- apply: put one attempt's patch into the working tree. Defaults to the recommended winner; pass `attempt` (A, B, ...) to choose another. Ask the user before calling it, and report the confidence and its reasons when you do.

Rules:
- A failing verify command is never outranked by the judge. If confidence is low, say why before recommending anything.
- Prefer cheap models for attempts and a stronger, different model as judge (model role names work). A judge that is the same model as the attempts tends to favor its own style.
- Set `autonomous` only when the attempts must edit files without prompting; the user sees it in the approval.
- Dependencies git does not track (node_modules, virtualenvs) are missing from the worktrees; pass `setup`, for example `npm ci`.
- Attempts cannot start attempts of their own.

# Parallel attempts with a judge

Run one task N times, each attempt in its own disposable git worktree, rank the
results with deterministic checks plus an independent judge model, and apply
only the winner when you approve.

## Use

```
atlas-agent attempt --n 3 "make the parser reject empty input"
atlas-agent attempt --n 3 --models fast,fast,large --judge large "fix the flaky retry test"
atlas-agent attempt --verify "go test ./..." --lint "go vet ./..." --apply "add a --dry-run flag"

atlas-agent attempts list | show <id> | apply <id> [--attempt B] | clean [--older-than 72h]
```

Inside a session the model can call the `attempt` tool (actions `run`, `list`,
`show`, `apply`; at most 5 attempts per call) and `/attempt <task>` asks it to.
Every `run` and every `apply` goes through the normal permission prompt.

Flags: `--n` (1-8), `--models` and `--judge` (model IDs or model role names,
resolved through `options.model_roles`; the default judge is the `judge` role,
else `evaluator`, else the default model), `--no-judge`, `--verify`, `--lint`,
`--setup` (run once per fresh worktree, e.g. `npm ci`, because untracked
dependencies are not in a worktree), `--parallel`, `--timeout`, `--yolo`
(auto-approve tool permissions inside the attempts; they run in worktrees but a
shell can still reach outside), `--keep`, `--apply`, `--json`.

## How it works

1. A snapshot of your tree is taken: HEAD, uncommitted tracked changes and
   untracked, non-ignored files. Each worktree is seeded from it, so attempts
   see your work in progress and each patch contains only the attempt's own
   change. Your working tree is never touched while attempts run.
2. Each attempt runs `atlas-agent run --json` as a subprocess with its own
   model, a private data directory, and a strategy hint (smallest change,
   test-first, root cause). Attempts cannot start attempts (`ATLAS_ATTEMPT_CHILD`).
3. The attempt's change is captured as a patch and the verify and lint commands
   run in its worktree. Verify commands come from `--verify`, else
   `.atlas/work.json`, else the detected test command (go, cargo, npm, pytest,
   make). Lint is `go vet` or `npm run lint` when detectable.
4. Distinct patches go to the judge as anonymous diffs plus real check results;
   the diffs are fenced as data. The judge must score every candidate or its
   verdict is discarded and the ranking falls back to checks alone.
5. Score: checks 60% and judge 40% (checks only without a judge). Within the
   checks: verify 75, lint 15, smaller diff 10. A failing verify command is
   never outranked by a judge preference.
6. Worktrees are removed (also on cancel or failure). Reports and patches are
   kept in `<data dir>/attempts/<run id>/` so `attempts apply` is a checked
   `git apply` of the stored patch; it refuses if the tree changed in a way
   that no longer applies, and refuses a second apply.

## Reading the report

Each attempt shows status, score, per-command PASS/FAIL, judge rationale and
concerns. The winner carries a confidence (`high`, `medium`, `low`, `none`) with
reasons: low when no verify command existed, the winner fails a check, or the
judge disagrees; medium when only one candidate existed, no judge ran, or the
top two are within 5 points. Using the same model as judge and attempts
produces a warning because judges favor their own style.

## Cost and limits

A run costs one full agent run per attempt plus one judge call; the total is in
the report. Use cheap roles for attempts and a stronger, different judge.
Failed attempts keep their partial patch for inspection; `attempts apply
--attempt X` can apply one explicitly.

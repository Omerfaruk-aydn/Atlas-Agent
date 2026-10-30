# Code review (`/review`)

`atlas-agent review [target]` and the TUI command `/review [target] [effort]`
review code changes with several independent reviewer passes and a
verification pass. A finding that verification refutes is dropped.

## Targets

| Target | Meaning |
| --- | --- |
| (none) | Uncommitted changes: staged, unstaged and untracked text files. |
| `123`, `#123`, `owner/repo#123`, PR URL | A GitHub pull request, read through `gh` (read-only). |
| `a..b`, `a...b` | A git range. |
| `ref` | If `ref` is an ancestor of HEAD: the changes since it. Otherwise a branch: what it adds to HEAD. |
| `--commit REF`, `--base REF` | One commit; changes since the merge base (kept from the earlier command). |

## Effort

| Effort | Reviewer passes | Verifiers per finding |
| --- | --- | --- |
| `low` | 1 | none (findings are marked unverified) |
| `medium` (default) | 2 | 1 |
| `high` | 3 | 1 |
| `max` | 4 | 2 (any rejection drops the finding) |

Passes have different focus (correctness, regressions, security and
concurrency, maintainability). Findings reported by several passes are merged
and gain confidence. Findings that name a file that does not exist, or a line
past the end of the file, are dropped before verification.

## Output

Findings are ranked by severity (critical, high, medium, low, nit), then
confidence. Each has `file:line`, a title, a concrete failure scenario, a
suggested fix, a confidence from 0 to 1, and whether it was verified. A summary,
the dropped findings with reasons, and warnings (for example a verifier that did
not answer) accompany them. `--json` prints the same as JSON.

## Flags

- `--effort`, `--model`, `--verify-model`: depth and models. Reviewer requests
  run as child `atlas-agent run --role review` processes, so they are read-only.
- `--comment`: post the findings to a pull request as one `COMMENT` review via
  `gh api`. Findings on changed lines become inline comments; the rest go in the
  review body. Only valid for a PR target; nothing is posted without the flag.
- `--fix`: after the report, run the coding agent on the findings. Local
  targets only. Add `--yolo` to let it accept permissions unattended.

## In the TUI

`/review` sends a prompt to the main agent describing the same plan (passes,
verification, ranked report), so the review stays in the session and can be
followed up. With `--comment` the agent is told to show the exact comments and
wait for a yes before posting; the `gh` call also goes through the permission
prompt.

Code: `internal/codereview` (pipeline, targets, comments), `internal/cmd/review.go`,
`internal/ui/model/review.go`.

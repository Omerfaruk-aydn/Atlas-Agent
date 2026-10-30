# Rehearsal (dry-run world)

Risky changes (migrations, mass refactors, dependency upgrades, formatters or
codemods over many files, destructive shell commands) can be rehearsed first.
Atlas copies the project into a temporary directory, makes the change there,
runs your checks before and after, and shows a preview. The real tree is only
written when you approve.

## Use

```
atlas-agent rehearse "upgrade lodash" --run "npm update lodash" --check "npm test"
atlas-agent rehearse list
atlas-agent rehearse show <id> [--patch] [--json]
atlas-agent rehearse apply <id> [--force]
atlas-agent rehearse discard <id>
```

- `--run` (repeatable) are the shell commands that make the change; the first
  failing one stops the rehearsal.
- `--check` (repeatable) are commands such as tests or builds. They run on the
  untouched copy first, so failures that already existed are not blamed on the
  change (`--skip-baseline` disables this).
- `--agent` lets a non-interactive agent make the change inside the copy
  instead (it auto-approves its own tool calls there).
- `--apply` applies at once when the verdict is clean.

The agent has the same capability as the `rehearse` tool (`run`, `show`,
`list`, `apply`, `discard`; `run` accepts `files` and `steps`). Both `run` and
`apply` go through the normal permission flow. When the bash tool is asked to
run a destructive command (recursive delete, `git reset --hard`, destructive
SQL or migrations, dependency upgrades, in-place mass edits), the permission
prompt carries a hint to rehearse it first.

## The preview

Verdict (`ok`, `unchecked`, `api_changes`, `breaks_checks`, `failed`,
`no_changes`), files added/modified/deleted with line counts, every command
with its exit code, checks before and after with newly failing tests (Go,
pytest, cargo and jest output are recognised), and added, removed or changed
exported Go declarations in the touched packages. A unified patch is stored
next to the report under `<data dir>/rehearsals/<id>/`.

## Guarantees and limits

- The real tree is never written during a rehearsal. Commands naming the real
  project path are rejected; use relative paths.
- Apply writes the rehearsed file contents, not a replay. It refuses when any
  touched file changed since the rehearsal, checks everything before writing,
  and rolls back if a write fails. `breaks_checks` and `failed` verdicts need
  `--force`.
- Paths ignored by git in the real project (build output) are not applied.
- `.git`, `.atlas` and `.claude/worktrees` are not copied; symbolic links are
  skipped; the copy is capped at 1 GiB.
- The copy isolates the project directory, not the machine. Commands keep the
  user's privileges and network access. Side effects outside files (databases,
  installed packages, remote services) are not applied; repeat them for real
  after applying.

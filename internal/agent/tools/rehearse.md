Rehearse a risky change in an isolated copy of the project and review a preview before anything touches the real tree. Use it for database migrations, mass refactors or renames, dependency upgrades, formatters or codemods over many files, and destructive shell commands (recursive deletes, git reset --hard, git clean).

Actions:
- run: copies the project (git-ignored build output included, so tests can run), runs `checks` on the untouched copy as a baseline, makes the change with `files` (full contents written first) and `steps` (shell commands run in order), then runs `checks` again. The first failing step stops the run. Returns the verdict, files changed with line counts, every command with its exit code, checks before and after with newly failing tests, and exported Go API additions, removals and changes.
- show / list: inspect stored rehearsals.
- apply: writes the rehearsed file changes into the real project after the user approves. It refuses when a touched file changed since the rehearsal. A verdict of failed or breaks_checks needs force=true, only after the user accepted the failure.
- discard: drop a rehearsal.

Rules:
- Commands must use relative paths. A command naming the real project path is rejected.
- The copy isolates the project tree only, not the machine: commands still run with the user's privileges and network, and the user approves each run. Do not rehearse anything whose side effects live outside the tree (a real database server, installed global packages, remote services); only file changes are applied, so re-run such commands for real after applying.
- Always give `checks` (for example the project's test command) so the preview can prove the change is safe; verdict "unchecked" means nothing verified it.
- Show the user the report before calling apply.

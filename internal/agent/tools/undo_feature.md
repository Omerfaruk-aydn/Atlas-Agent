Prepare and apply a reviewable inverse patch for explicitly selected feature commits.

Use `action: prepare` with 1-32 reachable, non-merge `commits`. Identify ownership
from real Git history; do not guess commits from an ambiguous feature name.
Preparation changes no project files or Git history. It reverses the selected
commits in a temporary index and returns the complete inverse patch, affected
paths, later commits touching those paths, file identities and plan fingerprint.
Overlapping changes that cannot apply cleanly are refused; no forced reset,
three-way conflict overwrite, checkout restoration or automatic commit is used.

Review the patch and possible dependents before `action: apply` with the exact
`plan_id`. `acknowledge_dependents` is required if later commits touch selected
paths. This list is a conservative signal, not a complete semantic dependency
graph. Application checks HEAD and every affected preimage again, requests
write permission and applies Git's complete patch to the working tree. Staged
index contents and commit history remain as they were. Unrelated current edits
survive only when the inverse patch applies cleanly; stale plans are refused.

After applying, inspect the diff and run relevant tests. A successful patch
does not prove the project still works. Uncommitted ownership, root/merge
commits, external API writes, database migrations, packages and remote changes
require separate review and are never silently reversed.

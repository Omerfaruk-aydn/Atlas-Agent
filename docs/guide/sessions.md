# Sessions, worktrees and teams

Working on several things at once without them trampling each other.

[Back to the README](../../README.md) · [All guides](README.md)

## Sessions, worktrees, and teams

**Sessions** are persistent and searchable. Every session is stored with its messages, tool calls,
token usage, and cost; `session_search` (tool) and `atlas-agent session search` (CLI) search across
all of them in a workspace. Subagent runs are child sessions, so `atlas-agent session tree` shows
what a delegated task actually did.

**Rewind** (F2) steps a session back to a chosen message of yours: a new session holds the
conversation up to and including that message, and the files the agent changed from there on are
restored. Nothing runs until you send the next message. Useful when a turn went somewhere you did not want and
you would rather not carry it in the context. Changes made through shell commands are not tracked
and are not undone.

**Worktrees** run a session in its own git worktree, so a long autonomous run does not collide with
the branch you are editing by hand.

**Teams** let several agents working on one task exchange short messages (`team_send`, `team_read`)
without going through the main session — a lightweight coordination channel for parallel work.

**Parallel attempts** (`atlas-agent attempt --n 3 "<task>"`, the `attempt` tool, `/attempt`) run the
same task several times in isolated git worktrees, score each attempt with verify commands, lint and
an independent judge model, and apply only the winner when you approve. See
[docs/parallel-attempts.md](../parallel-attempts.md).

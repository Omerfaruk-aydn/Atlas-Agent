# Recipes and prompting

Ready-made workflows for common jobs, and how to ask for work.

[Back to the README](../../README.md) · [All guides](README.md)

## Recipes

Worked examples, in the shape you would actually type them.

### Review a pull request before merging it

```text
> github_pr_view 412, then review it with the review and security subagents
  in parallel. I want concrete defects with file:line, not a summary.
```

`github_pr_view` pulls the metadata and full diff through the `gh` CLI, and `orchestrate` puts the
same diff in front of two specialists that bring different objections. Because they run
independently, agreement between them means something.

For a change you are unsure about rather than one you are checking:

```text
> debate: is the locking in this PR correct under concurrent writers?
  use review, security and backend, 3 rounds, planner as judge
```

### Hunt a flaky test

```text
> internal/server has a test that fails maybe one run in ten. find it and
  tell me why. run the suite with -count=20 if you need to.
```

The agent can run the suite repeatedly in the background (`bash` with `run_in_background`, then
`job_output`), read failures with `test_run`, and drop into `debugger` for a Go program whose
behavior depends on runtime state a log line will not show.

### Do a dependency bump safely

```text
> dep_audit, then bump anything with a known vulnerability, run the tests,
  and show me the diff before we commit
```

`dep_audit` reports both known vulnerabilities and how far behind each module is; `test_run` and
`git_diff` close the loop.

### Pre-commit sanity pass

```text
> pre_commit_guard on what I have staged, then suggest a conventional
  commit message for it
```

`pre_commit_guard` looks at `git diff --cached` for the class of mistake that is obvious in
hindsight — a stray debug print, a committed secret, a file that should not be in this change —
and `git_conventional_commit` proposes the type and scope from what actually changed.

If the pile is too big for one commit:

```text
> git_commit_split: how should I break this up?
```

It proposes an ordering where a package another changed package depends on is committed first, so
each commit builds.

### Understand an unfamiliar subsystem without wrecking your context

```text
> agent, using the research subagent: how does session compaction decide
  what to keep? name the files and the decision points.
```

The research subagent reads however many files it needs in a session of its own; you get the
answer, not the reading. See [Token economics](cost.md#token-economics-why-delegation-is-cheaper).

### Refactor with the LSP instead of grep

```text
> rename the Coordinator.RunAccepted method to RunWithAcceptance everywhere,
  then show me the call sites that changed
```

`lsp_rename` performs a true semantic rename that respects scope, shadowing and imports — the kind
of change a find-and-replace gets subtly wrong. `lsp_call_hierarchy` first, if you want the blast
radius before committing to it.

### Audit before shipping

```text
> scan_secrets across the repo, security_scan on internal/, and
  ci_cd_pipeline_debugger on .github/workflows — report anything real,
  skip the theoretical
```

Three different scanners with three different failure modes: committed credentials, source-level
security smells, and workflow mistakes that run green today and become supply-chain risk later.

### Bring a new machine up to speed

```text
> /init
```

Writes an `AGENTS.md` for the project by actually reading it — conventions, layout, how to build and
test — which every later session (and every subagent) then starts from.

### Let it grind on something bounded while you do something else

```text
> /goal every package under ./internal passes `go vet` and `go test`,
  without changing any public API
```

Then walk away. The run takes its own turns, a judge model checks the claim that it is finished
before it stops, and the goal stays in the sidebar so you can see what it is chasing when you come
back. `/goal` reports progress; `/goal clear` ends it.

### Set the whole thing up for cost

```text
> put the small model and the compact role on minimax's cheap model,
  keep opus for review, summarize at 70%, and cap this session at $2
```

One sentence, four settings: `set_model` (small), `set_role` (compact, review), and `set_field` on
`options.auto_summarize_at` and `options.max_session_cost`. Each change is shown to you for approval
in plain words before it is written.

### Drive a real browser that is already signed in

```text
> open the staging dashboard in my actual Chrome profile and tell me
  what the error banner on the billing page says
```

The `browser` tool can work from a snapshot of your real Chrome profile — cookies, local storage,
sessions — instead of a blank automation window with no logins, which is the difference between
"I cannot sign in" and an answer.

## Prompting tips

Atlas Agent is built to be autonomous, so the prompts that work best are the ones that state a
finish line rather than a first step.

**Give it a definition of done.** "Fix the failing test in internal/shell" beats "look at
internal/shell". The agent decides how many steps that takes; it cannot decide when to stop if you
have not said what done looks like.

**Say what not to touch.** Constraints prune whole branches of work: "without changing the public
API", "don't touch the generated files", "keep the existing error strings".

**Point at evidence, not vibes.** "The retry loop double-counts attempts — see
`internal/agent/coordinator.go:812`" starts from a fact. "Something is wrong with retries" starts
from a search.

**Delegate the reading, keep the deciding.** Ask a subagent to *find out*; make the call yourself in
the main session. That is the split the context economics reward.

**Escalate to `debate` only for judgment calls.** Questions the codebase settles should be answered
by reading the codebase. Questions about which design to commit to are what a debate is for.

**Use `/goal` for grinds, not for exploration.** A goal loop is at its best when success is checkable
— a suite that passes, a lint that comes back clean, every call site migrated. It is at its worst
when "done" is a matter of taste.

**Let it configure itself.** If a model is wrong for the job, say so in the session instead of
editing JSON: "put research on something with a bigger context window."

**Correct early.** A wrong assumption caught in turn two costs one turn; caught in turn ten it costs
ten, plus the context they filled.

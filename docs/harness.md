# Harness behavior

General mechanisms that help the model do its best work on any kind of
project. None of them adds or removes tools; they change how existing tools
are found, how output reaches the model, and how work is checked. Long work
is covered separately in [work-plan.md](work-plan.md).

## Finding the right tool

Tools outside the core set load on demand through `tool_search`. Three things
make sure the model knows a fitting tool exists:

- **Hints in the index.** The deferred tool index shows each name with a few
  words on when it helps, for example `local_app (build and run a local web app
  the user keeps using)`. Tools that only work on Go code say so. MCP tools use
  the first clause of their own description.
- **Search that understands the request.** Besides names and categories,
  `tool_search` matches per-tool aliases in English and Turkish, including the
  libraries a model would otherwise reach for through the shell ("playwright
  screenshot" and "sayfanın görüntüsüne bak" both find `browser`). A search that
  matches nothing suggests the closest tools by spelling, so a typo or an
  unusual inflection still finds the tool. Suggestions are never loaded on
  their own.
- **Shell nudges.** When a bash command does a job a tool exists for (a
  Playwright or Selenium script, `curl` of a public page, `xdotool`, `adb`,
  `dlv`), its result gets one line naming the tool. The command is never
  blocked, changed or delayed; each tool is named at most once per session, and
  only when the session is allowed to use it. `curl` against localhost, with a
  body or saving to a file is left alone.

## Long command output

Output above 12 KiB is saved in full to `.atlas/logs/cmd-*.log`. The model gets
the first lines, every error-like line from the middle with its line number, the
last lines and the log's path, so the failure is visible without filling the
context. The 50 newest logs are kept. If the log cannot be written, the output
is returned as before.

## Context compaction

Sessions on large context windows (over 200k tokens) are summarized at three
quarters of the window rather than at its very end, since the model's hold on
early details weakens long before the window fills. `options.auto_summarize_at`
still overrides this.

## Model routing by role

A model role named after a subagent that sets no model of its own routes that
subagent to the role's model, and a role named `task` does the same for the
generic sub-agent. For example, with

```json
{"options": {"model_roles": {"task": {"provider": "anthropic", "model": "claude-haiku-5-5"}}}}
```

exploration sub-agents run on the faster model while the main session keeps its
own. A role that fails to build is ignored with a warning.

## Visual review

When the evaluator is enabled and its model accepts images, the latest
screenshots the agent took in the turn (up to three, scaled to provider limits)
are attached to the review, with a rubric that weighs design quality and
originality above craft and function. A visible defect fails the work; a
generic look fails it only when the request asked for a polished design.

## Checks around the end of a turn

When a turn that changed files ends, Atlas looks at what the request changed
before the work counts as finished. It compares each edited file with its
content before the request's first edit (captured by the file tools, so files
changed only through the shell are not seen). Each finding goes back to the
agent once; it undoes the change or justifies it in its report, and the
evaluator sees the list.

- **Weakened checks.** Fewer test cases or assertions in a test file, a
  deleted test file, new skip, focus or disabled-test markers, new lint or
  type-check suppressions, lowered coverage thresholds, and verify commands
  removed or changed in `.atlas/work.json`.
- **Leftovers.** Merge conflict markers, likely credentials (never repeated in
  the finding), debug statements such as `debugger`, `dbg!` or `pdb.set_trace`
  (console output only in a file that did not print before), new TODO and
  FIXME markers, blocks of commented-out code and new files over 1 MiB.

## Requests with several parts

A request written as a list (bullets, numbers or `1. … 2. …` on one line)
becomes a checklist. It is shown in the system prompt for the whole request,
so compaction cannot drop an item, and after a compaction the request's
original wording is shown again. The evaluator checks the request item by
item and fails the work for a missing part; without an evaluator, the agent is
asked once to account for every item before it finishes.

## When a check keeps failing

The verification contract escalates (see [work-plan.md](work-plan.md)). At the
third failure in a row, in a git repository, Atlas snapshots the project into
two worktrees under `.atlas/alternatives/` and asks for one genuinely
different approach in each, in parallel sub-agents when the agent tool is
available. The check's runs in each worktree decide: the passing one is
brought into the project with `git diff`/`git apply`, the other removed. Two
extra contract rounds are granted for this.

A check that passes and fails with no code change in between is recorded as
flaky in `.atlas/work-state.json`. Its next failure neither escalates nor
counts as a regression, and the agent is told to re-run it first; failing
again makes it real.

## Error memory

A command that failed, followed by edits and a passing run of the same
command, is a fix. The failure's first error line, normalized (paths, numbers,
addresses and times removed), is saved with a one-line summary of each edit to
`.atlas/errors.json`. When the same error appears again, in any later session,
the fix that worked is appended to the failing output once per request.

## Context for the model

- **Installed toolchains.** Once per process, in the background, Atlas runs
  `--version` for common toolchains (Go, Node and its package managers,
  Python, Rust, Java, .NET, C compilers, Make, CMake, Ruby, PHP, Zig, Docker,
  Git) and lists what is installed and what is not.
- **Budget line.** Once the context is a third full or the turn has taken
  eight steps, a one-line note at the end of each request gives the context
  use, when compaction will happen and the step count. It sits after the cache
  breakpoints, so it never invalidates the cached conversation.
- **Known checks.** While a project has no verify commands, the commands that
  verified projects of the same kind (detected from marker files such as
  `go.mod` or `package.json`) before are suggested.

## Failure corpus

Requests that end with the contract unmet after its round limit, or evaluator
findings unresolved, are saved under `<data dir>/harness/corpus/`, with the
project's end state kept as a commit under `refs/atlas/corpus/` and the edited
files' earlier contents. Turn one into a bench task to measure a harness change
against real failures:

```
atlas eval corpus list
atlas eval corpus export <case-id> --suite bench/corpus
atlas eval corpus export <case-id> --suite bench/corpus --check go,test,./...
```

## Options

Everything on this page is on by default and can be turned off:

```json
{
  "options": {
    "harness": {
      "check_guard": false,
      "request_checklist": false,
      "parallel_alternatives": false,
      "flaky_checks": false,
      "error_memory": false,
      "environment": false,
      "diff_hygiene": false,
      "budget_line": false,
      "failure_corpus": false,
      "strategy_hints": false
    }
  }
}
```

## Measuring it: `atlas stats harness`

Atlas records harness events under `<data dir>/harness/events.jsonl`: contract
passes and send-backs, step-backs and regressions, checks that could not fail,
plan reviews, evaluator verdicts, tool searches that found nothing or were
rescued by a suggestion, shell nudges given and followed, turns stopped for
looping, compactions, saved outputs, weakened checks and leftovers, flaky
checks, alternatives tried, error fixes learned and recalled, and failure
cases saved. Nothing leaves the machine.

```
atlas stats harness
atlas stats harness --days 30
```

The report shows the rates that point at the next improvement, the commands
that verified work per kind of project, and the searches that found nothing:
each one is a missing alias or hint.

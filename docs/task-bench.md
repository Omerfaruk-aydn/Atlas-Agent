# Task bench

`atlas eval bench` measures whether a harness change helps on real work. It
answers "did the agent produce working code?" with hidden tests instead of
with another model's opinion.

## How a run works

1. Each task's `repo/` is copied into a fresh Git repository, one workspace
   per task, variant and attempt.
2. For a variant with a `config`, the config is written to `.atlas.json` in
   the workspace. It merges over your global configuration, so providers and
   keys keep working.
3. The agent runs non-interactively on the task prompt.
4. Afterwards the task's `hidden/` files are copied over the workspace and the
   `check` command runs. Files at the same paths are overwritten, so an agent
   cannot pass by editing or deleting the tests. A pass is the check exiting 0.

Variants are interleaved within each attempt (baseline, variant, baseline,
variant, ...), so a slow hour at the provider lands on every variant alike.
Attempts that fail because of the provider (rate limits, outages) are left out
of the pass rate and reported separately.

## Commands

```bash
# Prove every task fails at the start and passes with its reference solution.
atlas eval bench validate bench/tasks

# Run a comparison. This spends real model tokens.
atlas eval bench run bench/manifest.example.json results.json --allow-tools

# Print the comparison again from recorded results.
atlas eval bench report results.json

# After fixing a task's hidden tests, judge the preserved workspaces again.
# This runs no agent and spends no tokens.
atlas eval bench rescore bench/tasks results.json rescored.json
```

Run the bench from a shell where the check program (for example `go`) is on
`PATH`. The run stops before any model call when it is not, because otherwise
every attempt would fail for a reason that says nothing about the agent.

`--allow-tools` lets the model run tools without prompts. Use it only because
the runs happen in disposable workspaces under `.atlas-bench/` next to the
results file, which are kept so you can inspect what the agent did.

## Manifest

```json
{
  "suite": "tasks",
  "model": "provider/model-id",
  "repeats": 3,
  "variants": [
    { "name": "baseline" },
    { "name": "evaluator-gate", "config": { "options": { "evaluator": { "enabled": true } } } }
  ]
}
```

`suite` is relative to the manifest. `tasks` optionally limits the run to
named task IDs. The first variant is the baseline the report compares with.

## Writing a task

```text
bench/tasks/<id>/task.json   {"id", "prompt", "check": ["go", "test", "-count=1", "./..."]}
bench/tasks/<id>/repo/       starting files, including a go.mod or equivalent
bench/tasks/<id>/hidden/     the checking tests, never visible to the agent
bench/tasks/<id>/solution/   a reference fix, used only by `validate`
```

A fair task fails on its starting files and passes with the reference
solution; `validate` enforces both, and a unit test runs it over the shipped
tasks. State any signature the hidden tests rely on in the prompt, since the
agent cannot see them.

Agents often add tests of their own. Give the hidden tests a file name that
is not the visible test's name, and prefix their test functions and helpers
(`TestHidden...`, `hiddenClock`), so an agent's `TestFractionalRefill` cannot
collide with yours and fail the build. The first real run lost three correct
solutions to exactly that.

Make tasks hard enough to tell variants apart. The starter tasks are solved
by a current model in about six tool calls, so a variant that improves quality
cannot show it on them; they mainly catch regressions and broken setups.

## Scores and site tasks

A check may print a line `BENCH_SCORE: N` (0 to 100) for partial credit; the
report shows the mean score next to the pass rate, and compares variants by
score when their pass rates tie. Without that line a pass scores 100 and a
fail 0. In check arguments `{task}` becomes the task directory and `{suite}`
the suite directory, so a judge can live outside the workspace.

`bench/sites/` holds web site tasks judged by `bench/judges/site/judge.mjs`:

```bash
# Once: install the judge's Playwright and axe packages.
cd bench/judges/site && npm install

atlas eval bench validate bench/sites
```

The judge serves the workspace on a local port, opens it in an installed
Chrome or Edge (no browser download; set `BENCH_BROWSER_CHANNEL` to choose),
and runs the task's `hidden/.bench/checks.mjs` with Playwright. It also scores
generic quality: no console errors, no requests to other sites, no sideways
scrolling at 375px, tap targets of at least 24px, no axe WCAG 2 A/AA
violations in light and dark mode, and a dark-mode background. Generic points
are weighted by the share of task points earned, so an empty page that
happens to have no accessibility errors still scores 0. The task's
`threshold` (85 for `event-landing`) decides pass or fail.

Hidden checks find elements by role and accessible name (`getByRole`,
`getByLabel`), so the prompt must state those names. That also makes the
checks reward accessible markup instead of particular class names.

Two tasks ship: `event-landing` spells out every label and ARIA name, so it
mainly catches regressions; `event-landing-open` states goals only and its
checks find controls by role and a forgiving name pattern, so the builder has
to make the decisions.

Visual design is rated by a vision model, but only with `BENCH_DESIGN=1`,
because it costs tokens and is not exactly repeatable. The judge screenshots
the page (desktop light and dark, phone) into a temporary directory and runs
`atlas run` on them with a calibrated rubric (hierarchy, spacing, typography,
colour, polish; 3 means a generic template, 5 is almost never given). Set
`BENCH_ATLAS` to the Atlas binary and `BENCH_DESIGN_MODEL` to change the rater
(default `claude/claude-sonnet-5-5`). It adds 25 points, weighted by task
share like the other generic points.

```bash
BENCH_DESIGN=1 BENCH_ATLAS=./atlas atlas eval bench run manifest.json results.json --allow-tools
```

Rescoring with `BENCH_DESIGN=1` rates the saved workspaces again.

## Reading the result

A few attempts per task are noisy. Treat a difference of a few points as
unproven, and repeat each task several times before changing a default. The
report lists which tasks got better or worse so a regression on one task is
not hidden by gains on another.

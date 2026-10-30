# Self-improving quality loop

`atlas eval improve` closes the loop between real failures and harness
changes. It never applies a change: it produces evidence and a proposal file,
and you decide.

1. **Mine.** With `--mine`, the failure corpus (see `atlas eval corpus`) is
   scored for hardness and the hardest cases become bench tasks in `--suite`.
   A case scores higher when the request changed several files or
   directories, is loosely worded ("make it better somehow"), names no file
   or identifier, ended with findings or unmet checks, or ends on an error
   the error memory (`.atlas/errors.json`) has fixed more than once. Cases
   below `--min-hardness` (default 3) are skipped.
   Test files the failed run changed are placed in the task's `hidden/`
   directory as they were in the end state, and are copied over the workspace
   before the check, as for any bench task.
2. **Validate red-at-start.** A mined task is kept only if its check fails on
   the starting files on two consecutive runs, and fails for the right reason:
   a missing program, a timeout or a check that passes already reject the
   task and remove it from the suite. Mined tasks have no reference solution,
   so this does not prove the check can pass.
3. **A/B.** The `baseline` and the `candidate` variant run on every task,
   `--repeats` times (default 3), interleaved, through the normal bench. A
   variant is an Atlas config fragment (prompt files, gate options, any
   option set) written to `.atlas.json` in each workspace.
4. **Report and propose.** Results go to the terminal and to a proposal file
   (`improve-proposal.json`, `"applied": false` always).

```bash
# Mine and validate only: spends no tokens, runs no agent.
atlas eval improve --suite bench/mined --mine --plan

# Run the A/B. This spends real model tokens.
atlas eval improve --suite bench/mined --mine --model <provider/model> \
    --candidate-config variants/strict-gate.json --repeats 3 --allow-tools
```

Flags: `--baseline-config` (default: no overrides), `--task` (restrict tasks),
`--check` (replace the judge of mined cases), `--max-tasks`, `--timeout`,
`--proposal`, `--results` (raw bench records, readable with
`atlas eval bench report`).

## How honest is the result

The task, not the run, is the unit of comparison, because repeats of one task
are not independent. Each task gives one paired difference; the verdict is
`improved` or `regressed` only when an exact two-sided sign test over tasks
(p < 0.05) and a bootstrap 95% interval of the mean score difference agree.
Otherwise it is `inconclusive`. The report shows pass rates with Wilson 95%
intervals, the run-to-run standard deviation across repeats, flaky tasks,
cost and token deltas, and warns about:

- too few tasks (a sign test needs at least 6 untied tasks all one way),
- fewer than 3 repeats,
- a ceiling (baseline already passes 95% or more) or a floor (nothing passes),
- flaky tasks, lopsided provider failures, and a candidate that costs much
  more.

Runs that failed at the provider or never reached the model are excluded from
both variants and counted.

## Limits

Results hold for one model, these tasks and these repeats. Hidden tests
derived from a failed run were not written by a person; review the mined
tasks before trusting a loss. To adopt a candidate, copy its config into your
`atlasrc` or `atlas.json` yourself.

The unit tests exercise the whole loop offline with mock agents (an injected
command runner); no real model is called.

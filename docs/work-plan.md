# Work plans and the verification contract

Long work tends to go wrong in a few predictable ways. The agent loses track of
the work once the context is compacted or a new session starts. It calls work
done because its own report says so. It keeps patching the same failing idea,
breaks earlier features without noticing, or writes checks that cannot fail.
Atlas handles these with a plan the agent writes and checks that Atlas enforces.
None of it is specific to a kind of project: the agent writes the checks for the
project at hand.

## Files

The agent creates these with its ordinary file tools before implementing
substantial work. Small fixes do not need them.

`.atlas/work.json`:

```json
{
  "goal": "Bootable hobby kernel with a serial shell",
  "verify": [
    {"name": "build", "command": "make"},
    {"name": "boot", "command": "./scripts/boot-test.sh"}
  ],
  "features": [
    {"id": "F1", "title": "Boots and prints to serial",
     "acceptance": "boot log shows BOOT OK", "verify": "./scripts/boot-test.sh", "done": true},
    {"id": "F2", "title": "Timer interrupt", "verify": "./scripts/timer-test.sh"}
  ],
  "assumptions": ["x86_64 only", "GRUB as the bootloader"],
  "previewed": true
}
```

`.atlas/progress.md` holds short notes: the current state, decisions,
approaches that failed and why, and the exact next step.

Atlas writes its own bookkeeping next to them: `work-state.json` (which plan
version was reviewed, which features were followed up), `work-metrics.json`
(time and tokens per feature), `checkpoints.json` (green states) and
`logs/` (saved command output).

All of this lives in `.atlas/`, Atlas's default data directory, which it keeps
out of git with its own `.gitignore`.

When the agent builds the project somewhere other than the folder Atlas was
started in (for example, Atlas runs in your home folder and you ask for a
project on another drive), the first work plan it writes there makes that
folder the session's project. From then on the plan, its checks, the plan
review, the error memory and the failure corpus all use that folder.

## At the start of every request

The system prompt carries a compact summary of the plan:

- the goal, how many features are done and the next open ones;
- the time, turns and tokens the current feature has taken, with a warning
  when it costs far more than finished features did (or has taken many turns
  with nothing to compare), which is the cue to step back;
- the verification commands;
- the assumptions, so the agent repeats them in its report;
- a reminder to show the user a preview once the first feature works, until
  the plan says `"previewed": true`;
- the dependency versions in use, read from `package.json` (installed
  versions from `node_modules` when present), `go.mod`, `requirements.txt`,
  `pyproject.toml` and `Cargo.toml`, so the agent checks the documentation for
  those versions instead of writing APIs from memory;
- the end of the progress notes.

A new or compacted session resumes from there. When the context is compacted,
the summarizer is told the plan survives on its own and spends the summary on
the feature in progress, the last failing command and the next step. A plan
Atlas cannot parse is reported rather than ignored.

## When a turn that changed files ends

1. **Plan review.** A new or changed plan is reviewed once by a separate model
   with a fresh context: the evaluator model, else the advisor, else the
   session's own model. It looks for missing features, vague acceptance
   criteria, impossible ordering and checks that cannot fail. Findings go back
   to the agent. Marking features done does not trigger another review.
2. **The contract.** Every `verify` command, plus the `verify` command of each
   feature marked done, must have run after the last code change and exited 0.
   A run may wrap the command (`cd app && make`, `CI=1 npm test | tail`), but a
   run that throws away the exit status (`make || true`) does not count.
   Editing the plan or the progress file does not make earlier checks stale.
   Gaps go back to the agent as its next instruction, and repeated failures
   escalate:
   - the same check failing twice in a row asks for a step back: re-read the
     failure, write hypotheses, run one experiment that tells them apart;
   - three times prepares two git worktrees and asks for a genuinely
     different approach in each, in parallel sub-agents when available; the
     one whose check passes is brought into the project (see
     [harness.md](harness.md));
   - a check known to be flaky does not escalate on a single failure;
   - after four rounds the turn ends and the unmet checks are recorded on
     the message.
3. **Regressions.** Whenever the contract holds in a git repository, Atlas
   records a checkpoint: a commit object built from a temporary index, kept
   under `refs/atlas/green`. The user's index, HEAD, branches and files are
   never touched. When a check that passed at a checkpoint fails later, the
   report names the checkpoint and lists the files changed since, with a hint
   to bisect.
4. **Checks that cannot fail.** For each feature finished since the last
   follow-up, Atlas checks out the last green state from before the feature
   into a temporary worktree under `.atlas/probes/` and asks the agent to run
   the feature's check there. It must fail. If it passes, the check does not
   test the feature and the agent is asked to strengthen it. Installed
   dependencies are not part of a checkpoint, so a check may fail there for
   that reason alone; that errs on the safe side, because only a passing check
   is flagged.
5. **Breaker pass.** The same finished features get an adversarial pass: the
   agent is asked to find inputs and situations that break them (edge cases,
   hostile input, huge data, concurrency, interruptions, keyboard-only use)
   and write a failing test for each real problem. When the agent tool is
   available, it is told to delegate this to a sub-agent with a fresh context.
   Each feature is followed up once.
6. **Recipes.** The commands that verified the work are saved to project
   memory once, so the next session starts with known-good checks.

Steps 1, 4, 5 and the follow-up rounds are bounded. Every step fails open: an
error in git, the reviewer or the file system ends the turn as it would have
ended without it.

The contract is in addition to the [evaluator](../internal/agent/evaluator.go),
not a replacement for it. When an evaluator model is configured, it reviews the
code once the contract holds, and with the turn's latest screenshots it also
judges what the user will see.

## Options

Everything above is on by default. Each follow-up can be turned off:

```json
{
  "options": {
    "work_plan": {
      "plan_review": false,
      "prove_checks": false,
      "checkpoints": false,
      "breaker": false,
      "learn_recipes": false
    }
  }
}
```

## Writing good checks

The agent writes the checks for the project at hand. They are not built into
Atlas:

- A web app: the build, a typecheck and unit tests of its logic. The page
  itself is checked with the browser tool (each flow, the console, desktop and
  phone screenshots), not with a Playwright, Cypress or Selenium suite; such
  commands are not accepted as checks.
- A kernel: a script that boots the image in QEMU with a timeout and looks
  for a marker line on the serial log.
- A 3D scene: a script that renders fixed frames headlessly and checks the
  scene state, not just a screenshot.
- A library: its test suite.

A check that cannot fail proves nothing.

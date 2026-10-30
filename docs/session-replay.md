# Session replay, branching and compare

Three commands built on the existing session store, `rewind` and file
history. Nothing here modifies or deletes the source session.

## Timeline

```
atlas-agent session timeline <id> [--json]
```

Lists every message as a numbered step with time, role, model, a one-line
summary, tool errors and non-normal finish reasons. A step marked `*` has a
tool call that is still unanswered; a branch cannot end there because
providers reject a conversation with an unmatched tool call.

## Branch

```
atlas-agent session branch <id> --at <step|message-id> \
    [--model <m>] [--prompt <text>] [--restore-files] [--dry-run] [--run]
```

Creates a new top-level session holding the source's messages up to and
including the step, carrying over the file history so the branch can itself
be rewound. The branch is tagged `branch-of:<source hash>` and titled
`Branch: <source title>`.

- Any step is allowed as long as all tool calls so far are answered
  (unlike `session rewind`, which only forks at user messages). A refused
  step names the nearest usable one.
- Files on disk are left alone unless `--restore-files` is given, which
  rewrites them to their state at that step. Use `--dry-run` first to see how
  many files would be written or deleted.
- `--prompt` is the next message to send. When the step is a user message it
  is replaced by the prompt, so "retry this turn with different wording" is
  `branch --at 3 --prompt "..."`.
- `--model` selects the model for the continuation. Sessions do not store a
  model, so it is applied to the continue command, not to the stored data.
- Without `--run` the exact continue command is printed
  (`atlas-agent run -s <id> -m <model> "<prompt>"`). With `--run` (needs
  `--prompt`) this binary re-executes `run` on the branch, so the same
  permission mode and headless path apply.

## Compare

```
atlas-agent session compare <a> <b> [--json]
```

Reports, for two sessions (typically a session and its branch, or two
branches of one step):

- where the messages diverge (count of identical leading steps and what each
  side did next; message IDs and timestamps are ignored);
- tool calls and errors per side, and the tools whose counts differ;
- files each changed: only in A, only in B, changed by both with different
  content, or with identical content;
- tokens and cost per side, and the models that answered;
- the outcome of each: final finish reason and an excerpt of its last answer.

File comparison uses the recorded file history, so it reflects what the agent
wrote, not the current state of the working directory.

`session diff` remains available for the quick tool-count-only comparison.

## Limits

CLI only; there is no TUI timeline view. Token counts and cost are
session-level totals because messages do not store usage individually.

# Context and cost

Why delegation is cheaper, and the controls that bound the bill.

[Back to the README](../../README.md) · [All guides](README.md)

## Token economics: why delegation is cheaper

This is the part that is easy to miss, so here it is with numbers.

**The problem with a single context.** Every file you read stays in the conversation for the rest of
the session, and every subsequent turn re-sends the whole conversation. Reading a 600-line file once
does not cost you 600 lines — it costs 600 lines *times every turn that follows*. A long session
where the agent read forty files is paying for those forty files on every single request, whether or
not they are still relevant.

**What delegation changes.** A subagent's reading happens in its own session. Your main session
receives its answer, and nothing else.

Illustrative, for one "find how X works and summarize it" task in a large codebase:

| | Files read | Tokens in the main session | Cost on every later turn |
| --- | --- | --- | --- |
| Done inline | 18 | ~45,000 | ~45,000 re-sent, forever |
| Delegated to a subagent | 18 (in the subagent) | ~600 (the answer) | ~600 re-sent |

The subagent still pays to read those 18 files — once. What you avoid is re-sending them on every
turn for the rest of the session, and the context-window pressure that forces an early summarization.

**Where the savings compound:**

- **`delegate` runs the pieces in parallel**, so a four-part task takes about as long as its slowest
  part rather than the sum of all four.
- **Cheap models for cheap work.** Titles, summaries, and compaction do not need your best model.
  Point the `small` model type and the `compact` role at something cheap and the difference shows up
  on every single session.
- **Role-based routing.** Research on a fast, large-context model; final code changes on a strong
  one; the goal judge on a mid-tier one. You are not paying frontier prices for `git log` parsing.
- **Later summarization.** Because the main context grows slower, `auto_summarize_at` triggers less
  often, and each summarization loses less of what you actually care about.

**Controls that bound the bill directly** (see [Configuration reference](configuration.md#configuration-reference)):

| Option | What it bounds |
| --- | --- |
| `options.max_session_cost` | Refuses a new prompt once a session's accumulated USD cost hits the limit. |
| `options.max_steps_per_turn` | Stops a turn after N model/tool steps, whether or not it is making progress. |
| `options.max_concurrent_sub_agents` | Caps how many subagents run at once. |
| `options.auto_summarize_at` | Fraction of the context window at which the session compacts itself. |
| `options.tool_timeout` | Kills a single tool call after N seconds instead of burning a turn on it. |
| `models.small` + `model_roles.compact` | Which (cheap) model does the cheap work. |

And `usage` (tool) or `atlas-agent stats` (CLI) will tell you where the tokens actually went.

## Context and cost management

| Mechanism | What it does |
| --- | --- |
| **Auto-summarize** | At `options.auto_summarize_at` (fraction of the context window), the session compacts itself. Runs on the `compact` role's model, not the session's. |
| **Manual compaction** | `/summarize` in-session, or `atlas-agent session compact <id>`. |
| **Delegation** | Subagent reading never enters the main context. See [Token economics](#token-economics-why-delegation-is-cheaper). |
| **Small model** | Titles, summaries, and side work on a cheap model. |
| **Memory bounds** | `options.memory` bounds the prose carried between sessions so it cannot grow without limit. |
| **Session cost ceiling** | `options.max_session_cost` refuses new prompts past a USD limit. |
| **Step ceiling** | `options.max_steps_per_turn` stops a turn that is not converging. |
| **Tool timeout** | `options.tool_timeout` kills a stuck tool call instead of burning the turn. |
| **Subagent concurrency** | `options.max_concurrent_sub_agents` caps parallel spend. |
| **Usage reporting** | The `usage` tool in-session; `atlas-agent stats` and `atlas-agent provider usage` outside it. |

# Agents, delegation and goals

Subagents, orchestration, debate, autonomous goals and configuring by chat.

[Back to the README](../../README.md) · [All guides](README.md)

## Delegation: agent, orchestrate, delegate, debate

A subagent is a full agent session of its own: its own context window, its own tool calls, its own
reading of the codebase. It sees only the prompt you hand it — never your conversation — and only
its final answer comes back.

That constraint is the feature. The subagent can read twenty files to answer one question, and your
main session pays for one paragraph instead of twenty files.

| Tool | Shape | Reach for it when |
| --- | --- | --- |
| `agent` | One task → one subagent. | The work is self-contained and you want the conclusion, not the research trail. |
| `orchestrate` | Same prompt → several subagents, in parallel, answers side by side. | One answer is not worth taking on faith, and independent corroboration is the point. |
| `delegate` | Several *different* subtasks → several subagents, in parallel. | The task splits into pieces that do not need to see each other's output. |
| `debate` | A question → several subagents over multiple rounds, each answering the others, optionally judged. | The first pass came back split, or a decision is expensive to get wrong and cheap to think about longer. |
| `vibe` | A persistent background worker toward a goal, steerable while it runs. | Work should continue independently of the current turn. |

### `agent`

```text
> agent: find where session titles are generated and what model they use
```

Runs on the default task agent, or on a named subagent (`review`, `security`, `research`, …), or
picks a subagent automatically by matching the prompt against each one's description.

### `orchestrate`

Same question, several models or several specialists, independently. A majority answer means
something *because* none of them saw the others.

```text
> orchestrate this across review and security:
  "is this token refresh logic safe to run concurrently?"
```

### `delegate`

Different pieces, in parallel. Each subtask carries its own self-contained prompt.

```text
> delegate: (1) write the migration, (2) update the sqlc queries,
  (3) add the round-trip test — do them in parallel
```

### `debate`

The escalation from `orchestrate`. Round one is independent — nobody anchors on anyone. Every round
after that hands each agent what the others said and asks them to defend, concede, or change their
mind. What comes out is either a position that survived being argued against, or a disagreement
specific enough to be worth something.

```text
> debate: should the goal loop's completion decision be made by a judge model
  or by the main model? use review and security, 3 rounds, planner as judge
```

Parameters: `question`, `agent_names` (2–5), `rounds` (default 2, max 4), `judge_agent` (optional,
must be distinct from the debaters). Two rounds is usually right — one to stake out positions, one
to answer the others; beyond that they mostly converge on wording.

> [!TIP]
> Do not use `debate` for questions the codebase can settle. Three models speculating about how your
> code works produce three confident guesses and no evidence. Use it for judgment calls: an
> architecture you will build on, a migration, a security trade-off, a fix you cannot easily undo.

## Autonomous goals

```text
> /goal every endpoint in internal/server returns a typed error, not a bare string
```

That sends immediately, exactly like an ordinary prompt — the goal is the first turn, not a setting
you configure and then have to kick off separately.

From there the session keeps taking turns of its own toward the objective. Each turn ends, publishes,
and the next one begins without you typing anything.

**How it decides it is done.** Two things have to agree:

1. The agent calls the `goal` tool's `done` action, claiming the objective is reached.
2. A **judge model** — a second model that sees the goal and the work, not the agent's reasoning —
   confirms it. The judge is the `goal` model role when you have configured one, otherwise the
   advisor's model. If neither exists, the claim stands unchecked, and the run says so.

That separation is deliberate. A model asked "are you done?" at the end of its own turn is the worst
possible judge of the question.

**Bounds.** Every run carries a turn budget (default 10, hard ceiling 100). The agent can ask to
raise it through the `goal` tool when it judges the work bigger than it first looked, and the tool
caps rather than refuses an over-large request. The run ends when the goal is confirmed reached, the
budget is exhausted, or you clear it.

**Visibility.** The goal is shown in the sidebar's **Goal** section and in the header for the whole
run, and survives a restart — not just a toast that disappears after five seconds. `/goal` with no
argument reports what is being worked toward and how much budget is left. `/goal clear` (or `stop`,
`off`, `none`) ends it.

## Talk to it to configure it

`atlas_config` gives the agent the ability to change ATLAS-AGENT's own configuration, so a request
about how the tool behaves is work it does rather than instructions it hands back to you.

| You say | What it does |
| --- | --- |
| "use the cheap model for summaries" | `set_role` on `compact` |
| "put Sonnet on research" | `set_role` on `research` |
| "switch me to GPT for this project" | `set_model` with `scope: workspace` |
| "turn the browser tool off" | `disable_tool` |
| "what am I actually running right now?" | `list` |
| "make it summarize at 70% instead" | `set_field` on `options.auto_summarize_at` |
| "make me a subagent that only reviews UI/UX" | `save_subagent` |
| "what subagents do I have?" | `list_subagents` |
| "delete the frontend-critic subagent" | `delete_subagent` |

Every change that is not a read asks for permission first, described in words rather than as a tool
name — "Run the `research` role on claude/claude-sonnet-5 (global)", not
`atlas_config set_role`. Values are type-checked against the config schema *before* they are
written, so a model answering "make it 70%" with the string `"0.7"` where a number belongs is
refused rather than silently corrupting the config file.

Scope is `global` (everywhere you run Atlas Agent) or `workspace` (this project only).

## Subagents and modes

Ten specialist modes ship in the binary:

| Mode | What it is for |
| --- | --- |
| `backend` | Server-side code: APIs, data access, background work, concurrency, failure handling. |
| `frontend` | UI code: components, state, styling, accessibility, rendering performance. |
| `debug` | Root-causing a failing test, crash, or wrong behavior, then reporting the minimal fix. |
| `test` | Tests that fail for the right reason — real behavior, edge cases, error paths. |
| `review` | Correctness, security, and maintainability review with `file:line` evidence. |
| `security` | Exploitable vulnerabilities with an attack path for each. |
| `refactor` | Behavior-preserving restructuring in small verified steps. |
| `research` | Open questions answered with evidence and citations, verified separated from inferred. |
| `planner` | A feature request turned into an ordered, codebase-grounded implementation plan. |
| `docs` | READMEs, API references, guides, doc comments, grounded in what the code does. |

Each can run as a subagent (`agent`, `orchestrate`, `delegate`, `debate`) or be folded into the main
session with `options.session_mode`, so the two can never drift apart.

**Authoring your own.** A subagent is a Markdown file with YAML frontmatter:

```markdown
---
name: frontend-critic
description: Reviews UI work strictly from a UI/UX perspective — hierarchy, layout, typography, spacing, accessibility. Use when you want critique of how a UI looks and feels, not implementation correctness.
model: "@frontend"
---

You are "frontend-critic", a UI/UX-only reviewer.

Evaluate in this order: visual hierarchy, layout and spacing, typography,
color and contrast, interaction states, accessibility, responsiveness,
information architecture, consistency.

Report findings as a numbered list. For each: the issue and why it matters
to the user, a concrete fix in design terms, a severity (blocker / major /
minor / nit), and file:line when reviewing code. End with what is already
working well.
```

Drop it in `.atlas/agents/` (project scope) or your user config directory (global scope) as
`frontend-critic.md` — or just ask the agent to write it for you with `atlas_config`.

The `model` field names a **role**, not a model id, so the same subagent runs on whatever model that
role currently points at. Leave it empty and the subagent runs on the session's own model.

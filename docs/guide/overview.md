# Overview

What Atlas Agent is, and how it compares with other coding agents.

[Back to the README](../../README.md) · [All guides](README.md)

## What Atlas Agent is

One coding agent for the AI subscriptions you already pay for: ChatGPT, Claude, Copilot and more.

Atlas Agent is a coding agent that runs in your terminal. You give it a task in plain language; it
reads the code, searches, edits files, runs the tests, reads the failures, and keeps going until the
task is done — asking for permission before anything with a side effect, unless you have told it not
to bother.

That much it shares with every other agent CLI. Four things make it different:

**It is not tied to one vendor's model.** The model is a configuration value, not a product
decision. Anthropic, OpenAI, Google, MiniMax, xAI, DeepSeek, Moonshot/Kimi, Z.ai, Zhipu, NVIDIA NIM,
OpenCode, local models — and flat-rate coding-plan subscriptions like GitHub Copilot, ChatGPT, and
Google Antigravity, which many developers are already paying for and cannot use from most other
agent CLIs. You can also point *different parts of the workflow* at different models: an expensive
model for the main session, a cheap one for titles and summaries, another for research, another to
judge whether a goal is finished.

**It delegates by default instead of doing everything in one context.** A subagent runs in a session
of its own. Everything it reads, greps, and opens stays there; only its answer comes back. Reading
twenty files to answer one question costs you a paragraph of context instead of twenty files of it.
See [Token economics](cost.md#token-economics-why-delegation-is-cheaper) for what that is actually worth.

**It can argue with itself, on purpose.** `debate` puts a question to several subagents over multiple
rounds, each seeing and answering what the others said, optionally with a further subagent as judge.
Disagreement that survives the argument is reported as disagreement rather than averaged into a
consensus nobody reached — which is the useful outcome when a decision is expensive to get wrong.

**It configures itself from the conversation.** "Use the cheap model for summaries." "Put Sonnet on
research." "Turn the browser tool on." "Make me a subagent that only reviews UI/UX." These are all
just things you say; `atlas_config` makes the change and tells you what it did. You never have to go
find a settings dialog and translate your sentence into a form.

## How it compares

A rough map of where Atlas Agent sits. Other tools move fast, so treat the right-hand columns as
"last checked" rather than gospel, and check for yourself before making a decision on it.

| | **Atlas Agent** | Claude Code | Aider | Cursor CLI |
| --- | --- | --- | --- | --- |
| License | MIT | Proprietary | Apache-2.0 | Proprietary |
| Language / distribution | Go, single static binary | Node | Python | Node |
| Model choice | Any provider, per role | Anthropic models | Any provider | Anthropic/OpenAI via Cursor |
| Flat-rate coding plans | Copilot, ChatGPT, Antigravity | Claude subscription | — | Cursor subscription |
| Subagents | 10 built in, author your own | Yes | — | Limited |
| Parallel fan-out | `orchestrate`, `delegate` | Partial | — | — |
| Multi-round model debate | `debate` | — | — | — |
| Autonomous goal loop with judge | `/goal` | — | — | — |
| Configure from chat | `atlas_config` | Partial | — | — |
| LSP-backed refactors | 9 LSP tools | Via MCP | — | Editor-native |
| Built-in tool count | 80+ | ~15 | ~10 | ~15 |
| MCP support | Yes | Yes | — | Yes |
| Hooks | 5 events | Yes | — | — |
| Runs offline with a local model | Yes | — | Yes | — |

**Where the honest weaknesses are**, because a comparison table that only flatters the author is
worth nothing:

- Several coding-plan logins are **scaffolds**, not finished integrations — the OAuth flow works but
  the request envelope is a stub. They are marked as such in
  [Providers and coding plans](providers.md#providers-and-coding-plans); do not pick Atlas Agent *because* of a
  scaffolded plan.
- Some of the deeper analysis tools (`dead_code`, `impact_analysis`, `type_hierarchy`,
  `code_metrics`, `security_scan`, `generate_tests`) are **Go-specific**. Everything else is
  language-agnostic, but if you are not writing Go you get a smaller toolbox.
- There is no editor extension. This is a terminal tool that happens to talk to LSP servers, not an
  IDE integration.
- It is young. Expect sharper edges than a tool with a support team behind it.

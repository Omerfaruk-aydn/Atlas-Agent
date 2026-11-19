# Troubleshooting, FAQ and known gaps

Fixes for common problems, frequent questions, and where the project is incomplete.

[Back to the README](../../README.md) · [All guides](README.md)

## Troubleshooting

**`atlas-agent: command not found` after install.** The installer adds `~/.atlas-agent/bin` to your
`PATH` in your shell profile; open a new shell, or `source` your profile.

**"No providers configured".** Set an API key environment variable, run
`atlas-agent login <platform>`, or declare a provider in the config file. `atlas-agent doctor`
reports what it can and cannot see.

**A model rejects a parameter (`Unsupported parameter: max_output_tokens`).** Some coding-plan
backends accept a narrower parameter set than the vendor's public API. Atlas Agent handles the known
cases; if you hit a new one, please open an issue with the provider and exact error.

**A subagent fails with "needs the `<name>` model role assigned".** Built-in subagents reference a
model role of the same name. Assign it — `atlas_config` with `set_role`, or `/roles` — or clear the
subagent's `model` field so it runs on the session's own model.

**LSP diagnostics are stale or missing.** `lsp_restart`, or `atlas-agent lsp list` to see which
servers actually came up. `options.auto_lsp` controls automatic startup.

**Windows: shell tests or bash tools misbehave.** If `bash` on your `PATH` resolves to the WSL stub
(`C:\Windows\System32\bash.exe`) without a distribution installed, install Git Bash and ensure it
comes first on `PATH`.

**Everything is too slow / too expensive.** Point `small` and `compact` at a cheap model, lower
`auto_summarize_at`, cap `max_steps_per_turn`, and delegate more (see
[Token economics](cost.md#token-economics-why-delegation-is-cheaper)).

## FAQ

**Do I need an API key if I already pay for Copilot or ChatGPT?**
No. `atlas-agent login copilot` or `atlas-agent login chatgpt` uses the plan you already have.

**Can it run fully offline?**
Yes, against a local OpenAI-compatible endpoint (Ollama, LM Studio, llama.cpp, vLLM). Tool-calling
quality then depends on the local model.

**Does it send my code anywhere I did not configure?**
No. The only outbound traffic is to the provider you configured, plus whatever `fetch`/`browser`
does when the agent uses them — both governed by the domain allow/block lists.

**Is it only for Go?**
No. The agent, the LSP integration, and the git/infra/testing tooling are language-agnostic. A
subset of the deeper static-analysis tools is Go-specific — those are the ones parsing Go ASTs.

**How is this different from just using the vendor's own CLI?**
Model choice per role, delegation to subagents, multi-round debate, an autonomous goal loop with an
independent judge, and configuration from the conversation. If you only ever use one vendor's model
and one context, the difference is smaller.

**Is the goal loop safe to leave running?**
It is bounded by a turn budget, a step ceiling, an optional cost ceiling, and the same permission
layer as everything else. In `manual` mode it still asks before side effects. In `bypass` mode it
does not — use that only where you would be comfortable letting a script run unattended.

**Can I use it in CI?**
Yes: `atlas-agent run "<prompt>"` is non-interactive. Pair it with `plan` mode or a tight
`allowed_commands` list if it should not change anything.

## Known gaps

Where the project is honestly incomplete, and where help is most useful:

- **Scaffolded coding plans.** Claude, Grok, Windsurf and JetBrains have working OAuth but stubbed
  call layers. Finishing one means capturing the real request envelope against the official client
  and filling in `internal/oauth/<plan>` plus the matching provider.
- **Static analysis is Go-first.** `dead_code`, `impact_analysis`, `type_hierarchy`, `code_metrics`,
  `api_surface`, `security_scan`, `anti_pattern_scan`, `generate_tests` and `generate_docstring`
  parse Go. The same shapes exist for other languages; nobody has written them yet.
- **Sandboxing is Windows-only.** `options.sandbox` uses Job Objects; the Linux (cgroups/namespaces)
  and macOS equivalents are unimplemented no-ops.
- **`debate` and `orchestrate` cost real money.** Both scale with agents × rounds. Sensible defaults
  are in place, but there is no budget-aware planner that decides a debate is not worth running.
- **No editor extension.** The LSP integration means Atlas Agent understands your code, but it does
  not live inside your editor.
- **Windows shell edge cases.** Shell dispatch assumes a POSIX-ish `bash`; the WSL stub on a machine
  with no distribution installed produces confusing failures rather than a clear message.

Issues and PRs on any of these are welcome.

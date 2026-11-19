# Tools

The built-in tools, grouped by what they are for.

[Back to the README](../../README.md) · [All guides](README.md)

## Tools

80+ built-in tools. The `atlas_info` tool reports which are enabled in a live session; anything can be turned off
with `atlas_config` or `options.disabled_tools`.

### Editing and navigation

| Tool | What it does |
| --- | --- |
| `view` | Read a file, optionally a line range. |
| `write` | Create or overwrite a file, creating parent directories. |
| `edit` | Exact find-and-replace, with whitespace-tolerant matching and re-indentation. |
| `multiedit` | Several find-and-replace edits to one file, applied in sequence. |
| `glob` | Match files by glob pattern. |
| `ls` | List a directory, with ignore patterns and depth. |
| `inspect_file` | Look inside a `.zip`/`.tar`/`.tar.gz`, a SQLite database, or a Jupyter notebook. |
| `download` | Fetch a URL to a local file, with size and domain limits. |

### Code intelligence (LSP)

| Tool | What it does |
| --- | --- |
| `lsp_diagnostics` | Errors, warnings, and hints for a file or the whole project. |
| `lsp_references` | Every reference to a symbol — accurate where grep is not. |
| `lsp_definition` | Where a symbol is defined, skipping comments, strings, and partial matches. |
| `lsp_symbols` | A file's outline: symbols, kinds, line ranges. |
| `lsp_call_hierarchy` | Incoming or outgoing calls for a symbol — blast radius before a refactor. |
| `lsp_rename` | True semantic rename across files, respecting scope and imports. |
| `lsp_rename_file` | Move or rename a file, updating every reference to it first. |
| `lsp_replace_symbol` | Replace, insert, or delete a whole symbol by name, with exact boundaries. |
| `lsp_restart` | Restart one or all LSP clients when diagnostics go stale. |
| `project_model` | Living project model: what a change reaches, tests to run, owners, recorded decisions. See [docs/project-model.md](../project-model.md). |

### Static analysis

| Tool | What it does |
| --- | --- |
| `dead_code` | Declarations nothing in the tree references — deletion candidates. |
| `type_hierarchy` | Which Go types satisfy which interfaces, without compiling. |
| `import_graph` | How packages depend on each other, and where the import cycles are. |
| `impact_analysis` | Everything affected by changing a function, by transitive caller distance. |
| `code_metrics` | Cyclomatic complexity, length, nesting depth, signature arity. |
| `api_surface` | What a package exposes: exported functions, types, methods, fields, constants. |
| `semantic_code_search` | Find a declaration from a natural-language description. |
| `anti_pattern_scan` | Swallowed errors, misplaced `context.Context`, panics used instead of errors. |
| `security_scan` | Hardcoded credentials, broken crypto, disabled TLS verification, SQL/shell injection. |
| `security_verify` | Runs the proof tests behind security-audit findings and reports confirmed or unconfirmed. |
| `scan_secrets` | Committed API keys, tokens, private keys, passwords in connection strings. |
| `env_var_audit` | Every env var the code reads or writes, cross-checked against an example file. |
| `todo_scan` | TODO, FIXME, HACK, XXX, BUG, OPTIMIZE, DEPRECATED markers. |
| `doc_index` | A table of contents across a tree's Markdown, searchable by keyword. |
| `metric_export` | Every Prometheus metric constructed: name, type, help text, labels. |

### Testing and quality

| Tool | What it does |
| --- | --- |
| `test_run` | Run tests, get a structured result: what failed, why, how long. |
| `coverage_report` | Measure coverage and find the code no test reaches. |
| `lint_run` | Run the project's linter, findings grouped by file. |
| `dep_audit` | Dependencies with known vulnerabilities, and which are behind. |
| `generate_tests` | A table-driven test skeleton shaped from a function's own signature. |
| `generate_docstring` | Doc-comment stubs for exported declarations that have none. |
| `debugger` | Run a Go program under Delve: breakpoints, stepping, live variables. |

### Git and pull requests

| Tool | What it does |
| --- | --- |
| `git_status` | Staged, modified, untracked, and how the branch stands against upstream. |
| `git_diff` | Working tree, index, or between any two revisions. |
| `git_log` | Search history by path, author, message, date range, or branch. |
| `git_blame` | When each line last changed, by whom, in which commit. |
| `git_branches` | Branches with age, divergence, and whether already merged. |
| `git_conventional_commit` | A Conventional Commits type and scope for the staged changes. |
| `git_commit_split` | How to split a pile of changes into smaller commits, dependency-ordered. |
| `git_conflict_resolver` | Each conflicted region summarized: what each side changed, how big. |
| `pre_commit_guard` | Inspect staged changes for mistakes that are obvious only in hindsight. |
| `pr_describe` | A PR description drafted from the actual commits and diff. |
| `github_pr_view` | A PR's metadata and full diff via the `gh` CLI, without cloning. |
| `changelog_gen` | A changelog drafted from real git history, grouped for a reader. |
| `audit_trail` | One function's history through git line-history: who changed it, when, how. |

### Infrastructure and operations

| Tool | What it does |
| --- | --- |
| `docker_build_explain` | A Dockerfile's build stages, plus instruction-level mistakes that are easy to miss. |
| `k8s_manifest_lint` | Pod-spec mistakes that apply cleanly and surface later as OOM-kills or wide-open security contexts. |
| `terraform_lint` | Misconfigurations that are valid HCL, apply cleanly, and become incidents later. |
| `ci_cd_pipeline_debugger` | GitHub Actions mistakes that run green today and become supply-chain risk later. |
| `cloud_resource_costs` | Order-of-magnitude monthly cost from Terraform instance types and Kubernetes requests. |
| `log_tail` | The tail of a log file, filtered by substring or level, without loading all of it. |

### Delegation and collaboration

| Tool | What it does |
| --- | --- |
| `agent` | Run one task on one subagent. |
| `orchestrate` | Run the same prompt on several subagents in parallel. |
| `delegate` | Run several different subtasks in parallel. |
| `debate` | Multi-round argument between subagents, optionally judged. |
| `vibe` | A persistent, steerable background worker. |
| `goal` | How an autonomous run sizes its budget and claims completion. |
| `team_send` | Broadcast a message to every agent in the task's team. |
| `team_read` | Read what other agents in the team have sent. |

### System and self-configuration

| Tool | What it does |
| --- | --- |
| `bash` | Run a shell command, foreground or background, with command policy applied. |
| `job_output` | Read stdout/stderr from a background shell by id. |
| `job_kill` | Terminate a background shell. |
| `atlas_config` | Change providers, model roles, tool enablement, and subagents. |
| `atlas_info` | Current runtime state: model, provider, LSP/MCP status, skills, hooks, permissions. |
| `atlas_logs` | Recent entries from the agent's own log. |
| `exit_plan_mode` | Leave plan mode once a plan has been presented. |

### Knowledge and session

| Tool | What it does |
| --- | --- |
| `memory` | Record something worth carrying into future sessions, or revise it. |
| `facts` | Retain a fact for *this* session and recall it by keyword. |
| `todos` | A structured task list with pending/in-progress/completed state. |
| `question` | Ask the user a structured question and wait for the answer. |
| `session_search` | Search what was said in earlier sessions in this workspace. |
| `skill_manage` | Write a skill: instructions that load automatically when they match a later task. |
| `usage` | This session's token usage and cost so far. |
| `sourcegraph` | Search public code on Sourcegraph. |

### Browser and network

| Tool | What it does |
| --- | --- |
| `browser` | Drive a real browser: navigate, click, type, scroll, annotated screenshots, eval, snapshot, CDP. |
| `computer` | See the Windows screen and drive the real desktop: screenshot, click, drag, scroll, type, hotkeys. Opt-in via `/computer-use`. |
| `fetch` | Fetch a URL's content, subject to the domain allow/block lists. |
| `agentic_fetch` | Fetch and extract with a model in the loop, for pages that need reading rather than scraping. |

Web and UI work can also be checked in the running app before it counts as
done: set `options.product_verify.mode` to `auto` and Atlas opens the local dev
server with its own `browser` tool at three viewports, checks console errors,
failed requests, accessibility and load time, drives the flows in
`.atlas/verify.json`, and sends evidenced findings back to the agent. See
[docs/product-verification.md](../product-verification.md).

### MCP

| Tool | What it does |
| --- | --- |
| `list_mcp_resources` | List resource URIs exposed by a named MCP server. |
| `read_mcp_resource` | Read one resource by URI. |

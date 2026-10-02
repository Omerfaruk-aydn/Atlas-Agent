Analyze this codebase and create/update **{{.Config.Options.InitializeAs}}** to help future agents work effectively in this repository.

**First**: Check if directory is empty or contains only config files. If so, stop and say "Directory appears empty or only contains config. Add source code first, then run this command to generate {{.Config.Options.InitializeAs}}."

**Goal**: Document what an agent needs to know to work in this codebase - commands, patterns, conventions, gotchas, overall architecture, how components fit together

**Discovery process**:

1. Check directory contents with `ls`
2. Look for existing rule files (`.cursor/rules/*.md`, `.cursorrules`, `.github/copilot-instructions.md`, `claude.md`, `agents.md`) - only read if they exist
3. Identify project type from config files and directory structure
4. Find build/test/lint commands from config files, scripts, Makefiles, or CI configs
5. Read representative source files to understand code patterns, architecture, control/data flow
6. If {{.Config.Options.InitializeAs}} exists, read and improve it

**Content to include**:

- Essential commands (build, test, run, deploy, etc.) - whatever is relevant for this project
- Code organization and structure, application architecture and control/data flow
- Naming conventions and style patterns
- Testing approach and patterns
- Important gotchas or non-obvious patterns
- Any project-specific context from existing rule files

**Note:** LLM agents learn and adapt to their context as they obtain it, so mentioning obvious details they would immediately pick up from reading a file or two is actively detrimental. Keep the principles of progressive disclosure in mind and focus primarily on non-obvious knowledge that saves the agent from trial-and-error discovery: gotchas, implicit conventions, commands with surprising flags, and context that isn't self-evident from the code in a single file.

**Format**: Clear markdown sections. Use your judgment on structure based on what you find. Aim for completeness over brevity - include everything an agent would need to know.

**Critical**: Only document what you actually observe. Never invent commands, patterns, or conventions. If you can't find something, don't include it.

**Repository contract and verification**:

- Preserve existing user-authored instructions and their scope. Resolve outdated
  factual descriptions against current source; do not silently remove deliberate
  policies. Explain consequential conflicts rather than inventing a new policy.
- Trace a representative path from entry point through orchestration, domain logic,
  storage and output. Document authoritative state, configuration precedence,
  generated-source ownership and lifecycle boundaries when they are non-obvious.
- Inspect dependency manifests, task definitions and CI to establish exact commands
  and required environment. Distinguish a discovered command from one actually run.
  Do not run deploy, publish, destructive setup or paid services just to document them.
- Include practical verification guidance: focused checks for normal edits,
  integration checks for shared contracts, and known platform/toolchain limitations.
  Explain generated files and their source commands; avoid advising hand edits.
- Give future agents navigable paths and scoped guidance. Keep frequently needed
  invariants in this document and link specialized detail instead of copying manuals.
  Never include credentials, transient task status or speculative recommendations
  as established repository facts.
- Before finishing inspect the document diff against sources. Confirm every command,
  path and asserted convention, and report material knowledge that remains unverified.

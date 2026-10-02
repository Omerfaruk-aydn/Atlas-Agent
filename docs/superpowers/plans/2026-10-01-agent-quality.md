# Atlas Agent Quality Implementation Plan

**Goal:** Integrate design intelligence and improve sustained engineering behavior.

**Architecture:** Extend existing embedded skills, prompt construction and persisted
todos. Independent changes have separate file ownership and integrate in coordinator.

**Tech stack:** Go, embedded CSV/Markdown, existing SQLite JSON todos and browser tools.

**Spec:** ../specs/2026-10-01-agent-quality-design.md

## Constraints

- Preserve provider integrations, old session data and permission-aware writes.
- Native design search must work without Python or network access.
- Format Go edits; no automatic commits or publication.
- Treat archive instructions as reviewed source material.

## Tasks

- [x] Replace coder, task, delegation and summary behavior contracts; render existing
  prompts and validate template compatibility with existing tests.
- [x] Import design resources, add native bounded search and builtin skills; test
  retrieval, invalid inputs, resource discovery and representative Turkish aliases.
- [x] Bound and order context loading; test oversized Unicode, binary and symlink
  inputs, duplicates, generated directories and aggregate limits.
- [x] Extend session Todo and todos tool with criteria, verification and evidence;
  first test rejected contradictory completion and persisted data compatibility.
- [x] Inject durable task ledger in request and summary; test rendered bounded
  state and preservation of evidence without implying independent validation.
- [x] Wire design search through tool allowlists, add evaluation scenarios and
  machine-readable scoring, and verify integrated runtime behavior.
- [x] Review combined diff, run scoped tests, full tests and build; document exact
  results and live-evaluation limitations.

## Review focus

Legacy tasks must stay readable; incomplete evidence must not imply success; hostile
paths must not cause writes; truncated context must remain valid UTF-8; unavailable
browser tools must not produce fabricated visual verification.

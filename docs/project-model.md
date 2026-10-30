# Living project model

One incrementally refreshed view of the project, stored in
`.atlas/project_model.json`. It joins what Atlas already knows in separate
places:

| Part | Source |
| --- | --- |
| Modules (a directory of source files), dependencies, entry points | `codemap` (imports and resolved uses) and `package.json` |
| Test map: which tests cover which file | naming, package and imports |
| Ownership per module | commit history (`git log`, last 2000 non-merge commits) |
| Documents and architecture decision records | `docindex` over files the code map lists |
| Decisions | the fact store (`facts.jsonl`, tag `decision`) and ADR documents |

## Commands

```
atlas-agent project model build [--force] [--json]
atlas-agent project model show [module-or-file] [--budget N] [--json]
atlas-agent project model impact [files...] [--depth N] [--json]
```

`impact` without files assesses the files with uncommitted changes. A
directory stands for every file under it. The answer lists direct and
indirect dependents, modules, tests to run (nearest first) with commands,
module owners and the decisions that apply.

## Agent tool

`project_model` has the actions `summary`, `query`, `impact`, `tests`,
`owners`, `decisions`, `decide` and `refresh`. All but `decide` only read;
`decide` records a decision and asks permission, because it persists. Every
action first brings the model up to date, which re-reads only what changed.

## Incremental refresh

The model carries a fingerprint of file content hashes, the checked-out
commit and the recorded decisions. A refresh with an unchanged fingerprint
does nothing. Otherwise the code map re-reads only edited files (by size and
mtime, then content hash), the derived graph is recomputed in memory, and the
expensive parts are reused when their inputs did not change: ownership while
`HEAD` is the same, documents while no Markdown file changed.

## System prompt

When a model exists, a compact summary (default at most 2400 bytes) is added
to the coder system prompt under `# Project model`. The block is a pure
function of the stored model: it has no timestamps or per-turn data, so it is
identical on every turn and only changes when the project does, keeping the
prompt cache valid. If even the barest form exceeds the limit, nothing is
injected. The model is never built implicitly; run `project model build` or
call the tool once.

## Configuration

```
{
  "options": {
    "project_model": {
      "inject": true,
      "auto_refresh": true,
      "max_summary_bytes": 2400
    }
  }
}
```

`auto_refresh` refreshes an existing model when the prompt is built (limited
to 4 seconds; a slower project keeps its stored snapshot).

## Limits

The dependency graph is static: reflection, dynamic dispatch and generated
code are invisible, so an empty dependents list means no static use was
found. Test coverage is inferred, not measured. Ownership counts commits.

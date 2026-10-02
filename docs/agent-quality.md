# Agent engineering and interface quality

Atlas uses one engineering contract across discovery, implementation, integration
and verification. Large tasks should record observable acceptance criteria with the
`todos` tool and keep the ledger current when the user changes requirements.
Existing project architecture, dependencies and design systems take precedence over
generic templates. Built-in architecture, planning, research and review roles now
restrict direct edits and MCP tools while allowing scoped command execution
through `allow_commands: true`. Implementation belongs to an
implementation-capable specialist. Disjoint ownership is required for parallel
changes. These controls do not constitute an operating system filesystem sandbox.

See [Engineering execution](engineering-runtime.md) for dependency waves, isolated
workspaces, observed verification, UI evidence, runtime budgets, interruption
recovery and live model comparisons.

See [Role contracts](agent-roles.md) for responsibility contracts, task routing,
standard handoffs, independent quality checks and evidence-based model choice.

## Durable task evidence

Todos retain optional `acceptance_criteria`, `verification` and `evidence` fields in
the existing session JSON storage. No migration is needed. Verification values are
`pending`, `passed`, `failed`, `user_confirmed`, and `not_applicable`; evidence entries
contain `kind` (`command`, `inspection`, `user`) and `detail`. Legacy todos without
criteria remain readable. Omitting quality fields on an existing task preserves them.
Changing criteria or reopening a completed task invalidates inherited verification;
fresh evidence must support the new completion. `todos` action `list` reads a stored
record by zero-based `offset` to recover details absent from a compact ledger preview.
The same fields travel through REST/SSE session records and client save/read
conversions, so remote session updates retain their acceptance history.

Criteria-bearing tasks cannot complete without evidence and an accepted verification
state. Failed verification cannot complete. This validates reported consistency;
it does not authenticate execution. Report observed commands separately from explicit
user confirmations and inspect actual artifacts before accepting delegated claims.
The bounded ledger is supplied again each turn and to conversation compaction.

## Embedded interface guidance

Three builtins are available: `ui-ux-pro-max`, `apple-design`, and `tui-design`.
Use the first for web/mobile design; use Apple-inspired guidance selectively and
terminal guidance for TUI work. The original supplied reference documents and CSVs
remain archived under the builtins with provenance. Their license was not supplied;
do not infer third-party redistribution rights from their presence in this checkout.

`design_search` is an offline read-only native tool. It provides lexical candidates
from 11 domains and 13 stacks without Python, downloads or dependency installation.
Limited Turkish keyword aliases are supported; English queries provide broader
coverage. `design_system: true` retrieves candidates across seven domains for the
agent to synthesize, rather than automatically deciding the product's design.

Follow brief → design system → implementation → rendered verification. Preserve the
existing framework, tokens and components. Where useful, save `design-system/MASTER.md`
and scoped page overrides through normal permission-aware file tools. Exercise
keyboard interaction, relevant sizes, loading/error/empty/success states and real
submission paths. A source review or passing unit test alone is not visual evidence.
Existing browser/computer tools remain opt-in; explain unavailable visual checks.

## Context budgets

Configured project and global reference files share 256 KiB and 64-file limits;
each file is at most 64 KiB. Reads reject binary, malformed UTF-8 and symlink files.
Directory discovery excludes generated directories and common secret filenames;
explicitly configured individual secret files remain the user's responsibility.
Directory discovery is bounded at 8,192 entries, 4,096 candidates and depth 32.
Dense directories that cannot be completely enumerated within the remaining budget
are omitted, while known root instruction files are prioritized. Output order is
deterministic; omissions and UTF-8-safe truncation are disclosed to the model.

## Repeatable evaluation

Use a disposable checkout with a fixed starting revision, model settings and prompt
version. Execute the same scenario against each version. Independently inspect
acceptance results and retain commands, screenshots or trace locations as evidence.
Do not submit credentials or sensitive raw traces. Record metrics from actual usage.

```sh
atlas eval scenarios
atlas eval score results.json
```

`scenarios` lists nine engineering and UI/UX scenarios with stable criterion IDs.
`score` accepts a JSON array with one record per evaluated scenario:

```json
[
  {
    "scenario": "bug-regression",
    "model": "provider/model-id",
    "prompt_version": "git-revision",
    "claimed_complete": false,
    "checks": [
      {"criterion": "reproduced", "passed": true, "evidence": "Path to failing-before-fix test output"}
    ],
    "tool_calls": 12,
    "repeated_tool_calls": 1,
    "tokens": 4200,
    "duration_ms": 30000
  }
]
```

This is a format example, not a measured Atlas result. Missing criteria or evidence
count as unmet. The report includes evaluated/successful scenarios, acceptance rate,
false completions, repeated-call rate, tokens, duration and missing scenarios.
Scoring reads recorded results; it does not launch models or independently verify
their evidence. Compare like-for-like recorded runs before claiming improvement.

## Integration verification

On 2026-10-01, all 97 test packages passed in an uncached sequential test run.
The final binary built successfully; its scenario command listed nine scenarios,
and its scorer correctly flagged a claimed completion without acceptance evidence.
Go formatting and `git diff --check` were clean. Exact commands, environment and
results are recorded in [the verification record](agent-quality-verification-2026-10-01.json).
No live model comparison was run, so these results establish integration correctness
rather than a measured increase in model performance.

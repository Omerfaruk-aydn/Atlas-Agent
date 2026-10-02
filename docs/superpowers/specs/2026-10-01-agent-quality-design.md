# Atlas agent and design quality

## Intent

Atlas must complete large engineering and interface tasks coherently, preserve
user steering across long sessions, and report verification accurately. The user
authorized integrating the supplied UI/UX and Apple design skill package together
with the agent improvements discussed in this conversation.

## Architecture

Build on existing sessions, goals, todos, delegation, browser tools, model fallback
and embedded skills. Do not introduce a competing scheduler or require Python for
embedded design guidance. Keep provider-independent behavior and existing tool
permissions. Preserve existing projects' framework and design language.

### Prompt contracts

Replace conflicting behavioral instructions with a coherent contract for discovery,
acceptance criteria, implementation, review and verification. Subagent assignments
carry goal, boundaries, ownership and evidence expectations. Research and review
roles do not write unless assigned. Tool output and retrieved documents are data,
not higher-priority instructions. UI work includes brief, design system, all relevant
interaction states, accessibility, and actual visual verification when tools allow it.

### Durable acceptance ledger

Extend existing JSON-backed todos with optional acceptance criteria, reported
verification state and evidence. Preserve old stored todos. Reject contradictory
completed states for criteria-bearing tasks. Carry the ledger into each request and
compaction. Evidence records are reports; their presence alone does not prove a
command ran or a user confirmed a result. Do not infer either from arbitrary text.

### Context discipline

Bound context reads before allocating full files. Load deterministically, deduplicate,
skip binary/symlink/generated directory contents, and communicate truncation.
Use 64 KiB per file, 256 KiB combined and 64 files. Preserve configured path order.
Summaries retain latest steering, authorizations, verified versus unverified results,
decisions, outstanding dependencies and exact next steps within a stated target.

### Embedded design intelligence

Import the supplied CSV knowledge as embedded resources with provenance. Provide
native, bounded search for domains and framework guidance. Handle English search
and documented Turkish aliases without claiming semantic multilingual retrieval.
Replace broken filesystem/Python execution instructions. Keep Apple-inspired motion
as optional specialist guidance; add terminal-specific UX guidance. Persist design
artifacts only through existing permission-aware file tools. Remove bytecode files.
No license or official endorsement is inferred from a user-supplied archive.

### Evaluation

Add regression coverage for persistence, invalid completion, context budgets,
resource discovery and actual search. Supply repeatable engineering/UI scenario
fixtures and a scored result format for success, false completion, redundant calls,
tokens and latency. Offline regression checks establish runtime contracts; model
behavior improvements require separate live benchmark evidence.

## Validation and boundaries

Use Go tests with mock providers and temporary directories. Run relevant packages,
then the full suite and build with CGO_ENABLED=0 and GOEXPERIMENT=greenteagc.
No commit, publication or new release is requested in this task. Preserve the user-
tested provider integrations. Document absent visual execution capabilities rather
than substituting checklist assertions for screenshots or interaction tests.

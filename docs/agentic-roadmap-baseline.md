# Agentic roadmap: Phase 0 baseline

Date: 2026-10-27, Europe/Istanbul. This baseline describes the existing working
tree, not a clean HEAD checkout. No production code was changed or repaired.

## Environment and commands

- Repository: `D:\Atlas`.
- HEAD: `e5c2064aa04e9fec099def6238986d6ef3401374`.
- Toolchain: `go version go1.27.0 windows/amd64`; `go.mod` requests Go 1.26.6.
- `go` was initially absent from the process PATH. The installed executable is
  `C:\Users\Ömer&Ceylin\AppData\Local\Programs\Go\bin\go.exe`.
  Its directory was prepended only in the test command's environment.
- `CGO_ENABLED=0`, `GOEXPERIMENT=greenteagc`; `AI_AGENT` removed from the test
  process environment. User/machine environment configuration was not changed.
- Build and tests use `-p 1` to serialize package builds/linking on Windows.
  Tests retain their own existing parallelism; this is not `-parallel 1`.
- No real model benchmark, Antigravity provider test or credential-dependent
  model invocation was requested or run. Local tokenizer installation used a
  temporary dependency directory outside the repository.

| Command | Result | Elapsed wall time |
|---|---|---|
| `git status --short` | Captured before writing any audit artifact: 82 existing paths | Not timed |
| `git diff --stat` | 48 files changed, 1,264 insertions, 336 deletions | Not timed |
| `go build -p 1 ./...` | PASS, exit 0 | 17.895 seconds |
| `go test -p 1 ./internal/agent/... ./internal/config/... ./internal/cmd/...` | PASS, exit 0; seven tested packages, two packages without tests | 758.082 seconds |
| Audit-only template rendering and offline token counting | PASS, 16 fixtures | Not used as a performance benchmark |

The build log is empty because the build succeeded without diagnostic output.
Exact results and raw logs are under
[`agentic-roadmap-phase0-evidence/`](agentic-roadmap-phase0-evidence/).

No standalone `go vet`, project-wide formatter, `task lint:fix`, race test or
`atlas eval bench validate` was run for Phase 0. The requested baseline is build
plus the three specified test trees. A project-wide formatter/lint fixer could
rewrite the user's existing work; production quality gates and expanded bench
validation belong to the implementation phases. Race requires CGO, which is
disabled for this baseline. Ordinary `go test` uses its normal default vet
behavior; this is not a claim of a separate full vet run.

## Test results and existing failures

No failing package or test was observed in the requested baseline. The raw
output does not enumerate skipped tests, so this is a package-level success
claim, not a claim that every optional integration test executed. The known
`AI_AGENT` environment issue from the brief was prevented in this run; it is
not being claimed as a newly reproduced failure. Clean HEAD was not tested
separately, and no result is attributed to a specific uncommitted change.

| Package (under `internal/`) | Result | Package-reported test time |
|---|---|---:|
| `agent` | PASS | 86.222 s |
| `agent/agenttest` | No test files | — |
| `agent/notify` | No test files | — |
| `agent/prompt` | PASS | 7.685 s |
| `agent/tools` | PASS | 378.481 s |
| `agent/tools/mcp` | PASS | 9.288 s |
| `config` | PASS | 9.123 s |
| `cmd` | PASS | 15.818 s |
| `cmd/clientserverrace` | PASS | 0.939 s |

Wall time includes build/link and test-launch overhead, so it exceeds the sum
of package-reported test times. The package named `clientserverrace` passed in
the normal run; that package name does not mean the Go race detector ran.
See [raw test output](agentic-roadmap-phase0-evidence/tests-output.txt) and
[machine-readable result](agentic-roadmap-phase0-evidence/tests-result.json).

## Prompt size measurements

The measurements render the actual `coder.md.tpl` or `task.md.tpl` plus the
shared `agent_contract.md.tpl`, using Go's `text/template`. The fixture retains
the protected authorization block unchanged. Protocol text/wrappers/order match
`withToolProtocols` for palettes containing only `browser`, `computer`, or both;
these categories have no additional `taskCategoryGuidance` entry. Routing text
is included whenever either tool protocol is loaded. `none` means no advertised
tool protocol message, so its difference from the other rows also includes the
shared routing text.

Two profiles isolate the baseline from project instructions:

- `core`: fixed working directory `D:/Atlas`, git-repo flag true, platform
  `windows`, date `10/27/2026`, empty git-status text, empty task-protocol recipe,
  no skills/role guidance/memory/global context/optional config limits/LSPs.
- `project_agents`: the same inputs with the current root `AGENTS.md` in one
  scoped project-context block (`path=AGENTS.md`, `origin=.`, `scope=project`).

**These are reproducible system-text fixtures, not complete live requests.**
Normal discovery can add builtin/user skill metadata, task-intent protocols,
other context files, provider prefix, MCP instructions, goals, browser/work
contracts, runtime guidance and tool schemas. Those variable layers are excluded
explicitly. No real session or provider was opened to measure them.

`o200k_base` counts are exact for that reference tokenizer (tiktoken 0.14.0),
summed independently per system-message text. They do not claim to match every
supported provider or include provider chat framing/schema overhead. Atlas
approximate counts use `usage_fallback.go:approxTokenCount`, namely
`sum(ceil(UTF-8 bytes / 4))`; `context inspect` uses a different rune-based
estimate. Raw byte counts preserve checked-out source line endings.

| Mode | Profile | Loaded protocols | UTF-8 bytes | o200k_base tokens | Atlas approximate tokens |
|---|---|---|---:|---:|---:|
| coder | core | none | 15,313 | 3,254 | 3,829 |
| coder | core | browser | 28,360 | 5,762 | 7,091 |
| coder | core | desktop | 42,976 | 8,627 | 10,745 |
| coder | core | browser + desktop | 50,873 | 10,190 | 12,719 |
| task | core | none | 15,292 | 3,193 | 3,823 |
| task | core | browser | 28,339 | 5,701 | 7,085 |
| task | core | desktop | 42,955 | 8,566 | 10,739 |
| task | core | browser + desktop | 50,852 | 10,129 | 12,713 |
| coder | project_agents | none | 25,841 | 5,729 | 6,461 |
| coder | project_agents | browser | 38,888 | 8,237 | 9,723 |
| coder | project_agents | desktop | 53,504 | 11,102 | 13,377 |
| coder | project_agents | browser + desktop | 61,401 | 12,665 | 15,351 |
| task | project_agents | none | 25,761 | 5,654 | 6,441 |
| task | project_agents | browser | 38,808 | 8,162 | 9,703 |
| task | project_agents | desktop | 53,424 | 11,027 | 13,357 |
| task | project_agents | browser + desktop | 61,321 | 12,590 | 15,331 |

With the core fixture, the routing-plus-browser message adds 2,508 reference
tokens; routing-plus-desktop adds 5,373; loading both adds 6,936 (routing is
shared). These are input-text sizes, not measured extra cost or cached usage.
No Phase 1 golden assertion or >5% threshold test has been added.

Machine-readable counts and per-message SHA-256 fingerprints are in
[`agentic-roadmap-prompt-sizes.json`](agentic-roadmap-prompt-sizes.json).
The audit-only source is saved with `.go.txt` so it does not become a Go package
or alter `go build ./...` discovery. Reproduce from the repository root with
the Go bin directory on PATH:

```powershell
$measurementDir = Join-Path $env:TEMP ('atlas-prompt-measure-' + [guid]::NewGuid())
New-Item -ItemType Directory -Path $measurementDir | Out-Null
Copy-Item docs/agentic-roadmap-phase0-evidence/measure-prompts.go.txt (Join-Path $measurementDir 'main.go')
go run (Join-Path $measurementDir 'main.go') > (Join-Path $measurementDir 'rendered.json')
py -3 -m pip install --target (Join-Path $measurementDir 'libs') tiktoken==0.14.0
$env:PYTHONPATH = Join-Path $measurementDir 'libs'
py -3 docs/agentic-roadmap-phase0-evidence/count-prompt-tokens.py (Join-Path $measurementDir 'rendered.json') (Join-Path $measurementDir 'sizes.json')
```

The reproduction command is documentation, not an instruction to overwrite an
existing report. Rendering does not run shell config, hooks, skills or models.

## Change ownership and preservation check

Only new documentation/evidence files under `docs/agentic-roadmap-*` belong to
Phase 0. `docs/agentic-roadmap-master-prompt.md` is existing user work and was
read without modification. No existing production file or test was edited,
formatted or restored; the two original deletions remain deletions. No commit,
push or pull request was made.

Final hash/status comparison: all 82 initial path records match, including the
two existing deletions; the original tracked diff statistic is unchanged. The
initial file hashes and missing-path markers are retained in the evidence
directory so preservation can be verified independently of a diff against HEAD.
Only the new audit artifacts are additional untracked work. The final comparison
is saved in `preservation-result.json`, alongside the final status and diff
statistic. All 40 unique roadmap IDs, the standalone/embedded diagram match,
16 prompt fixtures, referenced internal paths and Python helper syntax were
checked; the temporary Go helper was formatted with gofumpt.

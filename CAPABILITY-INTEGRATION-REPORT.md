# Agent capability integration report

Date: 2026-10-16. Scope: continue the unfinished capability work in the current
working tree. No commit, push, tag or release. Installed binaries and user
processes were not changed. Existing support/Funnel changes were preserved.

## Completion follow-up (2026-10-14 to 2026-10-16)

The original 25-section prompt was recovered from the local conversation record
and used for the implementation audit. The earlier integration reused existing
engines but left reusable procedure synthesis, patch-only branch isolation,
guide unloading and actual delivery selection incomplete. Those implementation
paths have now been added; the integration map documents all 25 runtime paths.
This is a scope-completion statement, not a guarantee of perfect model behavior.

- `workflow learn extract/list/inspect/export/forget` creates project-scoped
  procedure candidates from observed successful operations and current checks.
  Conditions, versions and validity limits are mandatory. Original sources and
  evidence are rechecked on inspection/export. Empty steps are synthesized from
  tool names; arguments/outputs are never copied automatically. Export invokes
  existing `skill_manage`, preserving its hooks, permissions and pause metadata.
- `workflow branch fork/propose/status/merge` provides patch-only isolation
  without creating a checkout. Snapshots and notes are separate per alternative.
  Literal source locations and symlinks are validated before reading; reads and
  writes invoke existing view/edit. All bases are checked before integration,
  and edit rechecks an exact byte hash after approval. Partial/unknown integration
  is journaled and never automatically replayed. Binary/new-file execution still
  uses the existing permitted workspace mechanism rather than a pretend sandbox.
- `workflow expertise list/load/unload` loads enabled skill guidance on demand.
  Declared exclusive groups/conflicts must be resolved. Released guide bodies are
  removed from later model requests and summary input; original audit messages
  remain unchanged. Project/user instructions and permissions remain authoritative.
- Evaluation manifests can enable `select_delivery` with explicit bounded
  `delivery_files`. A separate actual model invocation selects/combines candidate
  artifacts in a fresh workspace, then independent checks score that delivered
  result. The summary separates measured delivery success/selection losses from
  the older retrospective prediction. External checker results are not given to
  the selecting model; missing measurements stay absent rather than becoming zero.
- Adapter invocation accounting includes outer retries/failovers and is exposed
  separately from model-response counts. Its declared scope excludes title/compact
  helpers and HTTP retries internal to an adapter; it is not advertised as a wire
  traffic or billing counter. Capability output now includes structured-output scope.

New regression tests exercise source invalidation, isolated proposals, changed
bases, approval-time edits, partial integration and replay rejection, permission
metadata preservation, guide conflicts/unloading, actual delivery scoring and
independent checker execution, plus invocation counting and JSON output.

The first follow-up full test run found a CLI Bridge identity-redaction defect:
a short registered credential could incidentally occur inside a real SHA-256
digest and corrupt the stored executable/profile identity. Native digest fields
now retain their structural identity under partial matches; exact whole-field
credentials remain redacted. Arbitrary output objects and stdout remain protected.
A deterministic collision regression covers both partial and exact matches and
the affected Bridge checks pass. The final follow-up verification and binary
details are recorded below; earlier measurements remain historical.

On 2026-10-16, a full test invocation encountered missing `vet.cfg` files in the
Windows temporary build tree. Only that task-owned Go test process and its child
were stopped; the suite was restarted with `GOTMPDIR` under the ignored dev
directory on D:. This was an environment/build-tree failure, not a passed suite.
That first D: location was inside the repository, which made outside-repository
Git fixtures discover the parent Atlas checkout. The task-owned run was stopped
and the final run uses `D:\AtlasCapabilityTemp`, outside any project checkout.
The assertions were retained; these failed environment runs are not counted as
successful verification.
The next complete run passed 140 tested packages and all 84 packages without
tests, but evaluation failed one existing assertion: the explanatory text no
longer explicitly said that predicted selection was "not an observed" selection.
That distinction was restored in the implementation, preserving the assertion.

### Final follow-up verification

- The completed repository-wide run is `dist/dev/capability-complete-oct16-final-tests.log`:
  140 tested packages passed, 84 packages had no tests, evaluation failed the
  explanatory-text assertion described above. No other package failed.
- After restoring that explicit distinction, the entire evaluation package passed
  with `-count=1` (`capability-complete-oct16-eval-fixed.log`, 3.350 seconds).
  Thus every tested package passed across that complete run and the focused fix
  verification; this is **not** a claim of one all-green full invocation after
  the final text-only edit.
- A redundant repeat full invocation was stopped by matching its task-owned Go
  driver to the exact delivery log command. It is not reported as a completed
  or successful suite. No user process was terminated.
- Final-source repository-wide golangci-lint: 0 issues. `go vet ./...` completed
  successfully with empty output. Changed Go files are formatted, log-capitalization
  and `git diff --check` are clean (Git emits benign existing CRLF warnings).
- The final follow-up binary was rebuilt after the explanatory-text correction;
  its version, SHA-256 and size appear in Usage below. `--version`/`--help` pass.
- Race testing and new real-provider/physical desktop acceptance were not run in
  this follow-up. Earlier live measurements are historical, not measurements of
  the new method/branch/expertise/delivery additions.

## Earlier takeover outcome and limits

Provider reasoning continuity, working memory, method-driven discovery, pipeline
data processing and evaluation accounting are connected to the real agent loop.
The 25-item integration map is in `docs/agent-capabilities.md`; it explicitly
identifies existing mechanisms reused and capabilities that were not newly
created. This does not demonstrate a model-capacity percentage or zero defects.

Real Atlas model execution was attempted on four provider paths. ChatGPT passed
a small independent coding acceptance task three times, including the final
binary. Claude was rate-limited,
MiniMax lacked balance and the NVIDIA endpoint was unavailable for the account.
Those three runs provide infrastructure-error evidence, not model-quality scores.
There is no valid pre-Claude baseline comparison: the reported scratchpad
baseline executable was not located in the project dist tree. No speed increase
or broad quality increase is claimed.

## Changes added during takeover

- Refresh working memory after tools and queued corrections, rather than
  repeatedly sending the turn's initial projection.
- Keep a completed contract when the user only says to continue; reset working
  sets on a subsequent new request. Long requests retain up to 24 KiB and an
  original fingerprint/truncation marker instead of silently retaining 2 KiB.
- Preserve critical user constraints above the soft projection budget, with a
  pointer to the complete contract for consequential decisions.
- Bind user-origin work items to existing shared completion accounting. Stale
  checks remain open; declared blocking questions remain visible. Internal
  continuation is bounded and cannot become a user request. A stopped gate's
  conclusion is recorded for JSON consumers as well as the TUI.
- Retain the fallback model's own reasoning while excluding foreign blocks.
  Adapt every transport attempt, including a provider switch within a retry,
  rather than relying only on PrepareStep. Use fallback-native options and its
  configured/default output budget; respect its image support, including images
  already converted into user attachments. Keep semantic captions intact.
- Resolve effort reporting from the parsed adapter options; explicitly state
  that catalog/configuration capability reports are not live upstream probes.
  Capability notices are separated by session.
- Expose existing context, code graph, design, experiment, candidate and
  verification tools when a method or investigation phase is chosen. Broad
  profiles alone retain normal discovery behavior. Authorization is intersected
  with the live palette; no tool executes as a result of discovery.
- Add description/schema detail lookup to tool_search. Definitions are copied
  before normalization and scope differences are stated.
- Clear a selected candidate when its evaluation suite changes. Prevent checks
  from different source generations proving one candidate. Validate workspace
  ownership and avoid reporting a task ID alone as an isolated workspace.
- Distinguish structured failure codes, including client metadata, rather than
  collapsing quoted JSON error codes. Separate failures of different tools and
  exempt legitimate polling. Successful observations reset a stalled episode.
- Report overflow in pipeline transform sources instead of silently dropping
  iterations. Refuse truncated transformed arguments. Preserve JSON arrays and
  integer identity, compare bounded numeric sorts exactly, and keep numeric and
  string values distinct during deduplication and bindings.
- Correct evaluation attempt numbering, duplicate detection and provider-failed
  first-attempt handling. Retrospective selection is labeled predicted, not an
  observed selection. Normal end-turn is labeled a completion-claim proxy.
  Provider failure cannot count as task success; stale/absent work contracts
  cannot establish runtime verification. Payment and account-endpoint errors
  are classified separately from model behavior.
- Fix formatting/lint and Work-panel translation suffix handling.
- Isolate simulated desktop controllers: independent fake desktops no longer
  queue behind one global physical-desktop lease. Production tools continue to
  use the shared controller. A regression test holds one simulated lease while
  another backend enumerates windows, without relaxing the timeout or assertion.

## Live acceptance

Fixture: allocator.py must split a nonnegative integer into positive integer
count portions, preserve the sum, place larger portions first and reject invalid
values including booleans. acceptance.py is fixed outside the model's changes.
Each provider receives its own folder and state directory. The actual Atlas exe
and model loop execute the assignment; this is not a scripted tool driver.

| Provider/model | Independent checker | Wall time | Model responses | Tool calls | Tool errors |
|---|---|---:|---:|---:|---:|
| chatgpt/gpt-6.1-sol, first run | Passed | 41.9 s | 7 | 9 | 0 |
| chatgpt/gpt-6.1-sol, repeat | Passed | 58.9 s | 11 | 10 | 0 |
| chatgpt/gpt-6.1-sol, final binary | Passed | 56.9 s | 9 | 13 | 0 |
| claude/claude-haiku-5-5 | No quality result: 429 | 38.7 s | unavailable | unavailable | unavailable |
| minimax/MiniMax-M2.7 | No quality result: 402 balance | 3.1 s | unavailable | unavailable | unavailable |
| nvidia-nim/moonshotai/kimi-k2.6 | No quality result: account endpoint 404 | 2.2 s | unavailable | unavailable | unavailable |

The runs are not a controlled before/after comparison. The repeat and final run's folder had
tests created during the first run. Model-response counts exclude transport
retries; printed prompt-token counts are session context accounting, not a
complete invoice or cumulative provider request measurement.

Artifacts: `dist/dev/capability-acceptance/`. Raw stderr remains local and can
contain account identifiers; it is not reproduced here. Real browser/desktop,
visual design, research, long workflows and live multi-candidate selection were
not evaluated through real models in this task.

## Verification

New regression tests cover memory refresh, continuation provenance, completion
gating, constraints, current evidence, candidate generation isolation, provider
switches, capability reporting, pipeline data integrity and tool-help scope.
Existing fixture and browser tests exercise additional paths.

An early full run used sources still being edited and failed i18n coverage; the
final catalog was retested successfully. A subsequent full run exposed shared
simulator lease contention; the isolated scenario passed five repeats, then the
fixture-controller fix passed long-workflow and isolation checks. Assertions were
not weakened. Obsolete test-driver processes created by this task were stopped
after failures; no user processes were terminated.

The final binary also passed the real ChatGPT acceptance task. This validates
that particular code task, not every capability or other providers' behavior.
`go test -p 1 ./...` completed with exit code 0 on the final source, after the
completion-gate changes. An earlier full JSON run also completed all 225
packages with no failures and recorded 71 environment-dependent skipped tests;
those skips are not counted as live verification.
Repo-wide golangci-lint reports 0 issues; go vet, formatting of changed Go
files, git diff --check and log-capitalization checks are clean.
Race testing requires a CGO compiler; gcc is not available on this machine.

## Earlier development binary (superseded by the follow-up build)

- Path: `D:\Atlas\dist\dev\atlas-agent-capability-dev.exe`.
- Version: `v0.16.4-agent-capability-dev`.
- Size: 159,926,784 bytes.
- SHA-256: `20603F3364FD9EEF3A5FAECA5F3A52BCAA22B2F61B3FA4B403899541F785137F`.
- Build: `CGO_ENABLED=0`, `GOEXPERIMENT=greenteagc`, module-qualified version flag.
- Installed application unchanged. No commit, push, tag or release performed.
- This binary includes other pre-existing working-tree changes, including support
  integration; those are not claimed as new capability work in this report.

## Usage

Current follow-up development build (2026-10-16):

- Path: `D:\Atlas\dist\dev\atlas-agent-capability-complete-dev.exe`.
- Version: `v0.16.4-agent-capability-complete-dev`.
- Size: 160,084,480 bytes.
- SHA-256: `96CA45ADD2339B9F1F6C6D085C5D8CAE68ED0BC899D7EAE8624236F9FBE11318`.
- `--version` and `--help` pass. Installed executable unchanged.

```powershell
& "D:\Atlas\dist\dev\atlas-agent-capability-complete-dev.exe"
```

The earlier development build remains available at its separate path:

```powershell
& "D:\Atlas\dist\dev\atlas-agent-capability-dev.exe"
```

Use `/work` for recorded work. For a configured model's local capability report:

```powershell
& "D:\Atlas\dist\dev\atlas-agent-capability-dev.exe" models capabilities --json
```

See the integration map for work-budget settings and tool-detail discovery.

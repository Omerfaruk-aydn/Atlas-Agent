# Atlas Yürütme ve Toparlanma Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Gerçek komut izolasyonu, ortam hazırlama, doğrulanmış resume ve sınırlı onarım döngüsü.

**Architecture:** Yeni execution runner mevcut invoke/izin/bütçenin altında
çalışır. Checkpoint ve repair kayıtları A0 artifact deposunda tutulur; coordinator
çalıştırma, inceleme ve tamamlamanın tek otoritesi olarak kalır.

**Tech Stack:** Go 1.26.6, stdlib os/exec, mevcut shell/hooks, yapılandırılmış OCI runtime.

**Spec:** `docs/superpowers/specs/2026-10-02-agent-platform-design.md`, B1-B4.

## Global Constraints

- Go 1.26.6; `CGO_ENABLED=0`, `GOEXPERIMENT=greenteagc`.
- A0 `ArtifactRef`, `Record`, PutRecord/ReadRecord/ListRecords sözleşmeleri tüketilir.
- Container-required: network none, CPU 2, memory 2 GiB, process 128, timeout 10 dakika.
- Image digest zorunlu; otomatik install/pull ve sessiz host fallback yoktur.
- Repair en çok 3 deneme, deneme başına 2 teşhis kontrolü/1 uzman çağrısı.
- State 1 MiB, artifact 32 MiB, koleksiyon başına 128 referans.
- İzin/bütçe ve önceki shell/Windows Job Object davranışı korunur.
- Ana planın sequential test, baseline ve scoped commit kuralları uygulanır.

## Review Focus

- Shell builtin veya hook'un container-required yolundan host'a kaçması.
- Container ID kaydından önce çökme ve ilgisiz container'ın öldürülmesi.
- Yeniden kullanılan PID'nin eski background job sanılması.
- Lockfile değiştiğinde eski kurulum planının çalıştırılması.
- Farklı call ID ile failure circuit/repair bütçesinin aşılması.

### Task B1: İzolasyon runner'ı ve tüm komut giriş noktaları

**Files:** Create `internal/execution/types.go`, `oci.go`, `registry.go`,
`oci_test.go`, `oci_integration_test.go`, `internal/agent/tools/execution.go`.
Modify `internal/config/config.go`, `schema.json`, `internal/config/store.go`,
`internal/agent/tools/bash.go`, `job_output.go`, `job_kill.go`,
`internal/shell/run.go`, `internal/shell/shell.go`, `internal/hooks/runner.go`,
`internal/agent/coordinator.go`; tests `internal/config/execution_test.go`,
`internal/agent/tools/execution_test.go`, `internal/hooks/execution_test.go`.

**Interfaces:** execution
`ExecutionPolicy{Mode, RuntimePath, Image, Network string; ReadOnly bool;
CPUs float64; MemoryBytes int64; MaxProcesses int; TimeoutMS int64;
EnvironmentKeys []string}`;
`Request{RunID, Root, TaskID string; Argv, Env []string; Policy ExecutionPolicy}`;
`Result{RunID, Backend, ContainerID, HostOS, ExecutionOS, Status string;
ExitCode *int; OutputHash, ErrorHash string; Done bool;
OutputRef, ErrorRef engineering.ArtifactRef}`;
`RunHandle{RunID, Backend, ContainerID string}`;
`Runner` interface `Run(ctx context.Context, req Request) (Result, error)`,
`Start(ctx context.Context, req Request) (RunHandle, error)`,
`Observe(ctx context.Context, id string) (Result, error)`,
`Cancel(ctx context.Context, id string) error`;
`NewRunner(ctx context.Context, policy ExecutionPolicy, store *engineering.Store) (Runner, error)`;
`ValidatePolicy(ctx context.Context, p ExecutionPolicy) error`.
New config `Options.Execution *Execution` mirrors policy snake_case JSON fields;
`config.ExecutionPolicy` conversion defined in tools adapter (config does not
import execution). Existing Sandbox config stays compatible.

- [x] **Step 1:** `TestOCIRequiredNeverFallsBack` unavailable runtime/image or
  unsupported policy => error, native mock spawn count 0. TestOCIArgumentsAndEnvironment digest enforced, argv separate, no host home/socket mount,
  env secret absent, ro/network/resources present. `TestOCICancelIdentityRace`
  persist before start or recover by run label; cancel touches only owned ID.
  `TestExecutionHooksAndBuiltins` builtin write/hook reach container path,
  permission denied produces zero spawn; legacy path unchanged.
- [x] **Step 2:** `go test -count=1 -p 1 -ldflags='-w -s' -timeout 3m ./internal/execution ./internal/agent/tools ./internal/hooks ./internal/config -run 'TestOCI|TestExecution'` run; FAIL
  from missing target capability, not an unrelated dependency error.
- [x] **Step 3:** OCI preflight inspect local image (no pull), strict policy and
  capability checks; explicit argv os/exec (no generated shell CLI). Validate
  full mount root/output containment, container exit observations and cleanup
  label. A0 records store run identity/result; output streamed to private bounded
  artifact files, no credential env dump. PutRecord linked refs'e OutputRef ve
  ErrorRef eklenir; GC gözlemlenmiş çıktıyı silemez. No unsupported domain allow-list.
- [x] **Step 4:** Route whole bash script and hooks inside container with image
  shell, not individual host external commands; original permissions/hooks stay
  before invocation. Background observe/cancel use runner identity; verify/test/
  lint reach this same bash adapter. Exact source/run budget checks remain.
- [x] **Step 5:** Config merge/schema and prompt capability descriptions include
  legacy/container-required and actual execution OS. LSP/MCP stay explicitly out
  of this command isolation boundary. Cancellation applies min(policy,budget).
  `task schema` mevcut JSON schema üretimini çalıştırır; yalnızca bu görevin
  option değişiklikleriyle ilgili generated diff tutulur.
- [x] **Step 6:** All four package tests pass. Add `TestOCIRealIsolation` behind
  explicit env fixture runtime+digest: external host file unavailable, ro write
  fails, network none denies connect, cancelled run leaves no owned child
  container; unrelated container remains. No runtime => skip+unavailable record.
- [x] **Step 7:** Exact scoped diff: `feat: enforce isolated command execution policies`.

Test assertions: `require.Error(t, unavailableErr)`,
`require.Zero(t, nativeSpawnCount)`, `require.False(t, result.Done)`,
`require.NotContains(t, forwardedEnv, "SECRET=value")`.

### Task B2: Ortam inspect/plan/apply/verify

**Files:** Create `internal/environment/types.go`, `inspect.go`, `plan.go`,
`environment_test.go`, `internal/agent/environment_workflow.go`.
Modify `internal/agent/workflow_tool.go`, `internal/cmd/workflow.go`;
tests `internal/agent/environment_workflow_test.go`, `internal/cmd/workflow_test.go`.

**Interfaces:** environment
`ToolRequirement{Name, Constraint, Manifest string}`;
`Command{Argv []string; Directory string; NeedsNetwork bool}`;
`EnvironmentPlan{Root, SourceFingerprint string; Requirements []ToolRequirement;
Sources []engineering.SourceReference; Commands []Command; Conflicts []string}`;
`EnvironmentReport{PlanHash string; ObservedVersions map[string]string;
Passed bool; Gaps []string}`;
`Inspect(ctx context.Context, root string) (EnvironmentPlan, error)`;
`ValidatePlan(ctx context.Context, plan EnvironmentPlan) error`;
agent `environmentWorkflow(ctx context.Context, p WorkflowParams, call fantasy.ToolCall, invoke tools.ToolInvoker) (fantasy.ToolResponse, error)`.
WorkflowParams new `EnvironmentAction string`, `EnvironmentPlan *environment.EnvironmentPlan`;
version probes/checks run through invoke, not environment package os/exec.

- [x] **Step 1:** `TestEnvironmentInspectNoExecution`: Go/Node/Python/Rust
  fixture read-only, spawn count 0. `TestEnvironmentLockConflict` npm/yarn
  simultaneous => Conflicts nonempty/no install selected.
  `TestEnvironmentApplyStaleOrDenied` changed lock or denied permission => no invocation. Version
  output missing => unavailable, unsuccessful install => Passed false.
- [x] **Step 2:** `go test -count=1 -p 1 -ldflags='-w -s' -timeout 3m ./internal/environment ./internal/agent ./internal/cmd -run TestEnvironment` FAIL on missing behavior.
- [x] **Step 3:** Bounded manifest inspection, local-only argv plans, explicit
  Python virtualenv and project cwd, no global install. Lock hash and tool
  constraints capture; network needs visible; conflict blocks apply.
- [x] **Step 4:** Workflow actions and no-provider CLI inspect/plan bind;
  apply source-check+permission+invoke+budget, verify observes new probe results.
  Persist plan/report via A0; cancelled apply requires fresh inspect.
- [x] **Step 5:** Target packages pass; literal metacharacters remain argv data,
  non-project cwd rejected; actual smoke inspect paid calls 0/mutation 0.
- [x] **Step 6:** Exact scoped diff: `feat: add source-verified environment preparation`.

Test assertions: `require.Zero(t, inspectSpawnCount)`,
`require.NotEmpty(t, conflictedPlan.Conflicts)`,
`require.Error(t, stalePlanErr)`, `require.False(t, interruptedReport.Passed)`.

### Task B3: Checkpoint ve restart sonrası resume uzlaştırması

**Files:** Create `internal/engineering/checkpoint.go`, `checkpoint_test.go`,
`internal/agent/resume_workflow.go`, `resume_workflow_test.go`.
Modify `internal/agent/delivery_stage.go`, `workflow_tool.go`,
`internal/cmd/workflow.go`, `internal/engineering/runtime.go`.

**Interfaces:** engineering
`Checkpoint{ID, Root, SessionID, MessageID, PlanFingerprint, SourceFingerprint string;
Stage int; TaskFingerprints map[string]string; Workspaces, Operations []string}`;
`ResumePlan{CheckpointID, Root, PlanFingerprint, SourceFingerprint string;
StateRevision uint64; ReadyTasks, ReverifyTasks, AmbiguousOperations []string}`;
`SaveCheckpoint(ctx context.Context, checkpoint Checkpoint) (Record, error)`;
`ResumeInput{Checkpoint Checkpoint;
State State; Source string; TaskSpecs, TaskStates map[string]string}`:
`PlanResume(ctx context.Context, input ResumeInput) (ResumePlan, error)`.
Engineering session paketini import etmez; task spec/status verilerini
coordinator bu map'lere dönüştürür.
Agent `resumeWorkflow(ctx context.Context, p WorkflowParams, call fantasy.ToolCall) (fantasy.ToolResponse, error)`.
WorkflowParams `CheckpointID string`, `ResumePlan *engineering.ResumePlan`.

- [x] **Step 1:** `TestCheckpointRestartAndSourceChange` persisted store reload
  retains checkpoint; changed source lists reverify and cannot advertise pass.
  `TestResumeUnknownEffectsAndPIDReuse` ambiguous operation remains unresolved
  and execute count 0; changed process creation identity/PID => ambiguous.
  `TestResumeRevisionConflict` stale plan apply => error, old state unchanged.
- [x] **Step 2:** `go test -count=1 -p 1 -ldflags='-w -s' -timeout 3m ./internal/engineering ./internal/agent ./internal/cmd -run 'TestCheckpoint|TestResume'` FAIL from missing behavior.
- [x] **Step 3:** Checkpoint A0 storage, auto post-stage and explicit creation;
  pure reconciliation over supplied spec/status/source, no file restores/replay.
  B1 container identity replaces PID-only inference; host unknown persists.
- [x] **Step 4:** CLI checkpoint/resume-plan inspect without LLM; resume apply
  compare revision/hash then pending task scheduling in coordinator. It is not
  rewind; ambiguous mutating operations must use existing explicit recover first.
- [x] **Step 5:** All target packages pass; actual temp fixture restart smoke
  no mutation by plan, no double-run of successful operation.
- [x] **Step 6:** Exact scoped diff: `feat: reconcile execution checkpoints before resume`.

Test assertions: `require.Equal(t, checkpoint.ID, reloaded.ID)`,
`require.NotEmpty(t, plan.AmbiguousOperations)`, `require.Zero(t, replayCount)`,
`require.Error(t, staleRevisionErr)`.

### Task B4: Kanıta bağlı sınırlı onarım döngüsü

**Files:** Create `internal/engineering/repair.go`, `repair_test.go`,
`internal/agent/repair_workflow.go`, `repair_workflow_test.go`.
Modify `internal/agent/workflow_tool.go`, `runtime_guard.go`,
`internal/agent/templates/agent_contract.md.tpl`.

**Interfaces:** engineering
`RepairAttempt{ID, Hypothesis, EvidenceHash, SourceFingerprint string;
DiagnosticRunIDs []string; ImplementationRunID, VerificationRunID string}`;
`RepairCase{ID, TaskID, TaskFingerprint, FailedOperationID, Status string;
Attempts []RepairAttempt}`;
`ValidateRepair(ctx context.Context, state State, repair RepairCase) error`.
Agent `repairWorkflow(ctx context.Context, p WorkflowParams, call fantasy.ToolCall, invoke tools.ToolInvoker) (fantasy.ToolResponse, error)`.
WorkflowParams `Repair *engineering.RepairCase`; TaskID+OperationID existing.
Case ID runtime-managed; attempt key based on failed operation/spec+evidence hash.

- [x] **Step 1:** `TestRepairBoundsAndEvidence` 4th attempt/3rd diagnostic/2nd
  implementation blocked; same evidence different call ID blocked.
  `TestRepairObservedSuccessOnly` invented operation/check or source mutated => unresolved.
  `TestRepairCancellationAndDenied` cancel history retained, permission/unavailable
  => blocked rather than fabricated code diagnosis; budget stops next call.
- [x] **Step 2:** `go test -count=1 -p 1 -ldflags='-w -s' -timeout 3m ./internal/engineering ./internal/agent -run TestRepair` FAIL on missing controls.
- [x] **Step 3:** Persist case before execution, guarded transitions, pure
  validation 3/2/1; actual failed journal linkage. Diagnostics verify, implement
  debug specialist, verify and required independent review all through invoke.
- [x] **Step 4:** Wire workflow repair with truthful capability prompt; already
  running/pending repair can't recursively create new root repair case. B3
  checkpoint resume retains attempt counts and rejects ambiguous replay.
- [x] **Step 5:** Target tests pass; complete package regression verifies
  existing repeated-failure circuit and budget behavior unchanged.
- [x] **Step 6:** Exact scoped diff: `feat: orchestrate bounded evidence-based repairs`.

Test assertions: `require.Error(t, fourthAttemptErr)`,
`require.LessOrEqual(t, implementationCallsPerAttempt, 1)`,
`require.LessOrEqual(t, diagnosticCallsPerAttempt, 2)`,
`require.NotEqual(t, "resolved", forgedCase.Status)`.

## Alt proje çıkış kontrolü

- [x] B1-B4 formatter/diff checks; documented backend availability and OS.
- [x] Real OCI outcome ayrı; mock invoker testleri OS izolasyonu kanıtı değil.
- [x] Checkpoint/repair/environment API'leri C ve D planındaki tüketicilerle aynı.

Execution closure: see docs/agent-platform-progress.md and docs/agent-platform-verification.json. Checkboxes track closure under recorded rulings; unavailable platform acceptance is not a passing runtime test. E1 aggregate RED was waived because its initial failure was a test assertion, not missing runtime behavior. The graph smoke uses the production tool fixture rather than inventing a CLI command.

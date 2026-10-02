# Atlas Uzman Koordinasyonu Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Sürümlü uzman sözleşmeleri, bulgu-onarım takibi ve çalıştırılabilir proje tarifleri.

**Architecture:** Kayıtlar A0 artifact deposunda, görevler mevcut todos/session
servisinde kalır. Coordinator yeni sözleşme ve bulgu kapılarını mevcut dispatch,
review ve stage advancement içine uygular; recipes bu akışı kullanır.

**Tech Stack:** Go 1.26.6, mevcut engineering/session, strict JSON, agent roles.

**Spec:** `docs/superpowers/specs/2026-10-02-agent-platform-design.md`, C1-C3.

## Global Constraints

- Go 1.26.6; `CGO_ENABLED=0`, `GOEXPERIMENT=greenteagc`.
- State 1 MiB; artifact 32 MiB; koleksiyon başına 128 referans.
- Recipe version 1; en çok 64 step; cycle/duplicate/unknown param/role reddedilir.
- Task/spec/source revision, izin ve budget korunur; recipes yetki vermez.
- Eski handoff/Markdown komutları uyumlu kalır; publish/benchmark eklenmez.
- A0 record API, A2 ContextPacket ve B runner/checkpoint/repair kullanılır.
- Ana planın sequential test, baseline ve scoped commit kuralları uygulanır.

## Review Focus

- Consumer'ın eski contract revizyonunu stage pass için kullanması.
- Eşzamanlı review aynı bulgudan iki onarım görevi oluşturması.
- fixed/waived durumunun verified olarak gösterilmesi.
- Recipe parametresinin shell metni olarak değerlendirilmesi.
- Aktif delivery planının slash komutuyla sessiz üzerine yazılması.

### Task C1: Sözleşme revizyonları ve consumer kapıları

**Files:** Create `internal/engineering/contracts.go`, `contracts_test.go`,
`internal/agent/contract_workflow.go`, `contract_workflow_test.go`.
Modify `internal/agent/workflow_tool.go`, `task_context.go`, `delivery_stage.go`,
`internal/session/quality_gate.go`, `internal/engineering/delivery.go`.

**Interfaces:** engineering
`ContractCheck{Name, Tool, InputJSON string}`;
`ContractRevision{ID, Root, OwnerTaskID, Description, Invariants string;
Revision uint64; ConsumerTaskIDs []string; Sources []SourceReference;
Checks []ContractCheck}`;
`SaveContract(ctx context.Context, contract ContractRevision, expected uint64) (Record, error)`;
`ContractsForTask(ctx context.Context, root, taskID string) ([]ContractRevision, error)`;
`ValidateContractRefs(ctx context.Context, root, taskID string, refs []Record) error`.
DeliveryPlan new `TaskContractRefs map[string][]Record` assigned by runtime;
caller forged accepted revisions never authoritative. Agent
`contractWorkflow(ctx context.Context, p WorkflowParams, call fantasy.ToolCall, invoke tools.ToolInvoker) (fantasy.ToolResponse, error)`.
WorkflowParams `Contract *engineering.ContractRevision`, `ContractAction string`.

- [x] **Step 1:** `TestContractCASAndConsumers`: rev1 two consumers, rev2
  expected1 update; old refs invalid both; expected1 repeated update => error.
  `TestContractStageRequiresCurrentCheck` old run/source cannot pass, real new
  checks permit. `TestContractSourcesAndDispatch` outside path rejected;
  dispatched ContextPacket contains actual accepted revision; caller claim ignored.
- [x] **Step 2:** `go test -count=1 -p 1 -ldflags='-w -s' -timeout 3m ./internal/engineering ./internal/agent ./internal/session -run TestContract` FAIL missing behavior.
- [x] **Step 3:** Pure validated record model, CaptureSources at registration,
  CAS update; duplicate consumers/unknown task IDs rejected in coordinator.
  Bind task refs at plan registration; revisions trigger reverify requirement
  without deleting historical completion. Source changes invalidate current refs.
- [x] **Step 4:** Workflow register/query/revise/check + A2 retrieval;
  shared session completion and stage gate validate actual accepted refs.
  Checks through invoke+verify with new task/run journal observations.
- [x] **Step 5:** All target package tests pass; concurrent revise test one
  success/one conflict, no lost update.
- [x] **Step 6:** Scoped commit `feat: version expert contracts and invalidate stale consumers`.

Test assertions: `require.Error(t, oldConsumerRefErr)`,
`require.Equal(t, uint64(2), revision.Revision)`,
`require.Error(t, simultaneousRevisionConflict)`, `require.False(t, oldStage.Passed)`.

### Task C2: Bulgu kayıtları, onarım görevleri ve bağımsız yeniden kontrol

**Files:** Create `internal/engineering/findings.go`, `findings_test.go`,
`internal/agent/remediation_workflow.go`, `remediation_workflow_test.go`.
Modify `internal/subagents/handoff.go`, `internal/agent/role_quality.go`,
`workflow_tool.go`, `delivery_stage.go`, `internal/session/quality_gate.go`,
`internal/subagents/contracts_test.go`; add `internal/subagents/handoff_findings_test.go`.

**Interfaces:** subagents optional
`HandoffFinding{Path, Issue, Expected, Evidence string; StartLine, EndLine, Severity int}`;
Handoff adds `Findings []HandoffFinding` (omitempty, max32).
Engineering `Finding{ID, Root, TaskID, TaskFingerprint, ReviewerExecutionID,
ImplementerExecutionID, SourceFingerprint, Status, Path, Issue, Expected,
Evidence, RemediationTaskID, WaiverReason string; StartLine, EndLine, Severity int;
Checks []ContractCheck; VerificationRunIDs []string}`;
`SaveFinding(ctx context.Context, namespace string, finding Finding, expected uint64) (Record, error)`;
`TaskFindings(ctx context.Context, namespace, taskID string) ([]Finding, error)`;
`FindingBlocks(ctx context.Context, finding Finding, currentSource string) bool`.
Agent `remediationWorkflow(ctx context.Context, p WorkflowParams, call fantasy.ToolCall, invoke tools.ToolInvoker) (fantasy.ToolResponse, error)`;
WorkflowParams `FindingIDs []string`, `FindingAction string`, existing TaskID/Evidence.

- [x] **Step 1:** `TestFindingsIdempotentTasks` same issue/spec twice or concurrent
  reviewers => one stable finding/repair task, overlapping files serial dependencies.
  `TestFindingsIndependentVerification` fixed self-report and reused checks
  cannot set verified; separate reviewer+fresh source+new journal checks can.
  `TestFindingsWaiverAndStale` only explicit authorized user waiver, reason
  required, waived != verified; changed source => needs reinspection, not closed.
  `TestHandoffFindingsCompatibility` old JSON valid/new bad ranges rejected.
- [x] **Step 2:** `go test -count=1 -p 1 -ldflags='-w -s' -timeout 3m ./internal/engineering ./internal/subagents ./internal/agent ./internal/session -run 'TestFindings|TestHandoffFindings'` FAIL.
- [x] **Step 3:** Runtime IDs from task/spec+normalized defect signature, CAS
  transitions; handoff reports remain untrusted. Findings imported on review,
  blocking severity 0-2 prevents pass; severity3 informational remains visible.
  If reviewer reports changes_required without structured findings, quality
  still blocked and requests structured findings; no invented defect locations.
- [x] **Step 4:** Remediate creates todo IDs derived from finding IDs, atomic
  session mutation and optimistic revision check. Current plan revised with
  requirement mapping before new repair tasks dispatch; completed stages are
  historical, not silently reused. B4 repair can handle failed checks within
  remediation task, without bypassing required independent review.
- [x] **Step 5:** Verify current findings in existing shared completion gate
  and advance. Different reviewer execution ID and matching new checks required;
  human waiver exposed only by explicit CLI/user control request with trusted
  provenance, never agent-callable finding action. Model text or automatic
  permission grant does not authorize waiver. Expose status through workflow trace.
- [x] **Step 6:** Target full packages pass; cancellation between finding/save
  and todo creation reconciles by deterministic remediation ID, no duplicate.
- [x] **Step 7:** Scoped commit `feat: track review findings through independent remediation`.

Test assertions: `require.Len(t, repairTasks, 1)`,
`require.NotEqual(t, "verified", selfReportedFinding.Status)`,
`require.NotEqual(t, "verified", waivedFinding.Status)`,
`require.Equal(t, "verified", independentlyCheckedFinding.Status)`.

### Task C3: JSON tarifler, CLI ve slash komutları

**Files:** Create `internal/workflows/types.go`, `load.go`, `compile.go`,
`recipes_test.go`, `builtin/feature-delivery.json`, `builtin/migration-review.json`,
`builtin/release-check.json`, `internal/agent/recipe_workflow.go`,
`recipe_workflow_test.go`, `internal/cmd/workflow_recipes.go`,
`workflow_recipes_test.go`; modify `internal/commands/commands.go`,
`internal/config/config.go`, `schema.json`, `internal/agent/workflow_tool.go`,
`internal/ui/model/ui.go` and `internal/ui/model/slash_completion_test.go`.

**Interfaces:** workflows
`Parameter{Name, Type string; Required bool; Default json.RawMessage}`;
`Step{ID, Role, Prompt string; DependsOn, OwnedPaths, Criteria []string;
Checks []Check}`; `Check{Name, Directory string; Argv []string}`;
`Requirement{ID, Description string; StepIDs []string}`;
`Recipe{ID string; Version int; Parameters []Parameter; Steps []Step;
Requirements []Requirement}`;
`Compiled{RecipeHash, ParametersHash string; Tasks []session.Todo;
Plan engineering.DeliveryPlan; Checks map[string][]Check}`;
`RecipeRun{ID, SessionID, RecipeHash, ParametersHash, Status string}`;
`Load(ctx context.Context, paths []string) ([]Recipe, error)`;
`Compile(ctx context.Context, recipe Recipe, params map[string]json.RawMessage, roles []string) (Compiled, error)`;
`Validate(ctx context.Context, recipe Recipe, roles []string) error`.
Agent `recipeWorkflow(ctx context.Context, p WorkflowParams, call fantasy.ToolCall, invoke tools.ToolInvoker) (fantasy.ToolResponse, error)`;
WorkflowParams `RecipeID string`, `RecipeAction string`, `RecipeParams map[string]json.RawMessage`.
Config `Options.WorkflowPaths []string` (JSON workflow_paths).

- [x] **Step 1:** `TestRecipesStrictSchemaAndCycles` unknown fields/version,
  65th step, duplicate ID/cycle/missing requirement/role error.
  `TestRecipesLiteralArguments` parameter "$(...) ; ..." stays literal one argv value;
  no shell.Command built by string concatenation.
  `TestRecipesActivePlanAndResumeHash` nonempty plan not overwritten, edited recipe invalidates resume.
  `TestRecipesLegacyCommandsAndPalette` old Markdown entries same and exact
  `/workflow:<id>` listed once; name collision explicit.
- [x] **Step 2:** `go test -count=1 -p 1 -ldflags='-w -s' -timeout 3m ./internal/workflows ./internal/commands ./internal/cmd ./internal/agent ./internal/ui/model -run TestRecipes` FAIL.
- [x] **Step 3:** strict JSON bounded files (1 MiB), typed params with no
  embedded interpreter; validate graph/role/ownership. Compile requirements and
  ordered stages using existing task/delivery validators. Check argv routed
  through B1 runner via existing permitted verification tool (add structured
  argv adapter in `internal/agent/tools/verify.go` if needed, no shell expansion).
- [x] **Step 4:** CLI recipes/validate/run and agent actions; run --plan-only
  compiles without model call or session mutation and returns a preview.
  Normal run persists RecipeRun
  before tasks, revision-lock active-plan check; compile-to-save interruptions
  reconcile from run hash. B3 checkpoint binds recipe/parameter hash for resume.
- [x] **Step 5:** Existing custom command palette adapter and slash handling;
  no IO in UI.Update, load via tea.Cmd. Builtins only explicit role/check use;
  release-check has no publish or credential mutation step. Prompt follows
  declared phases but runtime performs gates. `task schema` workflow_paths
  config alanının mevcut schema.json üretimini günceller.
- [x] **Step 6:** Target packages pass, CLI validate paid calls 0, literal
  argv injection fixture harmless. Disabled role and permission-denied run
  leave gates intact, no silent model/provider fallback.
- [x] **Step 7:** Scoped commit `feat: execute versioned project workflow recipes`.

Test assertions: `require.Error(t, sixtyFifthStepErr)`,
`require.Equal(t, "$(...) ; ...", compiled.Checks["check"][0].Argv[1])`,
`require.Error(t, activePlanOverwriteErr)`, `require.Zero(t, planOnlyProviderCalls)`.

## Alt proje çıkış kontrolü

- [x] C1-C3 tests+formatter+diff; old Handoff JSON fixture regression passes.
- [x] ContractRevision/Finding/RecipeRun artifact refs remain queryable after restart.
- [x] Task trace distinguishes declared success, actual checks and current review.

# Atlas Düzenleme ve Deneyim Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Önizlemeli anlamsal düzenleme, gerçek kullanıcı senaryoları ve CLI/server/TUI ortak kontrol paneli.

**Architecture:** LSP değişiklikleri kaynak/izin kontrolleriyle planlanır.
Browser/PTY senaryoları gerçek gözlemleri artifact olarak saklar. TUI mevcut
tek UI modeli içinde coordinator snapshot ve mutation API'lerini kullanır.

**Tech Stack:** Go 1.26.6, mevcut LSP/offset encoding, browser, Bubble Tea v2,
Windows ConPTY/x/sys, Unix PTY adapter.

**Spec:** `docs/superpowers/specs/2026-10-02-agent-platform-design.md`, D1-D3.

## Global Constraints

- Go 1.26.6; `CGO_ENABLED=0`, `GOEXPERIMENT=greenteagc`.
- Edit target ownership/source/permission ve mevcut quality gates korunur.
- Scenario version 1, max64 step/64 assertion, timeout10 dakika.
- Artifact32 MiB; mevcut ui_verify image16 MiB/32Mpixel sınırı ayrıca korunur.
- PTY kanıtı gerçek backend'den gelir; pipe/renderer testi PTY pass sayılmaz.
- Panel80x24/120x40; 40x12'de kapatma/durdurma erişilebilir kalır.
- `internal/ui/AGENTS.md` tamamı uygulama öncesi okunur; IO Update/draw'da yoktur.
- A-C sözleşmeleri ve ana planın test/baseline/scoped commit kuralları tüketilir.

## Review Focus

- UTF-16/CRLF aralıklarının farklı byte konumlarına uygulanması.
- Partial edit rollback'in sonraki kullanıcı değişikliğini ezmesi.
- Screenshot veya pipe logunun terminal interaction kanıtı sayılması.
- SSE reconnect/out-of-order olaylarla panelin eski duruma dönmesi.
- Stop düğmesine basılır basılmaz aktif görevin reassign edilmesi.

### Task D1: LSP edit planı ve güvenli apply journal

**Files:** Create `internal/lsp/util/edit_plan.go`, `edit_plan_test.go`,
`internal/agent/tools/lsp_edit_plan.go`, `lsp_edit_plan.md`, `lsp_edit_plan_test.go`.
Modify `internal/agent/tools/lsp_rename.go`, `lsp_replace_symbol.go`,
`lsp_rename_file.go`, `internal/agent/coordinator.go`, `runtime_guard.go`.

**Interfaces:** lsputil
`PlannedChange{Path, Destination, Operation, BeforeHash, AfterHash string;
Content []byte}`; `EditPlan{ID, Root string; Encoding powernap.OffsetEncoding;
Changes []PlannedChange}`; `EditResult{PlanID, Status string; Applied, Restored,
Conflicted []string}`;
`PrepareWorkspaceEdit(ctx context.Context, root string, edit protocol.WorkspaceEdit, encoding powernap.OffsetEncoding) (EditPlan, error)`;
`ValidateEditPlan(ctx context.Context, root string, plan EditPlan) error`;
`ApplyEditPlan(ctx context.Context, root, journalDir string, plan EditPlan) (EditResult, error)`;
`RecoverEditPlan(ctx context.Context, root, journalDir, id string) (EditResult, error)`.
Tool params `EditPlanParams{Action, PlanID string}` for inspect/apply/recover;
original rename/replace params get `Preview bool` (optional/backward compatible).
Plan payload stored with A0, binary contents encoded bounded JSON; journalDir
always runtime-selected path under engineering store, not model filesystem input.

- [x] **Step 1:** `TestEditPlanUnicodeAndCRLF` multi-file emoji rename UTF-16
  and UTF-8 exact expected bytes/CRLF; overlapping ranges error. TestEditPlanTargetBoundaries symlink escape/create/delete outside root rejected; missing
  delete file flagged. `TestEditPlanStaleAndRecovery` changed before apply
  => writes0; injected second-write failure partial journal; edited first file
  by user then rollback preserves that user content and reports conflict.
  `TestEditPlanPermissionsAndOwnership` all actual LSP destinations checked,
  denied => mutation0; quality/read-only specialist cannot apply.
- [x] **Step 2:** `go test -count=1 -p 1 -ldflags='-w -s' -timeout 3m ./internal/lsp/util ./internal/agent/tools -run TestEditPlan` FAIL expected missing target behavior.
- [x] **Step 3:** Prepare content in memory using existing encoding helpers;
  validate workspace operations, source hashes and complete target set. Stage
  temp contents/history, then atomic per-file write with durable journal.
  No multi-file OS atomicity claim; journal identities via safe hash paths.
- [x] **Step 4:** Tool preview returns exact changed paths/diff+PlanID. Apply
  validates ownership/permissions for actual plan paths and revalidates source
  immediately before write. Existing direct rename/replace route same pipeline;
  filetracker/history and guardedTool mutation accounting remain correct.
- [x] **Step 5:** Expose recovery inspect/apply through tool and B3 resume
  gaps. Unresolved edit journal blocks certification; rollback hash-conditional.
  Follow-up diagnostics/verify stored separately, no auto passed from apply.
- [x] **Step 6:** Full target packages pass; golden descriptions/help updated
  only if legitimate output changed; old semantic edit tests still pass.
- [x] **Step 7:** Scoped commit `feat: preview and journal source-checked semantic edits`.

Test assertions: `require.Equal(t, expectedCRLFContent, actualContent)`,
`require.Error(t, staleSourceErr)`, `require.Zero(t, writesAfterPermissionDenial)`,
`require.Equal(t, userEdit, preservedContentAfterRollback)`.

### Task D2: Web/PTY senaryo runner ve source-bound kanıt

**Files:** Create `internal/scenarios/types.go`, `runner.go`, `runner_test.go`,
`internal/terminal/types.go`, `pty_windows.go`, `pty_unix.go`, `pty_test.go`,
`internal/agent/tools/scenario.go`, `scenario.md`, `scenario_test.go`,
`internal/cmd/workflow_scenarios.go`, `workflow_scenarios_test.go`.
Modify `internal/agent/tools/ui_verify.go`, `internal/engineering/ui_evidence.go`,
`internal/agent/coordinator.go`, `internal/execution/types.go`.
Dependency registration files: `go.mod`, `go.sum`; pinned existing Unix PTY
module is promoted without unrelated version upgrades.

**Interfaces:** execution adds
`TerminalSize{Width, Height int}`;
`TerminalSession` interface embedding `io.ReadWriteCloser`,
`Resize(ctx context.Context, size TerminalSize) error`,
`Wait(ctx context.Context) (Result, error)`, `Identity() RunHandle`;
optional `TerminalRunner` interface
`StartTerminal(ctx context.Context, req Request, size TerminalSize) (TerminalSession, error)`.
Terminal adapter export `Start(ctx context.Context, req execution.Request, size execution.TerminalSize) (execution.TerminalSession, error)`;
requires legacy host mode; non-host must use runner's TerminalRunner or
explicit unsupported result, never bypass isolation with native PTY.
Unix adapter uses `github.com/creack/pty v1.1.24` already present in go.sum,
promoted to direct dependency only after confirming module compatibility; Windows
ConPTY uses current x/sys/windows and process handles, no CGO.

Scenarios `Step{Action, Selector, Value string; Width, Height int}`;
`Assertion{Kind, Selector, Expected string}`;
`Scenario{ID, Target, URL string; Version int; Argv []string; Steps []Step;
Assertions []Assertion; TimeoutMS int64; Width, Height int}`;
`ScenarioRun{ID, SourceFingerprint, Target, Status string; Passed bool;
Artifacts []engineering.ArtifactRef; Result execution.Result; Gaps []string}`;
`Validate(ctx context.Context, scenario Scenario) error`;
`Run(ctx context.Context, scenario Scenario, backend Backend) (ScenarioRun, error)`;
`Backend` interface `Run(ctx context.Context, scenario Scenario) (ScenarioRun, error)`;
tools defines web/terminal adapters, so scenarios imports no agent/tools.

- [x] **Step 1:** `TestScenarioLimitsAndSource` max65 step/assertion rejected;
  source mutation invalidates passed observations. `TestScenarioUnavailable`
  missing browser/PTY or incompatible isolation => unavailable with Passed false,
  no native escape. `TestScenarioWebPersistence` local fixture create/edit/restart
  actual persisted value assertion. `TestPTYRealInputResizeCancel` child fixture
  running inside real PTY, input echo/focus prompt, resize, scroll output, exit
  and cancellation observed. `TestScenarioArtifactProvenance` screenshot vs
  raw PTY transcript distinct; forged artifact/pass metadata not accepted.
- [x] **Step 2:** `go test -count=1 -p 1 -ldflags='-w -s' -timeout 3m ./internal/scenarios ./internal/terminal ./internal/agent/tools ./internal/cmd -run 'TestScenario|TestPTY'` FAIL target
  behavior; missing platform dependency is unavailable, not acceptable red proof.
- [x] **Step 3:** Strict scenario parser and per-run timeout/action bounds;
  adapters use invoke/permissions/budget. Browser adapter existing ui_verify,
  actual assertions and bounded images; spec scenario steps not shell scripts.
- [x] **Step 4:** Real PTY launch/read/write/resize/wait/cancel, Windows child
  handles/owned Job Object, Unix process group; bounded transcript output in
  artifact store. Cleanup only owned process identity; output byte limit breach
  stops process and reports non-pass. Add safe VT interpretation of captured
  output as separate evidence without stripping original bytes.
- [x] **Step 5:** Tool and CLI validate/run/report; persist ScenarioRun before
  effect, artifacts linked to source/run IDs. Link actual captures into existing
  design/critique stage validation. Renderer tests remain separate from PTY tests.
- [x] **Step 6:** Full target packages pass; real local browser fixture and
  real host ConPTY result recorded; Unix real PTY separate platform result or
  unavailable until run there. Cross-compile success is not runtime test.
- [x] **Step 7:** Scoped commit `feat: verify real web and terminal user scenarios`.

Test assertions: `require.False(t, unavailableRun.Passed)`,
`require.NotEmpty(t, terminal.Identity().RunID)`,
`require.Equal(t, requestedSize, observedSize)`,
`require.Equal(t, "cancelled", cancelledRun.Status)`.

### Task D3: Revision'lı workflow API ve agent kontrol paneli

**Files:** Create `internal/engineering/snapshot.go`, `internal/agent/workflow_control.go`,
`workflow_control_test.go`, `internal/proto/workflow.go`,
`internal/server/workflow.go`, `workflow_test.go`,
`internal/ui/model/workflow_panel.go`, `workflow_panel_test.go`.
Modify `internal/agent/coordinator.go`, `internal/app/app.go`,
`internal/engineering/runtime.go`, `internal/workspace/workspace.go`,
`app_workspace.go`, `client_workspace.go`, `internal/backend/backend.go`,
`internal/server/events.go`, `server.go`, `internal/proto/requests.go`,
`internal/ui/model/ui.go`, `keys.go`,
`internal/ui/styles/styles.go`, `quickstyle.go`, `internal/cmd/workflow.go`.

**Interfaces:** engineering
`WorkflowTask{ID, Content, Status, Agent, SpecFingerprint string;
DependsOn, OwnedPaths []string}`;
`WorkflowSnapshot{SessionID, Revision string; Tasks []WorkflowTask;
Usage Usage; Limits Limits; Stage int; Findings []Finding;
Checks []Check; Executions []RoleExecution; Checkpoints []Checkpoint;
Capabilities map[string]string}`;
`WorkflowControl{TaskID, Action, Agent, ExpectedRevision string}`.
Coordinator additions
`WorkflowSnapshot(ctx context.Context, sessionID string) (engineering.WorkflowSnapshot, error)`;
`WorkflowControl(ctx context.Context, sessionID string, action engineering.WorkflowControl) error`.
Workspace additions same context/session methods; proto aliases DTO shape,
backend HTTP client + server authenticated routes GET workflow snapshot and POST
control; event payload session/revision. Revision combines engineering revision,
session task fingerprint and related record revisions to avoid missing updates.

- [x] **Step 1:** `TestWorkflowSnapshotClientServerParity` local/server same
  session snapshot revision/values; unauthorized route denied.
  `TestWorkflowControlStopAndReassign` pending reassign invalidates specs, active reassign
  refused until cancel+observed stop+reconciliation, revision mismatch error.
  `TestWorkflowPanelResizeAndKeys` 80x24/120x40/40x12 text fits ANSI-aware
  bounds; escape close/stop remains reachable. `TestWorkflowPanelReconnect`
  old/reordered event ignored, reconnect full snapshot updates; IO count in
  Update/draw 0. Known test env secret absent from serialized snapshots.
- [x] **Step 2:** `go test -count=1 -p 1 -ldflags='-w -s' -timeout 3m ./internal/agent ./internal/server ./internal/workspace ./internal/ui/model -run 'TestWorkflowSnapshot|TestWorkflowControl|TestWorkflowPanel'` FAIL missing behavior.
- [x] **Step 3:** Snapshot assembly with bounded reads and revision retry;
  control CAS pending task reassignment and explicit cancellation. Pause stops
  new dispatch but is not acknowledged process exit. No UI direct todo mutation.
- [x] **Step 4:** Authenticated server routes, backend/client methods, app events
  and scoped subscriptions. Coalesce progress safely; final state retained,
  source-of-truth snapshot on reconnect. CLI status uses same read model.
- [x] **Step 5:** Read UI AGENTS; integrate imperative panel in sole UI model
  using tea.Cmd for load/actions, messages for state, token-driven styles and
  ANSI-aware layout. Slash/key entry, focus routing, bounds and accessible stop.
- [x] **Step 6:** All target packages pass; existing workspace fakes updated
  for new interface. OpenAPI üretimi mevcut Taskfile komutuyla:
  `go run github.com/swaggo/swag/cmd/swag@v1.16.6 init --generalInfo main.go --dir . --output internal/swagger --packageName swagger --parseDependency --parseInternal --parseDepth 5`.
  Üretilen `internal/swagger/docs.go`, `swagger.json`, `swagger.yaml` bu görevin
  scoped diff'ine dahildir; geçerli endpoint request/response DTO'larıyla eşleşir.
  Real D2 PTY fixture exercises panel keyboard/resize/cancel in addition to
  renderer tests. New capability availability label accurate for local/server OS.
- [x] **Step 7:** Scoped commit `feat: expose coordinated agent controls in CLI server and TUI`.

Test assertions: `require.Equal(t, localSnapshot.Revision, serverSnapshot.Revision)`,
`require.Error(t, activeReassignErr)`, `require.Zero(t, synchronousUpdateIOCount)`,
`require.LessOrEqual(t, ansi.StringWidth(renderedLine), 40)`.

## Alt proje çıkış kontrolü

- [x] D1-D3 format+diff+target tests; snapshots contain no credential content.
- [x] Actual browser/ConPTY/Unix-PTY availability and results reported separately.
- [x] Control panel reflects final coordinator state; E1 merged integration begins.

Execution closure: see docs/agent-platform-progress.md and docs/agent-platform-verification.json. Checkboxes track closure under recorded rulings; unavailable platform acceptance is not a passing runtime test. E1 aggregate RED was waived because its initial failure was a test assertion, not missing runtime behavior. The graph smoke uses the production tool fixture rather than inventing a CLI command.

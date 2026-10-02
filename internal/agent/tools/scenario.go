package tools

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/jsonstrict"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/scenarios"
	"github.com/google/uuid"
)

const ScenarioToolName = "scenario"

//go:embed scenario.md
var scenarioDescription string

type ScenarioParams struct {
	Action   string             `json:"action" description:"validate, run or report"`
	Scenario scenarios.Scenario `json:"scenario,omitempty"`
	RunID    string             `json:"run_id,omitempty"`
}

type ScenarioServices struct {
	CommandPolicy CommandPolicy
	Root          string
	SessionID     string
	Store         *engineering.Store
	Invoke        ToolInvoker
	Approve       func(context.Context, permission.CreatePermissionRequest) (bool, error)
}

type SavedScenario struct {
	Root      string                `json:"root"`
	SessionID string                `json:"session_id"`
	TaskID    string                `json:"task_id,omitempty"`
	Scenario  scenarios.Scenario    `json:"scenario"`
	Run       scenarios.ScenarioRun `json:"run"`
}

func scenarioNamespace(root, sessionID, id string) (string, error) {
	if _, err := uuid.Parse(id); err != nil || sessionID == "" {
		return "", fmt.Errorf("scenario report requires session and run UUID")
	}
	return "scenario-runs-" + engineering.Hash(filepath.Clean(root)+"\x00"+sessionID) + "-" + id[:2], nil
}

func ReadScenario(ctx context.Context, services ScenarioServices, id string) (SavedScenario, engineering.Record, error) {
	if services.Store == nil {
		return SavedScenario{}, engineering.Record{}, fmt.Errorf("scenario artifact store unavailable")
	}
	ns, err := scenarioNamespace(services.Root, services.SessionID, id)
	if err != nil {
		return SavedScenario{}, engineering.Record{}, err
	}
	record, data, err := services.Store.ReadRecord(ctx, ns, id)
	if err != nil {
		return SavedScenario{}, record, err
	}
	var saved SavedScenario
	if err := json.Unmarshal(data, &saved); err != nil {
		return saved, record, err
	}
	if saved.Root != services.Root || saved.SessionID != services.SessionID || saved.Run.ID != id {
		return saved, record, fmt.Errorf("scenario report identity mismatch")
	}
	return saved, record, nil
}

type scenarioBackend struct {
	ScenarioServices
	call        fantasy.ToolCall
	record      engineering.Record
	saved       SavedScenario
	index       int
	operationID string
}

func (b *scenarioBackend) Source(ctx context.Context) (string, error) {
	return engineering.SourceFingerprint(ctx, b.Root, b.Store.Dir())
}

func (b *scenarioBackend) ReadArtifact(ctx context.Context, ref engineering.ArtifactRef) ([]byte, error) {
	return b.Store.ReadArtifact(ctx, ref)
}

func (b *scenarioBackend) save(ctx context.Context, run scenarios.ScenarioRun) error {
	ns, err := scenarioNamespace(b.Root, b.SessionID, run.ID)
	if err != nil {
		return err
	}
	b.saved.Run = run
	data, err := json.Marshal(b.saved)
	if err != nil {
		return err
	}
	refs := append([]engineering.ArtifactRef{}, run.Artifacts...)
	if b.record.Ref.Hash != "" {
		refs = append(refs, b.record.Ref)
		refs = append(refs, b.record.Linked...)
	}
	unique := make([]engineering.ArtifactRef, 0, len(refs))
	seen := map[engineering.ArtifactRef]bool{}
	for _, ref := range refs {
		if !seen[ref] {
			seen[ref] = true
			unique = append(unique, ref)
		}
	}
	b.record, err = b.Store.PutRecordStrict(ctx, ns, run.ID, b.record.Revision, data, unique...)
	return err
}

func (b *scenarioBackend) Run(ctx context.Context, scenario scenarios.Scenario) (scenarios.ScenarioRun, error) {
	source, err := b.Source(ctx)
	if err != nil {
		return scenarios.ScenarioRun{}, err
	}
	run := scenarios.ScenarioRun{ID: scenario.RunID, ScenarioID: scenario.ID, SourceFingerprint: source, Target: scenario.Target, Status: "prepared", Artifacts: []engineering.ArtifactRef{}, Gaps: []string{}}
	b.saved = SavedScenario{Root: b.Root, SessionID: b.SessionID, TaskID: engineering.GetScope(ctx, b.SessionID).TaskID, Scenario: scenario, Run: run}
	if err := b.save(ctx, run); err != nil {
		return run, err
	}
	if scenario.Target == "web" {
		return b.web(ctx, scenario, run)
	}
	return b.terminal(ctx, scenario, run)
}

func ExecuteScenario(ctx context.Context, services ScenarioServices, scenario scenarios.Scenario, call fantasy.ToolCall) (scenarios.ScenarioRun, error) {
	if services.Store == nil || services.SessionID == "" || !filepath.IsAbs(services.Root) {
		return scenarios.ScenarioRun{}, fmt.Errorf("scenario requires an absolute workspace, session and artifact store")
	}
	if execution.IsReadOnly(ctx) {
		return scenarios.ScenarioRun{}, fmt.Errorf("read-only execution cannot run interaction scenarios")
	}
	if err := services.Store.Check(ctx, services.SessionID, engineering.GetScope(ctx, services.SessionID).TaskID); err != nil {
		return scenarios.ScenarioRun{}, err
	}
	backend := &scenarioBackend{ScenarioServices: services, call: call}
	if err := scenarios.Validate(ctx, scenario); err != nil {
		return scenarios.ScenarioRun{}, err
	}
	operation, err := services.Store.Begin(ctx, services.SessionID, call.ID, ScenarioToolName, call.Input, engineering.GetScope(ctx, services.SessionID).TaskID)
	if err != nil {
		return scenarios.ScenarioRun{}, err
	}
	backend.operationID = operation
	started := time.Now()
	run, err := scenarios.Run(ctx, scenario, backend)
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if budgetErr := services.Store.CheckOperation(cleanup, services.SessionID, operation); budgetErr != nil {
		run.Passed = false
		run.Status = "failed"
		err = budgetErr
	}
	if run.SourceFingerprint != "" {
		after, sourceErr := backend.Source(cleanup)
		if sourceErr != nil {
			run.Passed = false
			run.Status = "failed"
			err = sourceErr
		} else if after != run.SourceFingerprint {
			run.Passed = false
			run.Status = "stale"
			if mutationErr := services.Store.Update(cleanup, services.SessionID, func(st *engineering.State) error { st.Generation++; st.Failures = map[string]int{}; return nil }); mutationErr != nil {
				return run, mutationErr
			}
		}
	}
	if chargeErr := services.Store.Charge(cleanup, services.SessionID, backend.saved.TaskID, 0, 0, time.Since(started)); chargeErr != nil {
		run.Passed = false
		return run, chargeErr
	}
	if backend.record.Revision != 0 {
		if saveErr := backend.save(cleanup, run); saveErr != nil {
			run.Passed = false
			return run, fmt.Errorf("persist scenario result: %w", saveErr)
		}
	}
	if run.Passed && backend.record.Revision != 0 {
		if captureErr := backend.registerEvidence(cleanup, scenario, run); captureErr != nil {
			run.Passed = false
			return run, captureErr
		}
	}
	data, _ := json.Marshal(run)
	if finishErr := services.Store.FinishObserved(cleanup, services.SessionID, operation, err == nil && run.Passed, false, engineering.Hash(string(data)), run.Observed); finishErr != nil {
		run.Passed = false
		return run, finishErr
	}
	return run, err
}

func (b *scenarioBackend) registerEvidence(ctx context.Context, scenario scenarios.Scenario, run scenarios.ScenarioRun) error {
	for _, ref := range run.Artifacts {
		if scenario.Target == "web" && ref.Kind != "scenario-screenshot" || scenario.Target == "tui" && ref.Kind != "pty-transcript" {
			continue
		}
		proof := b.record.Ref
		evidence := engineering.UIEvidence{ID: run.ID, TaskID: b.saved.TaskID, Path: filepath.Join(b.Store.Dir(), "artifacts", ref.Hash+".blob"), Hash: ref.Hash, Width: scenario.Width, Height: scenario.Height, Target: scenario.Target, SourceFingerprint: run.SourceFingerprint, AssertionsPassed: true, Origin: "Observed scenario; visual inspection remains required", RecordedAt: time.Now().UnixMilli(), ScenarioStore: b.Store.Dir(), ScenarioProof: &proof}
		for _, step := range scenario.Steps {
			if step.Action == "resize" {
				evidence.Width = step.Width
				evidence.Height = step.Height
			}
		}
		if err := engineering.ValidateUIArtifact(ctx, evidence); err != nil {
			return err
		}
		return b.Store.Update(ctx, b.SessionID, func(st *engineering.State) error {
			st.UIEvidence = append(st.UIEvidence, evidence)
			st.Checks = append(st.Checks, engineering.Check{TaskID: b.saved.TaskID, RunID: run.ID, Name: "interaction scenario " + scenario.ID, Tool: ScenarioToolName, Passed: true, Evidence: "source=" + run.SourceFingerprint + "; proof=" + proof.Hash, CheckedAt: time.Now().UnixMilli()})
			return nil
		})
	}
	return fmt.Errorf("scenario observation artifact missing")
}

func NewScenarioTool(root string, store *engineering.Store, permissions permission.Service, invoke ToolInvoker, policies ...CommandPolicy) fantasy.AgentTool {
	return fantasy.NewAgentTool(ScenarioToolName, scenarioDescription, func(ctx context.Context, p ScenarioParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		services := ScenarioServices{Root: root, SessionID: GetSessionFromContext(ctx), Store: store, Invoke: invoke}
		if len(policies) > 0 {
			services.CommandPolicy = policies[0]
		}
		if scope := engineering.GetScope(ctx, services.SessionID); scope.WriteRoot != "" {
			services.Root = scope.WriteRoot
		}
		if permissions != nil {
			services.Approve = permissions.Request
		}
		var result any
		var err error
		if err := jsonstrict.Validate(ctx, []byte(call.Input)); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		switch p.Action {
		case "validate":
			if err := strictScenarioInput(ctx, call.Input, &p); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			err = scenarios.Validate(ctx, p.Scenario)
			result = map[string]bool{"valid": err == nil, "executed": false}
		case "run":
			if err := strictScenarioInput(ctx, call.Input, &p); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			result, err = ExecuteScenario(ctx, services, p.Scenario, call)
		case "report":
			result, _, err = ReadScenario(ctx, services, p.RunID)
		default:
			err = fmt.Errorf("scenario action must be validate, run or report")
		}
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		data, err := json.Marshal(result)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		response := fantasy.NewTextResponse(string(data))
		if run, ok := result.(scenarios.ScenarioRun); ok {
			response.IsError = !run.Passed
		}
		return response, nil
	})
}

func strictScenarioInput(ctx context.Context, input string, p *ScenarioParams) error {
	if err := jsonstrict.Validate(ctx, []byte(input)); err != nil {
		return err
	}
	if len(input) > 1024*1024 {
		return fmt.Errorf("scenario tool input exceeds 1 MiB")
	}
	var raw struct {
		Action   string          `json:"action"`
		Scenario json.RawMessage `json:"scenario"`
		RunID    string          `json:"run_id,omitempty"`
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(input)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return err
	}
	scenario, err := scenarios.Parse(ctx, raw.Scenario)
	if err != nil {
		return err
	}
	p.Scenario = scenario
	return nil
}

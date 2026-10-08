package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
)

type guardedTool struct {
	fantasy.AgentTool
	store    *engineering.Store
	sessions session.Service
}

func (t *guardedTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	scope := engineering.GetScope(ctx, tools.GetSessionFromContext(ctx))
	if call.Name == "verify" {
		var params tools.VerifyParams
		if json.Unmarshal([]byte(call.Input), &params) == nil && params.TaskID != "" {
			if scope.TaskID != "" && scope.TaskID != params.TaskID {
				return fantasy.NewTextErrorResponse("task_id cannot override the assigned task"), nil
			}
			ctx = engineering.WithScope(ctx, scope.SessionID, params.TaskID)
			scope = engineering.GetScope(ctx, scope.SessionID)
		}
	}
	if err := checkToolOwnership(scope, call); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	if t.sessions != nil && scope.TaskID != "" && call.Name != "verify" && tools.CallMutates(call.Name, call.Input) {
		if refusal := sharedTaskDispatchRefusal(ctx, t.store, t.sessions, scope.SessionID, scope.TaskID); refusal != "" {
			return fantasy.NewTextErrorResponse(refusal), nil
		}
	}
	if call.Name == tools.LSPEditPlanToolName {
		var params tools.EditPlanParams
		if json.Unmarshal([]byte(call.Input), &params) == nil && (params.Action == "inspect" || params.Action == "recover") {
			// Recovery observes/restores a persisted operation; it cannot be
			// made unreachable by an exhausted implementation budget. The
			// tool still checks every target, permissions and read-only mode.
			response, err := t.AgentTool.Run(ctx, call)
			var meta struct {
				Mutation bool `json:"semantic_mutation"`
			}
			if params.Action == "recover" && json.Unmarshal([]byte(response.Metadata), &meta) == nil && meta.Mutation {
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer cancel()
				if updateErr := t.store.Update(cleanup, scope.SessionID, func(state *engineering.State) error {
					state.Generation++
					state.Failures = map[string]int{}
					return nil
				}); updateErr != nil {
					return response, errors.Join(err, updateErr)
				}
			}
			return response, err
		}
	}
	if refusal := t.resendRefusal(ctx, scope.SessionID, call); refusal != "" {
		return fantasy.NewTextErrorResponse(refusal), nil
	}
	id, err := t.store.Begin(ctx, scope.SessionID, call.ID, call.Name, call.Input, scope.TaskID)
	if err != nil {
		resp := fantasy.NewTextErrorResponse(err.Error())
		resp.StopTurn = true
		return resp, nil
	}
	if call.Name == "verify" && scope.TaskID != "" && t.sessions != nil {
		current, getErr := t.sessions.Get(ctx, scope.SessionID)
		if getErr != nil {
			return fantasy.ToolResponse{}, getErr
		}
		for _, todo := range current.Todos {
			if todo.ID != scope.TaskID {
				continue
			}
			if updateErr := t.store.Update(ctx, scope.SessionID, func(st *engineering.State) error {
				for i := range st.Operations {
					if st.Operations[i].ID == id {
						st.Operations[i].TaskFingerprint = session.TaskFingerprint(todo)
					}
				}
				return nil
			}); updateErr != nil {
				return fantasy.ToolResponse{}, updateErr
			}
			break
		}
	}
	if call.Name == AgentToolName {
		var params AgentParams
		if json.Unmarshal([]byte(call.Input), &params) == nil && params.AgentName != "" {
			if err := t.store.Update(ctx, scope.SessionID, func(st *engineering.State) error {
				for i := range st.Operations {
					if st.Operations[i].ID == id {
						st.Operations[i].AgentName = params.AgentName
					}
				}
				return nil
			}); err != nil {
				return fantasy.ToolResponse{}, err
			}
		}
	}
	resp, runErr := t.AgentTool.Run(ctx, call)
	if ctx.Err() != nil && (runErr != nil || resp.IsError) || errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
		abandoned, abandon := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		_ = t.recordOutcome(abandoned, scope.SessionID, id, call, outcomeResult(call.Name, resp, runErr, tools.CallMutates(call.Name, call.Input), true), resp)
		abandon()
		return resp, runErr
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if call.Name == tools.JobOutputToolName && runErr == nil && !resp.IsError {
		var meta tools.JobOutputResponseMetadata
		if json.Unmarshal([]byte(resp.Metadata), &meta) == nil && meta.Done && meta.ExitCode != nil {
			state, err := t.store.Read(cleanup, scope.SessionID)
			if err != nil {
				return resp, err
			}
			for _, op := range state.Operations {
				if op.Status == "running" && op.BackgroundID == meta.ShellID && meta.ShellID != "" {
					evidence, observed := operationOutcome(tools.BashToolName, resp, nil)
					if err := t.store.Update(cleanup, scope.SessionID, func(st *engineering.State) error {
						for _, record := range st.Outcomes.Records {
							if record.OpID != op.ID {
								continue
							}
							if *meta.ExitCode == 0 {
								record.Phase = outcome.PhaseVerified
							} else {
								record.Phase = outcome.PhaseUnknown
							}
							record.At = time.Now().UnixMilli()
							st.Outcomes.Record(record)
							break
						}
						return nil
					}); err != nil {
						return resp, err
					}
					if err := t.store.FinishObserved(cleanup, scope.SessionID, op.ID, *meta.ExitCode == 0, false, evidence, observed); err != nil {
						return resp, err
					}
				}
			}
		}
	}
	if call.Name == tools.BashToolName && runErr == nil && !resp.IsError {
		var meta tools.BashResponseMetadata
		if json.Unmarshal([]byte(resp.Metadata), &meta) == nil && meta.Background && meta.ShellID != "" {
			err := t.store.Update(cleanup, scope.SessionID, func(st *engineering.State) error {
				for i := range st.Operations {
					if st.Operations[i].ID == id {
						st.Operations[i].BackgroundID = meta.ShellID
						st.Outcomes.Record(outcome.Record{OpID: id, Tool: call.Name, Phase: outcome.PhaseDispatched, Mutation: true, Subjects: outcomeSubjects(call), Fingerprint: outcomeFingerprint(call)})
					}
				}
				return nil
			})
			return resp, err
		}
	}
	mutation := call.Name == "edit" || call.Name == "write" || call.Name == "multiedit" || call.Name == tools.ReplaceSymbolToolName || call.Name == tools.RenameToolName || call.Name == tools.LspRenameFileToolName
	if call.Name == tools.ReplaceSymbolToolName || call.Name == tools.RenameToolName || call.Name == tools.LspRenameFileToolName || call.Name == tools.LSPEditPlanToolName {
		var meta struct {
			Mutation bool `json:"semantic_mutation"`
		}
		mutation = json.Unmarshal([]byte(resp.Metadata), &meta) == nil && meta.Mutation
		if mutation && (runErr != nil || resp.IsError) {
			if err := t.store.Update(cleanup, scope.SessionID, func(state *engineering.State) error {
				state.Generation++
				state.Failures = map[string]int{}
				return nil
			}); err != nil {
				return resp, err
			}
		}
	}
	if call.Name == "worktree" {
		var params tools.WorktreeParams
		mutation = json.Unmarshal([]byte(call.Input), &params) == nil && params.Action == "apply"
	}
	if call.Name == "undo_feature" {
		var meta tools.UndoFeatureMetadata
		mutation = json.Unmarshal([]byte(resp.Metadata), &meta) == nil && meta.Applied
		if mutation && (runErr != nil || resp.IsError) {
			if err := t.store.Update(cleanup, scope.SessionID, func(state *engineering.State) error {
				state.Generation++
				state.Failures = map[string]int{}
				return nil
			}); err != nil {
				return resp, err
			}
		}
	}
	evidence, observed := operationOutcome(call.Name, resp, runErr)
	passed := tools.ToolSucceeded(call.Name, resp, runErr)
	if (call.Name == "data_boundary" || call.Name == tools.CLIBridgeToolName) && runErr == nil && !resp.IsError && !resp.StopTurn {
		var meta struct {
			Prepared bool `json:"prepared"`
		}
		if json.Unmarshal([]byte(resp.Metadata), &meta) == nil && meta.Prepared {
			// Probe creation, CLI Bridge previews and started background
			// runs are successful preparation, not observed checks.
			passed = true
		}
	}
	if err := t.recordOutcome(cleanup, scope.SessionID, id, call, outcomeResult(call.Name, resp, runErr, mutation || tools.CallMutates(call.Name, call.Input), false), resp); err != nil {
		return resp, fmt.Errorf("persist tool outcome %s: %w", id, err)
	}
	if err := t.store.FinishObserved(cleanup, scope.SessionID, id, passed, mutation, evidence, observed); err != nil {
		return resp, fmt.Errorf("persist tool completion %s: %w", id, err)
	}
	return resp, runErr
}

func operationOutcome(tool string, response fantasy.ToolResponse, err error) (string, bool) {
	if err != nil {
		return engineering.Hash(err.Error()), false
	}
	if tool == tools.BashToolName {
		return tools.ToolEvidenceHash(tool, response, err), tools.ToolOutcomeObserved(tool, response, err)
	}
	if tool == "verify" {
		var result struct {
			Passed *bool `json:"passed"`
			Checks []struct {
				Name        string `json:"name"`
				Passed      bool   `json:"passed"`
				Observed    bool   `json:"observed"`
				Output      string `json:"output"`
				OutcomeHash string `json:"outcome_hash"`
			} `json:"checks"`
		}
		if json.Unmarshal([]byte(response.Content), &result) == nil && result.Passed != nil && len(result.Checks) > 0 {
			observed := true
			failedHash := ""
			for _, check := range result.Checks {
				observed = observed && check.Observed
				if !check.Passed {
					failedHash = check.OutcomeHash
				}
			}
			if !*result.Passed && len(failedHash) == 64 {
				return failedHash, observed
			}
			data, _ := json.Marshal(result)
			return engineering.Hash(string(data)), observed
		}
	}
	return engineering.Hash(response.Content), tools.ToolOutcomeObserved(tool, response, err)
}

func engineeringTokens(u fantasy.Usage) int64 {
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return max(0, u.InputTokens) + max(0, u.OutputTokens) + max(0, u.CacheCreationTokens) + max(0, u.CacheReadTokens)
}

func (a *sessionAgent) engineeringReviewCall(ctx context.Context, sessionID string, model Model, prompt string) fantasy.AgentStreamCall {
	return fantasy.AgentStreamCall{
		Prompt: prompt,
		PrepareStep: func(callCtx context.Context, opts fantasy.PrepareStepFunctionOptions) (context.Context, fantasy.PrepareStepResult, error) {
			return callCtx, fantasy.PrepareStepResult{Messages: opts.Messages}, a.checkEngineering(callCtx, sessionID)
		},
		OnStepFinish: func(step fantasy.StepResult) error {
			return a.chargeEngineeringStep(ctx, sessionID, model, step)
		},
	}
}

func (a *sessionAgent) checkEngineering(ctx context.Context, sessionID string) error {
	if a.engineering == nil {
		return nil
	}
	scope := engineering.GetScope(ctx, sessionID)
	return a.engineering.Check(ctx, scope.SessionID, scope.TaskID)
}

func (a *sessionAgent) chargeEngineeringStep(ctx context.Context, sessionID string, model Model, step fantasy.StepResult) error {
	if a.engineering == nil {
		return nil
	}
	u := step.Usage
	p := model.CatwalkCfg
	cost := (p.CostPer1MIn*float64(u.InputTokens) + p.CostPer1MOut*float64(u.OutputTokens) + p.CostPer1MInCached*float64(u.CacheCreationTokens) + p.CostPer1MOutCached*float64(u.CacheReadTokens)) / 1e6
	if override := a.openrouterCost(step.ProviderMetadata); override != nil {
		cost = *override
	}
	if model.FlatRate {
		cost = 0
	}
	scope := engineering.GetScope(ctx, sessionID)
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return a.engineering.Charge(cleanup, scope.SessionID, scope.TaskID, engineeringTokens(u), max(0, cost), 0)
}

func checkToolOwnership(scope engineering.Scope, call fantasy.ToolCall) error {
	if scope.WriteRoot == "" {
		return nil
	}
	if call.Name != "edit" && call.Name != "write" && call.Name != "multiedit" && call.Name != "bash" {
		// Semantic tools check every actual LSP target after planning. Opaque
		// previews cannot be authorized by their input symbol/path alone.
		return nil
	}
	var p struct {
		FilePath   string `json:"file_path"`
		WorkingDir string `json:"working_dir"`
	}
	if err := json.Unmarshal([]byte(call.Input), &p); err != nil {
		return err
	}
	file := p.FilePath
	if call.Name == "bash" {
		file = p.WorkingDir
		if file == "" {
			return nil
		}
	}
	if file == "" {
		return nil
	}
	if !filepath.IsAbs(file) {
		file = filepath.Join(scope.WriteRoot, file)
	}
	root, err := filepath.EvalSymlinks(scope.WriteRoot)
	if err != nil {
		return err
	}
	file = filepath.Clean(file)
	ancestor := file
	var suffix []string
	for {
		_, err := os.Lstat(ancestor)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return fmt.Errorf("cannot resolve tool destination")
		}
		suffix = append(suffix, filepath.Base(ancestor))
		ancestor = parent
	}
	resolved, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return err
	}
	for i := len(suffix) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, suffix[i])
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("tool destination escapes the assigned workspace")
	}
	if call.Name == "bash" || len(scope.OwnedPaths) == 0 {
		return nil
	}
	for _, owned := range scope.OwnedPaths {
		owned = filepath.Clean(filepath.FromSlash(owned))
		if owned == "." || strings.EqualFold(rel, owned) || strings.HasPrefix(strings.ToLower(rel), strings.ToLower(owned)+string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf("file is outside this task's declared ownership; hand the change to its owner")
}

func guardTools(ts []fantasy.AgentTool, store *engineering.Store, sessions ...session.Service) []fantasy.AgentTool {
	if store == nil {
		return ts
	}
	out := make([]fantasy.AgentTool, len(ts))
	for i, t := range ts {
		// Recovery/status and budget controls must remain usable after exhaustion.
		if t.Info().Name == "workflow" || t.Info().Name == tools.ScenarioToolName {
			out[i] = t
		} else {
			out[i] = &guardedTool{AgentTool: t, store: store}
			if len(sessions) > 0 {
				out[i].(*guardedTool).sessions = sessions[0]
			}
		}
	}
	return out
}

func (a *sessionAgent) engineeringContext(ctx context.Context, sessionID string) (context.Context, func(), error) {
	if a.engineering == nil {
		return ctx, func() {}, nil
	}
	scope := engineering.GetScope(ctx, sessionID)
	ctx = engineering.WithScope(ctx, scope.SessionID, scope.TaskID)
	if err := a.engineering.Check(ctx, scope.SessionID, scope.TaskID); err != nil {
		return ctx, func() {}, err
	}
	st, err := a.engineering.Read(ctx, scope.SessionID)
	if err != nil {
		return ctx, func() {}, err
	}
	remaining := int64(0)
	for _, account := range []engineering.TaskAccount{{Limits: st.Limits, Usage: st.Usage}, st.Tasks[scope.TaskID]} {
		if account.Limits.MaxDurationMS > 0 {
			r := account.Limits.MaxDurationMS - account.Usage.DurationMS
			if remaining == 0 || r < remaining {
				remaining = r
			}
		}
	}
	started := time.Now()
	var cancel context.CancelFunc = func() {}
	if remaining > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(remaining)*time.Millisecond)
	}
	return ctx, func() {
		cancel()
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer done()
		_ = a.engineering.Update(cleanup, scope.SessionID, func(st *engineering.State) error {
			ms := time.Since(started).Milliseconds()
			if scope.TaskID == "" {
				st.Usage.DurationMS += ms
			} else {
				account := st.Tasks[scope.TaskID]
				account.Usage.DurationMS += ms
				st.Tasks[scope.TaskID] = account
			}
			return nil
		})
	}, nil
}

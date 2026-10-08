package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/qualityflows"
)

//go:embed handover.md
var handoverDescription string

//go:embed undo_feature.md
var undoFeatureDescription string

type HandoverParams struct {
	Manifest string `json:"manifest" description:"Project-relative JSON specification with literal README-backed argv steps and explicit output/file expectations."`
}

type UndoFeatureParams struct {
	Action                string   `json:"action" description:"prepare or apply; prepare is read-only apart from storing the reviewed plan."`
	Commits               []string `json:"commits,omitempty" description:"Explicit 1-32 reachable non-merge commits belonging to this feature, for prepare."`
	PlanID                string   `json:"plan_id,omitempty" description:"Exact fingerprint returned by prepare; required for apply."`
	AcknowledgeDependents bool     `json:"acknowledge_dependents,omitempty" description:"True only after reviewing later commits that touch affected paths."`
}

type UndoFeatureMetadata struct {
	Applied bool   `json:"applied"`
	PlanID  string `json:"plan_id"`
}

func checkUndoOwnership(ctx context.Context, root string, paths []string) error {
	scope := engineering.GetScope(ctx, GetSessionFromContext(ctx))
	if scope.WriteRoot != "" && !qualityflows.SameRoot(scope.WriteRoot, root) {
		return fmt.Errorf("rollback root differs from assigned workspace")
	}
	if len(scope.OwnedPaths) == 0 {
		return nil
	}
	for _, path := range paths {
		allowed := false
		for _, owned := range scope.OwnedPaths {
			owned = filepath.ToSlash(filepath.Clean(owned))
			if filepath.Separator == '\\' {
				path = strings.ToLower(path)
				owned = strings.ToLower(owned)
			}
			if owned == "." || path == owned || strings.HasPrefix(path, owned+"/") {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("rollback path outside task ownership: %s", path)
		}
	}
	return nil
}

func NewHandoverTool(root string, store *engineering.Store, invoke ToolInvoker) fantasy.AgentTool {
	return fantasy.NewAgentTool("handover", handoverDescription, func(ctx context.Context, p HandoverParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		if execution.IsReadOnly(ctx) {
			return fantasy.NewTextErrorResponse("handover execution is unavailable in read-only mode"), nil
		}
		if store == nil || GetSessionFromContext(ctx) == "" {
			return fantasy.NewTextErrorResponse("engineering store and session required"), nil
		}
		data, err := qualityflows.ReadFile(root, p.Manifest, 256*1024)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		var spec qualityflows.HandoverSpec
		if err := qualityflows.Decode(data, &spec); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		index := 0
		var stopped *fantasy.ToolResponse
		run := func(ctx context.Context, dir string, argv []string) (qualityflows.Observation, error) {
			index++
			encoded, _ := json.Marshal(BashParams{WorkingDir: dir, Argv: argv, Description: "README handover rehearsal"})
			if invoke == nil {
				return qualityflows.Observation{}, fmt.Errorf("guarded command dispatch unavailable")
			}
			// Only the tool-owned disposable project becomes the child command's
			// workspace. The original ownership context remains unchanged.
			scope := engineering.GetScope(ctx, GetSessionFromContext(ctx))
			ctx = engineering.WithScope(ctx, scope.SessionID, scope.TaskID)
			ctx = engineering.WithOwnership(ctx, dir, scope.OwnedPaths)
			if binding, ok := execution.GetBinding(ctx); ok {
				binding.Root = dir
				ctx = execution.WithBinding(ctx, binding)
			}
			resp, err := invoke(ctx, fantasy.ToolCall{ID: fmt.Sprintf("%s-handover-%d", call.ID, index), Name: BashToolName, Input: string(encoded)})
			if err != nil {
				return qualityflows.Observation{}, err
			}
			if resp.IsError || resp.StopTurn {
				if resp.StopTurn {
					stopped = &resp
				}
				return qualityflows.Observation{}, fmt.Errorf("command denied or unavailable: %s", truncateVerification(resp.Content, 1024))
			}
			var meta BashResponseMetadata
			if json.Unmarshal([]byte(resp.Metadata), &meta) != nil || meta.ExitCode == nil || meta.Background || meta.EndTime < meta.StartTime {
				return qualityflows.Observation{}, fmt.Errorf("no observed foreground command result")
			}
			return qualityflows.Observation{ExitCode: *meta.ExitCode, Output: meta.Output, DurationMS: meta.EndTime - meta.StartTime}, nil
		}
		report, err := qualityflows.RunHandover(ctx, root, spec, run)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := saveQualityReport(saveCtx, store, GetSessionFromContext(ctx), call.ID, "handover", report); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if stopped != nil {
			return *stopped, nil
		}
		encoded, _ := json.Marshal(report)
		resp := fantasy.NewTextResponse(string(encoded))
		resp.IsError = !report.Passed
		return fantasy.WithResponseMetadata(resp, MeasuredCheckMetadata{Observed: true, Passed: report.Passed}), nil
	})
}

func NewUndoFeatureTool(root string, store *engineering.Store, perms permission.Service) fantasy.AgentTool {
	return fantasy.NewAgentTool("undo_feature", undoFeatureDescription, func(ctx context.Context, p UndoFeatureParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		if store == nil || GetSessionFromContext(ctx) == "" {
			return fantasy.NewTextErrorResponse("session and engineering store required"), nil
		}
		namespace := "undo-plans-" + engineering.Hash(root+"\x00"+GetSessionFromContext(ctx))
		var plan qualityflows.UndoPlan
		switch p.Action {
		case "prepare":
			var err error
			plan, err = qualityflows.PrepareUndo(ctx, root, p.Commits)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			encoded, _ := json.Marshal(plan)
			_, _, existing := store.ReadRecord(ctx, namespace, plan.ID)
			if existing != nil {
				if _, err := store.PutRecord(ctx, namespace, plan.ID, 0, encoded); err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
			}
		case "apply":
			if execution.IsReadOnly(ctx) {
				return fantasy.NewTextErrorResponse("rollback application is unavailable in read-only mode"), nil
			}
			_, data, err := store.ReadRecord(ctx, namespace, p.PlanID)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			if err := json.Unmarshal(data, &plan); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			if !qualityflows.SameRoot(plan.Root, root) || plan.ID != p.PlanID {
				return fantasy.NewTextErrorResponse("rollback identity mismatch"), nil
			}
			if err := checkUndoOwnership(ctx, root, plan.Paths); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			if perms == nil {
				return fantasy.NewTextErrorResponse("permission service unavailable"), nil
			}
			approved, err := perms.Request(ctx, permission.CreatePermissionRequest{SessionID: GetSessionFromContext(ctx), Path: root, ToolCallID: call.ID, ToolName: "undo_feature", Action: "write", Description: "Apply reviewed feature inverse patch; retain unrelated edits and Git history", Params: p})
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if !approved {
				return NewPermissionDeniedResponse(perms), nil
			}
			if err := qualityflows.ApplyUndo(ctx, plan, p.PlanID, p.AcknowledgeDependents); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			if err := saveQualityReport(ctx, store, GetSessionFromContext(ctx), call.ID, "undo", map[string]any{"plan_id": plan.ID, "applied": true, "verification_required": true}); err != nil {
				return fantasy.WithResponseMetadata(fantasy.NewTextErrorResponse("patch applied but recording failed: "+err.Error()), UndoFeatureMetadata{Applied: true, PlanID: plan.ID}), nil
			}
		default:
			return fantasy.NewTextErrorResponse("action must be prepare or apply"), nil
		}
		encoded, _ := json.Marshal(map[string]any{"plan": plan, "applied": p.Action == "apply", "verification_required": true})
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(string(encoded)), UndoFeatureMetadata{Applied: p.Action == "apply", PlanID: plan.ID}), nil
	})
}

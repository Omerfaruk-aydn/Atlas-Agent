package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/diff"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/filetracker"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/history"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lsp"
	lsputil "github.com/Omerfaruk-aydn/Atlas-Agent/internal/lsp/util"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/google/uuid"
)

const LSPEditPlanToolName = "lsp_edit_plan"

//go:embed lsp_edit_plan.md
var editPlanDescription string

type EditPlanParams struct {
	Action string `json:"action" description:"inspect, apply or recover; permissions and source checks apply to every actual target"`
	PlanID string `json:"plan_id" description:"Persisted plan identity from a semantic tool preview"`
}

type SemanticEditPermissionsParams struct {
	PlanID     string                  `json:"plan_id"`
	Operation  string                  `json:"operation"`
	FilePath   string                  `json:"file_path"`
	OldContent string                  `json:"old_content"`
	NewContent string                  `json:"new_content"`
	DiffRef    engineering.ArtifactRef `json:"diff_ref"`
	Recovery   bool                    `json:"recovery"`
}

type SemanticEditServices struct {
	Root        string
	Store       *engineering.Store
	Permissions permission.Service
	History     history.Service
	Tracker     filetracker.Service
	Manager     *lsp.Manager
}

type semanticPlanRecord struct {
	ID        string                  `json:"id"`
	SessionID string                  `json:"session_id"`
	TaskID    string                  `json:"task_id,omitempty"`
	Root      string                  `json:"root"`
	Status    string                  `json:"status"`
	PlanRef   engineering.ArtifactRef `json:"plan_ref"`
	DiffRef   engineering.ArtifactRef `json:"diff_ref"`
}

func semanticSourceUnchanged(ctx context.Context, root, path, expected string) error {
	_, current, err := lsputil.ReadEditSource(ctx, root, path)
	if err != nil {
		return err
	}
	if current != expected {
		return fmt.Errorf("semantic request source changed while awaiting LSP: %s", path)
	}
	return nil
}

func semanticNamespace(root, sessionID, id string) (string, error) {
	if _, err := uuid.Parse(id); err != nil || sessionID == "" || !filepath.IsAbs(root) {
		return "", fmt.Errorf("invalid semantic plan identity")
	}
	return "semantic-plans-" + engineering.Hash(root+"\x00"+sessionID) + "-" + id[:2], nil
}

func semanticServices(root string, permissions permission.Service, files history.Service, tracker filetracker.Service, manager *lsp.Manager, configured []SemanticEditServices) SemanticEditServices {
	services := SemanticEditServices{Root: root}
	if len(configured) > 0 {
		services = configured[0]
	}
	if services.Root == "" {
		services.Root, _ = os.Getwd()
	}
	if services.Store == nil {
		services.Store = engineering.NewStore(filepath.Join(services.Root, ".atlas"))
	}
	services.Permissions, services.History, services.Tracker, services.Manager = permissions, files, tracker, manager
	return services
}

func (s SemanticEditServices) root(ctx context.Context) (string, error) {
	root, err := s.sourceRoot(ctx)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(root)
}

func (s SemanticEditServices) sourceRoot(ctx context.Context) (string, error) {
	root := s.Root
	if scope := engineering.GetScope(ctx, GetSessionFromContext(ctx)); scope.WriteRoot != "" {
		root = scope.WriteRoot
	}
	return filepath.Abs(root)
}

func (s SemanticEditServices) ownership(ctx context.Context, plan lsputil.EditPlan) error {
	root, err := s.root(ctx)
	if err != nil {
		return err
	}
	sameRoot := filepath.Clean(root) == filepath.Clean(plan.Root)
	if runtime.GOOS == "windows" {
		sameRoot = strings.EqualFold(filepath.Clean(root), filepath.Clean(plan.Root))
	}
	if !sameRoot {
		return fmt.Errorf("semantic plan belongs to another root")
	}
	scope := engineering.GetScope(ctx, GetSessionFromContext(ctx))
	for _, change := range plan.Changes {
		rel, err := filepath.Rel(root, change.Path)
		if err != nil || !filepath.IsLocal(rel) {
			return fmt.Errorf("semantic edit target escapes root")
		}
		if len(scope.OwnedPaths) == 0 {
			continue
		}
		owned := false
		for _, path := range scope.OwnedPaths {
			path = filepath.Clean(filepath.FromSlash(path))
			matches := rel == path || strings.HasPrefix(rel, path+string(filepath.Separator))
			if runtime.GOOS == "windows" {
				matches = strings.EqualFold(rel, path) || strings.HasPrefix(strings.ToLower(rel), strings.ToLower(path)+string(filepath.Separator))
			}
			if path == "." || filepath.IsLocal(path) && matches {
				owned = true
			}
		}
		if !owned {
			return fmt.Errorf("semantic target is outside task ownership: %s", change.Path)
		}
	}
	return nil
}

func (s SemanticEditServices) mark(ctx context.Context, meta semanticPlanRecord, expected engineering.Record, status string) (engineering.Record, error) {
	ns, err := semanticNamespace(meta.Root, meta.SessionID, meta.ID)
	if err != nil {
		return engineering.Record{}, err
	}
	meta.Status = status
	data, err := json.Marshal(meta)
	if err != nil {
		return engineering.Record{}, err
	}
	refs := []engineering.ArtifactRef{meta.PlanRef, meta.DiffRef}
	if expected.Ref.Hash != "" {
		refs = append(refs, expected.Ref)
		refs = append(refs, expected.Linked...)
	}
	// Historical statuses and the complete plan/diff remain reachable.
	unique := make([]engineering.ArtifactRef, 0, len(refs))
	for _, ref := range refs {
		found := false
		for _, seen := range unique {
			if ref == seen {
				found = true
			}
		}
		if !found {
			unique = append(unique, ref)
		}
	}
	var record engineering.Record
	err = s.Store.Update(ctx, meta.SessionID, func(state *engineering.State) error {
		if state.SemanticEdits == nil {
			state.SemanticEdits = map[string]engineering.SemanticEditState{}
		}
		if _, exists := state.SemanticEdits[meta.ID]; !exists && len(state.SemanticEdits) >= 128 {
			return fmt.Errorf("semantic edit state exceeds 128 records")
		}
		var err error
		record, err = s.Store.PutRecordStrict(ctx, ns, meta.ID, expected.Revision, data, unique...)
		if err != nil {
			return err
		}
		state.SemanticEdits[meta.ID] = engineering.SemanticEditState{ID: meta.ID, Root: meta.Root, TaskID: meta.TaskID, Status: status, Record: record}
		return nil
	})
	return record, err
}

func (s SemanticEditServices) read(ctx context.Context, id string) (semanticPlanRecord, engineering.Record, lsputil.EditPlan, error) {
	root, err := s.root(ctx)
	if err != nil {
		return semanticPlanRecord{}, engineering.Record{}, lsputil.EditPlan{}, err
	}
	scope := engineering.GetScope(ctx, GetSessionFromContext(ctx))
	ns, err := semanticNamespace(root, scope.SessionID, id)
	if err != nil {
		return semanticPlanRecord{}, engineering.Record{}, lsputil.EditPlan{}, err
	}
	record, data, err := s.Store.ReadRecord(ctx, ns, id)
	if err != nil {
		return semanticPlanRecord{}, record, lsputil.EditPlan{}, err
	}
	var meta semanticPlanRecord
	if err := json.Unmarshal(data, &meta); err != nil {
		return meta, record, lsputil.EditPlan{}, err
	}
	if meta.ID != id || meta.Root != root || meta.SessionID != scope.SessionID || scope.TaskID != "" && meta.TaskID != scope.TaskID {
		return meta, record, lsputil.EditPlan{}, fmt.Errorf("semantic record identity or task mismatch")
	}
	data, err = s.Store.ReadArtifact(ctx, meta.PlanRef)
	if err != nil {
		return meta, record, lsputil.EditPlan{}, err
	}
	var plan lsputil.EditPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return meta, record, plan, err
	}
	if plan.ID != id || plan.Root != root {
		return meta, record, plan, fmt.Errorf("semantic plan artifact identity mismatch")
	}
	return meta, record, plan, nil
}

func semanticResponse(meta semanticPlanRecord, plan lsputil.EditPlan, result *lsputil.EditResult) fantasy.ToolResponse {
	type target struct {
		Path        string `json:"path"`
		Destination string `json:"destination,omitempty"`
		Operation   string `json:"operation"`
		BeforeHash  string `json:"before_hash"`
		AfterHash   string `json:"after_hash"`
	}
	targets := make([]target, 0, len(plan.Changes))
	for _, change := range plan.Changes {
		targets = append(targets, target{change.Path, change.Destination, change.Operation, change.BeforeHash, change.AfterHash})
	}
	data, _ := json.Marshal(map[string]any{"plan_id": meta.ID, "status": meta.Status, "plan_ref": meta.PlanRef, "diff_ref": meta.DiffRef, "targets": targets, "result": result, "certified": false})
	return fantasy.NewTextResponse(string(data))
}

func (s SemanticEditServices) submit(ctx context.Context, plan lsputil.EditPlan, preview bool, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	fail := func(err error) (fantasy.ToolResponse, error) { return fantasy.NewTextErrorResponse(err.Error()), nil }
	if !preview && execution.IsReadOnly(ctx) {
		return fail(fmt.Errorf("read-only specialists cannot apply semantic edits"))
	}
	if err := s.ownership(ctx, plan); err != nil {
		return fail(err)
	}
	if len(plan.Changes) == 0 {
		return fantasy.NewTextResponse("No source changes generated."), nil
	}
	if err := lsputil.ValidateEditPlan(ctx, plan.Root, plan); err != nil {
		return fail(err)
	}
	scope := engineering.GetScope(ctx, GetSessionFromContext(ctx))
	if scope.SessionID == "" || s.Store == nil {
		return fail(fmt.Errorf("semantic plan requires a session and artifact store"))
	}
	data, err := json.Marshal(plan)
	if err != nil {
		return fail(err)
	}
	planRef, err := s.Store.PutArtifact(ctx, "semantic-edit-plan", data)
	if err != nil {
		return fail(err)
	}
	var exactDiff strings.Builder
	for _, change := range plan.Changes {
		before, hash, err := lsputil.ReadEditSource(ctx, plan.Root, change.Path)
		if err != nil || hash != change.BeforeHash {
			return fail(fmt.Errorf("semantic source changed while preparing diff"))
		}
		rel, _ := filepath.Rel(plan.Root, change.Path)
		text, _, _ := diff.GenerateDiff(string(before), string(change.Content), filepath.ToSlash(rel))
		fmt.Fprintf(&exactDiff, "Operation: %s; destination: %s\n%s\n", change.Operation, change.Destination, text)
		if exactDiff.Len() > 32*1024*1024 {
			return fail(fmt.Errorf("semantic diff exceeds artifact limit"))
		}
	}
	diffRef, err := s.Store.PutArtifact(ctx, "semantic-edit-diff", []byte(exactDiff.String()))
	if err != nil {
		return fail(err)
	}
	meta := semanticPlanRecord{ID: plan.ID, SessionID: scope.SessionID, TaskID: scope.TaskID, Root: plan.Root, PlanRef: planRef, DiffRef: diffRef}
	var record engineering.Record
	previous, previousRecord, previousPlan, readErr := s.read(ctx, plan.ID)
	if readErr == nil {
		previousData, _ := json.Marshal(previousPlan)
		if previous.Status != "preview" || engineering.Hash(string(previousData)) != engineering.Hash(string(data)) {
			return fail(fmt.Errorf("semantic plan identity already has another state"))
		}
		meta, record = previous, previousRecord
	} else if errors.Is(readErr, os.ErrNotExist) {
		record, err = s.mark(ctx, meta, engineering.Record{}, "preview")
		if err != nil {
			return fail(err)
		}
	} else {
		return fail(readErr)
	}
	meta.Status = "preview"
	if preview {
		return semanticResponse(meta, plan, nil), nil
	}
	return s.apply(ctx, meta, record, plan, call, false)
}

func (s SemanticEditServices) apply(ctx context.Context, meta semanticPlanRecord, record engineering.Record, plan lsputil.EditPlan, call fantasy.ToolCall, recover bool) (fantasy.ToolResponse, error) {
	fail := func(err error) (fantasy.ToolResponse, error) { return fantasy.NewTextErrorResponse(err.Error()), nil }
	if execution.IsReadOnly(ctx) {
		return fail(fmt.Errorf("read-only specialists cannot apply or recover semantic edits"))
	}
	if err := s.ownership(ctx, plan); err != nil {
		return fail(err)
	}
	if !recover {
		if err := lsputil.ValidateEditPlan(ctx, plan.Root, plan); err != nil {
			return fail(err)
		}
	}
	if s.Permissions == nil || GetSessionFromContext(ctx) == "" {
		return fail(fmt.Errorf("semantic mutation requires ordinary session permissions"))
	}
	action := "apply"
	restoration := map[string]lsputil.PlannedChange{}
	approvedHashes := map[string]string{}
	if recover {
		action = "recover"
		if err := lsputil.ValidateEditJournal(ctx, plan.Root, filepath.Join(s.Store.Dir(), "semantic-edits"), plan); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fail(err)
		}
		proposed, err := lsputil.PreviewEditRecovery(ctx, plan.Root, filepath.Join(s.Store.Dir(), "semantic-edits"), plan.ID)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fail(err)
		}
		for _, change := range proposed {
			restoration[change.Path] = change
		}
	}
	for _, change := range plan.Changes {
		before, hash, err := lsputil.ReadEditSource(ctx, plan.Root, change.Path)
		if err != nil {
			return fail(err)
		}
		approvedHashes[change.Path] = hash
		if restore, found := restoration[change.Path]; found && restore.BeforeHash != hash {
			return fail(fmt.Errorf("recovery source changed before permission preview"))
		}
		after := change.Content
		if recover {
			after = before
			if restore, found := restoration[change.Path]; found {
				after = restore.Content
			}
		}
		ok, err := s.Permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: GetSessionFromContext(ctx), ToolCallID: call.ID, ToolName: LSPEditPlanToolName, Action: action, Path: change.Path, Description: action + " semantic edit " + change.Operation + ": " + change.Path, Params: SemanticEditPermissionsParams{PlanID: plan.ID, Operation: change.Operation, FilePath: change.Path, OldContent: string(before), NewContent: string(after), DiffRef: meta.DiffRef, Recovery: recover}})
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if !ok {
			return NewPermissionDeniedResponse(s.Permissions), nil
		}
	}
	if err := s.ownership(ctx, plan); err != nil {
		return fail(err)
	}
	for _, change := range plan.Changes {
		_, hash, err := lsputil.ReadEditSource(ctx, plan.Root, change.Path)
		if err != nil || hash != approvedHashes[change.Path] {
			return fail(fmt.Errorf("semantic source changed during permission review: %s", change.Path))
		}
	}
	if !recover {
		if err := lsputil.ValidateEditPlan(ctx, plan.Root, plan); err != nil {
			return fail(err)
		}
	}
	if !recover {
		if err := s.Store.Check(ctx, meta.SessionID, engineering.GetScope(ctx, GetSessionFromContext(ctx)).TaskID); err != nil {
			return fail(err)
		}
	}
	if s.History != nil {
		for _, change := range plan.Changes {
			before, hash, err := lsputil.ReadEditSource(ctx, plan.Root, change.Path)
			if hash == "missing" && err == nil {
				continue
			}
			if err != nil {
				return fail(err)
			}
			if _, err := s.History.CreateVersion(ctx, GetSessionFromContext(ctx), change.Path, string(before), GetMessageFromContext(ctx)); err != nil {
				return fail(err)
			}
		}
	}
	status := "applying"
	if recover {
		status = "recovering"
	}
	record, err := s.mark(ctx, meta, record, status)
	if err != nil {
		return fail(err)
	}
	meta.Status = status
	journalDir := filepath.Join(s.Store.Dir(), "semantic-edits")
	var result lsputil.EditResult
	if recover {
		result, err = lsputil.RecoverEditPlan(ctx, plan.Root, journalDir, plan.ID, plan)
		if errors.Is(err, os.ErrNotExist) && lsputil.ValidateEditPlan(ctx, plan.Root, plan) == nil {
			// Admission can be interrupted before the first journal write. A
			// complete unchanged target set proves no current edit effects;
			// explicit recovery reconciles that intent without replaying it.
			result, err = lsputil.EditResult{PlanID: plan.ID, Status: "rolled_back"}, nil
		}
	} else {
		result, err = lsputil.ApplyEditPlan(ctx, plan.Root, journalDir, plan)
	}
	if result.Status == "" {
		result.Status = status
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	mutation := false
	for _, change := range plan.Changes {
		_, hash, readErr := lsputil.ReadEditSource(cleanup, plan.Root, change.Path)
		mutation = mutation || readErr != nil || hash != approvedHashes[change.Path]
	}
	withMutation := func(response fantasy.ToolResponse) fantasy.ToolResponse {
		return fantasy.WithResponseMetadata(response, struct {
			Mutation bool `json:"semantic_mutation"`
		}{Mutation: mutation})
	}
	if _, markErr := s.mark(cleanup, meta, record, result.Status); markErr != nil {
		return withMutation(fantasy.NewTextErrorResponse(fmt.Sprintf("edit journal needs reconciliation: %v", markErr))), nil
	}
	meta.Status = result.Status
	if s.Tracker != nil {
		for _, path := range append(result.Applied, result.Restored...) {
			s.Tracker.RecordRead(cleanup, GetSessionFromContext(ctx), path)
		}
	}
	if s.Manager != nil {
		if mutation && !recover && result.Status == "applied" {
			for _, change := range plan.Changes {
				if change.Destination == "" || change.Operation != "delete" {
					continue
				}
				if client := findLSPClient(s.Manager, change.Path); client != nil {
					if err := client.DidRenameFiles(cleanup, change.Path, change.Destination); err != nil {
						slog.Warn("Failed to notify LSP after semantic rename", "error", err)
					}
				}
			}
		}
		notifyLSPs(cleanup, s.Manager, "")
	}
	response := withMutation(semanticResponse(meta, plan, &result))
	if err != nil || result.Status == "conflicted" || result.Status == "applying" || result.Status == "recovering" {
		response.IsError = true
		if err != nil {
			response.Content += "\n" + err.Error()
		}
	}
	return response, nil
}

func NewLSPEditPlanTool(services SemanticEditServices) fantasy.AgentTool {
	services = semanticServices(services.Root, services.Permissions, services.History, services.Tracker, services.Manager, []SemanticEditServices{services})
	return fantasy.NewAgentTool(LSPEditPlanToolName, editPlanDescription, func(ctx context.Context, p EditPlanParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		meta, record, plan, err := services.read(ctx, p.PlanID)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		switch p.Action {
		case "inspect":
			if err := lsputil.ValidateEditJournal(ctx, plan.Root, filepath.Join(services.Store.Dir(), "semantic-edits"), plan); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			result, err := lsputil.InspectEditJournal(ctx, plan.Root, filepath.Join(services.Store.Dir(), "semantic-edits"), plan.ID)
			if errors.Is(err, os.ErrNotExist) {
				return semanticResponse(meta, plan, nil), nil
			}
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			return semanticResponse(meta, plan, &result), nil
		case "apply":
			return services.apply(ctx, meta, record, plan, call, false)
		case "recover":
			return services.apply(ctx, meta, record, plan, call, true)
		default:
			return fantasy.NewTextErrorResponse("use inspect, apply or recover"), nil
		}
	})
}

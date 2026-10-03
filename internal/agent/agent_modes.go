package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
)

// AgentBatchItem identifies one independently observable unit of work.
type AgentBatchItem = engineering.AgentBatchItem

type agentBatchRow = engineering.AgentBatchRow

type agentBatchReport = engineering.AgentBatchReport

func (c *coordinator) runAgentMode(ctx context.Context, args AgentParams, p agentToolCallParams) (fantasy.ToolResponse, error) {
	switch args.Mode {
	case "", "single":
		if args.RetryFailed || len(args.Items) > 0 {
			return fantasy.NewTextErrorResponse("batch items and retry_failed require mode=batch"), nil
		}
		return c.runAgentToolCall(ctx, p)
	case "batch":
		if args.RetryFailed && len(args.Items) == 0 {
			if c.engineering == nil {
				return fantasy.NewTextErrorResponse("batch requires durable storage"), nil
			}
			_, data, err := c.engineering.ReadRecord(ctx, engineering.AgentBatchNamespace(p.sessionID), args.BatchID)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			var report agentBatchReport
			if err := json.Unmarshal(data, &report); err != nil {
				return fantasy.ToolResponse{}, err
			}
			if err := json.Unmarshal(report.Request, &args); err != nil {
				return fantasy.ToolResponse{}, err
			}
			args.RetryFailed = true
			p.agentName, p.auto, p.qualityOnly, p.workspaceID = args.AgentName, args.Auto, args.QualityOnly, args.WorkspaceID
			p.route = subagents.RouteRequest{TaskType: args.TaskType, RequiredTools: args.RequiredTools, Output: args.ExpectedOutput}
		}
		return c.runAgentBatch(ctx, args, p)
	case "architect_edit":
		return c.runArchitectEditor(ctx, args, p)
	default:
		return fantasy.NewTextErrorResponse("mode must be single, batch or architect_edit"), nil
	}
}

func validateAgentBatch(args AgentParams) error {
	if strings.TrimSpace(args.BatchID) == "" || strings.ContainsAny(args.BatchID, "\x00\r\n") || len(args.BatchID) > 128 || len(args.Items) == 0 || len(args.Items) > 128 || len(args.Prompt) > 8192 || !strings.Contains(args.Prompt, "{{item}}") || args.SessionKey != "" {
		return fmt.Errorf("batch requires an ID (at most 128 characters), 1–128 items, a prompt template (at most 8192 bytes) containing {{item}}, and no shared session_key")
	}
	seen := map[string]bool{}
	for _, item := range args.Items {
		if item.ID == "" || len(item.ID) > 128 || seen[item.ID] || strings.TrimSpace(item.Input) == "" || len(item.Input) > 8192 {
			return fmt.Errorf("batch item IDs must be unique and bounded; each input must contain 1–8192 bytes")
		}
		seen[item.ID] = true
	}
	return nil
}

// Batches persist each transition before running a row. Interrupted work stays
// running and requires explicit reconciliation instead of automatic replay.
// Serial execution preserves write ordering and still shares the runner cap.
func (c *coordinator) runAgentBatch(ctx context.Context, args AgentParams, p agentToolCallParams) (fantasy.ToolResponse, error) {
	if err := validateAgentBatch(args); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	if c.engineering == nil {
		return fantasy.NewTextErrorResponse("batch requires durable storage"), nil
	}
	ns := engineering.AgentBatchNamespace(p.sessionID)
	release, err := c.engineering.WorkflowLock(ctx, ns+"\x00"+args.BatchID)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	defer release()
	identityArgs := args
	identityArgs.RetryFailed = false
	identity, err := json.Marshal(struct {
		Args   AgentParams
		Roles  []*subagents.Subagent
		Models map[config.SelectedModelType]config.SelectedModel
		Tools  []string
		Scope  engineering.Scope
	}{identityArgs, p.discovered, c.cfg.Config().Models, p.availableTools, engineering.GetScope(ctx, p.sessionID)})
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	record, data, err := c.engineering.ReadRecord(ctx, ns, args.BatchID)
	report := agentBatchReport{ID: args.BatchID, Fingerprint: engineering.Hash(string(identity))}
	request, marshalErr := json.Marshal(identityArgs)
	if marshalErr != nil {
		return fantasy.ToolResponse{}, marshalErr
	}
	report.Request = request
	if err == nil {
		if err = json.Unmarshal(data, &report); err != nil {
			return fantasy.ToolResponse{}, err
		}
		if report.Fingerprint != engineering.Hash(string(identity)) {
			return fantasy.NewTextErrorResponse("batch definition changed; use a new batch_id"), nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fantasy.ToolResponse{}, err
	} else {
		records, err := c.engineering.ListRecords(ctx, ns)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if len(records) >= 64 {
			return fantasy.NewTextErrorResponse("session batch limit reached (64)"), nil
		}
		if args.RetryFailed {
			return fantasy.NewTextErrorResponse("cannot retry an unknown batch"), nil
		}
		for _, item := range args.Items {
			report.Rows = append(report.Rows, agentBatchRow{AgentBatchItem: item, Status: "pending"})
		}
	}
	save := func() error {
		data, err := json.Marshal(report)
		if err != nil {
			return err
		}
		record, err = c.engineering.PutRecordStrict(ctx, ns, args.BatchID, record.Revision, data)
		return err
	}
	for i := range report.Rows {
		row := &report.Rows[i]
		if row.Status != "pending" && (!args.RetryFailed || row.Status != "failed") {
			continue
		}
		if err := ctx.Err(); err != nil {
			return fantasy.ToolResponse{}, err
		}
		row.Status = "running"
		row.Attempts++
		row.UpdatedAt = time.Now().UnixMilli()
		if err := save(); err != nil {
			return fantasy.ToolResponse{}, err
		}
		call := p
		call.prompt = strings.ReplaceAll(args.Prompt, "{{item}}", row.Input)
		call.toolCallID = fmt.Sprintf("%s-%s-%d", p.toolCallID, engineering.Hash(row.ID)[:16], row.Attempts)
		resp, runErr := c.runAgentToolCall(ctx, call)
		if ctx.Err() != nil {
			return fantasy.ToolResponse{}, ctx.Err()
		}
		row.Status = "succeeded"
		row.Output = resp.Content
		if runErr != nil {
			row.Status = "failed"
			row.Output = runErr.Error()
		} else if resp.IsError {
			row.Status = "failed"
		}
		if len(row.Output) > 16384 {
			row.Output = row.Output[:16384] + "\n[Output truncated]"
		}
		row.UpdatedAt = time.Now().UnixMilli()
		if err := save(); err != nil {
			return fantasy.ToolResponse{}, err
		}
	}
	data, err = json.Marshal(report)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	return fantasy.NewTextResponse(string(data)), nil
}

func (c *coordinator) runArchitectEditor(ctx context.Context, args AgentParams, p agentToolCallParams) (fantasy.ToolResponse, error) {
	if len(args.OwnedPaths) == 0 {
		return fantasy.NewTextErrorResponse("architect_edit requires explicit owned_paths"), nil
	}
	if err := session.ValidateTaskGraph([]session.Todo{{ID: "architect-edit", OwnedPaths: args.OwnedPaths}}); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	scope := engineering.GetScope(ctx, p.sessionID)
	for _, requested := range args.OwnedPaths {
		allowed := len(scope.OwnedPaths) == 0
		for _, owner := range scope.OwnedPaths {
			owner = path.Clean(strings.ReplaceAll(owner, `\`, "/"))
			candidate := path.Clean(strings.ReplaceAll(requested, `\`, "/"))
			allowed = allowed || owner == "." || candidate == owner || strings.HasPrefix(candidate, owner+"/")
		}
		if !allowed {
			return fantasy.NewTextErrorResponse("architect_edit cannot expand parent ownership"), nil
		}
	}
	root := scope.WriteRoot
	if root == "" {
		root = c.cfg.WorkingDir()
	}
	ctx = engineering.WithScope(ctx, scope.SessionID, scope.TaskID)
	ctx = engineering.WithOwnership(ctx, root, args.OwnedPaths)
	if args.Architect == "" || args.Editor == "" || args.Architect == args.Editor || args.SessionKey != "" || args.WorkspaceID != "" || args.QualityOnly {
		return fantasy.NewTextErrorResponse("architect_edit requires different named architect and editor roles; shared session_key and workspace_id are unsupported"), nil
	}
	architect, ok := subagents.Find(p.discovered, args.Architect)
	if !ok {
		return fantasy.NewTextErrorResponse("unknown architect"), nil
	}
	editorRole, ok := subagents.Find(p.discovered, args.Editor)
	if !ok {
		return fantasy.NewTextErrorResponse("unknown editor"), nil
	}
	if editorRole.ReadOnly {
		return fantasy.NewTextErrorResponse("editor role must support file editing"), nil
	}
	copy := *architect
	copy.ReadOnly = true
	copy.AllowCommands = false
	p.discovered = append([]*subagents.Subagent{&copy}, p.discovered...)
	// A fresh cache ensures the enforced read-only role cannot reuse a writable
	// instance from an earlier ordinary invocation.
	p.subagentInstances = csync.NewMap[string, SessionAgent]()
	architectWorker, err := c.buildSubagentSessionAgent(ctx, p.agentCfg, &copy)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	architectWorker.SetHooks(nil, nil, nil)
	p.subagentInstances.Set(args.Architect, architectWorker)
	p.route = subagents.RouteRequest{}
	p.agentName = args.Architect
	p.auto = false
	p.toolCallID += "-architect"
	p.prompt = "Design a concrete implementation plan for the following task. Inspect files using read-only tools. Do not execute commands or edit files. Specify exact files, bounded changes, acceptance criteria and verification.\n\n" + args.Prompt
	if c.engineering == nil {
		return fantasy.NewTextErrorResponse("architect_edit requires ownership guards and durable storage"), nil
	}
	source, err := engineering.SourceFingerprint(ctx, root, c.engineering.Dir())
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	plan, err := c.runAgentToolCall(ctx, p)
	if err != nil || plan.IsError {
		return plan, err
	}
	if len(plan.Content) > 65536 {
		return fantasy.NewTextErrorResponse("architect plan exceeds 64 KiB; narrow the task"), nil
	}
	current, err := engineering.SourceFingerprint(ctx, root, c.engineering.Dir())
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	if current != source {
		return fantasy.NewTextErrorResponse("project changed while the architect was planning; rebuild the plan before editing"), nil
	}
	editor, err := c.buildSubagentSessionAgent(ctx, p.agentCfg, editorRole)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	concrete, ok := editor.(*sessionAgent)
	if !ok {
		return fantasy.NewTextErrorResponse("editor tool restrictions unavailable"), nil
	}
	allowed := append(slices.Clone(subagents.ReadTools), "edit", "write", "multiedit")
	var editorTools []fantasy.AgentTool
	for _, tool := range concrete.tools.Copy() {
		if slices.Contains(allowed, tool.Info().Name) {
			editorTools = append(editorTools, tool)
		}
	}
	editor.SetTools(editorTools)
	editor.SetHooks(nil, nil, nil)
	p.subagentInstances.Set(args.Editor, editor)
	p.agentName = args.Editor
	p.toolCallID += "-editor"
	p.prompt = "Implement only the bounded file edits described in the architect plan within these owned paths: " + strings.Join(args.OwnedPaths, ", ") + ". Treat the plan as proposals: confirm them against current files and project instructions. Stop and report contradictions rather than broadening scope. Commands, MCP and delegation are disabled in this editing stage. Report files changed and checks that the coordinator must execute independently.\n\nOriginal task:\n" + args.Prompt + "\n\nArchitect plan:\n" + plan.Content
	result, err := c.runAgentToolCall(ctx, p)
	if err != nil {
		return result, err
	}
	return fantasy.WithResponseMetadata(result, struct {
		Architect string `json:"architect"`
		Editor    string `json:"editor"`
		Plan      string `json:"plan"`
	}{args.Architect, args.Editor, plan.Content}), nil
}

package tools

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/procown"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/transfer"
)

const RemoteNodeToolName = "remote_node"

//go:embed remote_node.md
var remoteNodeDescription string

// RemoteNodeParams is deliberately flat; plan steps are one level of flat
// objects.
type RemoteNodeParams struct {
	Action      string           `json:"action" enum:"list,capabilities,exec,push,pull,status,cancel,plan" description:"list paired nodes; capabilities of one node; exec a literal argv; push/pull one file; status/cancel an operation_id; plan runs up to 16 steps across nodes."`
	Node        string           `json:"node,omitempty" description:"Paired node name from list (e.g. wsl). Required except for list and plan."`
	Argv        []string         `json:"argv,omitempty" description:"exec: literal program and arguments, no shell. Use [\"bash\",\"-lc\",\"...\"] only when shell syntax is needed."`
	Cwd         string           `json:"cwd,omitempty" description:"exec: directory relative to the node root."`
	TimeoutMS   int64            `json:"timeout_ms,omitempty" description:"exec: limit in milliseconds (default 600000, max 3600000)."`
	LocalPath   string           `json:"local_path,omitempty" description:"push source / pull destination, relative to this workspace."`
	RemotePath  string           `json:"remote_path,omitempty" description:"push destination / pull source, relative to the node root."`
	Overwrite   bool             `json:"overwrite,omitempty" description:"push/pull: replace an existing file."`
	OperationID string           `json:"operation_id,omitempty" description:"status/cancel: operation_id from an earlier result."`
	Steps       []nodes.PlanStep `json:"steps,omitempty" description:"plan: steps {id,node,action(exec|push|pull),argv,cwd,timeout_ms,local_path,remote_path,overwrite,depends_on}. node may be \"local\" for exec on this computer."`
}

const remoteNodeExample = `{"action":"exec","node":"wsl","argv":["go","test","./..."],"cwd":"."}`

type remoteNodeTool struct {
	permissions permission.Service
	workingDir  string
	registry    *nodes.Registry
	runsDir     string
}

// NewRemoteNodeTool builds the cross-computer tool over paired nodes.
func NewRemoteNodeTool(permissions permission.Service, workingDir, devicesDir string) fantasy.AgentTool {
	t := &remoteNodeTool{permissions: permissions, workingDir: workingDir, registry: nodes.OpenRegistry(devicesDir), runsDir: filepath.Join(devicesDir, "runs")}
	return fantasy.NewAgentTool(RemoteNodeToolName, remoteNodeDescription, func(ctx context.Context, _ RemoteNodeParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p RemoteNodeParams
		if err := strictDecode(call.Input, &p, remoteNodeExample); err != nil {
			return deviceErrorResponse(err), nil
		}
		resp, err := t.run(ctx, p, call)
		if err != nil {
			return deviceErrorResponse(err), nil
		}
		return resp, nil
	})
}

func (t *remoteNodeTool) approve(ctx context.Context, call fantasy.ToolCall, action, description string, params any) error {
	if t.permissions == nil {
		return nil
	}
	ok, err := t.permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: GetSessionFromContext(ctx), ToolCallID: call.ID, ToolName: RemoteNodeToolName, Action: action, Description: description, Params: params, Path: t.workingDir})
	if err != nil {
		return err
	}
	if !ok {
		return deviceErr("permission_denied", "", "the user denied this remote action; nothing was sent", "Do not retry the same action unless the user asks.")
	}
	return nil
}

func (t *remoteNodeTool) run(ctx context.Context, p RemoteNodeParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	switch p.Action {
	case "list":
		entries, err := t.registry.List()
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		out := make([]map[string]any, 0, len(entries)+1)
		out = append(out, map[string]any{"name": "local", "os": runtimeOS(), "note": "this computer; usable as node \"local\" in plan exec steps"})
		for _, e := range entries {
			out = append(out, map[string]any{"name": e.Name, "node_id": e.NodeID, "os": e.OS, "arch": e.Arch, "network": e.Network, "capabilities": e.Capabilities, "root": e.Root, "isolated": e.Isolated})
		}
		hint := ""
		if len(entries) == 0 {
			hint = "No nodes are paired. Ask the user to run \"atlas node serve --root <dir>\" on the other computer and then the printed \"atlas node pair\" command here. Atlas never installs anything for this."
		}
		return deviceResponse(map[string]any{"ok": true, "nodes": out, "hint": hint}), nil
	case "capabilities":
		client, entry, err := t.client(p.Node)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		info, err := client.Info(ctx)
		if err != nil {
			return fantasy.ToolResponse{}, classifyNodeErr(err, entry.Name)
		}
		return deviceResponse(map[string]any{"ok": true, "node": entry.Name, "info": info, "network": entry.Network}), nil
	case "exec", "push", "pull":
		step := nodes.PlanStep{ID: "single", Node: p.Node, Action: p.Action, Argv: p.Argv, Cwd: p.Cwd, TimeoutMS: p.TimeoutMS, LocalPath: p.LocalPath, RemotePath: p.RemotePath, Overwrite: p.Overwrite}
		if p.Node == "local" {
			return fantasy.ToolResponse{}, deviceErr("invalid_target", "node", "single-step actions address a paired node; run local commands with bash", "Use bash for this computer, or node \"local\" inside a plan.")
		}
		if err := nodes.ValidatePlan([]nodes.PlanStep{step}); err != nil {
			return fantasy.ToolResponse{}, deviceErr("invalid_request", "", err.Error(), "Check the action's required fields.")
		}
		if err := t.approve(ctx, call, p.Action, describeStep(step), p); err != nil {
			return fantasy.ToolResponse{}, err
		}
		res := t.runStep(ctx, GetSessionFromContext(ctx), call.ID, step)
		resp := deviceResponse(map[string]any{"ok": res.State == taskstate.Completed, "result": res})
		resp.IsError = res.State != taskstate.Completed
		return resp, nil
	case "status", "cancel":
		if p.OperationID == "" {
			return fantasy.ToolResponse{}, deviceErr("invalid_request", "operation_id", "status and cancel need operation_id", "Use the operation_id from a previous result.")
		}
		client, entry, err := t.client(p.Node)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		var op nodes.Operation
		if p.Action == "cancel" {
			if err := t.approve(ctx, call, "cancel", "Cancel operation "+p.OperationID+" on node "+entry.Name, p); err != nil {
				return fantasy.ToolResponse{}, err
			}
			op, err = client.Cancel(ctx, p.OperationID)
		} else {
			op, err = client.Operation(ctx, p.OperationID)
		}
		if err != nil {
			return fantasy.ToolResponse{}, classifyNodeErr(err, entry.Name)
		}
		return deviceResponse(map[string]any{"ok": true, "node": entry.Name, "node_id": entry.NodeID, "operation": op}), nil
	case "plan":
		return t.plan(ctx, p, call)
	}
	return fantasy.ToolResponse{}, deviceErr("invalid_action", "action", fmt.Sprintf("unknown action %q", p.Action), "Use list, capabilities, exec, push, pull, status, cancel or plan.")
}

func runtimeOS() string { return runtime.GOOS }

func (t *remoteNodeTool) client(name string) (*nodes.Client, nodes.Entry, error) {
	if name == "" {
		return nil, nodes.Entry{}, deviceErr("invalid_request", "node", "node is required", "Call remote_node list to see paired node names.")
	}
	c, e, err := t.registry.ClientFor(name)
	if err != nil {
		return nil, e, deviceErr("unknown_node", "node", err.Error(), "Call remote_node list; pair new nodes with \"atlas node pair\".")
	}
	return c, e, nil
}

func describeStep(s nodes.PlanStep) string {
	switch s.Action {
	case "exec":
		return fmt.Sprintf("Run on %s: %s (cwd %q)", s.Node, strings.Join(s.Argv, " "), s.Cwd)
	case "push":
		return fmt.Sprintf("Copy %s from this workspace to %s:%s", s.LocalPath, s.Node, s.RemotePath)
	default:
		return fmt.Sprintf("Copy %s:%s into this workspace at %s", s.Node, s.RemotePath, s.LocalPath)
	}
}

// classifyNodeErr turns client errors into the tool contract.
func classifyNodeErr(err error, node string) error {
	var api *nodes.APIError
	if errors.As(err, &api) {
		return &DeviceToolError{Code: api.Code, Message: api.Message, InputSent: api.InputSent, Effect: orStr(api.Effect, "none"), NextStep: nodeNextStep(api.Code)}
	}
	var pin *nodes.PinError
	if errors.As(err, &pin) {
		return &DeviceToolError{Code: "fingerprint_mismatch", Message: pin.Error(), Effect: "none", NextStep: "Do not continue. The node's certificate changed; the user must re-pair after verifying the node."}
	}
	var conn *nodes.ConnError
	if errors.As(err, &conn) {
		effect := "none"
		if conn.Sent {
			effect = "unknown"
		}
		return &DeviceToolError{Code: "node_unreachable", Message: node + ": " + conn.Error(), InputSent: conn.Sent, Effect: effect, NextStep: "Check that \"atlas node serve\" is running on the node; if an operation_id was returned, reconcile it with status before retrying."}
	}
	return err
}

func nodeNextStep(code string) string {
	switch code {
	case "unauthorized":
		return "This computer's token was revoked or is unknown; the user must pair again."
	case "capability_denied":
		return "The node operator did not grant this capability; ask the user to restart the node with --allow."
	case "stale_observation":
		return "Observe again and aim using the new observation."
	case "operation_conflict":
		return "This operation_id already ran with different arguments; use status to read it."
	}
	return ""
}

func orStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// runStep executes one step with reconciliation: transport failures are
// retried with the same operation_id (the node deduplicates), and
// cancellation is forwarded to the node.
func (t *remoteNodeTool) runStep(ctx context.Context, sessionID, callID string, step nodes.PlanStep) nodes.StepResult {
	opID := nodes.OperationID(sessionID, callID, step.ID)
	res := nodes.StepResult{ID: step.ID, Node: step.Node, Action: step.Action, OperationID: opID, Effect: "none", StartedAt: time.Now().UnixMilli()}
	if step.Node == "local" {
		return t.runLocalOnce(ctx, sessionID, step, res)
	}
	client, entry, err := t.client(step.Node)
	if err != nil {
		return failStep(res, err)
	}
	res.NodeID, res.OS = entry.NodeID, entry.OS
	var lastErr error
	for attempt := range 3 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		if ctx.Err() != nil {
			break
		}
		switch step.Action {
		case "exec":
			var op nodes.Operation
			op, lastErr = client.Exec(ctx, nodes.ExecRequest{OperationID: opID, Argv: step.Argv, Cwd: step.Cwd, TimeoutMS: step.TimeoutMS})
			if lastErr == nil {
				return opResult(res, op)
			}
		case "push":
			local, rerr := transfer.Resolve(t.workingDir, step.LocalPath)
			if rerr != nil {
				return failStep(res, deviceErr(transfer.Code(rerr), "local_path", rerr.Error(), "local_path must stay inside this workspace."))
			}
			var fr nodes.FileResult
			fr, lastErr = client.Push(ctx, opID, local, step.RemotePath, step.Overwrite, entry.MaxFileBytes)
			if lastErr == nil {
				res.State, res.Effect, res.Path, res.SHA256, res.Replayed = taskstate.Completed, "applied", fr.Path, fr.SHA256, fr.Replayed
				return res
			}
		case "pull":
			var fr nodes.FileResult
			fr, lastErr = client.Pull(ctx, step.RemotePath, t.workingDir, step.LocalPath, step.Overwrite, entry.MaxFileBytes)
			if lastErr == nil {
				res.State, res.Effect, res.Path, res.SHA256, res.Replayed = taskstate.Completed, "applied", fr.Path, fr.SHA256, fr.Replayed
				return res
			}
		}
		var conn *nodes.ConnError
		if !errors.As(lastErr, &conn) {
			break // API errors are final; only transport failures are retried.
		}
	}
	if ctx.Err() != nil && step.Action == "exec" {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if op, err := client.Cancel(cctx, opID); err == nil {
			out := opResult(res, op)
			if out.State != taskstate.Completed {
				out.State = taskstate.Canceled
			}
			return out
		}
		res.State, res.Effect = taskstate.Canceled, "unknown"
		res.Error = "canceled; the node could not be reached to confirm cancellation"
		res.ErrorCode = "cancel_unconfirmed"
		return res
	}
	return failStep(res, classifyNodeErr(lastErr, step.Node))
}

func opResult(res nodes.StepResult, op nodes.Operation) nodes.StepResult {
	res.State, res.Effect, res.ExitCode = op.State, op.Effect, op.ExitCode
	res.Stdout, res.Stderr, res.Error, res.Replayed = op.Stdout, op.Stderr, op.Error, op.Replayed
	if op.StartedAt != 0 {
		res.StartedAt = op.StartedAt
	}
	res.EndedAt = op.EndedAt
	return res
}

func failStep(res nodes.StepResult, err error) nodes.StepResult {
	res.State = taskstate.Failed
	res.Error = err.Error()
	var de *DeviceToolError
	if errors.As(err, &de) {
		res.ErrorCode, res.Effect, res.Error = de.Code, de.Effect, de.Message
	}
	res.EndedAt = time.Now().UnixMilli()
	return res
}

func (t *remoteNodeTool) runLocal(ctx context.Context, step nodes.PlanStep, res nodes.StepResult) nodes.StepResult {
	if err := ctx.Err(); err != nil {
		res = failStep(res, err)
		res.State = taskstate.Canceled
		return res
	}
	res.NodeID, res.OS = "local", runtimeOS()
	dir := t.workingDir
	if step.Cwd != "" && step.Cwd != "." {
		d, err := transfer.Resolve(t.workingDir, step.Cwd)
		if err != nil {
			return failStep(res, err)
		}
		dir = d
	}
	limit := 10 * time.Minute
	if step.TimeoutMS > 0 {
		limit = min(time.Duration(step.TimeoutMS)*time.Millisecond, time.Hour)
	}
	var stdout, stderr strings.Builder
	proc, err := procown.Start(procown.Spec{Argv: step.Argv, Dir: dir, Stdout: &limitedWriter{w: &stdout, n: 256 << 10}, Stderr: &limitedWriter{w: &stderr, n: 256 << 10}})
	if err != nil {
		return failStep(res, err)
	}
	cctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	code, werr := proc.Wait(cctx)
	res.Effect, res.ExitCode = "applied", &code
	res.Stdout, res.Stderr = stdout.String(), stderr.String()
	res.EndedAt = time.Now().UnixMilli()
	switch {
	case ctx.Err() != nil:
		res.State, res.Error, res.Effect = taskstate.Canceled, "canceled; process tree stopped", "unknown"
	case errors.Is(cctx.Err(), context.DeadlineExceeded):
		res.State, res.Error = taskstate.Failed, "timed out; process tree stopped"
	case code != 0:
		res.State, res.Error = taskstate.Failed, fmt.Sprintf("exit status %d", code)
	case werr != nil:
		res.State, res.Error = taskstate.Failed, werr.Error()
	default:
		res.State = taskstate.Completed
	}
	return res
}

type limitedWriter struct {
	w interface{ WriteString(string) (int, error) }
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		chunk := p
		if len(chunk) > l.n {
			chunk = chunk[:l.n]
		}
		_, _ = l.w.WriteString(string(chunk))
		l.n -= len(chunk)
	}
	return len(p), nil
}

// planRecord is persisted before a plan result is reported.
type planRecord struct {
	ID        string           `json:"id"`
	SessionID string           `json:"session_id"`
	CallID    string           `json:"call_id"`
	Steps     []nodes.PlanStep `json:"steps"`
	Result    nodes.PlanResult `json:"result"`
	UpdatedAt int64            `json:"updated_at"`
}

func (t *remoteNodeTool) plan(ctx context.Context, p RemoteNodeParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	if err := nodes.ValidatePlan(p.Steps); err != nil {
		return fantasy.ToolResponse{}, deviceErr("invalid_plan", "steps", err.Error(), "Fix the plan; nothing was run.")
	}
	// Resolve every node before running anything, so a typo cannot leave
	// half a plan executed.
	for _, s := range p.Steps {
		if s.Node == "local" {
			continue
		}
		if _, _, err := t.registry.Get(s.Node); err != nil {
			return fantasy.ToolResponse{}, deviceErr("unknown_node", "steps", fmt.Sprintf("step %s: %v", s.ID, err), "Call remote_node list; nothing was run.")
		}
	}
	var lines []string
	for _, s := range p.Steps {
		dep := ""
		if len(s.DependsOn) > 0 {
			dep = " after " + strings.Join(s.DependsOn, ",")
		}
		lines = append(lines, s.ID+": "+describeStep(s)+dep)
	}
	if err := t.approve(ctx, call, "plan", "Run a cross-device plan:\n"+strings.Join(lines, "\n"), p); err != nil {
		return fantasy.ToolResponse{}, err
	}
	sessionID := GetSessionFromContext(ctx)
	rec := planRecord{ID: nodes.OperationID(sessionID, call.ID, "plan"), SessionID: sessionID, CallID: call.ID, Steps: p.Steps}
	path := filepath.Join(t.runsDir, rec.ID+".json")
	persist := func() error {
		rec.UpdatedAt = time.Now().UnixMilli()
		return taskstate.WriteJSONAtomic(path, rec)
	}
	if err := persist(); err != nil {
		return fantasy.ToolResponse{}, deviceErr("persist_failed", "", "cannot record the plan before running it: "+err.Error(), "Check disk space and permissions.")
	}
	result := nodes.RunPlan(ctx, p.Steps, 4, func(ctx context.Context, s nodes.PlanStep) nodes.StepResult {
		return t.runStep(ctx, sessionID, call.ID, s)
	}, nil)
	rec.Result = result
	if err := persist(); err != nil {
		// The outcome is real but not durable; never report it as completed.
		result.State = taskstate.Failed
		resp := deviceResponse(map[string]any{"ok": false, "plan_id": rec.ID, "result": result, "error": "plan finished but its result could not be persisted: " + err.Error()})
		resp.IsError = true
		return resp, nil
	}
	resp := deviceResponse(map[string]any{"ok": result.State == taskstate.Completed, "plan_id": rec.ID, "summary": result.Summary(), "result": result})
	resp.IsError = result.State != taskstate.Completed
	return resp, nil
}

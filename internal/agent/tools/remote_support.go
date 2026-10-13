package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/support"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/transfer"
)

// RemoteSupportToolName is the tool that works on the computer connected
// through /support.
const RemoteSupportToolName = "remote_support"

//go:embed remote_support.md
var remoteSupportDescription string

// RemoteSupportParams is flat so every provider sees the same schema.
type RemoteSupportParams struct {
	Action string `json:"action" enum:"status,system_info,list,exec,verify_install,push,pull,observe,automation,input,launch,tool,operation_status,cancel,request_permission,permission_status" description:"status: the connected computer and its grants. system_info: read-only diagnostics. list: a folder. exec: literal argv. verify_install: independently probe a recorded installation. push/pull: send or fetch one file (SHA-256 verified). observe/automation: read the remote screen. input/launch/tool: control the remote desktop or built-ins. operation_status/cancel: reconcile or stop an operation. request_permission/permission_status: ask the computer owner for more."`
	// DeviceID must name the connected computer for every action except
	// status, so a call can never land on another computer.
	DeviceID       string         `json:"device_id,omitempty" description:"remote_device_id from status. Required for every action except status; a different id is refused."`
	Argv           []string       `json:"argv,omitempty" description:"exec/launch: literal program and arguments (no shell)."`
	Cwd            string         `json:"cwd,omitempty" description:"exec: allowed folder label and path, e.g. \"Work/logs\"."`
	TimeoutMS      int64          `json:"timeout_ms,omitempty" description:"exec: limit in milliseconds (default 600000)."`
	RemotePath     string         `json:"remote_path,omitempty" description:"list/push/pull: path starting with an allowed folder label, e.g. \"Desktop/report.pdf\". Empty for list shows the folders."`
	LocalPath      string         `json:"local_path,omitempty" description:"push source / pull destination on this computer, relative to this workspace."`
	Conflict       string         `json:"conflict,omitempty" enum:"error,rename,overwrite" description:"push/pull when the destination exists: error (default), rename, or overwrite (push needs the owner's file_modify grant)."`
	OperationID    string         `json:"operation_id,omitempty" description:"operation_status/cancel: operation_id from an earlier result."`
	SessionID      string         `json:"session_id,omitempty" description:"Bind this call to the session_id from status. Required in saved workflows; a new session on the same device is refused."`
	Generation     uint64         `json:"generation,omitempty" description:"tool: current generation from status. Old workflow/observation generations are refused."`
	Tool           string         `json:"tool,omitempty" enum:"browser,computer,tool_pipeline" description:"tool: run this built-in on the supported computer, including native browser workflows and durable desktop flows."`
	Arguments      map[string]any `json:"arguments,omitempty" description:"tool: the selected built-in's usual arguments as an object, not a JSON string. Only host browser/computer actions are allowed inside a remote pipeline."`
	VerifyArgv     []string       `json:"verify_argv,omitempty" description:"verify_install: independent literal argv probe, such as the installed application's --version or winget list --id. Installer commands are rejected."`
	ExpectedOutput string         `json:"expected_output,omitempty" description:"verify_install: required nonempty exact text expected in the probe stdout (application identity/version). Exit code zero alone is never proof."`
	// Desktop fields.
	Image         bool   `json:"image,omitempty" description:"observe: include the PNG (default false: geometry, windows and cursor only)."`
	ObservationID string `json:"observation_id,omitempty" description:"input: observation_id of the latest observe; input aimed with an older one is refused."`
	WindowID      string `json:"window_id,omitempty" description:"input/automation: remote window id from observe (never a local one)."`
	InputAction   string `json:"input_action,omitempty" enum:"click,double_click,right_click,move,drag,scroll,type,key,hotkey" description:"input: what to do."`
	X             int    `json:"x,omitempty" description:"input: remote screen x in the observation's coordinate space."`
	Y             int    `json:"y,omitempty" description:"input: remote screen y."`
	EndX          int    `json:"end_x,omitempty" description:"input drag: end x."`
	EndY          int    `json:"end_y,omitempty" description:"input drag: end y."`
	Text          string `json:"text,omitempty" description:"input type: text to type."`
	Key           string `json:"key,omitempty" description:"input key/hotkey: key name."`
	Modifiers     string `json:"modifiers,omitempty" description:"input hotkey: modifiers such as \"ctrl+shift\"."`
	ScrollY       int    `json:"scroll_y,omitempty" description:"input scroll: vertical notches, positive up."`
	Query         string `json:"query,omitempty" enum:"windows,inspect,tree,find,monitors" description:"automation: read-only accessibility query. inspect reads the tree; tree is a compatibility alias."`
	Name          string `json:"name,omitempty" description:"automation find: element name."`
	Role          string `json:"role,omitempty" description:"automation find: element role."`
	// Permission fields.
	Capabilities []string `json:"capabilities,omitempty" description:"request_permission: capabilities to ask the computer owner for."`
	Reason       string   `json:"reason,omitempty" description:"request_permission: why, in words the owner understands."`
	PermissionID string   `json:"permission_id,omitempty" description:"permission_status: id from request_permission."`
}

const remoteSupportExample = `{"action":"system_info","device_id":"node-0123456789abcdef"}`

// SupportControllerFunc returns the active supporting end, or nil when this
// computer is not supporting another one.
type SupportControllerFunc func() *support.Controller

type remoteSupportTool struct {
	permissions permission.Service
	workingDir  string
	controller  SupportControllerFunc
	// reconcileWait bounds how long an unknown outcome waits for the
	// connection to come back before it is reported unreconciled.
	reconcileWait time.Duration
}

// NewRemoteSupportTool builds the tool over the /support controller. It
// never falls back to this computer: without a connected computer every
// action fails with no_support_target.
func NewRemoteSupportTool(permissions permission.Service, workingDir string, controller SupportControllerFunc) fantasy.AgentTool {
	t := &remoteSupportTool{permissions: permissions, workingDir: workingDir, controller: controller, reconcileWait: 15 * time.Second}
	return fantasy.NewAgentTool(RemoteSupportToolName, remoteSupportDescription, t.handle)
}

func (t *remoteSupportTool) handle(ctx context.Context, _ RemoteSupportParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	var p RemoteSupportParams
	if err := strictDecode(call.Input, &p, remoteSupportExample); err != nil {
		return withToolOutcome(deviceErrorResponse(err), ToolOutcome{Dispatch: "none", Kind: outcome.KindObservation, Claim: "invalid_request"}), nil
	}
	resp := t.run(ctx, p, call)
	if t.controller != nil {
		var res supportResult
		if json.Unmarshal([]byte(resp.Content), &res) == nil {
			record(t.controller(), p, res)
		}
	}
	return resp, nil
}

// supportResult is the envelope of every result: the target computer, the
// operation, whether anything was sent, whether it applied and whether the
// result is verified.
type supportResult struct {
	OK             bool             `json:"ok"`
	Action         string           `json:"action"`
	Device         supportDevice    `json:"device"`
	OperationID    string           `json:"operation_id,omitempty"`
	InputSent      bool             `json:"input_sent"`
	Effect         support.Effect   `json:"effect"`
	Verified       string           `json:"verification"`
	Result         any              `json:"result,omitempty"`
	Error          *support.Error   `json:"error,omitempty"`
	Checkpoint     *supportCheck    `json:"checkpoint,omitempty"`
	Reconciled     *nodes.Operation `json:"reconciled,omitempty"`
	Note           string           `json:"note,omitempty"`
	Image          []byte           `json:"-"`
	ImageMediaType string           `json:"-"`
	NativeMetadata string           `json:"metadata,omitempty"`
	StopTurn       bool             `json:"waiting_for_user,omitempty"`
}

type supportDevice struct {
	RemoteDeviceID string `json:"remote_device_id,omitempty"`
	Name           string `json:"name,omitempty"`
	Fingerprint    string `json:"fingerprint,omitempty"`
	OS             string `json:"os,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	Generation     uint64 `json:"generation,omitempty"`
}

// supportCheck tells a resumed task what to check before continuing.
type supportCheck struct {
	SessionID      string `json:"session_id"`
	RemoteDeviceID string `json:"remote_device_id"`
	Generation     uint64 `json:"generation"`
	OperationID    string `json:"operation_id,omitempty"`
	ReconcileWith  string `json:"reconcile_with,omitempty"`
}

func deviceOf(r support.Remote) supportDevice {
	return supportDevice{RemoteDeviceID: r.RemoteDeviceID, Name: r.RemoteName, Fingerprint: support.ShortFingerprint(r.RemoteFingerprint), OS: r.OS, SessionID: r.SessionID, Generation: r.Generation}
}

var remoteSupportMutations = []string{"exec", "verify_install", "push", "pull", "input", "launch", "cancel", "tool"}

func remoteSupportMutates(action string) bool { return slices.Contains(remoteSupportMutations, action) }

func remoteSupportCallMutates(p RemoteSupportParams) bool {
	if p.Action != "tool" {
		return remoteSupportMutates(p.Action)
	}
	if p.Arguments == nil || !slices.Contains([]string{BrowserToolName, ComputerToolName, ToolPipelineName}, p.Tool) {
		return true
	}
	data, err := json.Marshal(p.Arguments)
	if err != nil {
		return true
	}
	return CallMutates(p.Tool, string(data))
}

// record keeps the supporter's side of the session history: what was asked
// of which device, its operation id and outcome, never arguments or data.
func record(ctl *support.Controller, p RemoteSupportParams, res supportResult) {
	if ctl == nil || p.Action == "status" {
		return
	}
	detail := p.Action
	switch {
	case len(p.Argv) > 0:
		detail += ": " + support.CommandSummary(p.Argv)
	case p.RemotePath != "":
		detail += ": " + p.RemotePath
	}
	state := "ok"
	if !res.OK {
		state = "failed"
		if res.Error != nil {
			state = string(res.Error.Code)
		}
	}
	fields := map[string]string{"action": p.Action, "verification": res.Verified, "device": res.Device.RemoteDeviceID}
	if m, ok := res.Result.(map[string]any); ok {
		if sum, ok := m["sha256"].(string); ok {
			fields["sha256"] = sum
		}
	}
	ctl.Record(support.Event{Kind: "operation", OperationID: res.OperationID, State: state, Effect: string(res.Effect), Detail: detail, Fields: fields})
}

func (t *remoteSupportTool) respond(res supportResult, mutation bool) fantasy.ToolResponse {
	data, err := json.Marshal(res)
	if err != nil {
		return deviceErrorResponse(err)
	}
	resp := fantasy.NewTextResponse(string(data))
	if len(res.Image) > 0 {
		resp = fantasy.NewImageResponse(res.Image, orStr(res.ImageMediaType, "image/png"))
		resp.Content = string(data)
	}
	resp.IsError = !res.OK
	resp.StopTurn = res.StopTurn
	if res.NativeMetadata != "" {
		var meta map[string]json.RawMessage
		if json.Unmarshal([]byte(res.NativeMetadata), &meta) == nil && meta != nil {
			if child := meta["atlas_outcome"]; child != nil {
				meta["child_atlas_outcome"] = child
				delete(meta, "atlas_outcome")
			}
			resp = fantasy.WithResponseMetadata(resp, meta)
		}
	}
	dispatch := "none"
	if res.InputSent {
		dispatch = "sent"
		if res.Effect == support.EffectUnknown {
			dispatch = "unknown"
		}
	}
	kind := outcome.KindObservation
	if mutation {
		kind = outcome.KindOutput
	}
	subject := "remote:" + res.Device.RemoteDeviceID
	return withToolOutcome(resp, ToolOutcome{Dispatch: dispatch, Mutation: mutation, Verified: res.OK && res.Verified == "verified", Source: res.OperationID, Subjects: []string{subject}, Kind: kind, Claim: res.Action + ":" + res.Verified})
}

func (t *remoteSupportTool) fail(action string, dev supportDevice, e *support.Error) fantasy.ToolResponse {
	res := supportResult{Action: action, Device: dev, Error: e, InputSent: e.InputSent, Effect: e.Effect, OperationID: e.OperationID, Verified: "not_verified"}
	if res.Effect == "" {
		res.Effect = support.EffectNone
	}
	return t.respond(res, remoteSupportMutates(action))
}

func (t *remoteSupportTool) run(ctx context.Context, p RemoteSupportParams, call fantasy.ToolCall) fantasy.ToolResponse {
	var ctl *support.Controller
	if t.controller != nil {
		ctl = t.controller()
	}
	if ctl == nil {
		return t.fail(p.Action, supportDevice{}, support.Errorf(support.CodeNoTarget, "this computer is not connected to another computer through /support"))
	}
	remote, client, terr := ctl.Target()
	dev := deviceOf(remote)
	if p.Action == "status" {
		return t.status(ctx, ctl, remote, terr)
	}
	if terr != nil {
		e := support.AsError(terr)
		if e == nil {
			e = support.Errorf(support.CodeRemoteOffline, "%v", terr)
		}
		e.RemoteDeviceID = remote.RemoteDeviceID
		return t.fail(p.Action, dev, e)
	}
	if p.DeviceID == "" {
		return t.fail(p.Action, dev, support.Errorf(support.CodeWrongTarget, "device_id is required; call status and pass remote_device_id"))
	}
	if p.DeviceID != remote.RemoteDeviceID {
		return t.fail(p.Action, dev, support.Errorf(support.CodeWrongTarget, "device_id %q is not the connected computer %s (%s); nothing was sent", p.DeviceID, remote.RemoteName, remote.RemoteDeviceID))
	}
	if p.SessionID != "" && p.SessionID != remote.SessionID {
		return t.fail(p.Action, dev, support.Errorf(support.CodeStaleConnection, "the support session changed; nothing was sent; inspect status and create a new plan"))
	}
	if p.Action == "tool" && (p.SessionID == "" || p.Generation == 0 || p.Generation != remote.Generation) {
		return t.fail(p.Action, dev, support.Errorf(support.CodeStaleConnection, "remote built-ins require session_id and the current generation from status; nothing was sent"))
	}
	if remote.ExpiresAt > 0 && time.Now().UnixMilli() > remote.ExpiresAt {
		return t.fail(p.Action, dev, support.Errorf(support.CodeSessionExpired, "the support session time ended"))
	}
	opID := nodes.OperationID(GetSessionFromContext(ctx), call.ID, "remote_support", remote.SessionID)
	if p.OperationID != "" && (p.Action == "operation_status" || p.Action == "cancel") {
		opID = p.OperationID
	}
	if remoteSupportCallMutates(p) {
		if e := t.approve(ctx, call, p, remote); e != nil {
			e.RemoteDeviceID = remote.RemoteDeviceID
			return t.fail(p.Action, dev, e)
		}
	}
	res := supportResult{OK: true, Action: p.Action, Device: dev, Effect: support.EffectNone, Verified: "observation"}
	var err error
	switch p.Action {
	case "system_info":
		res.Result, err = client.SystemInfo(ctx)
	case "list":
		res.Result, err = client.ListFiles(ctx, p.RemotePath, 500)
	case "observe":
		var obs nodes.DesktopObservation
		obs, err = client.Observe(ctx, p.Image)
		if err == nil {
			res.Result = observationView(obs)
			res.Image = obs.PNG
			if obs.Blank {
				// Windows shows UAC and the lock screen on the secure
				// desktop, which captures as blank; Atlas cannot see or
				// answer it.
				res.Note = "The remote capture is blank: the screen may be locked or showing a secure-desktop prompt such as UAC (secure_desktop). Only the person at that computer can answer it; do not send input."
			}
		}
	case "automation":
		var raw json.RawMessage
		query := orStr(p.Query, "windows")
		if query == "tree" {
			query = "inspect"
		}
		raw, err = client.Automation(ctx, nodes.AutomationRequest{Action: query, WindowID: p.WindowID, Name: p.Name, Role: p.Role, MaxElements: 200})
		res.Result = raw
	case "operation_status":
		if p.OperationID == "" {
			return t.fail(p.Action, dev, support.Errorf(support.CodeProtocol, "operation_status needs operation_id"))
		}
		res.OperationID = p.OperationID
		var op nodes.Operation
		op, err = client.Operation(ctx, p.OperationID)
		if err == nil {
			res.Result, res.Effect = op, support.Effect(op.Effect)
			res.Verified = "journal"
		}
	case "permission_status":
		if p.PermissionID == "" {
			return t.fail(p.Action, dev, support.Errorf(support.CodeProtocol, "permission_status needs permission_id"))
		}
		res.Result, err = ctl.PermissionStatus(ctx, p.PermissionID)
	case "request_permission":
		caps := make([]support.Capability, 0, len(p.Capabilities))
		for _, c := range p.Capabilities {
			caps = append(caps, support.Capability(c))
		}
		var req support.PermissionRequest
		req, err = ctl.RequestPermission(ctx, caps, p.Reason)
		res.Result = req
		res.Note = "The computer owner answers on their screen. Poll permission_status; do not assume a grant."
	default:
		return t.mutate(ctx, ctl, client, remote, p, opID)
	}
	if err != nil {
		e := support.Classify(err, remote.RemoteDeviceID, res.OperationID)
		return t.fail(p.Action, dev, e)
	}
	return t.respond(res, false)
}

func observationView(obs nodes.DesktopObservation) map[string]any {
	out := map[string]any{
		"observation_id": obs.ObservationID, "node_id": obs.NodeID, "session_id": obs.SessionID, "generation": obs.Generation,
		"monitor": obs.Monitor, "coordinate_space": obs.CoordinateSpace, "scale": obs.Scale, "width": obs.Width, "height": obs.Height,
		"blank": obs.Blank, "cursor": obs.Cursor, "captured_at": obs.CapturedAt,
	}
	if len(obs.Monitors) > 0 {
		out["monitors"] = obs.Monitors
	}
	if len(obs.Windows) > 0 {
		out["windows"] = obs.Windows
	}
	return out
}

func (t *remoteSupportTool) status(ctx context.Context, ctl *support.Controller, remote support.Remote, terr error) fantasy.ToolResponse {
	st := ctl.Status()
	res := supportResult{OK: terr == nil, Action: "status", Device: deviceOf(remote), Effect: support.EffectNone, Verified: "observation"}
	if terr == nil {
		if fresh, err := ctl.Refresh(ctx); err == nil {
			remote = fresh
			res.Device = deviceOf(fresh)
			st = ctl.Status()
		} else {
			terr = err
			res.OK = false
			res.Verified = "not_verified"
		}
	}
	view := map[string]any{"state": st.State, "reason": st.Reason}
	if st.Remote != nil {
		view["remote"] = map[string]any{
			"remote_device_id": remote.RemoteDeviceID, "name": remote.RemoteName, "fingerprint": remote.RemoteFingerprint,
			"os": remote.OS, "arch": remote.Arch, "session_id": remote.SessionID, "generation": remote.Generation,
			"capabilities": remote.Capabilities, "folders": remote.Roots, "owner_identity": remote.OwnerIdentity,
			"expires_at": time.UnixMilli(remote.ExpiresAt).Format(time.RFC3339), "desktop": remote.Desktop, "desktop_reason": remote.DesktopReason,
			"interactive_session": remote.Interactive, "network": remote.Network, "agent_paused": remote.AgentPaused, "screen_control_paused": remote.InputPaused,
		}
	}
	res.Result = view
	if terr != nil {
		res.Error = support.Classify(terr, remote.RemoteDeviceID, "")
		view["stale"] = true
		view["state"] = "unavailable"
		res.Note = "Live session status could not be read. Remote details are cached, not current evidence."
	}
	return t.respond(res, false)
}

func (t *remoteSupportTool) approve(ctx context.Context, call fantasy.ToolCall, p RemoteSupportParams, remote support.Remote) *support.Error {
	if t.permissions == nil {
		return nil
	}
	where := fmt.Sprintf("%s (%s, %s)", remote.RemoteName, remote.RemoteDeviceID, remote.OS)
	var what string
	switch p.Action {
	case "verify_install":
		what = "Verify an installation on " + where + ": " + strings.Join(p.VerifyArgv, " ")
	case "tool":
		what = "Run " + p.Tool + " on " + where
	case "exec":
		what = "Run on " + where + ": " + strings.Join(p.Argv, " ")
	case "launch":
		what = "Open on " + where + ": " + strings.Join(p.Argv, " ")
	case "push":
		what = fmt.Sprintf("Send %s from this workspace to %s:%s", p.LocalPath, where, p.RemotePath)
	case "pull":
		what = fmt.Sprintf("Copy %s:%s into this workspace at %s", where, p.RemotePath, p.LocalPath)
	case "input":
		what = fmt.Sprintf("%s on the screen of %s", p.InputAction, where)
	case "cancel":
		what = "Stop operation " + p.OperationID + " on " + where
	}
	ok, err := t.permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: GetSessionFromContext(ctx), ToolCallID: call.ID, ToolName: RemoteSupportToolName, Action: p.Action, Description: what, Params: p, Path: t.workingDir})
	if err != nil {
		return support.Errorf(support.CodeCapabilityDenied, "%v", err)
	}
	if !ok {
		return support.Errorf(support.CodeCapabilityDenied, "you declined this remote action; nothing was sent")
	}
	return nil
}

// mutate runs one side-effecting action. It is sent once. If the
// connection drops after sending, the outcome is unknown: the tool waits
// for the session to reconnect and reads the operation's journal entry
// instead of sending again.
func (t *remoteSupportTool) mutate(ctx context.Context, ctl *support.Controller, client *nodes.Client, remote support.Remote, p RemoteSupportParams, opID string) fantasy.ToolResponse {
	mutation := remoteSupportCallMutates(p)
	dev := deviceOf(remote)
	res := supportResult{OK: true, Action: p.Action, Device: dev, OperationID: opID, InputSent: true, Effect: support.EffectApplied, Verified: "not_verified"}
	res.Checkpoint = &supportCheck{SessionID: remote.SessionID, RemoteDeviceID: remote.RemoteDeviceID, Generation: remote.Generation, OperationID: opID, ReconcileWith: "operation_status"}
	var err error
	switch p.Action {
	case "verify_install":
		if p.OperationID == "" || len(p.VerifyArgv) == 0 || strings.TrimSpace(p.ExpectedOutput) == "" || slices.Contains(support.ExtraCapabilities(p.VerifyArgv), support.CapInstall) {
			return t.fail(p.Action, dev, support.Errorf(support.CodeProtocol, "verify_install requires the original operation_id, an independent non-installer verify_argv and nonempty expected_output"))
		}
		original, readErr := client.Operation(ctx, p.OperationID)
		if readErr != nil {
			return t.fail(p.Action, dev, support.Classify(readErr, dev.RemoteDeviceID, p.OperationID))
		}
		if original.SessionID != remote.SessionID || original.State != "completed" || original.Effect != "applied" || !slices.Contains(support.ExtraCapabilities(original.Argv), support.CapInstall) {
			return t.fail(p.Action, dev, support.Errorf(support.CodeProtocol, "the original operation must be a completed installation in this support session"))
		}
		var probe nodes.Operation
		probe, err = client.Exec(ctx, nodes.ExecRequest{OperationID: opID, Argv: p.VerifyArgv, Cwd: p.Cwd, TimeoutMS: p.TimeoutMS})
		if err == nil {
			res = supportOperationResult(res, probe)
			res.Result = map[string]any{"installation_operation_id": original.ID, "probe": probe, "expected_output": p.ExpectedOutput}
			res.Verified = "install_unverified"
			if res.OK && probe.ExitCode != nil && *probe.ExitCode == 0 && !probe.Truncated && strings.Contains(probe.Stdout, p.ExpectedOutput) {
				res.Verified = "verified"
				res.Note = "The independent probe returned the requested application identity/version. This verifies only that criterion, not every feature or the user's original problem."
			} else if res.OK {
				res.OK = false
				res.Error = &support.Error{Code: support.Code("install_unverified"), Message: "the independent probe did not prove the requested installation criterion", InputSent: res.InputSent, Effect: res.Effect, OperationID: opID, RemoteDeviceID: dev.RemoteDeviceID}
			}
		}
	case "tool":
		if p.Arguments == nil {
			return t.fail(p.Action, dev, support.Errorf(support.CodeProtocol, "tool requires an arguments object"))
		}
		input, marshalErr := json.Marshal(p.Arguments)
		if marshalErr != nil {
			return t.fail(p.Action, dev, support.Errorf(support.CodeProtocol, "%v", marshalErr))
		}
		var reply nodes.RemoteToolReply
		reply, err = client.RemoteTool(ctx, nodes.RemoteToolRequest{OperationID: opID, Tool: p.Tool, Input: input})
		if err == nil {
			if reply.Operation != nil {
				res = supportOperationResult(res, *reply.Operation)
			} else {
				res.InputSent, res.Effect = false, support.EffectNone
			}
			res.Result = map[string]any{"tool": p.Tool, "content": reply.Response.Content}
			res.Image, res.NativeMetadata, res.StopTurn = reply.Response.Data, reply.Response.Metadata, reply.Response.StopTurn
			res.ImageMediaType = reply.Response.MediaType
			if child, ok := ResponseOutcome(reply.Response); ok {
				res.InputSent = child.Dispatch != "none"
				if child.Dispatch == "unknown" {
					res.Effect = support.EffectUnknown
				}
				if child.Verified && res.OK && !reply.Response.IsError && !reply.Response.StopTurn {
					res.Verified = "verified"
				}
			}
			if reply.Response.IsError || reply.Response.StopTurn {
				res.OK = false
				if res.Error == nil {
					res.Error = &support.Error{Code: support.Code("remote_tool_stopped"), Message: reply.Response.Content, InputSent: res.InputSent, Effect: res.Effect, OperationID: opID, RemoteDeviceID: dev.RemoteDeviceID, NextStep: "Inspect the native result and resume only after the owner answers or the original target is verified."}
				}
			}
		}
	case "exec":
		if len(p.Argv) == 0 {
			return t.fail(p.Action, dev, support.Errorf(support.CodeProtocol, "exec needs argv"))
		}
		var op nodes.Operation
		op, err = client.Exec(ctx, nodes.ExecRequest{OperationID: opID, Argv: p.Argv, Cwd: p.Cwd, TimeoutMS: p.TimeoutMS})
		if err == nil {
			res = execResult(res, op, p.Argv)
		}
	case "launch":
		if len(p.Argv) == 0 {
			return t.fail(p.Action, dev, support.Errorf(support.CodeProtocol, "launch needs argv"))
		}
		var op nodes.Operation
		op, err = client.Launch(ctx, opID, p.Argv)
		if err == nil {
			res = supportOperationResult(res, op)
			if res.OK {
				res.Note = "Started on the remote desktop. Observe the remote screen to verify the window."
			}
		}
	case "input":
		var op nodes.Operation
		op, err = client.Input(ctx, nodes.DesktopInput{OperationID: opID, ObservationID: p.ObservationID, WindowID: p.WindowID, Action: p.InputAction, X: p.X, Y: p.Y, EndX: p.EndX, EndY: p.EndY, Text: p.Text, Key: p.Key, Modifiers: p.Modifiers, ScrollY: p.ScrollY})
		if err == nil {
			res = supportOperationResult(res, op)
			if res.OK {
				res.Note = "Observe again before the next pointer action; this observation is consumed."
			}
		}
	case "push":
		local, rerr := transfer.Resolve(t.workingDir, p.LocalPath)
		if rerr != nil {
			return t.fail(p.Action, dev, support.Errorf(support.CodePathDenied, "local_path must stay inside this workspace: %v", rerr))
		}
		conflict, cerr := transfer.ParseConflict(p.Conflict)
		if cerr != nil {
			return t.fail(p.Action, dev, support.Errorf(support.CodeProtocol, "%v", cerr))
		}
		start := time.Now()
		var up nodes.UploadResult
		up, err = client.Upload(ctx, opID, local, p.RemotePath, conflict, 0, nil)
		if err == nil {
			res.Result = map[string]any{"remote_path": up.Path, "size": up.Size, "sha256": up.SHA256, "replayed": up.Replayed, "seconds": time.Since(start).Seconds()}
			res.Verified = "verified"
			res.Note = "The remote computer checked the SHA-256 before installing the file. Received files are never run automatically."
		}
	case "pull":
		conflict, cerr := transfer.ParseConflict(p.Conflict)
		if cerr != nil {
			return t.fail(p.Action, dev, support.Errorf(support.CodeProtocol, "%v", cerr))
		}
		localRel := p.LocalPath
		if conflict == transfer.ConflictRename {
			dest, rerr := transfer.Destination(t.workingDir, localRel, transfer.ConflictRename)
			if rerr != nil {
				return t.fail(p.Action, dev, support.Errorf(support.CodePathDenied, "local_path must stay inside this workspace: %v", rerr))
			}
			if rel, rerr := filepath.Rel(t.workingDir, dest); rerr == nil {
				localRel = filepath.ToSlash(rel)
			}
		}
		var fr nodes.FileResult
		fr, err = client.Pull(ctx, p.RemotePath, t.workingDir, localRel, conflict == transfer.ConflictOverwrite, 0)
		if err == nil {
			res.Result = fr
			res.Verified = "verified"
			res.InputSent = true
			res.Note = "Downloaded into this workspace after size and SHA-256 checks."
		}
	case "cancel":
		if p.OperationID == "" {
			return t.fail(p.Action, dev, support.Errorf(support.CodeProtocol, "cancel needs operation_id"))
		}
		var op nodes.Operation
		op, err = client.Cancel(ctx, p.OperationID)
		if err == nil {
			res.Result, res.Effect = op, support.Effect(op.Effect)
			res.Verified = "journal"
		}
	default:
		return t.fail(p.Action, dev, support.Errorf(support.CodeProtocol, "unknown action %q", p.Action))
	}
	if err == nil {
		return t.respond(res, mutation)
	}
	e := support.Classify(err, remote.RemoteDeviceID, opID)
		// A dropped connection is what a shutdown does; reconciling would
		// wait for a computer that is switching off.
		res.OperationID = opID
		if out, ok := t.powerOutcome(powerKind, res, e, opID); ok {
			return t.respond(out, mutation)
		}
	}
	if r, ok := t.power.get(remote.RemoteDeviceID, remote.SessionID); ok && !e.InputSent && (e.Code == support.CodeRemoteOffline || e.Code == support.CodeConnectionLost) {
		return t.respond(t.powerOffline(p.Action, dev, r), mutation)
	}
	if e.Code == support.CodeOutcomeUnknown && p.Action != "pull" {
		return t.respond(t.reconcile(ctx, ctl, res, e, opID), mutation)
	}
	out := res
	out.OK, out.Error, out.InputSent, out.Effect = false, e, e.InputSent, e.Effect
	if !e.InputSent {
		out.Checkpoint = nil
	}
	return t.respond(out, mutation)
}

// reconcile reads the journal after an unknown outcome; it never resends.
func (t *remoteSupportTool) reconcile(ctx context.Context, ctl *support.Controller, res supportResult, e *support.Error, opID string) supportResult {
	res.OK, res.Error, res.Effect, res.InputSent = false, e, support.EffectUnknown, true
	deadline := time.Now().Add(t.reconcileWait)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		remote, client, err := ctl.Target()
		if err == nil && client != nil {
			if remote.RemoteDeviceID != res.Device.RemoteDeviceID || remote.SessionID != res.Device.SessionID {
				res.Note = "The connected device or session changed. The original outcome remains unknown; no journal was read on the new target and nothing was resent."
				return res
			}
			op, oerr := client.Operation(ctx, opID)
			if oerr == nil {
				res.Reconciled = &op
				res.Effect = support.Effect(op.Effect)
				res.Device = deviceOf(remote)
				res.Note = "The connection dropped after sending; the remote journal reports this operation as " + string(op.State) + ". It was not sent again."
				return res
			}
			if apiErr := (*nodes.APIError)(nil); errors.As(oerr, &apiErr) && apiErr.Code == "not_found" {
				res.Note = "The original session has no journal entry for this operation. This does not prove no effect; observe the original remote target before deciding how to recover. It was not sent again."
				return res
			}
		} else if code := support.CodeOf(err); code != support.CodeRemoteOffline {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(500 * time.Millisecond):
		}
	}
	res.Note = "The outcome is unknown and could not be reconciled yet. Do not repeat the action; call operation_status with this operation_id when the connection is back, or observe the remote state."
	return res
}

// supportOperationResult interprets the journal, not merely HTTP success.
func supportOperationResult(res supportResult, op nodes.Operation) supportResult {
	res.Result, res.Effect = op, support.Effect(op.Effect)
	res.InputSent = op.Effect != "none"
	if res.Effect == "" {
		res.Effect = support.EffectUnknown
	}
	res.OK = op.State == "completed" && res.Effect != support.EffectUnknown
	if !res.OK {
		code := support.Code("operation_failed")
		if res.Effect == support.EffectUnknown || !op.State.Terminal() {
			code = support.CodeOutcomeUnknown
			res.Effect = support.EffectUnknown
		}
		res.Error = &support.Error{Code: code, Message: orStr(op.Error, "the remote operation is not completed"), InputSent: res.InputSent, Effect: res.Effect, OperationID: res.OperationID, RemoteDeviceID: res.Device.RemoteDeviceID, NextStep: "Read operation_status or observe the original remote target. Do not repeat an operation with an unknown effect."}
	}
	return res
}

func execResult(res supportResult, op nodes.Operation, argv []string) supportResult {
	res = supportOperationResult(res, op)
	if res.Error != nil && res.Error.Code != support.CodeOutcomeUnknown {
		res.Error.Code = support.Code("command_failed")
		res.Error.NextStep = "Read stderr and the exit code; inspect the remote state before trying something else."
	}
	if slices.Contains(support.ExtraCapabilities(argv), support.CapInstall) {
		// An accepted install request and exit code 0 are not proof.
		res.Verified = "install_unverified"
		res.Note = "Not verified: confirm the installed program independently (for example winget list --id, the program's --version, or its file) before reporting success."
	} else {
		res.Note = "Exit code 0 shows the command ran; verify the user's original problem independently before reporting it solved."
	}
	return res
}

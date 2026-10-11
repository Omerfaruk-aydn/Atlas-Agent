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


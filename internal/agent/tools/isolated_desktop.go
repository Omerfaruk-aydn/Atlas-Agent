package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/isolation"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/transfer"
)

const IsolatedDesktopToolName = "isolated_desktop"

//go:embed isolated_desktop.md
var isolatedDesktopDescription string

var isolatedInputActions = []string{"click", "double_click", "right_click", "move", "drag", "scroll", "type", "key", "hotkey"}

// IsolatedDesktopParams is flat on purpose.
type IsolatedDesktopParams struct {
	Action        string   `json:"action" enum:"capabilities,start,status,stop,observe,windows,inspect,find,launch,click,double_click,right_click,move,drag,scroll,type,key,hotkey,import,export,handoff,resume" description:"capabilities/start/status/stop manage the environment; observe/windows/inspect/find read it; launch and input actions act inside it; import/export move files; handoff gives control to the user."`
	Node          string   `json:"node,omitempty" description:"Isolated node to target. Default: the Atlas sandbox (\"sandbox\"). Other values must be paired nodes that declare themselves isolated."`
	ObservationID string   `json:"observation_id,omitempty" description:"Required for pointer actions: observation_id from the latest observe of this environment."`
	X             int      `json:"x,omitempty" description:"Guest screen x in native guest pixels (from observe)."`
	Y             int      `json:"y,omitempty" description:"Guest screen y in native guest pixels."`
	EndX          int      `json:"end_x,omitempty" description:"drag end x."`
	EndY          int      `json:"end_y,omitempty" description:"drag end y."`
	Button        string   `json:"button,omitempty" description:"click button: left, right or middle."`
	Text          string   `json:"text,omitempty" description:"type: text to type into the focused guest control."`
	Key           string   `json:"key,omitempty" description:"key/hotkey: named key (enter, tab, esc, f5, up) or one character."`
	Modifiers     string   `json:"modifiers,omitempty" description:"hotkey modifiers joined with +, e.g. ctrl+shift."`
	ScrollY       int      `json:"scroll_y,omitempty" description:"scroll: vertical notches, positive scrolls up."`
	ScrollX       int      `json:"scroll_x,omitempty" description:"scroll: horizontal notches."`
	Argv          []string `json:"argv,omitempty" description:"launch: literal program and arguments inside the guest, e.g. [\"notepad.exe\"]."`
	WindowID      string   `json:"window_id,omitempty" description:"inspect/find: guest window_id from windows. Input: optional expected foreground window, checked again on the guest before input is sent."`
	Name          string   `json:"name,omitempty" description:"find: accessible name to search for."`
	Role          string   `json:"role,omitempty" description:"find: UI Automation control type, e.g. ControlType.Edit."`
	MaxElements   int      `json:"max_elements,omitempty" description:"inspect/find: maximum accessibility elements, 1-500 (default 150). A truncated tree cannot prove absence or a unique target."`
	LocalPath     string   `json:"local_path,omitempty" description:"import source / export destination, relative to this workspace."`
	GuestPath     string   `json:"guest_path,omitempty" description:"import destination / export source, relative to the guest exchange folder."`
	Overwrite     bool     `json:"overwrite,omitempty" description:"import/export: replace an existing file."`
	MemoryMB      int      `json:"memory_mb,omitempty" description:"start: guest memory in MiB (minimum 2048 is applied by Windows)."`
	Reason        string   `json:"reason,omitempty" description:"handoff: what the user should do."`
}

const isolatedDesktopExample = `{"action":"observe"}`

type isolatedDesktopTool struct {
	permissions permission.Service
	workingDir  string
	manager     *isolation.Manager
	registry    *nodes.Registry
}

// NewIsolatedDesktopTool builds the isolated desktop tool.
func NewIsolatedDesktopTool(permissions permission.Service, workingDir, devicesDir string, manager *isolation.Manager) fantasy.AgentTool {
	t := &isolatedDesktopTool{permissions: permissions, workingDir: workingDir, manager: manager, registry: nodes.OpenRegistry(devicesDir)}
	return fantasy.NewAgentTool(IsolatedDesktopToolName, isolatedDesktopDescription, func(ctx context.Context, _ IsolatedDesktopParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p IsolatedDesktopParams
		if err := strictDecode(call.Input, &p, isolatedDesktopExample); err != nil {
			return deviceErrorResponse(err), nil
		}
		resp, err := t.run(ctx, p, call)
		if err != nil {
			return deviceErrorResponse(err), nil
		}
		return resp, nil
	})
}

func (t *isolatedDesktopTool) approve(ctx context.Context, call fantasy.ToolCall, action, desc string, params any, safe bool) error {
	if t.permissions == nil {
		return nil
	}
	ok, err := t.permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: GetSessionFromContext(ctx), ToolCallID: call.ID, ToolName: IsolatedDesktopToolName, Action: action, Description: desc, Params: params, Path: t.workingDir, Safe: safe})
	if err != nil {
		return err
	}
	if !ok {
		return deviceErr("permission_denied", "", "the user denied this action; nothing was sent", "Do not retry unless the user asks.")
	}
	return nil
}

func (t *isolatedDesktopTool) target(name string) (*nodes.Client, nodes.Entry, error) {
	if name == "" {
		name = isolation.SandboxNodeName
	}
	c, e, err := t.registry.ClientFor(name)
	if err != nil {
		if name == isolation.SandboxNodeName {
			return nil, e, deviceErr("not_started", "node", "no Atlas sandbox is running", "Call isolated_desktop start (after capabilities shows windows_sandbox available).")
		}
		return nil, e, deviceErr("unknown_node", "node", err.Error(), "Call remote_node list.")
	}
	if !e.Isolated {
		return nil, e, deviceErr("not_isolated", "node", fmt.Sprintf("node %q does not declare an isolated environment; isolated_desktop will not drive a regular desktop", name), "Use remote_node for that node, or the computer tool for this desktop with the user's consent.")
	}
	return c, e, nil
}

func (t *isolatedDesktopTool) activeLease() *isolation.Status {
	if t.manager == nil {
		return nil
	}
	st, _ := t.manager.Active()
	return st
}

func (t *isolatedDesktopTool) run(ctx context.Context, p IsolatedDesktopParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	switch p.Action {
	case "capabilities":
		if t.manager == nil {
			return fantasy.ToolResponse{}, deviceErr("unsupported", "", "isolation is unavailable in this build", "")
		}
		return deviceResponse(map[string]any{"ok": true, "backends": t.manager.Capabilities(ctx)}), nil
	case "start":
		if t.manager == nil {
			return fantasy.ToolResponse{}, deviceErr("unsupported", "", "isolation is unavailable in this build", "")
		}
		if err := t.approve(ctx, call, "start", "Start a disposable Windows Sandbox controlled by Atlas", p, false); err != nil {
			return fantasy.ToolResponse{}, err
		}
		st, err := t.manager.Start(ctx, isolation.StartOptions{MemoryMB: p.MemoryMB})
		if err != nil {
			var ue *isolation.UnavailableError
			if errors.As(err, &ue) {
				return fantasy.ToolResponse{}, &DeviceToolError{Code: "isolation_unavailable", Message: ue.Error(), Effect: "none", NextStep: "Tell the user why. Do not use the computer tool on their real desktop instead unless they explicitly agree."}
			}
			return fantasy.ToolResponse{}, &DeviceToolError{Code: "start_failed", Message: err.Error(), Effect: "unknown", NextStep: "Report the failure; Atlas stopped only the processes it launched."}
		}
		return deviceResponse(map[string]any{"ok": true, "environment": st, "node": isolation.SandboxNodeName}), nil
	case "status":
		if t.manager == nil {
			return fantasy.ToolResponse{}, deviceErr("unsupported", "", "isolation is unavailable in this build", "")
		}
		list, err := t.manager.List()
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "environments": list}), nil
	case "stop":
		st := t.activeLease()
		if st == nil {
			return deviceResponse(map[string]any{"ok": true, "stopped": false, "note": "no Atlas sandbox is running"}), nil
		}
		if err := t.approve(ctx, call, "stop", "Close the Atlas sandbox "+st.Lease.ID+" and discard everything inside it", p, false); err != nil {
			return fantasy.ToolResponse{}, err
		}
		out, err := t.manager.Stop(st.Lease.ID)
		if err != nil {
			return fantasy.ToolResponse{}, &DeviceToolError{Code: "stop_incomplete", Message: err.Error(), Effect: "applied", NextStep: "Report which resources may remain."}
		}
		return deviceResponse(map[string]any{"ok": true, "stopped": true, "environment": out}), nil
	case "handoff", "resume":
		st := t.activeLease()
		if st == nil {
			return fantasy.ToolResponse{}, deviceErr("not_started", "", "no Atlas sandbox is running", "")
		}
		if p.Action == "resume" {
			out, err := t.manager.Resume(st.Lease.ID)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			return deviceResponse(map[string]any{"ok": true, "environment": out, "note": "Observe again before any input: the user may have changed the guest."}), nil
		}
		out, err := t.manager.Pause(st.Lease.ID, orStr(p.Reason, "user control"))
		resp := deviceResponse(map[string]any{"ok": true, "environment": out, "focus_error": errString(err), "note": "Automation is paused. Ask the user to tell you when they are done, then call resume."})
		resp.StopTurn = true
		return resp, nil
	}

	client, entry, err := t.target(p.Node)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if st := t.activeLease(); st != nil && entry.Name == isolation.SandboxNodeName && st.Record.State == taskstate.Paused && (slices.Contains(isolatedInputActions, p.Action) || p.Action == "launch") {
		return fantasy.ToolResponse{}, deviceErr("paused", "", "the environment is handed to the user; input is blocked until resume", "Wait for the user, then call resume and observe again.")
	}
	switch p.Action {
	case "observe":
		obs, err := client.Observe(ctx, true)
		if err != nil {
			return fantasy.ToolResponse{}, classifyNodeErr(err, entry.Name)
		}
		meta := map[string]any{"ok": true, "node": entry.Name, "node_id": entry.NodeID, "observation_id": obs.ObservationID, "width": obs.Width, "height": obs.Height, "blank": obs.Blank, "cursor": obs.Cursor, "windows": obs.Windows, "coordinates": "native guest pixels; scale the image coordinates by image_scale"}
		if obs.Blank {
			meta["warning"] = "capture_blank: the guest screen is a single color. The sandbox window may be minimized or the session disconnected; restore the window or call handoff. Do not aim input at this observation."
		}
		img := obs.PNG
		scale := 1.0
		if scaled, err := computer.DownscaleScreenshot(obs.PNG, computer.MaxScreenshotWidth); err == nil {
			img, scale = scaled.PNG, scaled.Scale
		}
		meta["image_scale"] = scale
		data, _ := json.Marshal(meta)
		resp := fantasy.NewImageResponse(img, "image/png")
		resp.Content = string(data)
		return resp, nil
	case "windows", "inspect", "find":
		raw, err := client.Automation(ctx, nodes.AutomationRequest{Action: p.Action, WindowID: p.WindowID, Name: p.Name, Role: p.Role, MaxElements: p.MaxElements})
		if err != nil {
			return fantasy.ToolResponse{}, classifyNodeErr(err, entry.Name)
		}
		return deviceResponse(map[string]any{"ok": true, "node": entry.Name, "result": raw}), nil
	case "launch":
		if len(p.Argv) == 0 {
			return fantasy.ToolResponse{}, deviceErr("invalid_request", "argv", "launch needs argv", `{"action":"launch","argv":["notepad.exe"]}`)
		}
		if err := t.approve(ctx, call, "launch", "Start inside the isolated guest: "+strings.Join(p.Argv, " "), p, true); err != nil {
			return fantasy.ToolResponse{}, err
		}
		op, err := client.Launch(ctx, nodes.OperationID(GetSessionFromContext(ctx), call.ID, "launch"), p.Argv)
		if err != nil {
			return fantasy.ToolResponse{}, classifyNodeErr(err, entry.Name)
		}
		return deviceResponse(map[string]any{"ok": op.State == taskstate.Completed, "node": entry.Name, "operation": op}), nil
	case "import", "export":
		if p.LocalPath == "" || p.GuestPath == "" {
			return fantasy.ToolResponse{}, deviceErr("invalid_request", "local_path", "import/export need local_path and guest_path", `{"action":"import","local_path":"build/app.zip","guest_path":"app.zip"}`)
		}
		desc := fmt.Sprintf("Copy %s into the isolated guest as %s", p.LocalPath, p.GuestPath)
		if p.Action == "export" {
			desc = fmt.Sprintf("Copy %s out of the isolated guest into %s", p.GuestPath, p.LocalPath)
		}
		if err := t.approve(ctx, call, p.Action, desc, p, false); err != nil {
			return fantasy.ToolResponse{}, err
		}
		var fr nodes.FileResult
		if p.Action == "import" {
			local, rerr := transfer.Resolve(t.workingDir, p.LocalPath)
			if rerr != nil {
				return fantasy.ToolResponse{}, deviceErr(transfer.Code(rerr), "local_path", rerr.Error(), "local_path must be inside this workspace.")
			}
			fr, err = client.Push(ctx, nodes.OperationID(GetSessionFromContext(ctx), call.ID, "import"), local, p.GuestPath, p.Overwrite, entry.MaxFileBytes)
		} else {
			fr, err = client.Pull(ctx, p.GuestPath, t.workingDir, p.LocalPath, p.Overwrite, entry.MaxFileBytes)
		}
		if err != nil {
			var te *transfer.Error
			if errors.As(err, &te) {
				return fantasy.ToolResponse{}, deviceErr(te.Code, "", te.Message, "")
			}
			return fantasy.ToolResponse{}, classifyNodeErr(err, entry.Name)
		}
		return deviceResponse(map[string]any{"ok": true, "node": entry.Name, "file": fr}), nil
	}
	if slices.Contains(isolatedInputActions, p.Action) {
		return t.input(ctx, p, call, client, entry)
	}
	return fantasy.ToolResponse{}, deviceErr("invalid_action", "action", fmt.Sprintf("unknown action %q", p.Action), "See the action list in the schema.")
}

func (t *isolatedDesktopTool) input(ctx context.Context, p IsolatedDesktopParams, call fantasy.ToolCall, client *nodes.Client, entry nodes.Entry) (fantasy.ToolResponse, error) {
	// Input inside an isolated guest cannot touch the host session, so it
	// is Safe like other computer actions once isolation is enabled; plan
	// mode still blocks it.
	if err := t.approve(ctx, call, p.Action, fmt.Sprintf("Isolated guest input: %s", p.Action), p, true); err != nil {
		return fantasy.ToolResponse{}, err
	}
	var before, after isolation.HostSnapshot
	var snapErr error
	if t.manager != nil {
		before, snapErr = t.manager.HostSnapshot()
	}
	op, err := client.Input(ctx, nodes.DesktopInput{OperationID: nodes.OperationID(GetSessionFromContext(ctx), call.ID, p.Action), ObservationID: p.ObservationID, WindowID: p.WindowID, Action: p.Action, X: p.X, Y: p.Y, EndX: p.EndX, EndY: p.EndY, Button: p.Button, Text: p.Text, Key: p.Key, Modifiers: p.Modifiers, ScrollX: p.ScrollX, ScrollY: p.ScrollY})
	if t.manager != nil && snapErr == nil {
		after, snapErr = t.manager.HostSnapshot()
	}
	if err != nil {
		return fantasy.ToolResponse{}, classifyNodeErr(err, entry.Name)
	}
	out := map[string]any{"ok": op.State == taskstate.Completed, "node": entry.Name, "node_id": entry.NodeID, "operation": op}
	if snapErr == nil && t.manager != nil {
		unchanged := before.ForegroundWindow == after.ForegroundWindow && before.CursorX == after.CursorX && before.CursorY == after.CursorY
		out["host_input_unchanged"] = unchanged
		if !unchanged {
			out["host_note"] = "The host foreground window or cursor changed during the call. Guest input does not use host input, so this is most likely the user working; it is reported for transparency."
		}
	}
	if slices.Contains([]string{"click", "double_click", "right_click", "drag", "move", "scroll"}, p.Action) {
		out["next"] = "Observe again before the next pointer action; the observation was consumed."
	}
	resp := deviceResponse(out)
	resp.IsError = op.State != taskstate.Completed
	return resp, nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

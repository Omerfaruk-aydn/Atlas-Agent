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


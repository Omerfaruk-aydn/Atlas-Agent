package nodes

import (
	"encoding/json"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
)

// Capability names a node grants to its paired controllers.
type Capability string

const (
	CapExec    Capability = "exec"
	CapFiles   Capability = "files"
	CapDesktop Capability = "desktop"
)

// Hello is the unauthenticated identity a node reveals.
type Hello struct {
	Protocol    int    `json:"protocol"`
	NodeID      string `json:"node_id"`
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
}

// Info is what an authenticated controller learns about a node.
type Info struct {
	Hello
	OS           string       `json:"os"`
	Arch         string       `json:"arch"`
	Hostname     string       `json:"hostname"`
	Root         string       `json:"root"`
	Capabilities []Capability `json:"capabilities"`
	// Desktop reports whether desktop control is actually usable, which
	// needs both the capability and an interactive session backend.
	Desktop       bool   `json:"desktop"`
	DesktopReason string `json:"desktop_reason,omitempty"`
	Isolated      bool   `json:"isolated"`
	DeviceID      string `json:"device_id"`
	MaxFileBytes  int64  `json:"max_file_bytes"`
	// Session fields are set only for session-scoped access.
	SessionID   string   `json:"session_id,omitempty"`
	Generation  uint64   `json:"generation,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	Roots       []string `json:"roots,omitempty"`
}

// PairRequest redeems a one-time pairing code.
type PairRequest struct {
	Code       string `json:"code"`
	DeviceName string `json:"device_name"`
}

// PairResponse carries the token exactly once.
type PairResponse struct {
	DeviceID string `json:"device_id"`
	Token    string `json:"token"`
	Info     Info   `json:"info"`
}

// ExecRequest runs a literal argv under the node root.
type ExecRequest struct {
	OperationID string   `json:"operation_id"`
	Argv        []string `json:"argv"`
	Cwd         string   `json:"cwd,omitempty"`
	TimeoutMS   int64    `json:"timeout_ms,omitempty"`
	// Wait false returns immediately with the running operation.
	Wait *bool `json:"wait,omitempty"`
}

// Operation is a journaled side effect on a node.
type Operation struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	DeviceID    string          `json:"device_id"`
	SessionID   string          `json:"session_id,omitempty"`
	Generation  uint64          `json:"generation,omitempty"`
	RequestHash string          `json:"request_hash"`
	State       taskstate.State `json:"state"`
	Argv        []string        `json:"argv,omitempty"`
	Cwd         string          `json:"cwd,omitempty"`
	ExitCode    *int            `json:"exit_code,omitempty"`
	Stdout      string          `json:"stdout,omitempty"`
	Stderr      string          `json:"stderr,omitempty"`
	Truncated   bool            `json:"truncated,omitempty"`
	Error       string          `json:"error,omitempty"`
	// Effect is none, applied or unknown.
	Effect    string `json:"effect"`
	StartedAt int64  `json:"started_at"`
	EndedAt   int64  `json:"ended_at,omitempty"`
	Path      string `json:"path,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	Size      int64  `json:"size,omitempty"`
	// Replayed is set on responses served from the journal for a repeated
	// operation_id instead of running again.
	Replayed bool `json:"replayed,omitempty"`
}

// APIError is the JSON body of every non-2xx response.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// InputSent tells the controller whether anything ran.
	InputSent bool   `json:"input_sent"`
	Effect    string `json:"effect"`
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

// FileResult describes a verified transfer.
type FileResult struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Replayed bool   `json:"replayed,omitempty"`
}

// DesktopObservation is a guest/node screen capture bound to its geometry.
// Coordinates are physical pixels of the node's virtual screen, 1:1 with
// the PNG; input aimed with it is refused once the geometry, connection
// generation or observation changes.
type DesktopObservation struct {
	ObservationID string `json:"observation_id"`
	NodeID        string `json:"node_id"`
	SessionID     string `json:"session_id,omitempty"`
	Generation    uint64 `json:"generation,omitempty"`
	// Monitor is "virtual": the capture spans every monitor.
	Monitor string `json:"monitor"`
	// CoordinateSpace names how x/y map onto the screen.
	CoordinateSpace string `json:"coordinate_space"`
	// Scale is image pixels per screen pixel (1 for a full capture).
	Scale      float64         `json:"scale"`
	Monitors   json.RawMessage `json:"monitors,omitempty"`
	Width      int             `json:"width"`
	Height     int             `json:"height"`
	PNG        []byte          `json:"png,omitempty"`
	Blank      bool            `json:"blank"`
	Cursor     [2]int          `json:"cursor"`
	Windows    json.RawMessage `json:"windows,omitempty"`
	CapturedAt int64           `json:"captured_at"`
}

// AutomationRequest is a read-only accessibility query on the node desktop.
type AutomationRequest struct {
	Action      string `json:"action"`
	WindowID    string `json:"window_id,omitempty"`
	Name        string `json:"name,omitempty"`
	Role        string `json:"role,omitempty"`
	MaxElements int    `json:"max_elements,omitempty"`
	// Refresh rediscovers the application catalog (apps only).
	Refresh bool `json:"refresh,omitempty"`
}

// DesktopInput is one input action bound to an observation.
type DesktopInput struct {
	OperationID   string `json:"operation_id"`
	ObservationID string `json:"observation_id"`
	WindowID      string `json:"window_id,omitempty"`
	Action        string `json:"action"`
	X             int    `json:"x,omitempty"`
	Y             int    `json:"y,omitempty"`
	EndX          int    `json:"end_x,omitempty"`
	EndY          int    `json:"end_y,omitempty"`
	Button        string `json:"button,omitempty"`
	Text          string `json:"text,omitempty"`
	Key           string `json:"key,omitempty"`
	Modifiers     string `json:"modifiers,omitempty"`
	ScrollY       int    `json:"scroll_y,omitempty"`
	ScrollX       int    `json:"scroll_x,omitempty"`
}

package tools

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/support"
)

// Power requests end the connection they were sent over. Losing the
// connection right after one is the expected result, not a failure, and
// the request is never sent a second time on its own.

// Power request kinds.
const (
	powerShutdown = "shutdown"
	powerRestart  = "restart"
)

// Power request states, in order.
const (
	// powerSent: the request left this computer; no answer yet.
	powerSent = "sent"
	// powerAccepted: the remote command exited 0 before the connection
	// dropped.
	powerAccepted = "accepted"
	// powerDisconnected: the connection dropped after the request, which
	// is what a shutdown or restart does.
	powerDisconnected = "disconnected_as_expected"
)

// powerRequest is the last power request sent to one support session.
type powerRequest struct {
	Kind        string `json:"kind"`
	State       string `json:"state"`
	DeviceID    string `json:"remote_device_id"`
	SessionID   string `json:"session_id"`
	OperationID string `json:"operation_id,omitempty"`
	SentAt      string `json:"sent_at"`
}

// powerLedger remembers power requests per device for the life of the
// tool, so later calls explain an offline computer instead of reporting a
// connection fault, and a second identical request is refused.
type powerLedger struct {
	mu       sync.Mutex
	requests map[string]*powerRequest
	now      func() time.Time
}

func (l *powerLedger) get(deviceID, sessionID string) (powerRequest, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.requests[deviceID]
	if r == nil {
		return powerRequest{}, false
	}
	if r.SessionID != sessionID && sessionID != "" {
		// A new support session: the computer came back (or someone
		// reconnected); the old request is history.
		delete(l.requests, deviceID)
		return powerRequest{}, false
	}
	return *r, true
}

func (l *powerLedger) set(r powerRequest) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.requests == nil {
		l.requests = map[string]*powerRequest{}
	}
	if r.SentAt == "" {
		now := time.Now
		if l.now != nil {
			now = l.now
		}
		r.SentAt = now().UTC().Format(time.RFC3339)
	}
	l.requests[r.DeviceID] = &r
}

func (l *powerLedger) advance(deviceID, state string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if r := l.requests[deviceID]; r != nil && powerRank(state) > powerRank(r.State) {
		r.State = state
	}
}

func powerRank(state string) int {
	return slices.Index([]string{powerSent, powerAccepted, powerDisconnected}, state)
}

// classifyPowerCommand recognizes a shutdown or restart request in literal
// argv, directly or through a PowerShell or cmd wrapper. Cancelling a
// pending shutdown (shutdown /a, -c) and logging off are not power-off
// requests.
func classifyPowerCommand(argv []string) (string, bool) {
	if len(argv) == 0 {
		return "", false
	}
	prog := strings.ToLower(strings.TrimSuffix(filepath.Base(strings.ReplaceAll(argv[0], `\`, "/")), ".exe"))
	args := make([]string, 0, len(argv)-1)
	for _, a := range argv[1:] {
		args = append(args, strings.ToLower(a))
	}
	switch prog {
	case "shutdown":
		has := func(flags ...string) bool {
			return slices.ContainsFunc(args, func(a string) bool { return slices.Contains(flags, a) })
		}
		switch {
		case has("/a", "-a", "-c"):
			return "", false
		case has("/r", "-r", "/g", "-g", "--reboot"):
			return powerRestart, true
		case has("/s", "-s", "/p", "-p", "-h", "--poweroff", "now", "+0"):
			return powerShutdown, true
		}
		return "", false
	case "poweroff", "halt":
		return powerShutdown, true
	case "reboot":
		return powerRestart, true
	case "systemctl":
		if slices.Contains(args, "poweroff") || slices.Contains(args, "halt") {
			return powerShutdown, true
		}
		if slices.Contains(args, "reboot") {
			return powerRestart, true
		}
	case "powershell", "pwsh":
		if i := slices.IndexFunc(args, func(a string) bool { return a == "-command" || a == "-c" }); i >= 0 && i+1 < len(args) {
			script := strings.Join(argv[i+2:], " ")
			// Only a simple literal invocation is classified. Quoted output,
			// pipelines and conditional scripts keep normal unknown semantics.
			if strings.ContainsAny(script, "'\";|&$`{}()") {
				return "", false
			}
			return classifyPowerCommand(strings.Fields(script))
		}
	case "cmd":
		if i := slices.IndexFunc(args, func(a string) bool { return a == "/c" || a == "/k" }); i >= 0 && i+1 < len(args) {
			return classifyPowerCommand(argv[i+2:])
		}
	case "restart-computer":
		return powerRestart, true
	case "stop-computer":
		return powerShutdown, true
	}
	return "", false
}

// powerOutcome turns the result of a sent power request into its expected
// state. A clean exit proves acceptance. A dropped connection leaves the
// actual machine state unknown, without authorizing a duplicate request.
func (t *remoteSupportTool) powerOutcome(kind string, res supportResult, e *support.Error, opID string) (supportResult, bool) {
	req := powerRequest{Kind: kind, State: powerSent, DeviceID: res.Device.RemoteDeviceID, SessionID: res.Device.SessionID, OperationID: opID}
	switch {
	case e == nil && res.OK:
		req.State = powerAccepted
	case e != nil && (e.Code == support.CodeOutcomeUnknown || e.Code == support.CodeConnectionLost || e.Code == support.CodeRemoteOffline) && e.InputSent:
		req.State = powerDisconnected
	default:
		return res, false
	}
	t.power.set(req)
	res.InputSent = true
	if req.State == powerAccepted {
		res.OK, res.Error, res.Checkpoint = true, nil, nil
		res.Effect = support.EffectApplied
	} else {
		res.OK, res.Error = false, e
		res.Effect = support.EffectUnknown
	}
	res.Verified = "power_" + req.State
	res.Result = map[string]any{"power_request": req}
	res.Note = powerNote(req)
	return res, true
}

func powerNote(r powerRequest) string {
	what := "shut down"
	after := "It stays offline until someone turns it on and reconnects with /support."
	if r.Kind == powerRestart {
		what = "restart"
		after = "It should come back after the restart; call status later. A new support session may be needed."
	}
	switch r.State {
	case powerAccepted:
		return "The remote computer accepted the request to " + what + ". Losing the connection next is expected, not a failure. Do not send it again and do not ask the user to confirm again. " + after
	case powerDisconnected:
		return "The connection dropped after the request to " + what + " was sent. This disconnect is expected for a power request, but the command outcome is still unknown; a network failure can look the same. Do not repeat the command or ask for the same permission again. " + after
	}
	return "A request to " + what + " was sent. Do not send it again. " + after
}

// powerOffline answers a call made while the computer is offline after a
// power request: status succeeds and explains, other actions fail with
// the reason instead of a generic connection fault.
func (t *remoteSupportTool) powerOffline(action string, dev supportDevice, r powerRequest) supportResult {
	t.power.advance(r.DeviceID, powerDisconnected)
	r.State = powerDisconnected
	res := supportResult{Action: action, Device: dev, Effect: support.EffectNone, Verified: "power_" + r.State, Result: map[string]any{"state": "offline_after_" + r.Kind, "power_request": r}}
	res.Note = powerNote(r)
	if action == "status" {
		res.OK = true
		return res
	}
	res.Error = support.Errorf(support.CodeRemoteOffline, "the computer is offline after it was asked to %s at %s; the cause is not confirmed and nothing new was sent", r.Kind, r.SentAt)
	res.Error.RemoteDeviceID = r.DeviceID
	res.Error.NextStep = "Report that the computer is unreachable after the power request; do not infer the cause or resend it. Continue only after it is back and status shows a session."
	return res
}

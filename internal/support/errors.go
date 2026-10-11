// Package support implements visible, owner-started remote IT support
// between two Atlas installations. The computer receiving support (the
// host) creates a short-lived one-time code; the supporting computer (the
// controller) enters it, both ends prove knowledge of the code over an
// end-to-end TLS channel, and the host's owner accepts the named, verified
// device before anything can run. The controller's model plans; the host
// only executes structured, permission-checked node operations through the
// existing internal/nodes API, its journal and internal/transfer.
//
// Two transports carry the same protocol: a direct TCP connection on the
// local network (found by UDP discovery or an explicit address), and an
// outbound WebSocket pair spliced by a relay you operate. A short code can
// not find a computer on another network by itself; internet mode needs a
// configured relay and fails with a configuration error without one.
package support

import (
	"errors"
	"fmt"
)

// Code is a stable, machine-readable failure class.
type Code string

const (
	CodePairingInvalid     Code = "pairing_invalid"
	CodePairingExpired     Code = "pairing_expired"
	CodePairingUsed        Code = "pairing_used"
	CodePairingRejected    Code = "pairing_rejected"
	CodePairingRateLimited Code = "pairing_rate_limited"
	CodeSessionRevoked     Code = "session_revoked"
	CodeSessionExpired     Code = "session_expired"
	CodeSessionPaused      Code = "session_paused"
	CodeControlPaused      Code = "control_paused"
	CodeCapabilityDenied   Code = "capability_denied"
	CodeRemoteOffline      Code = "remote_offline"
	CodeConnectionLost     Code = "connection_lost"
	CodeOutcomeUnknown     Code = "outcome_unknown"
	CodeStaleConnection    Code = "stale_connection"
	CodeStaleTarget        Code = "stale_target"
	CodeDesktopUnavailable Code = "remote_desktop_unavailable"
	CodeSecureDesktop      Code = "secure_desktop"
	CodeTransferInterrupt  Code = "transfer_interrupted"
	CodeHashMismatch       Code = "hash_mismatch"
	CodePathDenied         Code = "path_denied"
	CodeUnsupported        Code = "unsupported_platform"
	CodeRelayNotConfigured Code = "relay_not_configured"
	CodeNoTarget           Code = "no_support_target"
	CodeWrongTarget        Code = "wrong_target"
	CodeProtocol           Code = "protocol_error"
	// CodeLocal is a failure on the supporting computer before anything
	// was sent, such as a missing local file.
	CodeLocal Code = "local_error"
)

// Effect says whether a failed request may have changed the remote.
type Effect string

const (
	EffectNone    Effect = "none"
	EffectApplied Effect = "applied"
	EffectUnknown Effect = "unknown"
)

// Error is the structured failure every support layer returns.
type Error struct {
	Code           Code   `json:"code"`
	Message        string `json:"message"`
	InputSent      bool   `json:"input_sent"`
	Effect         Effect `json:"effect"`
	OperationID    string `json:"operation_id,omitempty"`
	RemoteDeviceID string `json:"remote_device_id,omitempty"`
	Retryable      bool   `json:"retryable"`
	NextStep       string `json:"next_step,omitempty"`
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

// Errorf builds an error that sent nothing and changed nothing.
func Errorf(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), Effect: EffectNone, NextStep: nextStep(code), Retryable: retryable(code)}
}

// AsError extracts a support error, or nil.
func AsError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return nil
}

// CodeOf returns the code of err, or the empty code.
func CodeOf(err error) Code {
	if e := AsError(err); e != nil {
		return e.Code
	}
	return ""
}

func retryable(code Code) bool {
	switch code {
	case CodeRemoteOffline, CodeConnectionLost, CodeSessionPaused, CodeControlPaused, CodeTransferInterrupt, CodeStaleTarget, CodeSecureDesktop, CodeDesktopUnavailable:
		return true
	}
	return false
}

func nextStep(code Code) string {
	switch code {
	case CodePairingInvalid:
		return "Check the code with the person at the other computer; codes are case-insensitive and contain no 0, 1, I, L, O or U."
	case CodePairingExpired:
		return "Ask the person at the other computer to create a new code."
	case CodePairingUsed:
		return "A code works once; ask for a new code."
	case CodePairingRejected:
		return "The computer owner declined; do not retry without talking to them."
	case CodePairingRateLimited:
		return "Too many attempts; the code was disabled. Ask for a new code."
	case CodeSessionRevoked:
		return "The computer owner ended the session. Access cannot be regained by reconnecting; a new session needs a new code and acceptance."
	case CodeSessionExpired:
		return "The session time ended. Start a new session with a new code."
	case CodeSessionPaused:
		return "The owner paused the support task; wait until they resume it. Nothing was run."
	case CodeControlPaused:
		return "The owner paused screen control; read-only steps still work. Nothing was sent."
	case CodeCapabilityDenied:
		return "This permission was not granted by the computer owner. Ask them with request_permissions, or continue without it."
	case CodeRemoteOffline:
		return "The remote computer is not reachable. Do not switch to this computer; wait for it to come back or end the task."
	case CodeConnectionLost, CodeOutcomeUnknown:
		return "The connection dropped after the request was sent. Do not resend it; reconcile with operation_status once reconnected, or observe the remote state."
	case CodeStaleConnection:
		return "The response belongs to an earlier connection and was discarded; read the current state again."
	case CodeStaleTarget:
		return "Observe the remote desktop again and aim with the new observation."
	case CodeDesktopUnavailable:
		return "The remote desktop cannot be captured or controlled now (locked, signed out or minimized remote session). Ask the owner; never use this computer's desktop instead."
	case CodeSecureDesktop:
		return "A system prompt such as UAC is on the secure desktop. The owner at the remote computer must answer it; Atlas cannot and will not bypass it."
	case CodeTransferInterrupt:
		return "Resume the transfer with the same operation_id; received data is verified before it is kept."
	case CodeHashMismatch:
		return "The received bytes did not match; the file was not delivered. Transfer it again."
	case CodePathDenied:
		return "Use a path inside one of the folders the owner allowed."
	case CodeUnsupported:
		return "This operation is not available on the remote platform."
	case CodeRelayNotConfigured:
		return "Internet mode needs your own relay: set support_relay_url (and support_relay_token_file) or use local network mode."
	case CodeNoTarget:
		return "Connect to a computer first with /support → Connect to another computer."
	case CodeWrongTarget:
		return "The request names a different support session or device than the active one; use the device shown by remote_support status."
	}
	return ""
}

// Unsent wraps a failure that happened before anything left this computer.
func Unsent(code Code, err error) *Error {
	e := Errorf(code, "%v", err)
	return e
}

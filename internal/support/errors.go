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


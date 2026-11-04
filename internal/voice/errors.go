// Package voice is Atlas's telephone and voice-assistant layer. It keeps
// four concerns apart: a [Transport] connects calls and moves audio, a
// recognizer and synthesizer (see provider.go) turn audio into text and
// back, the [Gateway] owns the call lifecycle, and the Atlas agent, reached
// through a [TurnRunner], does the thinking. Nothing in this package knows
// about a specific vendor; vendors live in subpackages.
package voice

import (
	"errors"
	"fmt"
)

// Code is a short error code that is safe to log, store and speak about.
// It never carries provider messages, URLs or secrets.
type Code string

// Error codes.
const (
	CodeProviderUnavailable Code = "provider_unavailable"
	CodeProviderAuth        Code = "provider_auth"
	CodeProviderQuota       Code = "provider_quota"
	CodeProviderProtocol    Code = "provider_protocol"
	CodeTransportFailed     Code = "transport_failed"
	CodeAgentFailed         Code = "agent_failed"
	CodePermissionRequired  Code = "permission_required"
	CodeBudgetExceeded      Code = "budget_exceeded"
	CodeNotConfigured       Code = "not_configured"
	CodeRejected            Code = "rejected"
	CodeCancelled           Code = "cancelled"
	CodeTimeout             Code = "timeout"
	CodeInvalidInput        Code = "invalid_input"
)

// Error is a typed voice error. Err may hold detail for logs; Code is what
// leaves the process.
type Error struct {
	Code Code
	Op   string
	Err  error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("%s: %s", e.Op, e.Code)
	}
	return fmt.Sprintf("%s: %s: %v", e.Op, e.Code, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// Errorf builds an [Error].
func Errorf(code Code, op string, err error) error {
	return &Error{Code: code, Op: op, Err: err}
}

// CodeOf extracts the code of err, or CodeAgentFailed for foreign errors.
func CodeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	if errors.Is(err, ErrCallEnded) {
		return CodeCancelled
	}
	return CodeAgentFailed
}

// Sentinel errors.
var (
	ErrCallEnded       = errors.New("call has ended")
	ErrCallExists      = errors.New("call is already active")
	ErrInvalidState    = errors.New("invalid call state transition")
	ErrBudgetExceeded  = errors.New("voice budget exceeded")
	ErrNotAllowed      = errors.New("caller is not allowed")
	ErrRateLimited     = errors.New("outbound call rate limit reached")
	ErrDuplicateEvent  = errors.New("event was already handled")
	ErrServiceDisabled = errors.New("voice assistant is disabled")
)

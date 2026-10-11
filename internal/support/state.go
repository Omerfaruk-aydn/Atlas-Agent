package support

import (
	"fmt"
	"slices"
	"sync"
)

// State is one connection lifecycle state, shared by both ends.
type State string

const (
	StateIdle               State = "idle"
	StateHosting            State = "hosting"
	StateConnecting         State = "connecting"
	StateAwaitingAcceptance State = "awaiting_acceptance"
	StateConnected          State = "connected"
	StatePaused             State = "paused"
	StateReconnecting       State = "reconnecting"
	StateDisconnected       State = "disconnected"
	StateExpired            State = "expired"
	StateRevoked            State = "revoked"
	StateFailed             State = "failed"
)

// Terminal reports a state no session can leave: a new session, with a
// new code and a new acceptance, is the only way forward.
func (s State) Terminal() bool {
	return s == StateDisconnected || s == StateExpired || s == StateRevoked || s == StateFailed
}

// Active reports whether remote operations may currently be accepted.
func (s State) Active() bool { return s == StateConnected || s == StatePaused }

var transitions = map[State][]State{
	StateIdle:       {StateHosting, StateConnecting},
	StateHosting:    {StateAwaitingAcceptance, StateExpired, StateRevoked, StateFailed, StateDisconnected},
	StateConnecting: {StateAwaitingAcceptance, StateFailed, StateExpired, StateDisconnected, StateRevoked},
	// A verified join consumes the code, so a declined or unanswered
	// request ends the offer instead of returning to hosting.
	StateAwaitingAcceptance: {StateConnected, StateRevoked, StateExpired, StateFailed, StateDisconnected},
	StateConnected:          {StatePaused, StateReconnecting, StateDisconnected, StateRevoked, StateExpired, StateFailed},
	StatePaused:             {StateConnected, StateReconnecting, StateDisconnected, StateRevoked, StateExpired, StateFailed},
	StateReconnecting:       {StateConnected, StatePaused, StateDisconnected, StateRevoked, StateExpired, StateFailed},
}

// CanTransition reports whether from may move to to.
func CanTransition(from, to State) bool { return slices.Contains(transitions[from], to) }

// TransitionError is a refused state change.
type TransitionError struct{ From, To State }

func (e *TransitionError) Error() string {
	return fmt.Sprintf("support session cannot move from %s to %s", e.From, e.To)
}


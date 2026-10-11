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

// Machine enforces transitions and owns the connection generation. Every
// (re)connection increments the generation; anything stamped with an older
// generation is stale and is never applied to the current connection.
type Machine struct {
	mu         sync.Mutex
	state      State
	generation uint64
	reason     string
	watchers   []func(State, uint64)
}

// NewMachine starts idle.
func NewMachine() *Machine { return &Machine{state: StateIdle} }

// State returns the current state, generation and last reason.
func (m *Machine) State() (State, uint64, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state, m.generation, m.reason
}

// Watch registers a callback for every applied transition. Callbacks run
// outside the lock and must not block.
func (m *Machine) Watch(fn func(State, uint64)) {
	m.mu.Lock()
	m.watchers = append(m.watchers, fn)
	m.mu.Unlock()
}

// Move applies a transition. A move to the current state is a no-op.
func (m *Machine) Move(to State, reason string) error {
	return m.move(to, reason, false, 0)
}

// Connect enters connected with a new generation and returns it.
func (m *Machine) Connect(reason string) (uint64, error) {
	if err := m.move(StateConnected, reason, true, 0); err != nil {
		return 0, err
	}
	_, gen, _ := m.State()
	return gen, nil
}

// MoveIf applies a transition only while generation is current, so a late
// event from an old connection cannot change a newer one.
func (m *Machine) MoveIf(generation uint64, to State, reason string) error {
	return m.move(to, reason, false, generation)
}

func (m *Machine) move(to State, reason string, bump bool, generation uint64) error {
	m.mu.Lock()
	if generation != 0 && generation != m.generation {
		m.mu.Unlock()
		return Errorf(CodeStaleConnection, "event for connection %d ignored; current connection is %d", generation, m.generation)
	}
	if m.state == to && !bump {
		m.mu.Unlock()
		return nil
	}
	if !CanTransition(m.state, to) {
		from := m.state
		m.mu.Unlock()
		return &TransitionError{From: from, To: to}
	}
	m.state, m.reason = to, reason
	if bump {
		m.generation++
	}
	gen := m.generation
	watchers := slices.Clone(m.watchers)
	m.mu.Unlock()
	for _, w := range watchers {
		w(to, gen)
	}
	return nil
}

package voice

import (
	"fmt"
	"sync"
	"time"
)

// State is a call's lifecycle state.
type State string

// States. The last six are terminal.
const (
	StateIdle        State = "idle"
	StateRinging     State = "ringing"
	StateConnecting  State = "connecting"
	StateActive      State = "active"
	StateListening   State = "listening"
	StateSpeaking    State = "speaking"
	StateToolRunning State = "tool_running"
	StateEnding      State = "ending"

	StateEnded          State = "ended"
	StateRejected       State = "rejected"
	StateNoAnswer       State = "no_answer"
	StateFailed         State = "failed"
	StateCancelled      State = "cancelled"
	StateBudgetExceeded State = "budget_exceeded"
)

// Terminal reports whether no transition leaves s.
func (s State) Terminal() bool {
	switch s {
	case StateEnded, StateRejected, StateNoAnswer, StateFailed, StateCancelled, StateBudgetExceeded:
		return true
	}
	return false
}

// InConversation reports whether audio may flow in s.
func (s State) InConversation() bool {
	switch s {
	case StateActive, StateListening, StateSpeaking, StateToolRunning:
		return true
	}
	return false
}

var transitions = map[State][]State{
	StateIdle:        {StateRinging, StateConnecting, StateRejected, StateFailed, StateCancelled, StateBudgetExceeded},
	StateRinging:     {StateConnecting, StateNoAnswer, StateRejected, StateFailed, StateCancelled, StateBudgetExceeded},
	StateConnecting:  {StateActive, StateEnding, StateFailed, StateCancelled, StateBudgetExceeded},
	StateActive:      {StateListening, StateSpeaking, StateToolRunning, StateEnding, StateFailed, StateCancelled, StateBudgetExceeded},
	StateListening:   {StateSpeaking, StateToolRunning, StateEnding, StateFailed, StateCancelled, StateBudgetExceeded},
	StateSpeaking:    {StateListening, StateToolRunning, StateEnding, StateFailed, StateCancelled, StateBudgetExceeded},
	StateToolRunning: {StateListening, StateSpeaking, StateEnding, StateFailed, StateCancelled, StateBudgetExceeded},
	StateEnding:      {StateEnded, StateFailed, StateCancelled, StateBudgetExceeded},
}

// CanTransition reports whether from -> to is allowed.
func CanTransition(from, to State) bool {
	for _, s := range transitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

// StateChange is one recorded transition.
type StateChange struct {
	From State     `json:"from"`
	To   State     `json:"to"`
	At   time.Time `json:"at"`
}

// Machine is a concurrency-safe call state machine.
type Machine struct {
	mu      sync.Mutex
	state   State
	history []StateChange
	now     func() time.Time
	// done is closed on the first terminal transition.
	done chan struct{}
}

// NewMachine starts a machine in StateIdle.
func NewMachine(now func() time.Time) *Machine {
	if now == nil {
		now = time.Now
	}
	return &Machine{state: StateIdle, now: now, done: make(chan struct{})}
}

// State returns the current state.
func (m *Machine) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// Done is closed once the call reaches a terminal state.
func (m *Machine) Done() <-chan struct{} { return m.done }

// Transition moves to s. Re-entering the current state is a no-op.
func (m *Machine) Transition(s State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == s {
		return nil
	}
	if !CanTransition(m.state, s) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidState, m.state, s)
	}
	m.history = append(m.history, StateChange{From: m.state, To: s, At: m.now()})
	m.state = s
	if s.Terminal() {
		close(m.done)
	}
	return nil
}

// TransitionFrom moves to s only while the state is one of from. It
// reports whether it moved.
func (m *Machine) TransitionFrom(s State, from ...State) bool {
	m.mu.Lock()
	cur := m.state
	m.mu.Unlock()
	for _, f := range from {
		if f == cur {
			return m.Transition(s) == nil
		}
	}
	return false
}

// Finish ends the call in terminal state s, going through StateEnding when
// the call was in conversation. It is safe to call more than once; only the
// first terminal state sticks.
func (m *Machine) Finish(s State) State {
	m.mu.Lock()
	cur := m.state
	m.mu.Unlock()
	if cur.Terminal() {
		return cur
	}
	if cur.InConversation() && s == StateEnded {
		_ = m.Transition(StateEnding)
	}
	if err := m.Transition(s); err != nil {
		// Fall back to a terminal state every live state accepts.
		if m.Transition(StateCancelled) != nil {
			_ = m.Transition(StateFailed)
		}
	}
	return m.State()
}

// History returns the recorded transitions.
func (m *Machine) History() []StateChange {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]StateChange(nil), m.history...)
}

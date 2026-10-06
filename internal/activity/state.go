package activity

import "time"

// RunState is the work state of the run that owns a control surface. It is
// deliberately separate from presentation: an island finishing its opening
// motion never authorizes or resumes work.
type RunState uint8

const (
	// StateIdle is a surface without a run.
	StateIdle RunState = iota
	// StateActive is a run that owns the surface before its first tool.
	StateActive
	// StateThinking waits on the model between operations.
	StateThinking
	// StateTool is a tool operation that is executing.
	StateTool
	// StateAwaitQuestion is blocked on the user's answer to a question.
	StateAwaitQuestion
	// StateAwaitPermission is blocked on the user's permission decision.
	StateAwaitPermission
	// StateDelivering hands a submitted decision to its pending request.
	StateDelivering
	// StateResuming continues the same run after an accepted decision.
	StateResuming
	// StateDenied continues after a refused permission; it never reads as
	// progress on the refused action.
	StateDenied
	// StateDone is a run that completed without cancellation or error.
	StateDone
	// StateFailed is a verified tool failure the model is reacting to.
	StateFailed
	// StateCancelled is a stopped run whose surfaces were torn down.
	StateCancelled
)

func (s RunState) String() string {
	return [...]string{"idle", "active", "thinking", "tool", "await_question", "await_permission", "delivering", "resuming", "denied", "done", "failed", "cancelled"}[s]
}

// Awaiting reports whether the run is blocked on an explicit user decision.
func (s RunState) Awaiting() bool {
	return s == StateAwaitQuestion || s == StateAwaitPermission || s == StateDelivering
}

// Terminal reports whether the run can no longer change its own state.
func (s RunState) Terminal() bool { return s == StateDone || s == StateCancelled }

// runTransitions lists every permitted work-state change. Cancellation is
// always permitted and handled separately.
var runTransitions = map[RunState][]RunState{
	StateIdle:            {StateActive, StateTool},
	StateActive:          {StateThinking, StateTool, StateAwaitQuestion, StateAwaitPermission, StateDone, StateFailed},
	StateThinking:        {StateThinking, StateTool, StateAwaitQuestion, StateAwaitPermission, StateDone, StateFailed},
	StateTool:            {StateTool, StateThinking, StateAwaitQuestion, StateAwaitPermission, StateDone, StateFailed},
	StateAwaitQuestion:   {StateAwaitQuestion, StateAwaitPermission, StateDelivering, StateResuming, StateDenied, StateThinking},
	StateAwaitPermission: {StateAwaitQuestion, StateAwaitPermission, StateDelivering, StateResuming, StateDenied, StateThinking},
	StateDelivering:      {StateAwaitQuestion, StateAwaitPermission, StateResuming, StateDenied, StateThinking},
	StateResuming:        {StateResuming, StateThinking, StateTool, StateAwaitQuestion, StateAwaitPermission, StateDone, StateFailed},
	StateDenied:          {StateThinking, StateTool, StateAwaitQuestion, StateAwaitPermission, StateDone, StateFailed},
	StateFailed:          {StateFailed, StateThinking, StateTool, StateAwaitQuestion, StateAwaitPermission, StateDone},
	// A new operation on a surface whose previous run ended starts fresh.
	StateDone:      {StateIdle, StateActive, StateTool},
	StateCancelled: {StateIdle, StateActive, StateTool},
}

// CanTransition reports whether a work-state change is permitted.
func CanTransition(from, to RunState) bool {
	if to == StateCancelled {
		return true
	}
	for _, next := range runTransitions[from] {
		if next == to {
			return true
		}
	}
	return false
}

// phaseFor maps work state to the character expression.
func phaseFor(s RunState, previous Phase) Phase {
	switch s {
	case StateTool, StateActive:
		return PhaseWorking
	case StateThinking, StateResuming:
		return PhaseThinking
	case StateAwaitQuestion, StateAwaitPermission, StateDelivering:
		return PhaseWaiting
	case StateFailed, StateDenied:
		return PhaseFailed
	case StateDone:
		return PhaseDone
	}
	return previous
}

// enterLocked applies a permitted work-state change and its expression. A
// refused change keeps the current state so a stale callback cannot rewind
// a newer decision. Callers hold m.mu.
func (m *Manager) enterLocked(s RunState) bool {
	if m.event.State == s && s != StateTool {
		return true
	}
	if !CanTransition(m.event.State, s) {
		return false
	}
	now := time.Now()
	m.event.State, m.event.StateAt = s, now
	m.event.Phase, m.event.PhaseAt = phaseFor(s, m.event.Phase), now
	return true
}

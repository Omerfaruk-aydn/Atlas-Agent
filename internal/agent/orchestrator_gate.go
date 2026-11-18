package agent

import (
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
)

// maxIdleWorkerRounds is how many times in a row the orchestrator is sent
// back to collect its workers without collecting any of them in between.
// A round in which an answer was read starts the count again, so a long
// run of workers is waited for as long as the orchestrator keeps making
// progress, while a model that only keeps trying to stop is let go.
const maxIdleWorkerRounds = 3

// orchestrating reports whether the session wears the orchestrator mode.
func (a *sessionAgent) orchestrating() bool {
	if a.isSubAgent || a.taskContextConfig == nil {
		return false
	}
	cfg := a.taskContextConfig.Config()
	if cfg == nil || cfg.Options == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(cfg.Options.SessionMode), subagents.OrchestratorMode)
}

// workersGate keeps an orchestrator from ending its turn while workers it
// started in the background are still running or their answers are unread:
// a report written then would claim results nobody has seen. The caller
// holds l.mu.
func (a *sessionAgent) workersGate(sessionID string, l *evaluatorLedger) (evaluatorDecision, bool) {
	if !a.orchestrating() || !a.harnessOptions().GateEnabled("background_workers") {
		return evaluatorDecision{}, false
	}
	decision, stop := workersDecision(sharedBackgroundRuns.pending(sessionID), l)
	if stop {
		outcome := "send_back"
		if decision.Continue == "" {
			outcome = "unresolved"
		}
		a.sessionTrace(sessionID).record(harnessmetrics.TraceEvent{Kind: "gate", Name: "background_workers", Outcome: outcome, Reason: "workers_pending"})
	}
	return decision, stop
}

// workersDecision is the gate's verdict for one state of the workers.
func workersDecision(state workerState, l *evaluatorLedger) (evaluatorDecision, bool) {
	if len(state.Running) == 0 && len(state.Unread) == 0 {
		l.workerRounds = 0
		return evaluatorDecision{}, false
	}
	if state.Collected > l.workerCollected {
		l.workerCollected = state.Collected
		l.workerRounds = 0
	}
	var sb strings.Builder
	if len(state.Running) > 0 {
		sb.WriteString("These workers are still running:\n- " + strings.Join(state.Running, "\n- ") + "\n")
	}
	if len(state.Unread) > 0 {
		sb.WriteString("These workers finished but their answers were not read:\n- " + strings.Join(state.Unread, "\n- ") + "\n")
	}
	if l.workerRounds >= maxIdleWorkerRounds {
		return evaluatorDecision{Unresolved: sb.String() + "The orchestrator ended its turn without collecting them."}, true
	}
	l.workerRounds++
	sb.WriteString("Do not finish yet. Call agent_result with wait=true and no id to collect the next answer, once per worker, " +
		"and act on each one. Stop a worker you no longer need with agent_result cancel=true. " +
		"Report only after every worker is collected and the integrated result is verified.")
	return evaluatorDecision{Continue: sb.String()}, true
}

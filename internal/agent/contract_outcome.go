package agent

import "github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"

// The shared completion check counts a finished task as verified only when
// a verification tool ran for it. Work verified the usual way, with the
// work plan's commands run through bash, never qualified, so after the gate
// had already seen every check pass the agent was sent back to "verify"
// again, re-ran the same commands and still could not close the task. A
// contract that passed with no code change since is machine evidence for
// the whole request, so applied work counts as verified.

const contractVerifiedReason = "the work plan's verification contract passed after the last code change"

// verifiedByContract marks applied requirements verified while the
// session's verification contract is green.
func (a *sessionAgent) verifiedByContract(sessionID string, requirements []outcome.Requirement) []outcome.Requirement {
	if !a.contractGreen(sessionID) {
		return requirements
	}
	for i := range requirements {
		if requirements[i].Phase == outcome.PhaseApplied {
			requirements[i].Phase = outcome.PhaseVerified
			requirements[i].Reason = contractVerifiedReason
		}
	}
	return requirements
}

// contractGreen reports whether the contract passed for the current
// request and no code changed after it.
func (a *sessionAgent) contractGreen(sessionID string) bool {
	if a.evaluatorLedgers == nil {
		return false
	}
	ledger, ok := a.evaluatorLedgers.Get(sessionID)
	if !ok {
		return false
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	return ledger.greenEdit != 0 && ledger.greenEdit == ledger.lastEdit+1
}

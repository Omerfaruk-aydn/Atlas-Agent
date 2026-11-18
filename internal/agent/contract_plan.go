package agent

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

// planlessPrompt asks, once per request, for a work plan when the request has
// already changed many files without one. Large work with no written outcome
// and no verify commands is the case the contract exists for; a small fix
// never reaches the threshold. The threshold counts distinct files, so
// editing one file many times does not.
func (a *sessionAgent) planlessPrompt(root string, ledger *evaluatorLedger) string {
	threshold := a.workPlanOptions().AutoPlanThreshold()
	if threshold == 0 || ledger.planAsked || workplan.IsAtlasHome(root) {
		return ""
	}
	seen := map[string]bool{}
	for _, file := range ledger.files {
		if file != "" {
			seen[file] = true
		}
	}
	if len(seen) < threshold || planFileExists(root) {
		return ""
	}
	ledger.planAsked = true
	return fmt.Sprintf("This request has changed %d files and there is no work plan. A change this size needs one so it can be checked from the outside.\n\n"+
		"Write %s/%s now: the goal, the features as observable outcomes with acceptance criteria, the verify commands that prove the work "+
		"(build, tests, a run that exercises it), and the assumptions you made. Then run those commands, fix what they show, and give the final report.",
		len(seen), workplan.Dir, workplan.PlanFile)
}

// planFileExists reports whether a plan file is present, readable or not.
// A broken plan is reported by the gate itself and must not be asked for again.
func planFileExists(root string) bool {
	_, err := os.Stat(filepath.Join(root, workplan.Dir, workplan.PlanFile))
	return err == nil
}

package agent

import (
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

// An agent started in one folder often builds the project in another: the
// user's home is the working directory and the request says "make a folder
// on D:". The work plan then lands in that other folder, and every harness
// check that reads the plan from the working directory would read nothing,
// or worse, an earlier draft left behind. When the agent writes a work plan
// outside the working directory, that plan's project becomes the session's
// project root, and the gate, plan review, error memory, corpus and plan
// block all follow it.

// projectRoot is the folder harness files are read from for sessionID: the
// project the agent moved to, or the working directory.
func (a *sessionAgent) projectRoot(sessionID string) string {
	if a.projectRoots != nil {
		if root, ok := a.projectRoots.Get(sessionID); ok && root != "" {
			return root
		}
	}
	return a.workingDir
}

// ledgerRoot is the project root the ledger's request works in.
func (a *sessionAgent) ledgerRoot(ledger *evaluatorLedger) string {
	if ledger != nil && ledger.root != "" {
		return ledger.root
	}
	return a.workingDir
}

// planFileRoot returns the project folder of path when path is a work plan
// file (<root>/.atlas/work.json), or "".
func planFileRoot(path string) string {
	path = filepath.Clean(path)
	if !strings.EqualFold(filepath.Base(path), workplan.PlanFile) {
		return ""
	}
	dir := filepath.Dir(path)
	if !strings.EqualFold(filepath.Base(dir), workplan.Dir) {
		return ""
	}
	return filepath.Dir(dir)
}

// notePlanWrite moves the session's project root to the project of a work
// plan the agent is writing. Scratch worktrees (probes, alternatives) are
// copies of the project, not a new one, and are ignored.
func (a *sessionAgent) notePlanWrite(sessionID, path string) {
	root := planFileRoot(path)
	if a.projectRoots == nil || root == "" || workplan.IsScratchDir(root) || sameRoot(root, a.projectRoot(sessionID)) {
		return
	}
	if sameRoot(root, a.workingDir) {
		a.projectRoots.Del(sessionID)
		root = a.workingDir
	} else {
		a.projectRoots.Set(sessionID, root)
	}
	if ledger, ok := a.evaluatorLedgers.Get(sessionID); ok {
		ledger.mu.Lock()
		ledger.root = root
		ledger.mu.Unlock()
	}
	slog.Info("Work plan moved to another project folder", "session_id", sessionID, "root", root)
}

func sameRoot(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

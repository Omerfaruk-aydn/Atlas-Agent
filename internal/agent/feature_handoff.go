package agent

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/shell"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

// Long work is built feature by feature, and once a feature is finished and
// checked, almost everything said while building it is spent: the files it
// read, the output of every run, the attempts that failed. Carrying it into
// the next feature resends it on every step and brings a compaction sooner,
// and a compaction is a model's summary of the conversation, which can drop
// what matters. The work plan and the progress notes are the record of the
// work by design, so at that boundary the conversation is replaced by a
// handoff built from them, with no model call: the user's own messages,
// the progress notes, the files changed, the jobs still running and the
// next feature. The plan itself is shown again with every request.
//
// A handoff happens only when nothing can be lost: a feature was marked
// done during the turn, every check the contract requires passed after the
// last change, the progress notes were written after the plan changed, and
// the context is large enough for the saving to matter.

// featureHandoffAt is the share of the context window in use above which a
// finished feature hands off. Below it the conversation is cheap to keep.
const featureHandoffAt = 0.4

// handoff limits keep the handoff message a fraction of what it replaces.
const (
	handoffMaxTurnText = 4 << 10
	handoffMaxProgress = 8 << 10
	handoffMaxFiles    = 60
)

// handoffWatch follows one turn for a feature boundary.
type handoffWatch struct {
	// baseline holds the features done when the turn started; only a
	// feature finished during the turn hands off.
	baseline map[string]bool
	// scratch replays the turn's steps on a copy of the evaluator ledger,
	// so the contract can be checked mid-turn without touching the ledger
	// the completion gate reads at the end of the turn.
	scratch *evaluatorLedger
	seen    int
	// feature is the finished feature that triggered the handoff.
	feature workplan.Feature
}

// newHandoffWatch prepares the watch for a turn, or returns nil when a
// handoff cannot happen in it.
func (a *sessionAgent) newHandoffWatch(sessionID string) *handoffWatch {
	if a.isSubAgent || a.engineering == nil || a.workingDir == "" || a.disableAutoSummarize.Get() ||
		!a.completionGateActive() || !a.workPlanOptions().FeatureHandoffEnabled() {
		return nil
	}
	plan, _, err := workplan.Load(a.projectRoot(sessionID))
	if err != nil || plan == nil || len(plan.Open()) == 0 {
		return nil
	}
	baseline := map[string]bool{}
	for _, id := range plan.DoneIDs() {
		baseline[id] = true
	}
	ledger := a.evaluatorLedgerFor(sessionID)
	ledger.mu.Lock()
	scratch := &evaluatorLedger{root: ledger.root, seq: ledger.seq, lastEdit: ledger.lastEdit, runs: maps.Clone(ledger.runs), noImages: true}
	ledger.mu.Unlock()
	return &handoffWatch{baseline: baseline, scratch: scratch}
}

// due reports whether the turn has reached a feature boundary that can hand
// off. It is called after every step with the steps so far.
func (w *handoffWatch) due(root string, steps []fantasy.StepResult, contextWindow, tokens int64) bool {
	if w == nil {
		return false
	}
	if contextWindow <= 0 || float64(tokens) < featureHandoffAt*float64(contextWindow) {
		return false
	}
	plan, _, err := workplan.Load(root)
	if err != nil || plan == nil || len(plan.Open()) == 0 {
		return false
	}
	finished, ok := w.finishedFeature(plan)
	if !ok || !notesCurrent(root) {
		return false
	}
	// The cheap conditions hold; replay the steps not yet seen.
	if w.seen < len(steps) {
		w.scratch.record(steps[w.seen:])
		w.seen = len(steps)
	}
	requirements := plan.Requirements()
	if len(requirements) == 0 || len(w.scratch.contractGaps(requirements)) > 0 {
		return false
	}
	w.feature = finished
	return true
}

// finishedFeature returns the latest feature marked done during the turn.
func (w *handoffWatch) finishedFeature(plan *workplan.Plan) (workplan.Feature, bool) {
	for _, feature := range slices.Backward(plan.Features) {
		if feature.Done && !w.baseline[feature.ID] {
			return feature, true
		}
	}
	return workplan.Feature{}, false
}


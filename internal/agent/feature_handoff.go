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

// notesCurrent reports whether the progress notes were written at or after
// the last change to the plan, so they describe the finished feature.
func notesCurrent(root string) bool {
	planInfo, err := os.Stat(filepath.Join(root, workplan.Dir, workplan.PlanFile))
	if err != nil {
		return false
	}
	notesInfo, err := os.Stat(filepath.Join(root, workplan.Dir, workplan.ProgressFile))
	if err != nil {
		return false
	}
	return !notesInfo.ModTime().Before(planInfo.ModTime())
}

// handOff replaces the conversation with a handoff for the next feature.
// It saves the handoff as the session's summary, the same way a compaction
// does, so the next request starts from it.
// scratchFiles are the files edited in the turn so far, which the ledger
// records only when the turn ends.
func (a *sessionAgent) handOff(ctx context.Context, sessionID string, finished workplan.Feature, scratchFiles []string) error {
	root := a.projectRoot(sessionID)
	plan, progress, err := workplan.Load(root)
	if err != nil || plan == nil {
		return fmt.Errorf("read work plan: %w", err)
	}
	state, err := a.engineering.Read(ctx, engineering.GetScope(ctx, sessionID).SessionID)
	if err != nil {
		return err
	}
	if state.Work == nil || len(state.Work.Turns) == 0 {
		return fmt.Errorf("no recorded user request to hand off")
	}
	ledger := a.evaluatorLedgerFor(sessionID)
	ledger.mu.Lock()
	changed := slices.Clone(ledger.files)
	ledger.mu.Unlock()
	for _, path := range scratchFiles {
		if !slices.Contains(changed, path) {
			changed = append(changed, path)
		}
	}
	currentSession, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	text := handoffText(handoffInput{
		finished: finished,
		plan:     plan,
		progress: progress,
		turns:    state.Work.Turns,
		changed:  changed,
		jobs:     runningJobs(root),
	})
	large := a.largeModel.Get()
	summary, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:             message.Assistant,
		Model:            large.ModelCfg.Model,
		Provider:         large.ModelCfg.Provider,
		IsSummaryMessage: true,
	})
	if err != nil {
		return err
	}
	summary.AppendContent(text)
	summary.AddFinish(message.FinishReasonEndTurn, "", "")
	if err := a.messages.Update(ctx, summary); err != nil {
		return err
	}
	a.recordHarness(harnessmetrics.FeatureHandoff, sessionID, nil)
	a.markCompacted(sessionID)
	currentSession.SummaryMessageID = summary.ID
	currentSession.PromptTokens = 0
	currentSession.CompletionTokens = int64(len(text) / 4)
	currentSession.EstimatedUsage = true
	if _, err := a.sessions.Save(ctx, currentSession); err != nil {
		return err
	}
	slog.Info("Handed off to a fresh context after a finished feature", "session_id", sessionID, "feature", finished.ID)
	return nil
}

// handoffInput is what a handoff is built from.
type handoffInput struct {
	finished workplan.Feature
	plan     *workplan.Plan
	progress string
	turns    []engineering.UserTurn
	changed  []string
	jobs     []shell.BackgroundShellInfo
}

// handoffText builds the message that replaces the conversation.
func handoffText(in handoffInput) string {
	var b strings.Builder
	open := in.plan.Open()
	fmt.Fprintf(&b, "# Handoff after feature %s\n\n", in.finished.ID)
	fmt.Fprintf(&b, "Feature %s (%s) is done and every check the contract requires passed after the last change. ", in.finished.ID, in.finished.Title)
	b.WriteString("The conversation that built it was replaced by this handoff so the next feature starts with a clean context. ")
	b.WriteString("Nothing here was summarized by a model: it is assembled from the work plan, the progress notes and the user's messages. ")
	fmt.Fprintf(&b, "The work plan (%s/%s) and its open features are shown with every request.\n\n", workplan.Dir, workplan.PlanFile)
	b.WriteString("You no longer remember the contents of any file or the output of any command. View a file before you edit it.\n")

	b.WriteString("\n## The user's messages, verbatim\n\n")
	for _, turn := range in.turns {
		if turn.Kind == "continuation" {
			continue
		}
		text := strings.TrimSpace(turn.Text)
		if len(text) > handoffMaxTurnText {
			text = strings.ToValidUTF8(text[:handoffMaxTurnText], "") + " [truncated]"
		}
		fmt.Fprintf(&b, "%d. %s\n", turn.Seq, strings.ReplaceAll(text, "\n", "\n   "))
	}

	if notes := strings.TrimSpace(in.progress); notes != "" {
		if len(notes) > handoffMaxProgress {
			notes = "[earlier notes omitted]\n" + strings.ToValidUTF8(notes[len(notes)-handoffMaxProgress:], "")
		}
		fmt.Fprintf(&b, "\n## Progress notes (%s/%s)\n\n%s\n", workplan.Dir, workplan.ProgressFile, notes)
	}

	if len(in.changed) > 0 {
		files := slices.Sorted(slices.Values(in.changed))
		b.WriteString("\n## Files changed for this request\n\n")
		for _, path := range files[:min(len(files), handoffMaxFiles)] {
			b.WriteString("- " + path + "\n")
		}
		if len(files) > handoffMaxFiles {
			fmt.Fprintf(&b, "- and %d more\n", len(files)-handoffMaxFiles)
		}
	}

	if len(in.jobs) > 0 {
		b.WriteString("\n## Background jobs still running\n\n")
		for _, job := range in.jobs {
			fmt.Fprintf(&b, "- %s: %s (started %s)\n", job.ID, job.Command, job.StartedAt.Format(time.TimeOnly))
		}
		b.WriteString("Read them with job_output and stop the ones you no longer need with job_kill.\n")
	}

	b.WriteString("\n## Next\n\n")
	if len(open) > 0 {
		next := open[0]
		fmt.Fprintf(&b, "Continue with feature %s: %s.", next.ID, next.Title)
		if acceptance := strings.TrimSpace(next.Acceptance); acceptance != "" {
			b.WriteString(" Acceptance: " + acceptance)
		}
		b.WriteString("\n")
	}
	b.WriteString("If the progress notes name a different next step, follow the notes. Keep them current as you work.\n")
	return b.String()
}


package agent

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

// Handoff starts a linked child session with an evidence-based continuation
// packet. It deliberately uses durable state rather than a model-written
// summary, so exact user constraints and verification claims are not paraphrased.
func (a *sessionAgent) Handoff(ctx context.Context, sessionID string) (session.Session, error) {
	if a.IsSessionBusy(sessionID) {
		return session.Session{}, ErrSessionBusy
	}
	source, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return session.Session{}, err
	}
	root := a.projectRoot(sessionID)
	plan, progress, planErr := workplan.Load(root)
	if planErr != nil {
		return session.Session{}, fmt.Errorf("read handoff work plan: %w", planErr)
	}
	var turns []engineering.UserTurn
	if a.engineering != nil {
		state, err := a.engineering.Read(ctx, engineering.GetScope(ctx, sessionID).SessionID)
		if err != nil {
			return session.Session{}, err
		}
		if state.Work != nil {
			turns = state.Work.Turns
		}
	}
	ledger := a.evaluatorLedgerFor(sessionID)
	ledger.mu.Lock()
	changed := slices.Clone(ledger.files)
	checks := make([]string, 0, len(ledger.runs))
	for command, run := range ledger.runs {
		if !run.failed {
			checks = append(checks, command)
		}
	}
	ledger.mu.Unlock()
	slices.Sort(checks)

	child, err := a.sessions.CreateChild(ctx, sessionID, "Handoff: "+source.Title)
	if err != nil {
		return session.Session{}, err
	}
	text := sessionHandoffText(source, plan, progress, turns, changed, checks)
	model := a.largeModel.Get()
	summary, err := a.messages.Create(ctx, child.ID, message.CreateMessageParams{
		Role: message.Assistant, Model: model.ModelCfg.Model, Provider: model.ModelCfg.Provider, IsSummaryMessage: true,
	})
	if err != nil {
		return session.Session{}, err
	}
	summary.AppendContent(text)
	summary.AddFinish(message.FinishReasonEndTurn, "", "")
	if err := a.messages.Update(ctx, summary); err != nil {
		return session.Session{}, err
	}
	child.SummaryMessageID = summary.ID
	child.CompletionTokens = int64(len(text) / 4)
	child.EstimatedUsage = true
	child, err = a.sessions.Save(ctx, child)
	if err != nil {
		return session.Session{}, err
	}
	a.recordHarness(harnessmetrics.FeatureHandoff, sessionID, nil)
	return child, nil
}

func sessionHandoffText(source session.Session, plan *workplan.Plan, progress string, turns []engineering.UserTurn, changed, checks []string) string {
	var b strings.Builder
	b.WriteString("# Clean-session handoff\n\n")
	fmt.Fprintf(&b, "Continue the work from linked parent session %s. This packet was assembled from durable state, not a model summary. Read files again before editing them.\n", source.ID)
	if goal := strings.TrimSpace(source.Goal); goal != "" {
		fmt.Fprintf(&b, "\n## Goal\n\n%s\n", goal)
	}
	b.WriteString("\n## User requests and constraints, verbatim\n")
	for _, turn := range turns {
		if turn.Kind != "continuation" && strings.TrimSpace(turn.Text) != "" {
			fmt.Fprintf(&b, "\n- %s", strings.ReplaceAll(turn.Text, "\n", "\n  "))
		}
	}
	if len(turns) == 0 {
		b.WriteString("\n- No additional recorded constraints.\n")
	}
	if plan != nil {
		b.WriteString("\n\n## Contract status\n")
		for _, feature := range plan.Features {
			status := "open"
			if feature.Done {
				status = "done"
			}
			fmt.Fprintf(&b, "\n- [%s] %s: %s", status, feature.ID, feature.Title)
			if feature.Acceptance != "" {
				fmt.Fprintf(&b, " — %s", feature.Acceptance)
			}
		}
		b.WriteByte('\n')
	}
	if len(changed) > 0 {
		b.WriteString("\n## Changed files\n")
		for _, path := range changed {
			fmt.Fprintf(&b, "\n- %s", path)
		}
		b.WriteByte('\n')
	}
	if len(checks) > 0 {
		b.WriteString("\n## Verification evidence observed in the parent\n")
		for _, check := range checks {
			fmt.Fprintf(&b, "\n- PASS: %s", check)
		}
		b.WriteString("\n\nRe-run required checks after any new change; do not treat old evidence as current proof.\n")
	}
	if notes := strings.TrimSpace(progress); notes != "" {
		fmt.Fprintf(&b, "\n## Decisions and progress notes\n\n%s\n", notes)
	}
	b.WriteString("\n## Open work\n\nContinue unfinished contract items and the user's latest request. Preserve all constraints above.\n")
	return b.String()
}

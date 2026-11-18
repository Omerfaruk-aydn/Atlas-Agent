package agent

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

// fallbackKeepTurns is how many of the user's most recent requests the
// fallback summary quotes verbatim, in addition to the first one.
const fallbackKeepTurns = 8

// fallbackHeader replaces the handoff packet's opening, which speaks of a
// linked parent session; a compaction summary stays in the same session.
const fallbackHeader = "# Context summary (assembled without a model)\n\n" +
	"The model that writes summaries failed, so this record was assembled from durable state: " +
	"your requests, the work plan, the files changed and the checks that passed. " +
	"Earlier messages are no longer in context. Read files again before editing them.\n"

// fallbackSummaryText builds the deterministic summary a failed compaction
// falls back to. It reuses the handoff packet's body so the two never drift,
// bounds the quoted user turns, and swaps the opening for one that fits a
// summary inside the same session.
func fallbackSummaryText(source session.Session, plan *workplan.Plan, progress string, turns []engineering.UserTurn, changed, checks []string) string {
	text := sessionHandoffText(source, plan, progress, boundUserTurns(turns), changed, checks)
	_, body, found := strings.Cut(text, "\n## ")
	if !found {
		return fallbackHeader + "\n" + text
	}
	return fallbackHeader + "\n## " + body
}

// boundUserTurns keeps the first request and the most recent
// fallbackKeepTurns, skipping automatic continuations. The first request
// states the goal; the latest ones carry the constraints that still apply.
func boundUserTurns(turns []engineering.UserTurn) []engineering.UserTurn {
	var real []engineering.UserTurn
	for _, turn := range turns {
		if turn.Kind != "continuation" && strings.TrimSpace(turn.Text) != "" {
			real = append(real, turn)
		}
	}
	if len(real) <= fallbackKeepTurns+1 {
		return real
	}
	return append([]engineering.UserTurn{real[0]}, real[len(real)-fallbackKeepTurns:]...)
}

// fallbackSummary writes a deterministic summary message into the session
// and makes it the compaction point, exactly as a model-written summary
// would. It is the last resort when both the compact model and the session's
// own model could not summarize: a session left above its context budget
// retries the same failing call on every later turn and never recovers.
func (a *sessionAgent) fallbackSummary(ctx context.Context, sessionID string) error {
	current, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	plan, progress, err := workplan.Load(a.projectRoot(sessionID))
	if err != nil {
		slog.Warn("Fallback summary could not read the work plan", "session_id", sessionID, "error", err)
		plan, progress = nil, ""
	}
	var turns []engineering.UserTurn
	if a.engineering != nil {
		if state, err := a.engineering.Read(ctx, engineering.GetScope(ctx, sessionID).SessionID); err == nil && state.Work != nil {
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

	text := fallbackSummaryText(current, plan, progress, turns, changed, checks)
	model := a.largeModel.Get()
	summary, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role: message.Assistant, Model: model.ModelCfg.Model, Provider: model.ModelCfg.Provider, IsSummaryMessage: true,
	})
	if err != nil {
		return err
	}
	summary.AppendContent(text)
	summary.AddFinish(message.FinishReasonEndTurn, "", "")
	if err := a.messages.Update(ctx, summary); err != nil {
		return err
	}
	current.SummaryMessageID = summary.ID
	current.CompletionTokens = int64(len(text) / 4)
	current.PromptTokens = 0
	current.EstimatedUsage = true
	_, err = a.sessions.Save(ctx, current)
	return err
}

// summarizeFailureNeedsFallback reports whether a failed summarize call
// should fall back to the deterministic summary. A cancelled or busy call is
// not a failure of the summary model, and an error that arrives after a new
// summary was already saved came from running a queued message, not from
// summarizing.
func (a *sessionAgent) summarizeFailureNeedsFallback(ctx context.Context, sessionID, summaryBefore string, err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, ErrSessionBusy) {
		return false
	}
	// A history too large for the summary model is not an outage: the turn
	// stops with that stated reason (see contextOverflowError) rather than
	// quietly replacing the history with a record built without a model.
	var overflow *contextOverflowError
	if errors.As(err, &overflow) {
		return false
	}
	after, getErr := a.sessions.Get(ctx, sessionID)
	if getErr != nil {
		return false
	}
	return after.SummaryMessageID == summaryBefore
}

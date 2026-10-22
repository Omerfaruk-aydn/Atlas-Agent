package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

// Every later step of long work stands on the plan: the features decide
// what gets built and the checks decide when it counts as done. A plan with
// a missing feature or a check that cannot fail sends hours of work in the
// wrong direction, and the agent that wrote it is the one least likely to
// notice. So a new or changed plan gets one review by a separate model with
// a fresh context, before the work goes on. Marking features done does not
// change what is reviewed, so progress never triggers another review.
//
// A revision made for the findings is a new version, and an open-ended
// reviewer always finds one more refinement: reviewing every version sent
// one run through five rounds that grew the plan from 10 to 62 pixel-probe
// tests while the user's own report waited. So a plan gets at most
// maxPlanReviews reviews, and the follow-up only checks that the earlier
// findings were addressed.

// planReviewMaxSteps bounds the reviewer's tool use; it mostly reads the
// plan it is given.
const planReviewMaxSteps = 8

// maxPlanReviews bounds the reviews of one plan: the first review and one
// follow-up on the revision.
const maxPlanReviews = 2

const planFollowUpPrompt = `This is a follow-up review. The agent already revised the plan for the earlier findings below. Fail only when one of those findings is still clearly unaddressed, or when a feature the request clearly needs is still missing. Do not raise new refinements, stricter thresholds or extra tests: those were the first review's job.

Earlier findings:
`

const planReviewSystemPrompt = `You review the work plan of an AI coding agent before it builds. The plan is the agent's own definition of the work: its features decide what gets built and its verify commands decide when the work counts as done. You have read-only tools to look at the project if you need to.

Fail the plan only for concrete problems that would make the result wrong or unverified:
- A feature the request clearly needs is missing, or a feature contradicts the request.
- A verify command cannot fail, or does not exercise the behavior it claims to prove (it only builds, only lints, only checks that a file exists, or swallows its exit status).
- An acceptance criterion is too vague to tell done from not done.
- The order is impossible: a feature depends on one listed after it.
Do not fail for style, wording, missing nice-to-haves or a different but valid design.

Reply in exactly this format and nothing else:
VERDICT: PASS
or
VERDICT: FAIL
- <problem: which feature or check, what is wrong, what would fix it>
- <one line per problem>`

// reviewPlan reviews the plan once per version. Optional mode preserves the
// historical best-effort behavior; risky and required policies fail closed
// when their required review cannot produce a valid verdict. The caller
// holds ledger.mu.
func (a *sessionAgent) reviewPlan(ctx context.Context, sessionID string, ledger *evaluatorLedger, plan *workplan.Plan) (decision evaluatorDecision, stop bool) {
	ctx, observation := startGate(ctx, "plan_review")
	outcome, reason := "skipped", "already_reviewed"
	defer func() {
		if stop {
			observation.decision(decision, true)
		} else {
			observation.finish(outcome, reason)
		}
	}()
	required := planReviewRequired(a.workPlanOptions().PlanReviewPolicy(), plan)
	reviewKey := plan.ReviewKey()
	if required {
		reviewKey += "\nrequired-review-v1"
	}
	key := workplan.Hash(reviewKey)
	if ledger.planReviewed == key {
		return evaluatorDecision{}, false
	}
	root := a.ledgerRoot(ledger)
	if workplan.LoadSessionState(root, sessionID).ReviewedPlan == key {
		ledger.planReviewed = key
		return evaluatorDecision{}, false
	}
	fail := func(cause string) (evaluatorDecision, bool) {
		if required {
			outcome = "fail_closed"
			return requiredPlanReviewFailure(cause)
		}
		return evaluatorDecision{}, false
	}
	// Record the version first: a review that fails is not retried on
	// every turn at the user's expense.
	ledger.planReviewed = key
	var earlier string
	exhausted := false
	if err := workplan.UpdateState(root, sessionID, func(state *workplan.State) error {
		state.ReviewedPlan = key
		if goal := workplan.Hash(plan.Goal); state.ReviewedGoal != goal {
			state.ReviewedGoal, state.PlanReviews, state.ReviewFindings = goal, 0, ""
		}
		if state.PlanReviews >= maxPlanReviews {
			exhausted = true
			return nil
		}
		earlier = state.ReviewFindings
		state.PlanReviews++
		return nil
	}); err != nil {
		slog.Warn("Failed to save work plan state", "error", err)
		return fail("review state could not be saved")
	}
	if exhausted {
		reason = "review_budget_exhausted"
		return fail("review budget is exhausted")
	}

	model, ok := a.reviewModel()
	if !ok {
		reason = "reviewer_unavailable"
		return fail("reviewer is unavailable")
	}
	raw, err := os.ReadFile(filepath.Join(a.ledgerRoot(ledger), workplan.Dir, workplan.PlanFile))
	if err != nil {
		outcome, reason = "fail_open", "plan_read_failed"
		slog.Warn("Failed to read work plan for review", "session_id", sessionID, "error", err)
		return fail("plan could not be read")
	}
	prompt := "User request:\n" + evaluatorTruncate(ledger.request) + "\n\nThe plan (" + workplan.Dir + "/" + workplan.PlanFile + "):\n" + evaluatorTruncate(string(raw))
	if earlier != "" {
		prompt = planFollowUpPrompt + earlier + "\n\n" + prompt
	}
	outcome, reason = "fail_open", "review_unavailable"
	reply, err := a.runReviewer(ctx, sessionID, model, planReviewSystemPrompt, prompt, nil, a.evaluatorTools.Copy(), planReviewMaxSteps)
	if err != nil {
		slog.Warn("Plan review failed", "session_id", sessionID, "error", err)
		return fail("reviewer execution failed")
	}
	pass, findings, parsed := parseEvaluatorVerdict(reply)
	a.recordHarness(harnessmetrics.PlanReviewed, sessionID, nil)
	if pass {
		outcome, reason = "pass", "review_accepted"
		return evaluatorDecision{}, false
	}
	if !parsed {
		reason = "invalid_report"
		slog.Warn("Plan review returned no usable verdict", "session_id", sessionID)
		return fail("reviewer returned an invalid verdict")
	}
	a.recordHarness(harnessmetrics.PlanReviewFindings, sessionID, nil)
	if err := workplan.UpdateState(root, sessionID, func(state *workplan.State) error {
		state.ReviewFindings = evaluatorTruncate(strings.TrimSpace(findings))
		return nil
	}); err != nil {
		slog.Warn("Failed to save work plan state", "error", err)
		return fail("review findings could not be saved")
	}
	return evaluatorDecision{Continue: "A reviewer with a fresh context read your work plan before the work goes further and found problems:\n\n" +
		strings.TrimSpace(findings) + "\n\nRevise " + workplan.Dir + "/" + workplan.PlanFile + " where a finding holds and say briefly why where it does not. " +
		"Then continue the work against the revised plan."}, true
}

// reviewModel picks the model for independent reviews: the evaluator's,
// else the advisor's, else the session's own large model, which still
// reviews independently because it starts from a fresh context.
func (a *sessionAgent) reviewModel() (Model, bool) {
	if m := a.evaluatorModel.Get().v; m != nil {
		return *m, true
	}
	if m := a.advisorModel.Get().v; m != nil {
		return *m, true
	}
	if a.largeModel == nil {
		return Model{}, false
	}
	m := a.largeModel.Get()
	return m, m.Model != nil
}

// workPlanSummaryNote tells the summarizer that the work plan survives
// compaction on its own, so the summary spends its words on what the plan
// does not hold: the state of the feature in progress.
func workPlanSummaryNote(root string) string {
	plan, _, err := workplan.Load(root)
	if err != nil || plan == nil {
		return ""
	}
	return "\n\n## Work plan\n\nA work plan exists (" + workplan.Dir + "/" + workplan.PlanFile + " and " + workplan.ProgressFile +
		"). It is shown again in full after this summary, so do not copy its features or checks. Spend the summary on what the plan " +
		"does not hold: the feature in progress and its id, the exact last failing command with its key error line, the files " +
		"changed for it, approaches tried that failed and why, and the exact next step."
}

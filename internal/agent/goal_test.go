package agent

import (
	"context"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func goalTestCoordinator(sessionID string, run *goalRun) *coordinator {
	c := &coordinator{goalRuns: csync.NewMap[string, *goalRun]()}
	if run != nil {
		c.goalRuns.Set(sessionID, run)
	}
	return c
}

func callGoalTool(t *testing.T, c *coordinator, sessionID string, params GoalParams) fantasy.ToolResponse {
	t.Helper()
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, sessionID)
	resp, err := c.goalTool().Run(ctx, fantasy.ToolCall{ID: "call", Name: GoalToolName, Input: mustJSON(t, params)})
	require.NoError(t, err)
	return resp
}

func mustJSON(t *testing.T, params GoalParams) string {
	t.Helper()
	switch {
	case params.Turns != 0:
		return `{"action":"` + params.Action + `","turns":` + itoa(params.Turns) + `}`
	default:
		return `{"action":"` + params.Action + `"}`
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}

// A budget beyond the ceiling is capped rather than refused: the agent
// asking for more than it may have is a misjudgement of the work, not a
// reason to leave the run on the default.
func TestGoalToolCapsTheBudgetAtTheCeiling(t *testing.T) {
	run := &goalRun{goal: "ship it", budget: goalDefaultBudget}
	c := goalTestCoordinator("s1", run)

	resp := callGoalTool(t, c, "s1", GoalParams{Action: "budget", Turns: goalHardCeiling + 500})
	require.False(t, resp.IsError)

	_, budget, _, _, _, _ := run.snapshot()
	require.Equal(t, goalHardCeiling, budget, "a budget over the ceiling must be capped, not honoured")
	require.Contains(t, resp.Content, "capped")
}

func TestGoalToolAcceptsABudgetWithinTheCeiling(t *testing.T) {
	run := &goalRun{goal: "ship it", budget: goalDefaultBudget}
	c := goalTestCoordinator("s1", run)

	require.False(t, callGoalTool(t, c, "s1", GoalParams{Action: "budget", Turns: 30}).IsError)

	_, budget, _, _, _, _ := run.snapshot()
	require.Equal(t, 30, budget)
}

func TestGoalToolRejectsANonPositiveBudget(t *testing.T) {
	run := &goalRun{goal: "ship it", budget: goalDefaultBudget}
	c := goalTestCoordinator("s1", run)

	require.True(t, callGoalTool(t, c, "s1", GoalParams{Action: "budget", Turns: 0}).IsError)

	_, budget, _, _, _, _ := run.snapshot()
	require.Equal(t, goalDefaultBudget, budget, "a rejected budget must leave the old one alone")
}

// "done" records a claim; it does not end the run on its own. The check
// at the end of the turn is what ends it, and that separation is the
// point of the tool.
func TestGoalToolDoneOnlyRecordsAClaim(t *testing.T) {
	run := &goalRun{goal: "ship it", budget: goalDefaultBudget}
	c := goalTestCoordinator("s1", run)

	require.False(t, callGoalTool(t, c, "s1", GoalParams{Action: "done"}).IsError)

	_, _, _, claimed, done, _ := run.snapshot()
	require.True(t, claimed, "the claim must be recorded")
	require.False(t, done, "claiming is not finishing; the check decides that")
}

func TestGoalToolRejectsAnUnknownAction(t *testing.T) {
	c := goalTestCoordinator("s1", &goalRun{goal: "ship it", budget: goalDefaultBudget})
	require.True(t, callGoalTool(t, c, "s1", GoalParams{Action: "whatever"}).IsError)
}

func TestGoalToolErrorsWhenNoGoalIsSet(t *testing.T) {
	c := goalTestCoordinator("s1", nil)
	require.True(t, callGoalTool(t, c, "s1", GoalParams{Action: "done"}).IsError)
}

// A coordinator built field by field -- which tests and some callers do
// -- has no goal map. Ending a turn on one must not panic.
func TestAdvanceGoalIsSafeWithoutAGoalMap(t *testing.T) {
	c := &coordinator{}
	require.NotPanics(t, func() {
		c.advanceGoal(t.Context(), "s1", nil)
	})

	_, _, _, ok := c.GoalStatus("s1")
	require.False(t, ok)
}

// A coordinator with neither a goal map nor a session service must not
// panic when a turn ends -- that is the shape tests and some callers
// build, and it is also what resumeGoalRun sees before anything is set.
func TestResumeGoalRunIsSafeWithNothingWiredUp(t *testing.T) {
	c := &coordinator{}
	run, ok := c.resumeGoalRun(t.Context(), "s1")
	require.False(t, ok)
	require.Nil(t, run)
}

// An in-memory run is returned as-is rather than being rebuilt, so its
// turn count and budget survive.
func TestResumeGoalRunKeepsTheRunItAlreadyHas(t *testing.T) {
	existing := &goalRun{goal: "ship it", budget: 30, used: 7}
	c := goalTestCoordinator("s1", existing)

	run, ok := c.resumeGoalRun(t.Context(), "s1")
	require.True(t, ok)
	require.Same(t, existing, run)

	_, budget, used, _, _, _ := run.snapshot()
	require.Equal(t, 30, budget)
	require.Equal(t, 7, used)
}

// A nil goal judge (never configured) must not block a goal claim -- the
// agent's own claim stands, the same as before goalJudge became live-
// reloadable.
func TestJudgeGoalWithNoJudgeTakesTheClaimAtFaceValue(t *testing.T) {
	c := &coordinator{goalJudge: csync.NewValue(ptrBox[Model]{})}
	ok, reason := c.judgeGoal(t.Context(), "s1", "ship the feature")
	require.True(t, ok)
	require.Empty(t, reason)
}

// A goal judge that was configured must stop being consulted immediately
// once a live config change clears it (e.g. the "goal" role is removed
// and the advisor it fell back to is also disabled) -- mirrors the "off"
// tests added for hooks, the advisor, and AutoEscalate.
func TestJudgeGoalStopsConsultingAClearedJudge(t *testing.T) {
	c := &coordinator{goalJudge: csync.NewValue(ptrBox[Model]{v: &Model{}})}
	require.NotNil(t, c.goalJudge.Get().v)

	c.goalJudge.Set(ptrBox[Model]{})

	ok, reason := c.judgeGoal(t.Context(), "s1", "ship the feature")
	require.True(t, ok, "with the judge cleared, the agent's own claim must stand again")
	require.Empty(t, reason)
}

package agent

import (
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/stretchr/testify/require"
)

func TestParseAdvisorReplyNone(t *testing.T) {
	severity, note := parseAdvisorReply("NONE: nothing to add")
	require.Equal(t, "NONE", severity)
	require.Equal(t, "nothing to add", note)
}

func TestParseAdvisorReplyEachSeverity(t *testing.T) {
	cases := map[string]string{
		"NIT: minor style thing":         "NIT",
		"CONCERN: missing a null check":  "CONCERN",
		"BLOCKER: this deletes the repo": "BLOCKER",
	}
	for reply, wantSeverity := range cases {
		severity, note := parseAdvisorReply(reply)
		require.Equal(t, wantSeverity, severity)
		require.NotEmpty(t, note)
	}
}

func TestParseAdvisorReplyUnrecognizedIsBlank(t *testing.T) {
	severity, note := parseAdvisorReply("looks fine to me")
	require.Empty(t, severity)
	require.Empty(t, note)
}

func TestParseAdvisorReplyTrimsWhitespace(t *testing.T) {
	severity, note := parseAdvisorReply("  \n CONCERN:   spacing issue  \n")
	require.Equal(t, "CONCERN", severity)
	require.Equal(t, "spacing issue", note)
}

func TestTruncateForAdvisorLeavesShortTextAlone(t *testing.T) {
	require.Equal(t, "hello", truncateForAdvisor("  hello  "))
}

func TestTruncateForAdvisorCutsLongText(t *testing.T) {
	long := strings.Repeat("x", advisorMaxReviewChars*2)
	got := truncateForAdvisor(long)
	require.Less(t, len(got), len(long))
	require.Contains(t, got, "truncated")
}

func TestInjectAdvisorNoteWithNoPendingNote(t *testing.T) {
	a := &sessionAgent{advisorNotes: csync.NewMap[string, string]()}
	require.Equal(t, "do the thing", a.injectAdvisorNote("s1", "do the thing"))
}

func TestInjectAdvisorNotePrependsAndClears(t *testing.T) {
	a := &sessionAgent{advisorNotes: csync.NewMap[string, string]()}
	a.advisorNotes.Set("s1", "Advisor (concern): watch the null check")

	got := a.injectAdvisorNote("s1", "next task")
	require.Equal(t, "Advisor (concern): watch the null check\n\nnext task", got)

	// Read-and-clear: a second call for the same session gets nothing.
	require.Equal(t, "next task 2", a.injectAdvisorNote("s1", "next task 2"))
}

func TestInjectAdvisorNoteIsPerSession(t *testing.T) {
	a := &sessionAgent{advisorNotes: csync.NewMap[string, string]()}
	a.advisorNotes.Set("s1", "note for s1")

	require.Equal(t, "p", a.injectAdvisorNote("s2", "p"), "a note queued for a different session must not leak")
}

func TestInjectAdvisorNoteWithNoPromptUsesTheNoteAlone(t *testing.T) {
	a := &sessionAgent{advisorNotes: csync.NewMap[string, string]()}
	a.advisorNotes.Set("s1", "note")
	require.Equal(t, "note", a.injectAdvisorNote("s1", ""))
}

func newTurnCountingAgent(everyN int) *sessionAgent {
	return &sessionAgent{
		advisorEveryNTurns: csync.NewValue(everyN),
		advisorTurnCounts:  csync.NewMap[string, int](),
	}
}

func TestShouldRunAdvisorPassDefaultsToEveryTurn(t *testing.T) {
	a := newTurnCountingAgent(0)
	for i := range 3 {
		require.True(t, a.shouldRunAdvisorPass("s1"), "turn %d", i+1)
	}
}

func TestShouldRunAdvisorPassEveryNthTurn(t *testing.T) {
	a := newTurnCountingAgent(3)
	got := make([]bool, 6)
	for i := range got {
		got[i] = a.shouldRunAdvisorPass("s1")
	}
	require.Equal(t, []bool{false, false, true, false, false, true}, got)
}

func TestShouldRunAdvisorPassCountsPerSession(t *testing.T) {
	a := newTurnCountingAgent(2)
	require.False(t, a.shouldRunAdvisorPass("s1"))
	require.False(t, a.shouldRunAdvisorPass("s2"), "a fresh session must start its own count, not inherit s1's")
	require.True(t, a.shouldRunAdvisorPass("s1"))
	require.True(t, a.shouldRunAdvisorPass("s2"))
}

func TestShouldRunAdvisorPassNegativeIntervalTreatedAsEveryTurn(t *testing.T) {
	a := newTurnCountingAgent(-1)
	require.True(t, a.shouldRunAdvisorPass("s1"))
	require.True(t, a.shouldRunAdvisorPass("s1"))
}

func TestAdvisorShouldNotifyDefaultsToConcern(t *testing.T) {
	require.False(t, advisorShouldNotify("NIT", ""), "unset threshold keeps the original NIT-never-notifies behavior")
	require.True(t, advisorShouldNotify("CONCERN", ""))
	require.True(t, advisorShouldNotify("BLOCKER", ""))
}

func TestAdvisorShouldNotifyHonorsConfiguredThreshold(t *testing.T) {
	require.True(t, advisorShouldNotify("NIT", "NIT"), "a NIT floor surfaces every non-NONE severity")
	require.False(t, advisorShouldNotify("CONCERN", "BLOCKER"), "a BLOCKER floor silences CONCERN")
	require.True(t, advisorShouldNotify("BLOCKER", "BLOCKER"))
}

func TestAdvisorShouldNotifyUnrecognizedThresholdFallsBackToConcern(t *testing.T) {
	require.False(t, advisorShouldNotify("NIT", "not-a-real-severity"))
	require.True(t, advisorShouldNotify("CONCERN", "not-a-real-severity"))
}

func TestShouldEscalateDefaultsToBlocker(t *testing.T) {
	require.False(t, shouldEscalate("CONCERN", ""), "unset threshold keeps escalation's own BLOCKER-only default")
	require.True(t, shouldEscalate("BLOCKER", ""))
}

func TestShouldEscalateHonorsConfiguredThreshold(t *testing.T) {
	require.True(t, shouldEscalate("CONCERN", "CONCERN"), "a CONCERN floor also escalates CONCERN")
	require.True(t, shouldEscalate("BLOCKER", "CONCERN"))
	require.False(t, shouldEscalate("NIT", "CONCERN"))
}

func TestShouldEscalateUnrecognizedThresholdFallsBackToBlocker(t *testing.T) {
	require.False(t, shouldEscalate("CONCERN", "not-a-real-severity"))
	require.True(t, shouldEscalate("BLOCKER", "not-a-real-severity"))
}

func TestRunEscalationPassNilModelPanicsAreCaught(t *testing.T) {
	// runEscalationPass is only ever called when a.escalateModel != nil
	// (see runAdvisorPass); this just pins that the panic recovery
	// covers a nil model the same way runAdvisorPass's covers a nil
	// advisor model, in case that invariant is ever violated by a future
	// caller.
	a := &sessionAgent{escalateModel: csync.NewValue(ptrBox[Model]{})}
	note, ok := a.runEscalationPass(t.Context(), "s1", "prompt", "response", "BLOCKER", "advisor note")
	require.False(t, ok)
	require.Empty(t, note)
}

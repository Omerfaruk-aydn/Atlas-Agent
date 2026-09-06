package model

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

// The sidebar's "Goal" section is what keeps a set goal visible once the
// confirmation toast is gone -- so it must actually show the goal, and
// say "None" rather than nothing when there isn't one, matching every
// other sidebar section (Jobs, LSPs, Modified Files, ...) beside it.
func TestGoalInfoShowsNoneWithoutASession(t *testing.T) {
	sty := styles.AtlasPantera()
	m := &UI{com: &common.Common{Styles: &sty}}
	require.Contains(t, m.goalInfo(40, true), "None")
}

func TestGoalInfoShowsTheSetGoal(t *testing.T) {
	sty := styles.AtlasPantera()
	m := &UI{
		com:     &common.Common{Styles: &sty},
		session: &session.Session{Goal: "ship the release"},
	}
	got := m.goalInfo(40, true)
	require.Contains(t, got, "ship the release")
	require.NotContains(t, got, "None")
}

func TestGoalCommandTakesTheRestOfTheLine(t *testing.T) {
	arg, ok := parseGoalCommand("/goal every test in ./internal passes")
	require.True(t, ok)
	require.Equal(t, "every test in ./internal passes", arg)
}

func TestGoalCommandIgnoresSurroundingWhitespace(t *testing.T) {
	arg, ok := parseGoalCommand("   /goal    ship the release   ")
	require.True(t, ok)
	require.Equal(t, "ship the release", arg)
}

// A bare /goal is the popup's and the form's business, not this path's.
func TestBareGoalCommandIsNotIntercepted(t *testing.T) {
	_, ok := parseGoalCommand("/goal")
	require.False(t, ok)
	_, ok = parseGoalCommand("/goal   ")
	require.False(t, ok)
}

// The command is "/goal", not "/goal" as a prefix of a longer word.
func TestGoalCommandDoesNotMatchALongerWord(t *testing.T) {
	for _, s := range []string{"/goalkeeper wins", "/goals for the week"} {
		_, ok := parseGoalCommand(s)
		require.False(t, ok, "%q must be sent as a prompt, not read as the goal command", s)
	}
}

func TestOrdinaryPromptIsNotIntercepted(t *testing.T) {
	for _, s := range []string{"my goal is to ship", "", "/compact", "goal"} {
		_, ok := parseGoalCommand(s)
		require.False(t, ok)
	}
}

func TestGoalCommandStopWordsAreArguments(t *testing.T) {
	// "clear" reaches the handler as an ordinary argument; it is the
	// handler that reads it as a stop rather than as a goal to set.
	arg, ok := parseGoalCommand("/goal clear")
	require.True(t, ok)
	require.Equal(t, "clear", arg)
}

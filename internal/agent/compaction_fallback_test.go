package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestBoundUserTurnsKeepsFirstAndMostRecent(t *testing.T) {
	var turns []engineering.UserTurn
	for i := range 20 {
		turns = append(turns, engineering.UserTurn{Kind: "user", Text: fmt.Sprintf("request %d", i)})
		turns = append(turns, engineering.UserTurn{Kind: "continuation", Text: "auto"})
	}
	got := boundUserTurns(turns)
	require.Len(t, got, fallbackKeepTurns+1)
	require.Equal(t, "request 0", got[0].Text, "the first request states the goal")
	require.Equal(t, "request 19", got[len(got)-1].Text)
	require.Equal(t, "request 12", got[1].Text)
	for _, turn := range got {
		require.NotEqual(t, "continuation", turn.Kind)
	}
}

func TestBoundUserTurnsLeavesShortHistoryWhole(t *testing.T) {
	turns := []engineering.UserTurn{{Kind: "user", Text: "a"}, {Kind: "user", Text: " "}, {Kind: "user", Text: "b"}}
	require.Equal(t, []engineering.UserTurn{{Kind: "user", Text: "a"}, {Kind: "user", Text: "b"}}, boundUserTurns(turns))
}

func TestFallbackSummaryTextReplacesTheHandoffOpening(t *testing.T) {
	source := session.Session{ID: "parent-1", Goal: "Ship the importer"}
	turns := []engineering.UserTurn{{Kind: "user", Text: "Do not commit\nRun all tests"}}
	text := fallbackSummaryText(source, nil, "Decision: keep API stable", turns, []string{"internal/a.go"}, []string{"go test ./..."})

	require.True(t, strings.HasPrefix(text, fallbackHeader), "the summary speaks of itself, not of a parent session")
	require.NotContains(t, text, "linked parent session")
	require.NotContains(t, text, "Clean-session handoff")
	require.Contains(t, text, "## Goal")
	require.Contains(t, text, "Ship the importer")
	require.Contains(t, text, "Do not commit\n  Run all tests", "user words are kept verbatim")
	require.Contains(t, text, "internal/a.go")
	require.Contains(t, text, "PASS: go test ./...")
	require.Contains(t, text, "Decision: keep API stable")
	require.Contains(t, text, "## Open work")
}

func TestFallbackSummaryTextWithoutAnyStateStillExplainsItself(t *testing.T) {
	text := fallbackSummaryText(session.Session{ID: "s"}, nil, "", nil, nil, nil)
	require.True(t, strings.HasPrefix(text, fallbackHeader))
	require.Contains(t, text, "No additional recorded constraints")
}

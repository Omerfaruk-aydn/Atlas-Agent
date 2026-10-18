package engineering

import (
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"

	"github.com/stretchr/testify/require"
)

func TestCandidateSuiteRevisionClearsSelection(t *testing.T) {
	t.Parallel()
	w := WorkState{Suite: &CandidateSuite{Checks: []string{"a"}, LockedAt: 1}, Selection: "C1", Candidates: []Candidate{{ID: "C1", Status: "selected", Results: map[string]CandidateResult{"a": {Passed: true}}}}}
	require.NoError(t, w.SetSuite([]string{"b"}, "check the boundary instead", time.Now()))
	require.Empty(t, w.Selection)
	require.Equal(t, "built", w.Candidates[0].Status)
	require.Empty(t, w.Candidates[0].Results)
}

func TestLongRequestKeepsLateRequirementsAndSourceIdentity(t *testing.T) {
	t.Parallel()
	var w WorkState
	request := strings.Repeat("requirement ", 800) + "final requirement: preserve compatibility"
	turn := w.RecordUserTurn(request, time.Now())
	require.Contains(t, turn.Text, "preserve compatibility")
	require.False(t, turn.Truncated)
	require.Equal(t, Hash(request), turn.Fingerprint)
	long := strings.Repeat("x", maxWorkTurnText+100)
	turn = w.RecordUserTurn(long, time.Now())
	require.True(t, turn.Truncated)
	require.Equal(t, Hash(long), turn.Fingerprint)
}

func TestReadingStateIsNotContractVerification(t *testing.T) {
	t.Parallel()
	w := WorkState{}
	w.RecordUserTurn("fix export", time.Now())
	item, err := w.AddItem("behavior", "export works", OriginUser, []int{1}, time.Now())
	require.NoError(t, err)
	for _, tool := range []string{"bash", "browser", "computer", "tool_pipeline"} {
		st := State{Operations: []Operation{op("read", tool, "completed", 0, 100)}}
		_, checked := LatestCheck(st)
		require.False(t, checked, tool)
		require.ErrorContains(t, w.Advance(st, item.ID, ItemVerified, []string{"read"}, time.Now()), "passing check")
		if tool != "bash" {
			st.Outcomes.Add(outcome.Evidence{ID: "assert", Source: "read", Kind: outcome.KindOutput, Subject: "export", Claim: "verified content"})
			require.NoError(t, w.Advance(st, item.ID, ItemVerified, []string{"read"}, time.Now()))
		}
	}
}

func TestCandidateChecksCannotMixSourceGenerations(t *testing.T) {
	t.Parallel()
	w := WorkState{Suite: &CandidateSuite{Checks: []string{"a", "b"}}, Candidates: []Candidate{{ID: "C1", Results: map[string]CandidateResult{"a": {Passed: true, Generation: 1}}}}}
	st := State{Operations: []Operation{op("v2", "verify", "completed", 2, 100)}, Checks: []Check{{RunID: "call-v2", Name: "b", Passed: true}}}
	require.NoError(t, w.RecordCandidateResult(st, "C1", "v2", time.UnixMilli(110)))
	_, complete := w.CandidateScore(w.Candidates[0])
	require.False(t, complete)
	require.NotContains(t, w.Candidates[0].Results, "a")
	_, err := w.Select("C1", "", time.UnixMilli(120))
	require.ErrorContains(t, err, "no candidate passed")
}

package evaluation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestActualDeliveryIsScoredSeparatelyFromPrediction(t *testing.T) {
	t.Parallel()
	a := rec("m", "bug", 0, false)
	a.AtlasVerified = true
	b := rec("m", "bug", 1, true)
	delivery := rec("m", "bug", 0, true)
	delivery.Delivery = true
	delivery.CandidateAttempts = []int{0, 1}
	delivery.Workspace = "delivered"
	r, err := SummarizeAttempts([]LiveRecord{a, b, delivery})
	require.NoError(t, err)
	require.Zero(t, r.SelectedRate)
	require.NotNil(t, r.DeliveredRate)
	require.Equal(t, 1.0, *r.DeliveredRate)
	require.True(t, r.Groups[0].DeliveryObserved)
	require.True(t, *r.Groups[0].DeliveredPassed)
	delivery.Checks[0].Passed = false
	r, err = SummarizeAttempts([]LiveRecord{a, b, delivery})
	require.NoError(t, err)
	require.Equal(t, 1, r.ActualSelectionLosses)
	delivery.CandidateAttempts = []int{99}
	_, err = SummarizeAttempts([]LiveRecord{a, b, delivery})
	require.ErrorContains(t, err, "unknown")
	r, err = SummarizeAttempts([]LiveRecord{a, b})
	require.NoError(t, err)
	require.Nil(t, r.DeliveredRate)
}

func TestDeliverySelectionInvokesModelAndRescoresWithoutLeakingCheckerResults(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "answer.txt"), []byte("baseline"), 0o644))
	_, err := RunCommand(t.Context(), root, "git", []string{"init"})
	require.NoError(t, err)
	_, err = RunCommand(t.Context(), root, "git", []string{"add", "answer.txt"})
	require.NoError(t, err)
	_, err = RunCommand(t.Context(), root, "git", []string{"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "fixture"})
	require.NoError(t, err)
	scenarios, err := Scenarios()
	require.NoError(t, err)
	c := LiveCase{Scenario: scenarios[0].ID, Prompt: "Set answer.txt to correct"}
	for _, criterion := range scenarios[0].Criteria {
		c.Checks = append(c.Checks, LiveCheck{Criterion: criterion, Command: []string{"checker"}})
	}
	m := LiveManifest{Repository: root, PromptVersion: "v1", Models: []string{"m"}, Repeats: 2, Cases: []LiveCase{c}, SelectDelivery: true, DeliveryFiles: []string{"answer.txt"}}
	modelCalls := 0
	checks := 0
	runner := func(ctx context.Context, dir, program string, args []string) ([]byte, error) {
		if program == "checker" {
			checks++
			return []byte("secret external checker result"), nil
		}
		modelCalls++
		if modelCalls == 3 {
			prompt := args[len(args)-1]
			require.Contains(t, prompt, "candidate_artifacts")
			require.NotContains(t, prompt, "secret external checker result")
			require.Contains(t, prompt, "candidate 1")
			require.Contains(t, prompt, "candidate 2")
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, "answer.txt"), []byte("candidate "+string(rune('0'+modelCalls))), 0o644))
		return json.Marshal(map[string]any{"finish_reason": "end_turn", "provider_steps": 1})
	}
	records, err := RunLive(t.Context(), m, "atlas-fixture", filepath.Join(t.TempDir(), "results.json"), true, runner)
	require.NoError(t, err)
	require.Len(t, records, 3)
	require.Equal(t, 3, modelCalls)
	require.Equal(t, 3*len(c.Checks), checks)
	require.True(t, records[2].Delivery)
	require.Equal(t, records[0].CaseHash, records[2].CaseHash)
	require.Equal(t, []int{0, 1}, records[2].CandidateAttempts)
	for _, r := range records {
		require.NotEqual(t, root, r.Workspace)
		require.False(t, strings.Contains(r.Workspace, ".."))
	}
}

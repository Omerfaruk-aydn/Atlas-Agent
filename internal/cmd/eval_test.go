package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/evaluation"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestEvalCommandScenariosAndScore(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	command := newEvalCommand()
	command.SetOut(&output)
	command.SetArgs([]string{"scenarios"})
	require.NoError(t, command.Execute())
	var scenarios []evaluation.Scenario
	require.NoError(t, json.Unmarshal(output.Bytes(), &scenarios))
	require.Len(t, scenarios, 9)
	file := filepath.Join(t.TempDir(), "results with spaces.json")
	require.NoError(t, os.WriteFile(file, []byte(`[{"scenario":"ui-states","claimed_complete":true,"checks":[]}]`), 0o644))
	output.Reset()
	command = newEvalCommand()
	command.SetOut(&output)
	command.SetArgs([]string{"score", file})
	require.NoError(t, command.Execute())
	var report evaluation.Report
	require.NoError(t, json.Unmarshal(output.Bytes(), &report))
	require.Equal(t, 1, report.FalseCompletions)
	require.Len(t, report.MissingScenarios, 8)
}

func TestEvalCommandRejectsOversizedAndMalformedResults(t *testing.T) {
	t.Parallel()
	for _, data := range [][]byte{[]byte("broken JSON"), bytes.Repeat([]byte(" "), 1024*1024+1)} {
		file := filepath.Join(t.TempDir(), "results.json")
		require.NoError(t, os.WriteFile(file, data, 0o644))
		command := newEvalCommand()
		command.SetOut(&bytes.Buffer{})
		command.SetErr(&bytes.Buffer{})
		command.SetArgs([]string{"score", file})
		require.Error(t, command.Execute())
	}
}

func TestLiveEvaluationUnattendedFlagIsAvailableOnRun(t *testing.T) {
	t.Parallel()
	flag := runCmd.Flags().Lookup("yolo")
	require.NotNil(t, flag)
	require.Equal(t, "bool", flag.Value.Type())
	require.Equal(t, "false", flag.DefValue)
}

func TestRunRoleOverridePreservesExplicitModelAndRejectsUnknownRole(t *testing.T) {
	t.Parallel()
	store := config.NewTestStore(&config.Config{Options: &config.Options{SessionMode: "review"}})
	command := &cobra.Command{}
	command.Flags().String("role", "", "")
	command.Flags().String("model", "", "")
	require.NoError(t, command.Flags().Set("role", "frontend"))
	require.NoError(t, command.Flags().Set("model", "provider/candidate"))
	require.NoError(t, applyRunRoleOverride(command, store))
	require.Equal(t, "frontend", store.Config().Options.SessionMode)
	require.True(t, store.Overrides().PreserveSelectedModel)
	require.NoError(t, command.Flags().Set("role", "none"))
	require.NoError(t, applyRunRoleOverride(command, store))
	require.Empty(t, store.Config().Options.SessionMode)
	require.NoError(t, command.Flags().Set("role", "unknown"))
	require.Error(t, applyRunRoleOverride(command, store))
}

func TestRecommendCommandRejectsMissingEvidence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	policy := evaluation.SelectionPolicy{Role: "backend", Scenarios: []string{"multi-layer-feature"}, Candidates: []string{"provider/candidate"}, PromptVersion: "v1"}
	data, err := json.Marshal(policy)
	require.NoError(t, err)
	policyPath := filepath.Join(dir, "policy.json")
	results := filepath.Join(dir, "results.json")
	require.NoError(t, os.WriteFile(policyPath, data, 0o600))
	require.NoError(t, os.WriteFile(results, []byte(`[]`), 0o600))
	command := newEvalCommand()
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"recommend", policyPath, results})
	require.ErrorContains(t, command.Execute(), "insufficient")
}

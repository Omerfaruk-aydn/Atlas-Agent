package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/evaluation"
	"github.com/stretchr/testify/require"
)

func TestPromptComparisonCLIReadsMatchedRecords(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	scenarios, err := evaluation.Scenarios()
	require.NoError(t, err)
	old := evaluation.LiveRecord{Run: evaluation.Run{Model: "fixture/model", PromptVersion: "before", Scenario: scenarios[0].ID}, CaseHash: "case", Baseline: "base", SessionID: "session", MetricsSource: "cumulative_runtime_ledger"}
	fresh := old
	fresh.PromptVersion = "after"
	fresh.SessionID = "after-session"
	paths := []string{filepath.Join(dir, "before.json"), filepath.Join(dir, "after.json")}
	for i, record := range []evaluation.LiveRecord{old, fresh} {
		data, err := json.Marshal([]evaluation.LiveRecord{record})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(paths[i], data, 0o600))
	}
	command := newEvalCommand()
	command.SetArgs([]string{"prompt-compare", paths[0], paths[1], "--min-samples", "1"})
	var output bytes.Buffer
	command.SetOut(&output)
	require.NoError(t, command.Execute())
	var report []evaluation.PromptComparison
	require.NoError(t, json.Unmarshal(output.Bytes(), &report))
	require.Len(t, report, 1)
	require.Equal(t, "before", report[0].BeforeVersion)
	require.Equal(t, "after", report[0].AfterVersion)
	require.Equal(t, 1, report[0].Samples)
}

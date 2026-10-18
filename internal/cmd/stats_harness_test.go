package cmd

import (
	"bytes"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func harnessCommand(t *testing.T, dataDir string, args ...string) (string, error) {
	t.Helper()
	cmd := &cobra.Command{RunE: statsHarnessCmd.RunE}
	cmd.Flags().String("data-dir", dataDir, "")
	cmd.Flags().Int("days", 14, "")
	require.NoError(t, cmd.Flags().Parse(args))
	var out bytes.Buffer
	cmd.SetOut(&out)
	err := cmd.RunE(cmd, nil)
	return out.String(), err
}

func TestStatsHarnessReportsRecordedEvents(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	r := harnessmetrics.New(func() string { return dir })
	r.Record(harnessmetrics.ContractSentBack, "s", nil)
	r.Record(harnessmetrics.ContractPassed, "s", nil)
	r.Record(harnessmetrics.ToolSearchEmpty, "s", map[string]string{"query": "ekran kaydi"})

	out, err := harnessCommand(t, dir)
	require.NoError(t, err)
	require.Contains(t, out, "Verification contract")
	require.Contains(t, out, "- ekran kaydi")
	require.Contains(t, out, "1 sessions")
}

func TestStatsHarnessWithoutEventsAndBadFlags(t *testing.T) {
	t.Parallel()

	out, err := harnessCommand(t, t.TempDir())
	require.NoError(t, err)
	require.Contains(t, out, "No harness events recorded yet")

	_, err = harnessCommand(t, t.TempDir(), "--days", "0")
	require.ErrorContains(t, err, "--days must be positive")
}

package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/evaluation"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/stretchr/testify/require"
)

func TestEvalTraceFiltersTurnsWithoutLoadingConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r := harnessmetrics.New(func() string { return dir })
	r.BeginTrace("s").Record(harnessmetrics.TraceEvent{Kind: "gate", Name: "evaluator", Outcome: "unverified"})
	r.BeginTrace("s").Record(harnessmetrics.TraceEvent{Kind: "gate", Name: "contract", Outcome: "pass"})
	cmd := newEvalCommand()
	cmd.PersistentFlags().String("data-dir", dir, "")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"trace", "s", "--turn", "1", "--json"})
	require.NoError(t, cmd.Execute())
	var events []harnessmetrics.TraceEvent
	require.NoError(t, json.Unmarshal(out.Bytes(), &events))
	require.Len(t, events, 2)
	require.Equal(t, "evaluator", events[1].Name)
	cmd = newEvalCommand()
	cmd.PersistentFlags().String("data-dir", dir, "")
	cmd.SetArgs([]string{"trace", "s", "--turn", "-1"})
	require.Error(t, cmd.Execute())
}

func TestEvalRetentionScoresRecordedSummary(t *testing.T) {
	t.Parallel()
	cases, err := evaluation.RetentionCases()
	require.NoError(t, err)
	file := filepath.Join(t.TempDir(), "summary.txt")
	require.NoError(t, os.WriteFile(file, []byte(cases[0].Summary), 0o600))
	cmd := newEvalCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"retention", cases[0].ID, file, "--json"})
	require.NoError(t, cmd.Execute())
	var reports []evaluation.RetentionReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &reports))
	require.Len(t, reports, 1)
	require.Equal(t, reports[0].Total, reports[0].Passed)
}

func TestStatsHarnessIncludesUnverifiedAndFailOpenSeparately(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tr := harnessmetrics.New(func() string { return dir }).BeginTrace("s")
	tr.Record(harnessmetrics.TraceEvent{Kind: "gate", Name: "evaluator", Outcome: "unverified"})
	tr.Record(harnessmetrics.TraceEvent{Kind: "gate", Name: "plan_review", Outcome: "fail_open"})
	out, err := harnessCommand(t, dir)
	require.NoError(t, err)
	require.Contains(t, out, "fail_open=1")
	require.Contains(t, out, "unverified=1")
}

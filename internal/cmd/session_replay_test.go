package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/history"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session/replay"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func newReplayCmd(t *testing.T, dataDir string, run func(*cobra.Command, []string) error) *cobra.Command {
	t.Helper()
	c := &cobra.Command{RunE: run}
	c.Flags().String("data-dir", dataDir, "")
	c.SetContext(t.Context())
	return c
}

func TestSessionTimelineText(t *testing.T) {
	dataDir := t.TempDir()
	sess, _ := seedRewindSession(t, dataDir, t.TempDir())

	sessionTimelineJSON = false
	c := newReplayCmd(t, dataDir, runSessionTimeline)
	var out bytes.Buffer
	c.SetOut(&out)
	require.NoError(t, c.RunE(c, []string{sess.ID}))
	require.Contains(t, out.String(), "2 step(s)")
	require.Contains(t, out.String(), "edit a")
}

func TestSessionTimelineJSON(t *testing.T) {
	dataDir := t.TempDir()
	sess, _ := seedRewindSession(t, dataDir, t.TempDir())

	sessionTimelineJSON = true
	t.Cleanup(func() { sessionTimelineJSON = false })
	c := newReplayCmd(t, dataDir, runSessionTimeline)
	var out bytes.Buffer
	c.SetOut(&out)
	require.NoError(t, c.RunE(c, []string{sess.ID}))

	var got timelineOutput
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	require.Len(t, got.Steps, 2)
	require.True(t, got.Steps[0].Branchable)
}

func TestSessionBranchThenCompare(t *testing.T) {
	dataDir := t.TempDir()
	sess, _ := seedRewindSession(t, dataDir, t.TempDir())

	sessionBranchAt, sessionBranchPrompt, sessionBranchModel = "1", "", "provider/model"
	sessionBranchDryRun, sessionBranchRestoreFiles, sessionBranchRun = false, false, false
	t.Cleanup(func() { sessionBranchAt, sessionBranchModel = "", "" })

	c := newReplayCmd(t, dataDir, runSessionBranch)
	var out bytes.Buffer
	c.SetOut(&out)
	require.NoError(t, c.RunE(c, []string{sess.ID}))
	require.Contains(t, out.String(), "Branched into session")
	require.Contains(t, out.String(), "atlas-agent -s ")
	require.Contains(t, out.String(), "-m provider/model")

	// The branch is a new session: compare it with its source.
	ctx, svc, cleanup, err := sessionSetup(c)
	require.NoError(t, err)
	list, err := svc.sessions.List(ctx)
	require.NoError(t, err)
	cleanup()
	require.Len(t, list, 2)
	var branchID string
	for _, s := range list {
		if s.ID != sess.ID {
			branchID = s.ID
		}
	}
	require.NotEmpty(t, branchID)

	sessionCompareJSON = true
	t.Cleanup(func() { sessionCompareJSON = false })
	cc := newReplayCmd(t, dataDir, runSessionCompare)
	var cmpOut bytes.Buffer
	cc.SetOut(&cmpOut)
	require.NoError(t, cc.RunE(cc, []string{sess.ID, branchID}))

	var got replay.Comparison
	require.NoError(t, json.Unmarshal(cmpOut.Bytes(), &got))
	require.Equal(t, 1, got.Divergence.CommonSteps)
	require.Equal(t, 2, got.A.Messages)
	require.Equal(t, 1, got.B.Messages)
	require.Empty(t, got.Divergence.B, "the branch ends where the source continues")
}

func TestSessionBranchDryRunCreatesNothing(t *testing.T) {
	dataDir := t.TempDir()
	sess, _ := seedRewindSession(t, dataDir, t.TempDir())

	sessionBranchAt, sessionBranchDryRun = "1", true
	t.Cleanup(func() { sessionBranchAt, sessionBranchDryRun = "", false })
	c := newReplayCmd(t, dataDir, runSessionBranch)
	var out bytes.Buffer
	c.SetOut(&out)
	require.NoError(t, c.RunE(c, []string{sess.ID}))
	require.Contains(t, out.String(), "Would create a branch")

	ctx, svc, cleanup, err := sessionSetup(c)
	require.NoError(t, err)
	defer cleanup()
	list, err := svc.sessions.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
}

func TestSessionBranchRunNeedsPrompt(t *testing.T) {
	sessionBranchRun, sessionBranchPrompt = true, ""
	t.Cleanup(func() { sessionBranchRun = false })
	c := newReplayCmd(t, t.TempDir(), runSessionBranch)
	require.ErrorContains(t, c.RunE(c, []string{"x"}), "--prompt")
}

func TestContinueArgs(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"-s", "abc"}, continueArgs("abc", "", ""))
	require.Equal(t, []string{"run", "-s", "abc", "-m", "m", "do it"}, continueArgs("abc", "m", "do it"))
	require.Equal(t, `run -s abc "do it"`, shellJoin(continueArgs("abc", "", "do it")))
}

func TestChangedFilesIgnoresBaselineOnly(t *testing.T) {
	t.Parallel()
	got := changedFiles([]history.File{
		{Path: "read.go", Version: 0, Content: "a"},
		{Path: "edit.go", Version: 0, Content: "a"},
		{Path: "edit.go", Version: 2, Content: "c", MessageID: "m2"},
		{Path: "edit.go", Version: 1, Content: "b", MessageID: "m1"},
		{Path: "new.go", Version: 0, Content: "n", MessageID: "m1"},
	})
	require.Equal(t, map[string]string{"edit.go": "c", "new.go": "n"}, got)
}

func TestWriteComparisonText(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	c := replay.Compare(
		replay.Side{ID: session.HashID("a"), Title: "first", Cost: 1},
		replay.Side{ID: session.HashID("b"), Title: "second", Cost: 3},
	)
	require.NoError(t, writeComparison(&out, c, false))
	require.Contains(t, out.String(), "identical")
	require.Contains(t, out.String(), "neither session changed any file")
	require.Contains(t, out.String(), "(+2.0000)")
}

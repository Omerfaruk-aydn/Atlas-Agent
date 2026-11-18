package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/planspec"
	"github.com/stretchr/testify/require"
)

func TestWalkthroughComparesApprovedPlanWithGitChanges(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	require.NoError(t, exec.CommandContext(t.Context(), "git", "-C", dir, "init", "-q").Run())
	require.NoError(t, os.WriteFile(filepath.Join(dir, "planned.go"), []byte("package x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "surprise.go"), []byte("package x"), 0o644))

	saveApprovedPlan(dir, planspec.Parse("[NEW] planned.go\n[NEW] never.go\n"))

	res, err := NewWalkthroughTool(dir).Run(t.Context(), fantasy.ToolCall{ID: "1", Name: WalkthroughToolName, Input: `{"goal":"demo"}`})
	require.NoError(t, err)
	require.Contains(t, res.Content, "**Goal:** demo")
	require.Contains(t, res.Content, "`planned.go`")
	require.Contains(t, res.Content, "Changed but not in the plan:\n- `surprise.go`")
	require.Contains(t, res.Content, "In the plan but not changed:\n- `never.go`")
	require.Contains(t, res.Content, "Nothing here was verified")
}

func TestSaveApprovedPlanSkipsFreeFormPlans(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	saveApprovedPlan(dir, planspec.Parse("just do it"))
	require.NoFileExists(t, filepath.Join(dir, ".atlas", approvedPlanFile))
}

package workplan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSpecTaskRoundTripAndConflict(t *testing.T) {
	root := t.TempDir()
	plan := &Plan{Goal: "Ship safely", Features: []Feature{{ID: "F1", Title: "Implement parser", Verify: "go test ./parser"}, {ID: "F2", Title: "Document behavior", Verify: "go test ./docs"}}}
	data, err := json.Marshal(plan)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, Dir), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, Dir, PlanFile), data, 0o644))

	snapshot, err := LoadSpec(root)
	require.NoError(t, err)
	for _, stage := range []SpecStage{SpecRequirements, SpecDesign, SpecTasks} {
		content := RenderSpec(plan)[stage]
		require.NoError(t, WriteSpecStage(root, snapshot, stage, content))
		snapshot.Documents[string(stage)] = digest([]byte(content))
		require.NoError(t, ApproveSpecStage(root, stage, snapshot.Documents[string(stage)]))
	}

	tasks := RenderTasks(plan)
	require.NoError(t, UpdateTasks(root, snapshot.ContractDigest, strings.Replace(tasks, "- [ ] F1", "- [x] F1", 1)))
	loaded, _, err := Load(root)
	require.NoError(t, err)
	require.True(t, loaded.Features[0].Done)
	require.False(t, loaded.Features[1].Done)

	// An edit based on the old contract cannot overwrite newer agent progress.
	require.ErrorContains(t, UpdateTasks(root, snapshot.ContractDigest, tasks), "changed after")
	require.NoError(t, CompleteFeature(root, "F2", true))
	loaded, _, err = Load(root)
	require.NoError(t, err)
	require.True(t, loaded.Features[1].Done)
	content, err := os.ReadFile(specPath(root, SpecTasks))
	require.NoError(t, err)
	require.Contains(t, string(content), "- [x] F2")
}

func TestSpecRejectsContractWeakening(t *testing.T) {
	root := t.TempDir()
	plan := &Plan{Goal: "Safe", Features: []Feature{{ID: "F1", Title: "Test", Verify: "go test ./..."}}}
	data, _ := json.Marshal(plan)
	require.NoError(t, os.MkdirAll(filepath.Join(root, Dir), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, Dir, PlanFile), data, 0o644))
	snapshot, err := LoadSpec(root)
	require.NoError(t, err)

	require.ErrorContains(t, UpdateTasks(root, snapshot.ContractDigest, "# Tasks\n"), "every work.json task")
	require.ErrorContains(t, UpdateTasks(root, snapshot.ContractDigest, "- [ ] OTHER — Test\n"), "not in work.json")
	require.ErrorContains(t, UpdateTasks(root, snapshot.ContractDigest, "- [ ] F1 — Weakened\n"), "title differs")
	require.ErrorContains(t, UpdateTasks(root, snapshot.ContractDigest, "- [ ] F1 — Test\n  Verification: `true`\n"), "verification differs")
}

func TestSpecApprovalBindsExactRevision(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, Dir, "spec"), 0o755))
	path := specPath(root, SpecRequirements)
	require.NoError(t, os.WriteFile(path, []byte("reviewed"), 0o644))
	require.ErrorContains(t, ApproveSpecStage(root, SpecRequirements, digest([]byte("stale"))), "changed after review")
	require.NoError(t, ApproveSpecStage(root, SpecRequirements, digest([]byte("reviewed"))))
	snapshot, err := LoadSpec(root)
	require.NoError(t, err)
	require.Equal(t, []SpecStage{SpecRequirements}, snapshot.ApprovedStages())
}

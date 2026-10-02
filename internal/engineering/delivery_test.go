package engineering

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProjectBriefAndKnowledgeSurviveRestartAndDetectDrift(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\ngo 1.27\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Use repository conventions"), 0o644))
	store := NewStore(t.TempDir())
	brief, err := store.PrepareProject(t.Context(), root)
	require.NoError(t, err)
	require.Contains(t, brief.Stacks, "go")
	require.Contains(t, brief.Instructions, "AGENTS.md")
	require.Contains(t, brief.Commands, "go test -count=1 ./...")
	sources, err := CaptureSources(t.Context(), root, []string{"go.mod"})
	require.NoError(t, err)
	record := KnowledgeRecord{ID: "state-owner", Kind: "decision", Context: "Avoid duplicate state", Decision: "Use the existing service", Consequences: []string{"One authoritative writer"}, Sources: sources}
	require.NoError(t, store.SaveKnowledge(t.Context(), root, record))
	reopened := &Store{dir: store.Dir()}
	stored, views, err := reopened.ProjectKnowledge(t.Context(), root)
	require.NoError(t, err)
	require.Equal(t, brief.MapFingerprint, stored.MapFingerprint)
	require.Len(t, views, 1)
	require.Equal(t, "current", views[0].Status)
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module changed\n"), 0o644))
	_, views, err = reopened.ProjectKnowledge(t.Context(), root)
	require.NoError(t, err)
	require.Equal(t, "stale", views[0].Status)
	require.ErrorContains(t, reopened.SaveKnowledge(t.Context(), root, record), "changed")
	_, err = CaptureSources(t.Context(), root, []string{"../outside"})
	require.Error(t, err)
	record.Kind = "lesson"
	record.Sources, err = CaptureSources(t.Context(), root, []string{"go.mod"})
	require.NoError(t, err)
	require.ErrorContains(t, reopened.SaveKnowledge(t.Context(), root, record), "linked")
}

func TestProjectBriefFindsPackageManagerWithoutExecutingScripts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"test":"exit 99","build":"touch forbidden"}}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "yarn.lock"), []byte("fixture"), 0o644))
	brief, err := NewStore(t.TempDir()).PrepareProject(t.Context(), root)
	require.NoError(t, err)
	require.Contains(t, brief.Commands, "yarn run test")
	_, err = os.Stat(filepath.Join(root, "forbidden"))
	require.True(t, os.IsNotExist(err))
}

func TestDesignGateRequiresFreshArtifactsAllStatesAndNoUnresolvedFindings(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "terminal.txt")
	require.NoError(t, os.WriteFile(path, []byte("Actual terminal transcript"), 0o644))
	plan := DeliveryPlan{Design: &DesignBrief{Target: "tui", TaskIDs: []string{"screen"}, MinimumWidth: 80, WideWidth: 160, States: []string{"success", "error"}, Criteria: []string{"usable"}}}
	state := State{UIEvidence: []UIEvidence{
		{ID: "narrow", Path: path, Hash: Hash("Actual terminal transcript"), Width: 80, Target: "tui", SourceFingerprint: "source", AssertionsPassed: true},
		{ID: "wide", Path: path, Hash: Hash("Actual terminal transcript"), Width: 160, Target: "tui", SourceFingerprint: "source", AssertionsPassed: true},
	}}
	stage := Stage{TaskIDs: []string{"screen"}}
	require.Error(t, plan.DesignReady(t.Context(), state, "source", stage))
	for _, id := range []string{"narrow", "wide"} {
		plan.Design.Critiques = append(plan.Design.Critiques, UICritique{ArtifactID: id, SourceFingerprint: "source", States: []string{"success"}, Findings: []DesignFinding{{Criterion: "usable", Passed: true, Evidence: "Inspected focus and wrapping"}}})
	}
	require.ErrorContains(t, plan.DesignReady(t.Context(), state, "source", stage), "error")
	plan.Design.Critiques[1].States = append(plan.Design.Critiques[1].States, "error")
	require.NoError(t, plan.DesignReady(t.Context(), state, "source", stage))
	plan.Design.Critiques[1].Findings[0].Passed = false
	require.ErrorContains(t, plan.DesignReady(t.Context(), state, "source", stage), "unresolved")
	plan.Design.Critiques[1].Findings[0].Passed = true
	require.Error(t, plan.DesignReady(t.Context(), state, "changed", stage))
	require.NoError(t, os.WriteFile(path, []byte("Edited evidence"), 0o644))
	require.ErrorContains(t, plan.DesignReady(t.Context(), state, "source", stage), "changed")
}

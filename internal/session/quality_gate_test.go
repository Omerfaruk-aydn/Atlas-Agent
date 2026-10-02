package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/stretchr/testify/require"
)

func TestCompletionGatePersistsAndRejectsUnreviewedOrChangedSource(t *testing.T) {
	root := t.TempDir()
	_, err := engineering.Git(t.Context(), root, "init")
	require.NoError(t, err)
	file := filepath.Join(root, "feature.go")
	require.NoError(t, os.WriteFile(file, []byte("package feature\n"), 0o644))
	store := engineering.NewStore(filepath.Join(root, "state"))
	dir := t.TempDir()
	conn, err := db.Connect(t.Context(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Release(dir)) })
	svc := NewService(db.New(conn), conn, EngineeringCompletionGate(store))
	sess, err := svc.Create(t.Context(), "quality")
	require.NoError(t, err)
	task := Todo{ID: "api", Content: "Implement API", Agent: "backend", Status: TodoStatusInProgress, AcceptanceCriteria: []string{"API works"}, OwnedPaths: []string{"feature.go"}}
	sess.Todos = []Todo{task}
	_, err = svc.Save(t.Context(), sess)
	require.NoError(t, err)
	run := engineering.RoleExecution{TaskID: task.ID, Agent: "backend", TaskFingerprint: TaskFingerprint(task), Root: root, RequireReview: true}
	require.NoError(t, store.Update(t.Context(), sess.ID, func(st *engineering.State) error { st.RoleExecutions[task.ID] = run; return nil }))
	sess.Todos[0].Status = TodoStatusCompleted
	sess.Todos[0].Verification = "passed"
	sess.Todos[0].Evidence = []TodoEvidence{{Kind: "command", Detail: "checks pass"}}
	_, err = svc.Save(t.Context(), sess)
	require.ErrorContains(t, err, "independent")
	run.Passed = true
	run.MachineChecks = []engineering.Check{{TaskID: "api", RunID: "quality-verify", Passed: true, Evidence: "observed checker exit 0"}}
	run.Handoff = &subagents.Handoff{TaskID: "api"}
	run.Reviews = []engineering.QualityRun{{Agent: "test", Handoff: &subagents.Handoff{TaskID: "api", Decision: "passed"}}, {Agent: "review", Handoff: &subagents.Handoff{TaskID: "api", Decision: "passed"}}}
	run.SourceFingerprint, err = engineering.SourceFingerprint(t.Context(), root, store.Dir())
	require.NoError(t, err)
	require.NoError(t, store.Update(t.Context(), sess.ID, func(st *engineering.State) error { st.RoleExecutions[task.ID] = run; return nil }))
	// Reopen the store to prove that the completion gate survives a restart.
	restarted := NewService(db.New(conn), conn, EngineeringCompletionGate(engineering.NewStore(filepath.Join(root, "state"))))
	require.NoError(t, os.WriteFile(file, []byte("package changed\n"), 0o644))
	_, err = restarted.Save(t.Context(), sess)
	require.ErrorContains(t, err, "source changed")
	require.NoError(t, os.WriteFile(file, []byte("package feature\n"), 0o644))
	_, err = restarted.Save(t.Context(), sess)
	require.NoError(t, err)
	// A completed task is historical; later unrelated work must not block usage saves.
	require.NoError(t, os.WriteFile(file, []byte("package next\n"), 0o644))
	sess.Cost = 0.1
	_, err = restarted.Save(t.Context(), sess)
	require.NoError(t, err)
	changed := sess
	changed.Todos = append([]Todo{}, sess.Todos...)
	changed.Todos[0].AcceptanceCriteria = []string{"New requirement"}
	_, err = restarted.Save(t.Context(), changed)
	require.ErrorContains(t, err, "independent")
}

package engineering

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCheckpointRestartAndSourceChange(t *testing.T) {
	t.Parallel()
	dir, root := t.TempDir(), t.TempDir()
	cp := Checkpoint{ID: uuid.NewString(), Root: root, SessionID: "session", SourceFingerprint: Hash("source"), PlanFingerprint: Hash("plan"), TaskFingerprints: map[string]string{"done": Hash("task")}}
	_, err := NewStore(dir).SaveCheckpoint(t.Context(), cp)
	require.NoError(t, err)
	restored, err := NewStore(dir).ReadCheckpoint(t.Context(), root, cp.SessionID, cp.ID)
	require.NoError(t, err)
	require.Equal(t, cp, restored)
	plan, err := PlanResume(t.Context(), ResumeInput{Checkpoint: restored, State: State{Revision: 4}, Source: Hash("changed"), TaskSpecs: cp.TaskFingerprints, TaskStates: map[string]string{"done": "completed"}})
	require.NoError(t, err)
	require.Equal(t, []string{"done"}, plan.ReverifyTasks)
	require.Empty(t, plan.ReadyTasks)
	_, err = NewStore(dir).ReadCheckpoint(t.Context(), filepath.Join(root, "foreign"), cp.SessionID, cp.ID)
	require.Error(t, err)
}

func TestResumeUnknownEffectsAndPIDReuse(t *testing.T) {
	t.Parallel()
	cp := Checkpoint{ID: uuid.NewString(), Root: t.TempDir(), SessionID: "session", SourceFingerprint: Hash("source"), PlanFingerprint: Hash("plan"), Operations: []string{"old"}, TaskFingerprints: map[string]string{"next": Hash("task")}}
	for _, operations := range [][]Operation{
		{{ID: "old", Status: "running", BackgroundID: "123"}},
		{},
		{{ID: "old", Status: "completed"}, {ID: "new", Status: "running", BackgroundID: "123"}},
	} {
		plan, err := PlanResume(t.Context(), ResumeInput{Checkpoint: cp, State: State{Operations: operations}, Source: cp.SourceFingerprint, TaskSpecs: cp.TaskFingerprints, TaskStates: map[string]string{"next": "pending"}})
		require.NoError(t, err)
		require.NotEmpty(t, plan.AmbiguousOperations)
		require.Empty(t, plan.ReadyTasks)
	}
}

func TestResumeRevisionConflict(t *testing.T) {
	t.Parallel()
	store := NewStore(t.TempDir())
	require.NoError(t, store.Update(t.Context(), "session", func(st *State) error { st.Profile = "feature"; return nil }))
	before, err := store.Read(t.Context(), "session")
	require.NoError(t, err)
	require.Error(t, store.UpdateRevision(t.Context(), "session", before.Revision-1, func(st *State) error { st.Profile = "migration"; return nil }))
	after, err := store.Read(t.Context(), "session")
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestCheckpointCannotRenewStaleStageEvidence(t *testing.T) {
	t.Parallel()
	delivery := &DeliveryPlan{Root: t.TempDir(), Stages: []Stage{{ID: "slice", TaskIDs: []string{"done"}, Passed: true, SourceFingerprint: Hash("old source")}}}
	cp := Checkpoint{ID: uuid.NewString(), Root: delivery.Root, SessionID: "session", SourceFingerprint: Hash("current source"), PlanFingerprint: delivery.Fingerprint(), TaskFingerprints: map[string]string{"done": Hash("task")}}
	plan, err := PlanResume(t.Context(), ResumeInput{Checkpoint: cp, State: State{Delivery: delivery}, Source: cp.SourceFingerprint, TaskSpecs: cp.TaskFingerprints, TaskStates: map[string]string{"done": "completed"}})
	require.NoError(t, err)
	require.Equal(t, []string{"done"}, plan.ReverifyTasks)
	require.Empty(t, plan.ReadyTasks)
}

func TestEditPlanResumeIsAmbiguousUntilRecovered(t *testing.T) {
	t.Parallel()
	cp := Checkpoint{ID: uuid.NewString(), Root: t.TempDir(), SessionID: "session", SourceFingerprint: Hash("source"), PlanFingerprint: Hash("plan"), TaskFingerprints: map[string]string{"pending": Hash("task")}}
	state := State{SemanticEdits: map[string]SemanticEditState{"edit": {ID: "edit", Status: "applying"}}}
	plan, err := PlanResume(t.Context(), ResumeInput{Checkpoint: cp, State: state, Source: cp.SourceFingerprint, TaskSpecs: cp.TaskFingerprints, TaskStates: map[string]string{"pending": "pending"}})
	require.NoError(t, err)
	require.Contains(t, plan.AmbiguousOperations, "semantic-edit:edit")
	require.Empty(t, plan.ReadyTasks)
}

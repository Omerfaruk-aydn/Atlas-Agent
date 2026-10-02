package workflows

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestRecipesActivePlanAndResumeHash(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	root, dir := t.TempDir(), t.TempDir()
	conn, err := db.Connect(ctx, dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Release(dir) })
	sessions := session.NewService(db.New(conn), conn)
	sess, err := sessions.Create(ctx, "recipe")
	require.NoError(t, err)
	store := engineering.NewStore(dir)
	r := recipeFixture()
	source := filepath.Join(root, "feature.json")
	data, err := json.Marshal(r)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(source, data, 0o600))
	compiled, err := Compile(ctx, r, map[string]json.RawMessage{"target": json.RawMessage(`"widget"`)}, []string{"backend", "review"})
	require.NoError(t, err)
	run, err := Install(ctx, store, sessions, sess.ID, root, compiled)
	require.NoError(t, err)
	state, err := store.Read(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, "active", state.Recipe.Status)
	require.Equal(t, root, state.Delivery.Root)
	require.NoError(t, ValidateBinding(ctx, store, sess.ID, compiled.RecipeHash, compiled.ParametersHash))
	require.NoError(t, Current(ctx, store, sess.ID, []string{source}, []string{"backend", "review"}))
	_, err = store.CleanArtifacts(ctx)
	require.NoError(t, err)
	saved, err := ReadCompiled(ctx, store, *state.Recipe)
	require.NoError(t, err)
	require.Equal(t, compiled.Checks, saved.Checks)
	// Reconcile interruption after the SQLite graph write, without duplicating
	// tasks or resetting their progress.
	require.NoError(t, store.Update(ctx, sess.ID, func(st *engineering.State) error { st.Recipe.Status = "prepared"; st.Delivery = nil; return nil }))
	retried, err := Install(ctx, store, sessions, sess.ID, root, compiled)
	require.NoError(t, err)
	require.Equal(t, run.ID, retried.ID)
	latest, err := sessions.Get(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, latest.Todos, 2)
	r.Steps[0].Prompt = "Changed ${target}"
	data, err = json.Marshal(r)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(source, data, 0o600))
	require.Error(t, Current(ctx, store, sess.ID, []string{source}, []string{"backend", "review"}), "editing the admitted recipe blocks dispatch and resume")
	edited, err := Compile(ctx, r, map[string]json.RawMessage{"target": json.RawMessage(`"widget"`)}, []string{"backend", "review"})
	require.NoError(t, err)
	require.Error(t, ValidateBinding(ctx, store, sess.ID, edited.RecipeHash, edited.ParametersHash))
	_, err = Install(ctx, store, sessions, sess.ID, root, edited)
	require.Error(t, err, "an active plan is never overwritten")
	other, err := sessions.Create(ctx, "existing tasks")
	require.NoError(t, err)
	other.Todos = []session.Todo{{ID: "old", Content: "Keep original", Status: session.TodoStatusPending}}
	_, err = sessions.Save(ctx, other)
	require.NoError(t, err)
	_, err = Install(ctx, store, sessions, other.ID, root, compiled)
	require.Error(t, err)
}

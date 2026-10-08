package agent

import (
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func TestShutdownWaitsForDetachedTitleWithoutActiveTurn(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	model := &deadlineTitleModel{}
	a := testSessionAgent(env, model, model, "system").(*sessionAgent)
	a.engineering = engineering.NewStore(t.TempDir())
	current, err := env.sessions.Create(t.Context(), "Original title")
	require.NoError(t, err)
	a.startTitle(t.Context(), current.ID, "Generate a title")
	require.Eventually(t, func() bool { return model.calls.Load() > 0 }, time.Second, time.Millisecond)
	a.CancelAll()
	a.titleMu.Lock()
	remaining := len(a.titleJobs)
	a.titleMu.Unlock()
	require.Zero(t, remaining)
	updated, err := env.sessions.Get(t.Context(), current.ID)
	require.NoError(t, err)
	require.Equal(t, "Original title", updated.Title, "shutdown must not start a detached fallback write")
}

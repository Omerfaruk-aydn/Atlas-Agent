package browser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInitialURLIsLaunchScoped(t *testing.T) {
	t.Parallel()
	var launched []Options
	m := newManager(Options{}, func(opts Options) (Session, error) {
		launched = append(launched, opts)
		return &fakeSession{}, nil
	})
	defer m.CloseAll()
	first, err := m.SessionContextURL(t.Context(), "chat", "https://example.com/task")
	require.NoError(t, err)
	reused, err := m.SessionContextURL(t.Context(), "chat", "https://example.com/next")
	require.NoError(t, err)
	require.Same(t, first, reused)
	_, err = m.SessionContext(t.Context(), "other")
	require.NoError(t, err)
	require.Len(t, launched, 2)
	require.Equal(t, "https://example.com/task", launched[0].initialURL)
	require.Empty(t, launched[1].initialURL)
	require.Empty(t, m.opts.initialURL)
	for _, invalid := range []string{"about:blank", "file:///test", "https:///missing", "https://user:password@example.com"} {
		_, err := m.SessionContextURL(t.Context(), "invalid", invalid)
		require.Error(t, err)
	}
	require.Len(t, launched, 2)
}

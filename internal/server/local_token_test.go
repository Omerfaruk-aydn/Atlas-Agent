package server

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocalTokenIsSharedAndScopedToPort(t *testing.T) {
	t.Setenv("ATLAS_SERVER_TOKEN", "")
	t.Setenv("ATLAS_AGENT_GLOBAL_DATA", t.TempDir())
	const workers = 12
	tokens := make([]string, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tokens[i], errs[i] = LocalToken("tcp", "127.0.0.1:4321")
		}()
	}
	wg.Wait()
	for i := range workers {
		require.NoError(t, errs[i])
		require.Equal(t, tokens[0], tokens[i])
	}
	require.Len(t, tokens[0], 64)
	alias, err := LocalToken("tcp", "localhost:4321")
	require.NoError(t, err)
	require.Equal(t, tokens[0], alias)
	other, err := LocalToken("tcp", "127.0.0.1:4322")
	require.NoError(t, err)
	require.NotEqual(t, tokens[0], other)
	remote, err := LocalToken("tcp", "example.com:4321")
	require.NoError(t, err)
	require.Empty(t, remote)
}

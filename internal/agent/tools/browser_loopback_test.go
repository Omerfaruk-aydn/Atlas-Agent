package tools

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoopbackRefused(t *testing.T) {
	t.Parallel()
	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.False(t, loopbackRefused(t.Context(), "http://"+addr+"/"), "a running server is reachable")
	require.NoError(t, listener.Close())
	require.True(t, loopbackRefused(t.Context(), "http://"+addr+"/"), "a stopped dev server is detected")

	require.False(t, loopbackRefused(t.Context(), "https://example.com/"), "remote pages are never probed")
	require.False(t, loopbackRefused(t.Context(), "about:blank"))
	require.True(t, isLoopbackHost("localhost"))
	require.True(t, isLoopbackHost("app.localhost"))
	require.True(t, isLoopbackHost("::1"))
	require.False(t, isLoopbackHost("10.0.0.2"))
}

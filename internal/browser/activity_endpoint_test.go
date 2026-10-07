package browser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeEndpointCandidatesAreStrictlyLoopback(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"http://127.0.0.1:9222", "ws://localhost:9222/devtools/browser/id", "http://[::1]:9222"} {
		port, ok := localBrowserEndpointPort(raw)
		require.True(t, ok)
		require.Equal(t, uint16(9222), port)
	}
	for _, raw := range []string{"https://remote.example:9222", "http://192.168.1.20:9222", "http://localhost.remote.example:9222", "http://user:secret@localhost:9222", "http://localhost:0", "http://localhost:65536", "file://localhost:9222", ""} {
		_, ok := localBrowserEndpointPort(raw)
		require.False(t, ok, raw)
	}
}

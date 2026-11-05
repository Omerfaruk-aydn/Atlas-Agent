package client

import (
	"net"
	"net/http"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/server"
	"github.com/stretchr/testify/require"
)

func TestSharedServerRequestIncludesConfiguredToken(t *testing.T) {
	t.Setenv("ATLAS_SERVER_TOKEN", "fixture-token")
	client, err := NewClient(t.TempDir(), "tcp", "127.0.0.1:1234")
	require.NoError(t, err)
	request, err := client.buildReq(t.Context(), http.MethodGet, "http://localhost/v1/health", nil, nil)
	require.NoError(t, err)
	require.Equal(t, "Bearer fixture-token", request.Header.Get("Authorization"))
}

func TestLocalClientAndServerAuthenticateAutomatically(t *testing.T) {
	t.Setenv("ATLAS_SERVER_TOKEN", "")
	t.Setenv("ATLAS_AGENT_GLOBAL_DATA", t.TempDir())
	ln, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx
	require.NoError(t, err)
	s := server.NewServer(nil, "tcp", ln.Addr().String())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ln) }()
	t.Cleanup(func() { _ = s.Close(); <-done })
	c, err := NewClient(t.TempDir(), "tcp", ln.Addr().String())
	require.NoError(t, err)
	require.NoError(t, c.Health(t.Context()))
}

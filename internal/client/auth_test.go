package client

import (
	"net/http"
	"testing"

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

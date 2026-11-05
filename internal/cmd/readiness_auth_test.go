package cmd

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProbeHealthIncludesServerToken(t *testing.T) {
	t.Setenv("ATLAS_SERVER_TOKEN", "readiness-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer readiness-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	require.NoError(t, probeHealth(t.Context(), server.Client(), server.URL, &url.URL{Scheme: "tcp"}))
}

package mcpoauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOAuthMetadataClientFiltersOnlyProtectedResourceScopes(t *testing.T) {
	t.Parallel()
	advertised := []string{"text_to_speech", "ads_read", "voice_generation"}
	mux := http.NewServeMux()
	for _, path := range []string{
		"/.well-known/oauth-protected-resource/mcp",
		"/.well-known/oauth-authorization-server",
	} {
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"scopes_supported": advertised,
			})
		})
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := newOAuthMetadataClient(http.DefaultTransport, server.URL, []string{"text_to_speech", "voice_generation"})
	for _, test := range []struct {
		name     string
		path     string
		expected []string
	}{
		{
			name:     "protected resource is restricted",
			path:     "/.well-known/oauth-protected-resource/mcp",
			expected: []string{"text_to_speech", "voice_generation"},
		},
		{
			name:     "authorization server remains unchanged",
			path:     "/.well-known/oauth-authorization-server",
			expected: advertised,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+test.path, nil)
			require.NoError(t, err)
			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			var metadata struct {
				Scopes []string `json:"scopes_supported"`
			}
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&metadata))
			require.Equal(t, test.expected, metadata.Scopes)
		})
	}
}

func TestOAuthMetadataClientPreservesAdvertisedScopesByDefault(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"scopes_supported":["text_to_speech","ads_read"]}`))
	}))
	t.Cleanup(server.Close)

	client := newOAuthMetadataClient(http.DefaultTransport, server.URL)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/.well-known/oauth-protected-resource/mcp", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	var metadata struct {
		Scopes []string `json:"scopes_supported"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&metadata))
	require.Equal(t, []string{"text_to_speech", "ads_read"}, metadata.Scopes)
}

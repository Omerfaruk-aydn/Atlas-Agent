package config

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

// ModelsEndpoint must stay in step with the request TestConnection makes,
// since provider add promises that `provider test` passes afterwards.
func TestModelsEndpointMatchesTestConnection(t *testing.T) {
	t.Parallel()
	for _, typ := range []catwalk.Type{catwalk.TypeOpenAICompat, catwalk.TypeOpenAI, catwalk.TypeAnthropic, catwalk.TypeGoogle} {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)

		pc := ProviderConfig{ID: "custom", Type: typ, BaseURL: srv.URL, APIKey: "k"}
		require.NoError(t, pc.TestConnection(IdentityResolver()), typ)

		endpoint, headers, ok := ModelsEndpoint(typ, srv.URL+"/", "k")
		require.True(t, ok, typ)
		require.Equal(t, srv.URL+gotPath, endpoint, typ)
		require.NotEmpty(t, headers, typ)
		for _, v := range headers {
			require.NotContains(t, endpoint, v, "the key stays out of the URL")
		}
	}
	_, _, ok := ModelsEndpoint(catwalk.TypeBedrock, "https://x", "k")
	require.False(t, ok)
}

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTokenAuthRejectsMissingAndIncorrectCredentials(t *testing.T) {
	t.Setenv("ATLAS_SERVER_TOKEN", "fixture-token")
	handler := TokenAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, authorization := range []string{"", "Bearer wrong", "Basic fixture-token", "Bearer fixture-token"} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/config", nil)
		request.Header.Set("Authorization", authorization)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if authorization == "Bearer fixture-token" {
			require.Equal(t, http.StatusNoContent, response.Code)
		} else {
			require.Equal(t, http.StatusUnauthorized, response.Code)
		}
	}
}

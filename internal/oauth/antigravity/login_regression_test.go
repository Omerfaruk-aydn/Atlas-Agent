package antigravity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDiscoverProjectFollowsOperationAndDefaultTier(t *testing.T) {
	withFastPolling(t)
	var starts, polls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NotEmpty(t, r.Header.Get("Client-Metadata"))
		switch r.URL.Path {
		case "/v1internal:loadCodeAssist":
			writeJSON(w, map[string]any{"allowedTiers": []map[string]any{{"id": "other"}, {"id": "free-tier", "isDefault": true}}})
		case "/v1internal:onboardUser":
			starts++
			var req onboardUserRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			require.Equal(t, "free-tier", req.TierID)
			writeJSON(w, map[string]any{"name": "operations/setup-123", "done": false})
		case "/v1internal/operations/setup-123":
			require.Equal(t, http.MethodGet, r.Method)
			polls++
			if polls == 1 {
				writeJSON(w, map[string]any{"done": false})
				return
			}
			writeJSON(w, map[string]any{"done": true, "response": map[string]any{"cloudaicompanionProject": map[string]string{"id": "project-ready"}}})
		default:
			t.Errorf("Unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	pointCodeAssistAt(t, srv)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	project, tier, err := discoverProject(ctx, "test-token", nil)
	require.NoError(t, err)
	require.Equal(t, "project-ready", project)
	require.Equal(t, "free-tier", tier)
	require.Equal(t, 1, starts)
	require.Equal(t, 2, polls)
}

func TestDiscoverProjectReportsTerminalFailures(t *testing.T) {
	for _, response := range []string{
		`{"done":true}`,
		`{"done":true,"error":{"code":7,"message":"Account is ineligible"}}`,
	} {
		t.Run(response, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1internal:loadCodeAssist" {
					writeJSON(w, map[string]any{"allowedTiers": []map[string]any{{"id": "free", "isDefault": true}}})
					return
				}
				_, _ = w.Write([]byte(response))
			}))
			defer srv.Close()
			pointCodeAssistAt(t, srv)
			_, _, err := discoverProject(t.Context(), "test-token", nil)
			require.Error(t, err)
			require.NotErrorIs(t, err, context.DeadlineExceeded)
		})
	}
}

func TestCallbackDoesNotClaimConnectionBeforeSetup(t *testing.T) {
	t.Parallel()
	s := &AuthSession{state: "expected", resultCh: make(chan authResult, 1)}
	w := httptest.NewRecorder()
	s.handleCallback(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/oauth-callback?state=wrong&code=test", nil))
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Empty(t, s.resultCh, "An unrelated callback must not consume the login")
	w = httptest.NewRecorder()
	s.handleCallback(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/oauth-callback?state=expected&code=test", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "Authorization received")
	require.NotContains(t, w.Body.String(), "is now connected")
	require.Equal(t, "test", (<-s.resultCh).code)
}

func TestDiscoverProjectPreservesPaidSubscription(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"currentTier":             map[string]string{"id": "free-tier"},
			"paidTier":                map[string]string{"id": "g1-pro-tier"},
			"cloudaicompanionProject": "account-project",
		})
	}))
	defer srv.Close()
	pointCodeAssistAt(t, srv)
	project, plan, err := discoverProject(t.Context(), "test-token", nil)
	require.NoError(t, err)
	require.Equal(t, "account-project", project)
	require.Equal(t, "g1-pro-tier", plan)
}

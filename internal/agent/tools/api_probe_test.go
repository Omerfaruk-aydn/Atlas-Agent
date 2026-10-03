package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

func TestAPIProbeAssertsJSONAndDoesNotFollowRedirect(t *testing.T) {
	t.Parallel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/target", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	perms := permission.NewPermissionService(t.TempDir(), true, nil)
	tool := NewAPIProbeTool(t.TempDir(), perms, URLPolicy{})
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "fixture")
	response, err := tool.Run(ctx, fantasy.ToolCall{Input: `{"url":"` + server.URL + `","required_keys":["ok"]}`})
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	require.True(t, ToolOutcomeObserved("api_probe", response, err))
	require.True(t, ToolSucceeded("api_probe", response, err))
	response, err = tool.Run(ctx, fantasy.ToolCall{Input: `{"url":"` + server.URL + `/redirect","expected_status":302}`})
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	require.Equal(t, 2, requests)
	store := engineering.NewStore(t.TempDir())
	verify := NewVerifyTool(t.TempDir(), store, tool.Run)
	input, err := json.Marshal(VerifyParams{Action: "run", Checks: []VerificationStep{{Name: "live-api", Tool: "api_probe", Input: json.RawMessage(`{"url":"` + server.URL + `","required_keys":["ok"]}`)}}})
	require.NoError(t, err)
	response, err = verify.Run(ctx, fantasy.ToolCall{ID: "api-verification", Input: string(input)})
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	require.Contains(t, response.Content, `"observed":true`)
	state, err := store.Read(ctx, "fixture")
	require.NoError(t, err)
	require.Len(t, state.Checks, 1)
	require.True(t, state.Checks[0].Passed)
	require.Equal(t, "api_probe", state.Checks[0].Tool)
}

package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

type startupBrowserSessions struct {
	*fakeBrowserSessions
	initialURL string
}

func (s *startupBrowserSessions) SessionContextURL(_ context.Context, id, url string) (browser.Session, error) {
	s.initialURL = url
	return s.Session(id)
}

func TestBrowserStartupURLHonorsPermissionAndGuards(t *testing.T) {
	for _, test := range []struct {
		name      string
		perms     permission.Service
		guard     string
		wantURL   string
		wantError bool
	}{
		{name: "authorized", perms: &mockPermissionService{}, wantURL: "https://example.com"},
		{name: "denied", perms: &denyingPermissionService{}, wantError: true},
		{name: "guarded", perms: &mockPermissionService{}, guard: "https://example.com", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			sessions := &startupBrowserSessions{fakeBrowserSessions: newFakeBrowserSessions()}
			params := BrowserParams{Action: "navigate", URL: "https://example.com"}
			params.Advanced.ExpectedOrigin = test.guard
			input, err := json.Marshal(params)
			require.NoError(t, err)
			ctx := context.WithValue(t.Context(), SessionIDContextKey, "startup-"+test.name)
			response, err := newBrowserTool(test.perms, t.TempDir(), sessions, browserDescription(false)).Run(ctx, fantasy.ToolCall{
				ID: "startup-call", Name: BrowserToolName, Input: string(input),
			})
			require.NoError(t, err)
			require.Equal(t, test.wantError, response.IsError)
			require.Equal(t, test.wantURL, sessions.initialURL)
			if test.name == "denied" {
				require.Empty(t, sessions.sessions)
			}
		})
	}
}

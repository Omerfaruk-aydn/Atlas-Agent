package server

import (
	"net/http/httptest"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestWithoutSideChatsHonoursIncludeSide(t *testing.T) {
	t.Parallel()

	sessions := []session.Session{
		{ID: "main"},
		{ID: "side", Tags: []string{session.SideChatTagPrefix + "main"}},
	}

	// The list and the search handlers both pass their results through this.
	plain := httptest.NewRequest("GET", "/v1/workspaces/w/sessions/search?q=x", nil)
	got := withoutSideChats(sessions, plain)
	require.Len(t, got, 1)
	require.Equal(t, "main", got[0].ID)

	desktop := httptest.NewRequest("GET", "/v1/workspaces/w/sessions/search?q=x&include_side=1", nil)
	require.Len(t, withoutSideChats(sessions, desktop), 2)
}

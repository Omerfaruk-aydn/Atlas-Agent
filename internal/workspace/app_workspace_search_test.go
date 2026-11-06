package workspace

import (
	"context"
	"errors"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestSessionsForIDsSkipsSideChatsAndMissing(t *testing.T) {
	t.Parallel()

	all := map[string]session.Session{
		"a": {ID: "a", Title: "A"},
		"b": {ID: "b", Title: "Side", Tags: []string{session.SideChatTagPrefix + "a"}},
		"c": {ID: "c", Title: "C", Tags: []string{"other"}},
	}
	get := func(_ context.Context, id string) (session.Session, error) {
		s, ok := all[id]
		if !ok {
			return session.Session{}, errors.New("not found")
		}
		return s, nil
	}

	got := sessionsForIDs(context.Background(), []string{"b", "c", "gone", "a"}, get)
	require.Len(t, got, 2)
	require.Equal(t, "c", got[0].ID, "the order of the matches is kept")
	require.Equal(t, "a", got[1].ID)
}

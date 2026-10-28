package engineering

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCatalogRemovesEvidenceFreeAndExpiredMethodsAndCountsOffersOnce(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	root := t.TempDir()
	key, err := methodKey(root)
	require.NoError(t, err)
	fresh := LearnedMethod{ID: "fresh", SessionID: "origin", CreatedAt: time.Now().UnixMilli(), Evidence: []EvidenceRef{{OperationID: "check"}}}
	old := fresh
	old.ID = "old"
	old.CreatedAt = time.Now().Add(-91 * 24 * time.Hour).UnixMilli()
	require.NoError(t, s.Update(t.Context(), key, func(st *State) error { st.LearnedMethods = []LearnedMethod{fresh, old, {ID: "phantom"}}; return nil }))
	methods, err := s.LearnedMethods(t.Context(), root)
	require.NoError(t, err)
	require.Len(t, methods, 1)
	count, err := s.OfferMethods(t.Context(), root, "task", []string{"fresh"})
	require.NoError(t, err)
	require.Equal(t, 1, count)
	count, err = s.OfferMethods(t.Context(), root, "task", []string{"fresh"})
	require.NoError(t, err)
	require.Zero(t, count)
	methods, err = s.LearnedMethods(t.Context(), root)
	require.NoError(t, err)
	require.Equal(t, 1, methods[0].Usage.Offered)
}

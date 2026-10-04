package i18n

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRelativeTimeLocalization(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	require.Equal(t, "3 dakika önce", RelativeTime("tr", now.Add(-3*time.Minute), now))
	require.Equal(t, "vor 1 Minute", RelativeTime("de", now.Add(-time.Minute), now))
	require.Equal(t, "منذ دقيقتين", RelativeTime("ar", now.Add(-2*time.Minute), now))
	require.Equal(t, "منذ 12 دقيقة", RelativeTime("ar", now.Add(-12*time.Minute), now))
	require.Equal(t, "tra 3 minuti", RelativeTime("it", now.Add(3*time.Minute), now))
}

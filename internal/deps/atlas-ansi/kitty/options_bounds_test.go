package kitty

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnmarshalTextRejectsOutOfRangeQuietValue(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"q=-1", "q=256"} {
		opts := Options{Quite: 7}
		require.NoError(t, opts.UnmarshalText([]byte(value)))
		require.Equal(t, byte(7), opts.Quite)
	}
	var opts Options
	require.NoError(t, opts.UnmarshalText([]byte("q=255")))
	require.Equal(t, byte(255), opts.Quite)
}

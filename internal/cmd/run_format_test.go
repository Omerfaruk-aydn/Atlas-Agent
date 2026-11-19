package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveOutputFormat(t *testing.T) {
	t.Parallel()

	require.Equal(t, "text", resolveOutputFormat(false, false, "text"))
	require.Equal(t, "json", resolveOutputFormat(true, false, "text"))
	require.Equal(t, "json", resolveOutputFormat(false, true, "json"))
	require.Equal(t, "stream-json", resolveOutputFormat(false, true, "stream-json"))
	require.Equal(t, "text", resolveOutputFormat(false, false, ""))
}

package agent

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

func modelWithWindow(window, output int64) Model {
	return Model{CatwalkCfg: catwalk.Model{ContextWindow: window, DefaultMaxTokens: output}}
}

func TestFallbackWindowDecidesWhenToCompact(t *testing.T) {
	t.Parallel()

	large, small := modelWithWindow(1_000_000, 16_000), modelWithWindow(128_000, 16_000)
	const used = 150_000

	// The same conversation is far from the primary's limit and past the
	// fallback's. Using the primary's window for both was the bug.
	require.False(t, shouldAutoSummarize(activeContextWindow(large), used, 0.8))
	require.True(t, shouldAutoSummarize(activeContextWindow(small), used, 0.8))
	require.True(t, fitsContextWindow(large, used))
	require.False(t, fitsContextWindow(small, used))
}

func TestUnknownWindowNeverBlocks(t *testing.T) {
	t.Parallel()
	require.True(t, fitsContextWindow(modelWithWindow(0, 0), 10_000_000))
}

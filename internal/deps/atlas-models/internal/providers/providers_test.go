package providers

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

// The muse catalog entry is the model picker's only view of the
// Meta subscription: ids must match the models the Messages API
// serves, and every model must carry a max-tokens default because
// Meta requires max_tokens on every request.
func TestMuseCatalogEntry(t *testing.T) {
	var entry *catwalk.Provider
	for _, p := range GetAll() {
		if p.ID == catwalk.InferenceProviderMuse {
			p := p
			entry = &p
		}
	}
	require.NotNil(t, entry, "the embedded catalog must include the muse provider")

	require.Equal(t, "Muse", entry.Name)
	require.Equal(t, "https://api.meta.ai", entry.APIEndpoint)
	require.Equal(t, catwalk.TypeMuse, entry.Type)
	require.Equal(t, "muse-spark-1.3", entry.DefaultLargeModelID)
	require.Equal(t, "muse-spark-1.2", entry.DefaultSmallModelID)

	ids := make(map[string]catwalk.Model, len(entry.Models))
	for _, m := range entry.Models {
		ids[m.ID] = m
	}
	for _, want := range []string{
		"muse-spark-1.3",
		"muse-spark-1.3-contributor",
		"muse-spark-1.2",
		"muse-spark-1.2-contributor",
		"muse-spark-1.1",
	} {
		m, ok := ids[want]
		require.True(t, ok, "catalog must include model %s", want)
		require.Positive(t, m.ContextWindow, "model %s needs a context window", want)
		require.Positive(t, m.DefaultMaxTokens, "model %s needs a max-tokens default; Meta rejects requests without max_tokens", want)
		require.True(t, m.CanReason, "Muse Spark always reasons; model %s must be marked accordingly", want)
		require.True(t, m.SupportsImages, "model %s supports image input", want)
	}
}

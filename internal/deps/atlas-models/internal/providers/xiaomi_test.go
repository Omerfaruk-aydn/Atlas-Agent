package providers

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

func TestXiaomiCatalog(t *testing.T) {
	t.Parallel()
	want := map[string]string{
		"xiaomi":                "https://api.xiaomimimo.com/v1",
		"xiaomi-token-plan-cn":  "https://token-plan-cn.xiaomimimo.com/v1",
		"xiaomi-token-plan-sgp": "https://token-plan-sgp.xiaomimimo.com/v1",
		"xiaomi-token-plan-ams": "https://token-plan-ams.xiaomimimo.com/v1",
	}
	keys := map[string]bool{}
	for _, p := range GetAll() {
		endpoint, ok := want[string(p.ID)]
		if !ok {
			continue
		}
		delete(want, string(p.ID))
		require.Equal(t, endpoint, p.APIEndpoint)
		require.Equal(t, catwalk.TypeOpenAICompat, p.Type)
		require.False(t, keys[p.APIKey], "API and regional plan credentials must be separate")
		keys[p.APIKey] = true
		models := map[string]catwalk.Model{}
		for _, m := range p.Models {
			require.NotContains(t, models, m.ID)
			models[m.ID] = m
			require.Positive(t, m.ContextWindow)
			require.Positive(t, m.DefaultMaxTokens)
			require.True(t, m.CanReason)
			require.Empty(t, m.ReasoningLevels, "MiMo exposes a thinking toggle, not effort levels")
			if p.ID != catwalk.InferenceProviderXiaomi {
				require.Zero(t, m.CostPer1MIn)
				require.Zero(t, m.CostPer1MOut)
				require.Zero(t, m.CostPer1MOutCached)
			}
		}
		require.Contains(t, models, p.DefaultLargeModelID)
		require.Contains(t, models, p.DefaultSmallModelID)
		require.True(t, models["mimo-v2.6-pro"].SupportsImages)
		require.True(t, models["mimo-v2.6-flash"].SupportsImages)
		if p.ID == catwalk.InferenceProviderXiaomi {
			require.Equal(t, 0.0036, models["mimo-v2.6-pro"].CostPer1MOutCached)
			require.Contains(t, models, "mimo-v2.6-pro-ultraspeed")
		} else {
			require.Len(t, models, 4, "only the four text models are supported by Token Plan")
		}
	}
	require.Empty(t, want, "all Xiaomi providers must be embedded")
}

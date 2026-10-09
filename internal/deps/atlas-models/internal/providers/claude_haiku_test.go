package providers

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

func TestClaudeHaiku55AccountCatalog(t *testing.T) {
	t.Parallel()
	for _, provider := range GetAll() {
		if provider.ID != catwalk.InferenceProviderClaude {
			continue
		}
		require.Equal(t, "claude-haiku-5-5", provider.DefaultSmallModelID)
		for _, model := range provider.Models {
			if model.ID != "claude-haiku-5-5" {
				continue
			}
			require.Equal(t, "Claude Haiku 5.5", model.Name)
			require.Equal(t, int64(1000000), model.ContextWindow)
			require.Equal(t, int64(128000), model.DefaultMaxTokens)
			require.True(t, model.CanReason)
			require.True(t, model.SupportsImages)
			require.Equal(t, []string{"low", "medium", "high", "xhigh", "max"}, model.ReasoningLevels)
			require.Equal(t, "medium", model.DefaultReasoningEffort)
			require.Zero(t, model.CostPer1MIn)
			require.Zero(t, model.CostPer1MOut)
			return
		}
	}
	t.Fatal("Claude Haiku 5.5 is missing from the account catalog")
}

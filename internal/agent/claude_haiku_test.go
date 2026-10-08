package agent

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/anthropic"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/embedded"
	"github.com/stretchr/testify/require"
)

func TestClaudeHaiku55CatalogEffortReachesAccountAdapter(t *testing.T) {
	t.Parallel()
	for _, provider := range embedded.GetAll() {
		if provider.ID != catwalk.InferenceProviderClaude {
			continue
		}
		for _, model := range provider.Models {
			if model.ID != "claude-haiku-5-5" {
				continue
			}
			for _, selected := range append([]string{"", "invalid"}, model.ReasoningLevels...) {
				t.Run(selected, func(t *testing.T) {
					t.Parallel()
					want := selected
					if selected == "" || selected == "invalid" {
						want = model.DefaultReasoningEffort
					}
					opts := getProviderOptions(Model{
						CatwalkCfg: model,
						ModelCfg: config.SelectedModel{
							Provider: "claude", ReasoningEffort: selected,
						},
					}, config.ProviderConfig{ID: "claude", Type: catwalk.TypeClaude})
					require.Len(t, opts, 1)
					parsed, ok := opts[anthropic.Name].(*anthropic.ProviderOptions)
					require.True(t, ok, "Claude account requests delegate to the Anthropic adapter")
					require.NotNil(t, parsed.Effort)
					require.Equal(t, anthropic.Effort(want), *parsed.Effort)
				})
			}
			return
		}
	}
	t.Fatal("Claude Haiku 5.5 is missing from the account catalog")
}

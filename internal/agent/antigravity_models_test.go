package agent

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/antigravity"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/embedded"
	"github.com/stretchr/testify/require"
)

func TestAntigravityCatalogEffortReachesAccountAdapter(t *testing.T) {
	t.Parallel()
	var provider catwalk.Provider
	for _, entry := range embedded.GetAll() {
		if entry.ID == catwalk.InferenceProviderAntigravity {
			provider = entry
			break
		}
	}
	require.NotEmpty(t, provider.Models)
	var tested int
	for _, model := range provider.Models {
		if model.ID != "claude-sonnet-5-5" && model.ID != "claude-opus-5-5" && model.ID != "gpt-oss-120b" {
			continue
		}
		tested++
		for _, selected := range append([]string{"", "invalid"}, model.ReasoningLevels...) {
			t.Run(model.ID+"/"+selected, func(t *testing.T) {
				t.Parallel()
				want := selected
				if selected == "" || selected == "invalid" {
					want = model.DefaultReasoningEffort
				}
				opts := getProviderOptions(Model{
					CatwalkCfg: model,
					ModelCfg:   config.SelectedModel{Provider: "antigravity", ReasoningEffort: selected},
				}, config.ProviderConfig{ID: "antigravity", Type: catwalk.TypeAntigravity})
				require.Len(t, opts, 1, "The model family must not change the account adapter")
				parsed, ok := opts[antigravity.Name].(*antigravity.ProviderOptions)
				require.True(t, ok)
				require.Equal(t, want, parsed.ThinkingLevel)
			})
		}
	}
	require.Equal(t, 3, tested)
}

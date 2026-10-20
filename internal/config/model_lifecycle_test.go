package config

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/embedded"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/env"
	"github.com/stretchr/testify/require"
)

func TestSavedBuiltInLifecycleDoesNotApplyToCustomHost(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "official", true: "custom"}[custom], func(t *testing.T) {
			cfg := &Config{}
			cfg.setDefaults(t.TempDir(), t.TempDir())
			pc := ProviderConfig{ID: "anthropic", APIKey: "test-key", Models: []catwalk.Model{
				{ID: "claude-opus-4-20250514", Name: "Saved old model"},
			}}
			if custom {
				pc.BaseURL = "https://my-private-host.invalid"
			}
			cfg.Providers.Set("anthropic", pc)
			environment := env.NewFromMap(map[string]string{"ANTHROPIC_API_KEY": "test-key"})
			var catalog []catwalk.Provider
			for _, p := range embedded.GetAll() {
				if p.ID == catwalk.InferenceProviderAnthropic {
					catalog = append(catalog, p)
				}
			}
			err := cfg.configureProviders(t.Context(), testStore(cfg), environment, NewShellVariableResolver(environment), catalog)
			require.NoError(t, err)
			if custom {
				require.NotNil(t, cfg.GetModel("anthropic", "claude-opus-4-20250514"))
			} else {
				require.Nil(t, cfg.GetModel("anthropic", "claude-opus-4-20250514"))
			}
		})
	}
}

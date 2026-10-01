package config

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/embedded"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/env"
	"github.com/stretchr/testify/require"
)

func TestLegacyZAIPlanKeyKeepsPlanEndpointAndDefault(t *testing.T) {
	known := embedded.GetAll()
	cfg := &Config{}
	cfg.setDefaults(t.TempDir(), "")
	testEnv := env.NewFromMap(map[string]string{"ZAI_API_KEY": "legacy-plan-key"})
	err := cfg.configureProviders(t.Context(), testStore(cfg), testEnv, NewShellVariableResolver(testEnv), known)
	require.NoError(t, err)
	plan, ok := cfg.Providers.Get("zai")
	require.True(t, ok)
	require.Equal(t, "https://api.z.ai/api/coding/paas/v4", plan.BaseURL)
	_, apiEnabled := cfg.Providers.Get("zai-api")
	require.False(t, apiEnabled, "plan credentials must not enable a separately billed API")
	large, small, err := cfg.defaultModelSelection(known)
	require.NoError(t, err)
	require.Equal(t, "zai", large.Provider)
	require.Equal(t, "zai", small.Provider)
}

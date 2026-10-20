package agent

import (
	"fmt"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

func TestOpenCodeTransportRouting(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, provider, transport, model, endpoint string
		cli, invalid                               bool
	}{
		{"free auto", "opencode-zen", "", "mimo-v2.6-flash-free", "https://opencode.ai/zen/v1", true, false},
		{"free responses", "opencode-zen", "auto", "muse-spark-1.3-contributor-free", "https://opencode.ai/zen/v1", true, false},
		{"paid API", "opencode-zen", "auto", "mimo-v2.6-flash", "https://opencode.ai/zen/v1", false, false},
		{"custom gateway", "opencode-zen", "auto", "mimo-v2.6-flash-free", "http://localhost:1234", false, false},
		{"force API", "opencode-zen", "api", "big-pickle", "https://opencode.ai/zen/v1", false, false},
		{"force CLI", "opencode-zen", "cli", "mimo-v2.6-flash", "", true, false},
		{"Go untouched", "opencode-go", "", "mimo-v2.6-flash", "https://opencode.ai/zen/go/v1", false, false},
		{"invalid", "opencode-zen", "unknown", "big-pickle", "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			use, err := useNativeCLI(config.ProviderConfig{ID: tc.provider, OpenCodeTransport: tc.transport}, tc.model, tc.endpoint)
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.cli, use)
		})
	}
}

func TestOpenCodeFreeModelBuildsCLIProvider(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)
	p, err := coord.buildProvider(config.ProviderConfig{ID: "opencode-zen", Type: catwalk.TypeOpenAICompat, BaseURL: "https://opencode.ai/zen/v1"}, config.SelectedModel{Model: "mimo-v2.6-flash-free"}, false)
	require.NoError(t, err)
	require.Contains(t, fmt.Sprintf("%T", p), "opencodecli.provider")
	require.Equal(t, "opencode-zen", p.Name())
}

func TestOpenCodeDocumentedInferenceEndpoints(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		provider, base, model, want string
		canonical                   bool
	}{
		{"opencode-zen", "https://opencode.ai/zen/v1", "claude-haiku-5-5", "https://opencode.ai/inference/anthropic", true},
		{"opencode-go", "https://opencode.ai/inference/go/v1", "gemini-4-argon-contributor", "https://opencode.ai/inference/go/google", true},
		{"opencode-go", "https://opencode.ai/zen/go/v1", "minimax-m3.1-flash-free", "https://opencode.ai/inference/go/openai/v1", true},
		{"opencode-zen", "http://localhost:1234/zen/v1", "claude-haiku-5-5", "http://localhost:1234/zen/v1", false},
		{"opencode-zen", "https://opencode.ai/custom/v1", "gpt-6-luna", "https://opencode.ai/custom/v1", false},
	} {
		base, canonical := gatewayAPIBase(tc.provider, tc.base, gatewayModelType(tc.provider, tc.model))
		require.Equal(t, tc.want, base)
		require.Equal(t, tc.canonical, canonical)
	}
	require.Equal(t, catwalk.TypeOpenAICompat, gatewayModelType("opencode-go", "minimax-m3.1-flash-free"))
}

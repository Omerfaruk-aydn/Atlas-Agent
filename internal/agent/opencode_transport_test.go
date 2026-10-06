package agent

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"

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
			use, err := useOpenCodeCLI(config.ProviderConfig{ID: tc.provider, OpenCodeTransport: tc.transport}, tc.model, tc.endpoint)
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.cli, use)
		})
	}
}

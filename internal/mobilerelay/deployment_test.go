package mobilerelay

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"
)

func TestDeploymentKeepsRelayListenerAndBuildContextPrivate(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../deploy/mobile-relay/compose.yaml")
	require.NoError(t, err)
	var compose struct {
		Services map[string]struct {
			Ports    []string
			Command  []string
			Secrets  []string
			ReadOnly bool `yaml:"read_only"`
		}
		Secrets map[string]struct{ File string }
	}
	require.NoError(t, yaml.Unmarshal(data, &compose))
	relay := compose.Services["relay"]
	require.Empty(t, relay.Ports, "the HTTP relay listener must not be published")
	require.True(t, relay.ReadOnly)
	require.Equal(t, []string{"connector_key"}, relay.Secrets)
	require.Contains(t, relay.Command, "--behind-proxy")
	require.Contains(t, compose.Services["caddy"].Ports, "443:443")
	require.Contains(t, compose.Secrets["connector_key"].File, "ATLAS_RELAY_KEY_FILE")
	data, err = os.ReadFile("../../deploy/mobile-relay/Dockerfile.dockerignore")
	require.NoError(t, err)
	require.Contains(t, string(data), "**\n")
	require.Contains(t, string(data), "!internal/mobilerelay/**")
	require.NotContains(t, string(data), "!internal/**", "only the relay source belongs in the image build")
	require.NotContains(t, string(data), "!.atlas", "desktop state must never enter the build context")
}

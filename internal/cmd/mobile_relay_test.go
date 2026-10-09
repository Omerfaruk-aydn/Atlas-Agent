package cmd

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobilerelay"
	"github.com/stretchr/testify/require"
)

func TestMobileRelayCommandsRequireExplicitPrivateConfiguration(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "connector.key")
	var output bytes.Buffer
	run := func(args ...string) error {
		output.Reset()
		command := newMobileRelayCommand()
		command.SetOut(&output)
		command.SetErr(&output)
		command.SetArgs(args)
		return command.ExecuteContext(t.Context())
	}
	require.NoError(t, run("keygen", "--out", path))
	token, err := mobilerelay.ReadToken(path)
	require.NoError(t, err)
	require.NotContains(t, output.String(), token)
	require.Error(t, run("keygen", "--out", path))
	for _, args := range [][]string{
		{"serve"},
		{"serve", "--public-url", "http://atlas.example.com", "--token-file", path},
		{"serve", "--public-url", "https://atlas.example.com", "--token-file", path, "--listen", "0.0.0.0:8787"},
		{"serve", "--public-url", "https://atlas.example.com", "--token-file", path, "--tls-cert", "missing.pem"},
	} {
		require.Error(t, run(args...))
		require.NotContains(t, output.String(), token)
	}
}

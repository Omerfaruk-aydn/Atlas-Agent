package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyLoginProviders(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"xiaomi", "xiaomi-token-plan-cn", "xiaomi-token-plan-sgp", "xiaomi-token-plan-ams", "opencode-go", "openai", "deepseek", "alibaba-coding", "minimax-m-plan", "zai-api"} {
		p, ok := apiKeyLoginProvider(id)
		require.True(t, ok, id)
		require.Equal(t, id, string(p.ID))
	}
	for _, id := range []string{"codex", "claude", "github-copilot", "muse", "unknown"} {
		_, ok := apiKeyLoginProvider(id)
		require.False(t, ok, id)
	}
	require.Equal(t, "xiaomi-token-plan-sgp", resolveLoginAlias("mimo-token-plan"))
}

func TestUnavailableAccountsRejectBeforeWorkspaceSetup(t *testing.T) {
	for _, id := range unavailableAccountProviders {
		_, ok := apiKeyLoginProvider(id)
		require.False(t, ok, id)
		require.False(t, isAccountLoginProvider(id), id)
		err := loginCmd.RunE(loginCmd, []string{id})
		require.ErrorContains(t, err, "model calls are not implemented", id)
	}
}

func TestReadLoginKey(t *testing.T) {
	t.Parallel()
	key, err := readLoginKey(strings.NewReader("tp-example\r\n"))
	require.NoError(t, err)
	require.Equal(t, "tp-example", key)
	for _, input := range []string{"", " \n", "key\nother", "key with spaces", strings.Repeat("x", maxLoginKeyBytes+1)} {
		_, err := readLoginKey(strings.NewReader(input))
		require.Error(t, err)
		if input != "" {
			require.NotContains(t, err.Error(), input, "errors must not echo credentials")
		}
	}
}

type keyLoginWorkspace struct {
	provider string
	key      any
	scope    config.Scope
}

func (*keyLoginWorkspace) Config() *config.Config { return nil }

func (w *keyLoginWorkspace) SetProviderAPIKey(scope config.Scope, provider string, key any) error {
	w.scope, w.provider, w.key = scope, provider, key
	return nil
}

func TestLoginXiaomiPlanKey(t *testing.T) {
	t.Parallel()
	p, ok := apiKeyLoginProvider("xiaomi-token-plan-ams")
	require.True(t, ok)
	ws := &keyLoginWorkspace{}
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("tp-private-test-key\n"))
	var output bytes.Buffer
	cmd.SetOut(&output)
	require.NoError(t, loginAPIKey(cmd, ws, p, false, true))
	require.Equal(t, "xiaomi-token-plan-ams", ws.provider)
	require.Equal(t, "tp-private-test-key", ws.key)
	require.Equal(t, config.ScopeGlobal, ws.scope)
	require.NotContains(t, output.String(), "tp-private-test-key")
}

func TestListLoginProviders(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	require.NoError(t, listLoginProviders(&out))
	for _, want := range []string{"xiaomi-token-plan-ams", "xiaomi", "chatgpt", "copilot", "opencode-go"} {
		require.Contains(t, out.String(), want)
	}
}

package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	minimaxoauth "github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/minimax"
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

func TestKimiLoginAliasesAndModes(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"kimi", "kimi-code", "kimi-plan", "kimi-membership"} {
		require.Equal(t, "kimi-code", resolveLoginAlias(id), id)
		require.True(t, isAccountLoginProvider(id), id)
		_, usesKey := apiKeyLoginProvider(id)
		require.False(t, usesKey, id)
	}
	for _, id := range []string{"kimi-coding", "kimi-coding-global"} {
		p, usesKey := apiKeyLoginProvider(id)
		require.True(t, usesKey, id)
		require.Equal(t, id, string(p.ID))
		require.False(t, isAccountLoginProvider(id), id)
	}
	for _, id := range []string{"kimi", "kimi-plan", "kimi-membership", "kimi-code", "kimi-coding", "kimi-coding-global"} {
		require.Contains(t, loginCmd.ValidArgs, id)
	}
}

func TestListLoginProvidersExplainsKimi(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	require.NoError(t, listLoginProviders(&out))
	text := out.String()
	require.Contains(t, text, "kimi-code")
	require.Contains(t, text, "Kimi Code subscription, no API key")
	require.Contains(t, text, "kimi-coding")
	require.Contains(t, text, "kimi = Kimi Code subscription login (no API key); kimi-coding = Kimi API key.")
}

func TestMiniMaxLoginAliasesAndModes(t *testing.T) {
	t.Parallel()
	for id, want := range map[string]string{
		"minimax":              "minimax-plan",
		"minimax-global":       "minimax-plan",
		"minimax-subscription": "minimax-plan",
		"minimax-plan":         "minimax-plan",
		"minimax-cn":           "minimax-plan-cn",
		"minimaxi":             "minimax-plan-cn",
		"minimax-plan-cn":      "minimax-plan-cn",
	} {
		require.Equal(t, want, resolveLoginAlias(id), id)
		require.True(t, isAccountLoginProvider(id), id)
		_, usesKey := apiKeyLoginProvider(id)
		require.False(t, usesKey, id)
		require.Contains(t, loginCmd.ValidArgs, id)
	}
	// The API-key paths stay reachable and unchanged.
	require.Equal(t, "minimax-coding", resolveLoginAlias("minimax-token-plan"))
	for id, want := range map[string]string{
		"minimax-coding":     "minimax-coding",
		"minimax-token-plan": "minimax-coding",
		"minimax-china":      "minimax-china",
		"minimax-m-plan":     "minimax-m-plan",
		"minimax-api":        "minimax",
	} {
		p, usesKey := apiKeyLoginProvider(id)
		require.True(t, usesKey, id)
		require.Equal(t, want, string(p.ID), id)
		require.False(t, isAccountLoginProvider(id), id)
	}
}

func TestApplyMiniMaxRegion(t *testing.T) {
	t.Parallel()
	id, region, err := applyMiniMaxRegion("minimax-plan", "")
	require.NoError(t, err)
	require.Equal(t, "minimax-plan", id)
	require.Equal(t, minimaxoauth.Global, region)

	id, region, err = applyMiniMaxRegion("minimax-plan", "cn")
	require.NoError(t, err)
	require.Equal(t, "minimax-plan-cn", id)
	require.Equal(t, minimaxoauth.China, region)

	id, region, err = applyMiniMaxRegion("minimax-plan-cn", "")
	require.NoError(t, err)
	require.Equal(t, "minimax-plan-cn", id)
	require.Equal(t, minimaxoauth.China, region)

	_, _, err = applyMiniMaxRegion("minimax-plan", "mars")
	require.Error(t, err)
	_, _, err = applyMiniMaxRegion("minimax-coding", "cn")
	require.ErrorContains(t, err, "--region")
}

func TestListLoginProvidersExplainsMiniMax(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	require.NoError(t, listLoginProviders(&out))
	text := out.String()
	require.Contains(t, text, "minimax-plan")
	require.Contains(t, text, "MiniMax Token Plan subscription, no API key")
	require.Contains(t, text, "minimax-coding")
	require.Contains(t, text, "minimax-api")
	require.Contains(t, text, "minimax = MiniMax subscription login (no API key); minimax-coding = API key.")
}

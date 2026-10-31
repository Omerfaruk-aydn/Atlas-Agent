package cmd

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/appenv"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func newProviderAddCmd(t *testing.T) *cobra.Command {
	t.Helper()
	c := &cobra.Command{Use: "add", Args: cobra.ExactArgs(1)}
	// A fresh flag set with the real definitions, so values do not leak
	// between tests.
	registerProviderAddFlags(c.Flags())
	c.SetContext(t.Context())
	return c
}

func TestProviderAddFlagParsing(t *testing.T) {
	c := newProviderAddCmd(t)
	require.NoError(t, c.ParseFlags([]string{
		"--base-url", "https://api.example.com/v1", "--type", "anthropic", "--name", "Example",
		"--api-key-env", "EXAMPLE_KEY", "--no-discover", "--model", "a", "--model", "b",
		"--scope", "workspace", "--overwrite", "--allow-insecure",
	}))
	in, err := parseProviderAddFlags(c, []string{"example"})
	require.NoError(t, err)

	p := in.params
	require.Equal(t, "example", p.ID)
	require.Equal(t, "https://api.example.com/v1", p.BaseURL)
	require.Equal(t, "anthropic", p.Type)
	require.Equal(t, "Example", p.Name)
	require.Equal(t, "EXAMPLE_KEY", p.APIKeyEnv)
	require.False(t, p.DiscoverModels)
	require.Equal(t, []string{"a", "b"}, p.Models)
	require.Equal(t, config.ScopeWorkspace, p.Scope)
	require.True(t, p.Overwrite)
	require.True(t, p.AllowInsecure)
	require.False(t, in.keyStdin)
}

func TestProviderAddFlagDefaultsAndErrors(t *testing.T) {
	c := newProviderAddCmd(t)
	require.NoError(t, c.ParseFlags([]string{"--base-url", "https://api.example.com/v1"}))
	in, err := parseProviderAddFlags(c, []string{"example"})
	require.NoError(t, err)
	require.True(t, in.params.DiscoverModels)
	require.Equal(t, config.ScopeGlobal, in.params.Scope)

	for name, args := range map[string][]string{
		"missing url":    {},
		"both key flags": {"--base-url", "https://x.example/v1", "--api-key-env", "K", "--api-key-stdin"},
		"bad scope":      {"--base-url", "https://x.example/v1", "--scope", "everywhere"},
	} {
		c := newProviderAddCmd(t)
		require.NoError(t, c.ParseFlags(args), name)
		_, err := parseProviderAddFlags(c, []string{"example"})
		require.Error(t, err, name)
	}

	// There is deliberately no flag that takes the key itself.
	require.Nil(t, providerAddCmd.Flags().Lookup("api-key"))
}

func TestProviderAddReadsTheKeyFromStdinAndNeverPrintsIt(t *testing.T) {
	const key = "sk-cli-SECRET-1122334455"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			http.Error(w, "bad "+r.Header.Get("Authorization"), http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"cli-model"}]}`)
	}))
	t.Cleanup(srv.Close)

	t.Setenv(appenv.Prefix+"GLOBAL_CONFIG", t.TempDir())
	t.Setenv(appenv.Prefix+"GLOBAL_DATA", t.TempDir())
	store, err := config.Init(t.TempDir(), t.TempDir(), false)
	require.NoError(t, err)

	run := func(stdin string) (string, error) {
		c := newProviderAddCmd(t)
		var out, errOut bytes.Buffer
		c.SetOut(&out)
		c.SetErr(&errOut)
		c.SetIn(strings.NewReader(stdin))
		require.NoError(t, c.ParseFlags([]string{"--base-url", srv.URL + "/v1", "--api-key-stdin", "--overwrite"}))
		in, err := parseProviderAddFlags(c, []string{"clitest"})
		require.NoError(t, err)
		err = addProvider(c, store, in)
		return out.String() + errOut.String(), err
	}

	out, err := run("wrong-key\n")
	require.Error(t, err)
	require.NotContains(t, out+err.Error(), "wrong-key")
	_, ok := store.Config().Providers.Get("clitest")
	require.False(t, ok, "a failed probe writes nothing")

	out, err = run(key + "\n")
	require.NoError(t, err)
	require.Contains(t, out, "cli-model")
	require.NotContains(t, out, key)
	_, ok = store.Config().Providers.Get("clitest")
	require.True(t, ok)
}

func TestProviderAddWithoutATerminalOrKeyExplains(t *testing.T) {
	t.Setenv(appenv.Prefix+"GLOBAL_CONFIG", t.TempDir())
	t.Setenv(appenv.Prefix+"GLOBAL_DATA", t.TempDir())
	store, err := config.Init(t.TempDir(), t.TempDir(), false)
	require.NoError(t, err)

	c := newProviderAddCmd(t)
	c.SetIn(strings.NewReader(""))
	require.NoError(t, c.ParseFlags([]string{"--base-url", "https://api.example.com/v1"}))
	in, err := parseProviderAddFlags(c, []string{"example"})
	require.NoError(t, err)
	err = addProvider(c, store, in)
	require.ErrorContains(t, err, "api_key_env")
}

package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-term"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/providersetup"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var providerAddCmd = &cobra.Command{
	Use:   "add <id>",
	Short: "Add a custom provider and discover its models",
	Long: "Add a custom model provider. The endpoint is probed (GET <base-url>/models) with your key " +
		"before anything is written, its models are listed, and the provider is saved.\n\n" +
		"The key comes from --api-key-env (the NAME of an environment variable; only the name is stored), " +
		"--api-key-stdin, or, when neither is given and stdin is a terminal, a hidden prompt. " +
		"A key is never accepted as a command-line argument, so it cannot end up in shell history.",
	Example: "  atlas-agent provider add acme --base-url https://api.acme.example/v1 --api-key-env ACME_API_KEY\n" +
		"  printf %s \"$KEY\" | atlas-agent provider add acme --base-url https://api.acme.example/v1 --api-key-stdin",
	Args: cobra.ExactArgs(1),
	RunE: runProviderAdd,
}

func init() {
	registerProviderAddFlags(providerAddCmd.Flags())
	providerCmd.AddCommand(providerAddCmd)
}

func registerProviderAddFlags(f *pflag.FlagSet) {
	f.String("base-url", "", "API base URL from the provider's documentation (required)")
	f.String("type", "openai-compat", "API type: openai-compat, openai, anthropic or google")
	f.String("name", "", "display name (defaults to the id)")
	f.String("api-key-env", "", "name of the environment variable holding the API key")
	f.Bool("api-key-stdin", false, "read the API key from standard input")
	f.Bool("no-discover", false, "do not list models from the endpoint; use --model instead")
	f.StringSlice("model", nil, "model id to include (repeatable); required with --no-discover")
	f.String("scope", "global", "where to save: global or workspace")
	f.Bool("overwrite", false, "replace an existing provider with the same id")
	f.Bool("allow-insecure", false, "allow plain http to a non-local host (the key is sent unencrypted)")
}

// providerAddInput is the parsed command line.
type providerAddInput struct {
	params   providersetup.Params
	keyStdin bool
}

// parseProviderAddFlags turns flags into setup parameters, checking only
// what is about the command line itself; the rest is providersetup's.
func parseProviderAddFlags(cmd *cobra.Command, args []string) (providerAddInput, error) {
	f := cmd.Flags()
	baseURL, _ := f.GetString("base-url")
	typ, _ := f.GetString("type")
	name, _ := f.GetString("name")
	env, _ := f.GetString("api-key-env")
	stdin, _ := f.GetBool("api-key-stdin")
	noDiscover, _ := f.GetBool("no-discover")
	models, _ := f.GetStringSlice("model")
	scopeName, _ := f.GetString("scope")
	overwrite, _ := f.GetBool("overwrite")
	insecure, _ := f.GetBool("allow-insecure")

	if baseURL == "" {
		return providerAddInput{}, errors.New("--base-url is required: find the provider's API base URL in its documentation")
	}
	if env != "" && stdin {
		return providerAddInput{}, errors.New("use either --api-key-env or --api-key-stdin, not both")
	}
	var scope config.Scope
	switch strings.ToLower(scopeName) {
	case "", "global":
		scope = config.ScopeGlobal
	case "workspace":
		scope = config.ScopeWorkspace
	default:
		return providerAddInput{}, fmt.Errorf("--scope must be global or workspace, not %q", scopeName)
	}
	return providerAddInput{
		keyStdin: stdin,
		params: providersetup.Params{
			ID:             args[0],
			Name:           name,
			Type:           typ,
			BaseURL:        baseURL,
			APIKeyEnv:      env,
			DiscoverModels: !noDiscover,
			Models:         models,
			Scope:          scope,
			Overwrite:      overwrite,
			AllowInsecure:  insecure,
		},
	}, nil
}

func runProviderAdd(cmd *cobra.Command, args []string) error {
	in, err := parseProviderAddFlags(cmd, args)
	if err != nil {
		return err
	}
	dataDir, _ := cmd.Flags().GetString("data-dir")
	debug, _ := cmd.Flags().GetBool("debug")
	store, err := config.Init("", dataDir, debug)
	if err != nil {
		return fmt.Errorf("failed to initialize config: %w", err)
	}
	return addProvider(cmd, store, in)
}

// addProvider is split out so a test can hand it a store and a fake
// transport instead of the real config and network.
func addProvider(cmd *cobra.Command, store providersetup.Store, in providerAddInput) error {
	params := in.params
	var prompt providersetup.KeyPrompter

	switch {
	case in.keyStdin:
		key, err := readLoginKey(cmd.InOrStdin())
		if err != nil {
			return err
		}
		params.Key = key
	default:
		if f, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(f.Fd()) {
			prompt = func(_ context.Context, req providersetup.KeyRequest) (string, error) {
				fmt.Fprintf(cmd.ErrOrStderr(), "API key for %s (sent only to %s; input hidden): ", req.ProviderID, req.Host)
				data, err := term.ReadPassword(f.Fd())
				fmt.Fprintln(cmd.ErrOrStderr())
				return string(data), err
			}
		}
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	res, err := providersetup.Setup(ctx, store, params, prompt)
	if err != nil {
		return err
	}
	fmt.Fprint(cmd.OutOrStdout(), res.Summary())
	return nil
}

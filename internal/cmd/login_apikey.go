package cmd

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/embedded"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-term"
	"github.com/spf13/cobra"
)

var loginAliases = map[string]string{
	"mimo":                "xiaomi",
	"alibaba-coding-plan": "alibaba-coding",
	"qwen-coding-plan":    "alibaba-coding",
	"alibaba-token-plan":  "alibaba-token-plan-sgp",
	"qwen-token-plan":     "alibaba-token-plan-sgp",
	"alibaba-token-team":  "alibaba-token-plan-team-sgp",
	"kimi-code":           "kimi-coding",
	"kimi-code-global":    "kimi-coding-global",
	"qiniu":               "qiniucloud",
	"qiniu-plan":          "qiniu-token-plan",
	"minimax-mplan":       "minimax-m-plan",
	"zai-coding-plan":     "zai",
	"xiaomi-token-plan":   "xiaomi-token-plan-sgp",
	"mimo-token-plan":     "xiaomi-token-plan-sgp",
	"opencode":            "opencode-zen",
	"minimax-token-plan":  "minimax-coding",
	"github":              "copilot",
	"github-copilot":      "copilot",
	"codex":               "chatgpt",
	"codex-ide":           "openai",
	"claude-plan":         "claude",
	"grok-web":            "grok",
	"supergrok":           "grok",
	"codeium":             "windsurf",
	"jb-ai":               "jetbrains",
	"augmentcode":         "augment",
	"factory-ai":          "factory",
	"muse-code":           "muse",
}

var accountLoginProviders = []string{
	"copilot", "chatgpt", "antigravity", "claude", "muse",
}

var unavailableAccountProviders = []string{
	"grok", "windsurf", "jetbrains", "augment", "factory", "coderabbit", "zed",
}

func resolveLoginAlias(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if resolved, ok := loginAliases[id]; ok {
		return resolved
	}
	return id
}

func isAccountLoginProvider(id string) bool {
	return slices.Contains(accountLoginProviders, resolveLoginAlias(id))
}

func apiKeyLoginProvider(id string) (catwalk.Provider, bool) {
	id = resolveLoginAlias(id)
	if isAccountLoginProvider(id) {
		return catwalk.Provider{}, false
	}
	for _, p := range embedded.GetAll() {
		if string(p.ID) == id && p.APIKey != "" {
			return p, true
		}
	}
	return catwalk.Provider{}, false
}

func addAPIKeyLoginCompletions() {
	loginCmd.ValidArgs = slices.DeleteFunc(loginCmd.ValidArgs, func(id string) bool {
		return slices.Contains(unavailableAccountProviders, resolveLoginAlias(id))
	})
	for _, p := range embedded.GetAll() {
		id := string(p.ID)
		if _, ok := apiKeyLoginProvider(id); ok && !slices.Contains(loginCmd.ValidArgs, id) {
			loginCmd.ValidArgs = append(loginCmd.ValidArgs, id)
		}
	}
	aliases := make([]string, 0, len(loginAliases))
	for alias := range loginAliases {
		aliases = append(aliases, alias)
	}
	slices.Sort(aliases)
	for _, alias := range aliases {
		if slices.Contains(unavailableAccountProviders, resolveLoginAlias(alias)) {
			continue
		}
		if !slices.Contains(loginCmd.ValidArgs, alias) {
			loginCmd.ValidArgs = append(loginCmd.ValidArgs, alias)
		}
	}
}

func listLoginProviders(out io.Writer) error {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PROVIDER\tAUTHENTICATION")
	for _, id := range accountLoginProviders {
		fmt.Fprintf(w, "%s\tAccount sign-in\n", id)
	}
	for _, p := range embedded.GetAll() {
		if _, ok := apiKeyLoginProvider(string(p.ID)); ok {
			fmt.Fprintf(w, "%s\tAPI / plan key (%s)\n", p.ID, p.Name)
		}
	}
	return w.Flush()
}

const maxLoginKeyBytes = 16 * 1024

func readLoginKey(in io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(in, maxLoginKeyBytes+1))
	if err != nil {
		return "", fmt.Errorf("reading API key: %w", err)
	}
	if len(data) > maxLoginKeyBytes {
		return "", fmt.Errorf("API key exceeds %d bytes", maxLoginKeyBytes)
	}
	key := strings.TrimSpace(string(data))
	if key == "" || strings.ContainsAny(key, "\r\n\t ") {
		return "", fmt.Errorf("enter a single non-empty API or plan key")
	}
	return key, nil
}

type apiKeyLoginWorkspace interface {
	Config() *config.Config
	SetProviderAPIKey(config.Scope, string, any) error
}

func loginAPIKey(cmd *cobra.Command, ws apiKeyLoginWorkspace, p catwalk.Provider, force, keyStdin bool) error {
	id := string(p.ID)
	if !force && ws.Config() != nil {
		if pc, ok := ws.Config().Providers.Get(id); ok && pc.APIKey != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "%s is already configured. Use --force to replace its key.\n", p.Name)
			return nil
		}
	}
	var key string
	var err error
	if keyStdin {
		key, err = readLoginKey(cmd.InOrStdin())
	} else {
		in, ok := cmd.InOrStdin().(*os.File)
		if !ok || !term.IsTerminal(in.Fd()) {
			return fmt.Errorf("interactive key entry requires a terminal; use --api-key-stdin for piped input")
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Enter the API / plan key for %s (input hidden): ", p.Name)
		var data []byte
		data, err = term.ReadPassword(in.Fd())
		fmt.Fprintln(cmd.ErrOrStderr())
		if err == nil {
			key, err = readLoginKey(strings.NewReader(string(data)))
		}
	}
	if err != nil {
		return err
	}
	if err := ws.SetProviderAPIKey(config.ScopeGlobal, id, key); err != nil {
		return fmt.Errorf("saving key for %s: %w", p.Name, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Saved the key for %s. Select a model to start using it.\n", p.Name)
	return nil
}

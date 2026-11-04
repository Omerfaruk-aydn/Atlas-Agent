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
	minimaxoauth "github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/minimax"
	"github.com/spf13/cobra"
)

var loginAliases = map[string]string{
	"mimo":                 "xiaomi",
	"alibaba-coding-plan":  "alibaba-coding",
	"qwen-coding-plan":     "alibaba-coding",
	"alibaba-token-plan":   "alibaba-token-plan-sgp",
	"qwen-token-plan":      "alibaba-token-plan-sgp",
	"alibaba-token-team":   "alibaba-token-plan-team-sgp",
	"qiniu":                "qiniucloud",
	"qiniu-plan":           "qiniu-token-plan",
	"minimax-mplan":        "minimax-m-plan",
	"zai-coding-plan":      "zai",
	"xiaomi-token-plan":    "xiaomi-token-plan-sgp",
	"mimo-token-plan":      "xiaomi-token-plan-sgp",
	"opencode":             "opencode-zen",
	"minimax-token-plan":   "minimax-coding",
	"github":               "copilot",
	"github-copilot":       "copilot",
	"codex":                "chatgpt",
	"codex-ide":            "openai",
	"claude-plan":          "claude",
	"grok":                 "grok-web",
	"supergrok":            "grok-web",
	"codeium":              "windsurf",
	"jb-ai":                "jetbrains",
	"augmentcode":          "augment",
	"factory-ai":           "factory",
	"muse-code":            "muse",
	"kimi":                 "kimi-code",
	"kimi-plan":            "kimi-code",
	"kimi-membership":      "kimi-code",
	"minimax":              "minimax-plan",
	"minimax-global":       "minimax-plan",
	"minimax-subscription": "minimax-plan",
	"minimax-cn":           "minimax-plan-cn",
	"minimaxi":             "minimax-plan-cn",
}

var accountLoginProviders = []string{
	"copilot", "chatgpt", "antigravity", "claude", "muse", "kimi-code", "minimax-plan", "minimax-plan-cn", "grok-web",
}

var unavailableAccountProviders = []string{
	"windsurf", "jetbrains", "augment", "factory", "coderabbit", "zed",
}

// miniMaxAPILoginName is the login name of the generic MiniMax API-key
// provider (catalog id "minimax"), since the bare name "minimax" now means
// the subscription sign-in.
const miniMaxAPILoginName = "minimax-api"

// loginName returns the name users type to log in to catalog provider id.
func loginName(id string) string {
	if id == "minimax" {
		return miniMaxAPILoginName
	}
	return id
}

// applyMiniMaxRegion folds --region into the resolved provider id for the
// MiniMax subscription logins and reports the matching OAuth region.
func applyMiniMaxRegion(provider, regionFlag string) (string, minimaxoauth.Region, error) {
	isPlan := provider == "minimax-plan" || provider == "minimax-plan-cn"
	if regionFlag == "" {
		if provider == "minimax-plan-cn" {
			return provider, minimaxoauth.China, nil
		}
		return provider, minimaxoauth.Global, nil
	}
	if !isPlan {
		return provider, minimaxoauth.Global, fmt.Errorf("--region applies only to the MiniMax subscription login (minimax); %s does not use it", provider)
	}
	region, err := minimaxoauth.ParseRegion(regionFlag)
	if err != nil {
		return provider, region, err
	}
	if region == minimaxoauth.China {
		return "minimax-plan-cn", region, nil
	}
	return "minimax-plan", region, nil
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
	if id == miniMaxAPILoginName {
		id = "minimax"
	} else if isAccountLoginProvider(id) {
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
		id := loginName(string(p.ID))
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
		fmt.Fprintf(w, "%s\t%s\n", id, accountLoginNote(id))
	}
	for _, p := range embedded.GetAll() {
		if _, ok := apiKeyLoginProvider(loginName(string(p.ID))); ok {
			fmt.Fprintf(w, "%s\tAPI / plan key (%s)\n", loginName(string(p.ID)), p.Name)
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	notes := []string{
		"kimi = Kimi Code subscription login (no API key); kimi-coding = Kimi API key.",
		"minimax = MiniMax subscription login (no API key); minimax-coding = API key.",
		"minimax-api = generic MiniMax pay-per-token API key; minimax-token-plan = alias of minimax-coding (API key).",
	}
	_, err := fmt.Fprintln(out, "\nNote: "+strings.Join(notes, "\n      "))
	return err
}

// accountLoginNote describes how an account sign-in provider authenticates.
func accountLoginNote(id string) string {
	if id == "kimi-code" {
		return "Account sign-in (Kimi Code subscription, no API key; alias: kimi)"
	}
	switch id {
	case "minimax-plan":
		return "Account sign-in (MiniMax Token Plan subscription, no API key; alias: minimax)"
	case "minimax-plan-cn":
		return "Account sign-in (MiniMax Token Plan subscription, China; aliases: minimax-cn, minimax --region cn)"
	}
	return "Account sign-in"
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

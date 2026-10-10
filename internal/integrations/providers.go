package integrations

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
)

// ConfigBackend is the part of config.ConfigStore the adapters use.
type ConfigBackend interface {
	Config() *config.Config
	Resolver() config.VariableResolver
	KnownProviders() []catwalk.Provider
	LoadedPaths() []string
	SetConfigField(scope config.Scope, key string, value any) error
	RemoveConfigField(scope config.Scope, key string) error
	RefreshOAuthToken(ctx context.Context, scope config.Scope, providerID string) error
}

var _ ConfigBackend = (*config.ConfigStore)(nil)

func filesOf(b ConfigBackend) configFiles {
	ws := ""
	if cfg := b.Config(); cfg != nil && cfg.Options != nil && cfg.Options.DataDirectory != "" {
		ws = filepath.Join(cfg.Options.DataDirectory, "atlas.json")
	}
	return newConfigFiles(b.LoadedPaths(), config.GlobalConfigData(), ws)
}

func scopeFor(src Source) config.Scope {
	if src.Scope == "workspace" {
		return config.ScopeWorkspace
	}
	return config.ScopeGlobal
}

// ProviderAdapter lists model providers (API keys) and signed-in accounts
// (OAuth subscriptions).
type ProviderAdapter struct {
	Backend ConfigBackend
	Secrets *Secrets
	// Test validates credentials; defaults to the provider's own free
	// connection test.
	Test func(ctx context.Context, pc config.ProviderConfig, r config.VariableResolver) error
}

func providerID(id string) string {
	_, rest, _ := strings.Cut(id, ":")
	return rest
}

// unverifiable lists providers whose key cannot be validated without a
// billed request; their connection test only checks the key's format.
func unverifiable(pc config.ProviderConfig) bool {
	switch catwalk.InferenceProvider(pc.ID) {
	case catwalk.InferenceProviderMiniMax, catwalk.InferenceProviderMiniMaxChina, catwalk.InferenceProviderAlibabaSingapore:
		return true
	}
	return pc.Type == catwalk.TypeBedrock || pc.Type == catwalk.TypeVercel
}

func localEndpoint(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	h := u.Hostname()
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

func envName(template string) string {
	t := strings.TrimSpace(template)
	if !strings.HasPrefix(t, "$") || strings.HasPrefix(t, "$(") {
		return ""
	}
	return strings.Trim(strings.TrimPrefix(t, "$"), "{}")
}

// Items implements Adapter.
func (a *ProviderAdapter) Items(_ context.Context, fp Fingerprinter) []Item {
	cfg := a.Backend.Config()
	if cfg == nil {
		return nil
	}
	files := filesOf(a.Backend)
	known := map[string]catwalk.Provider{}
	for _, p := range a.Backend.KnownProviders() {
		known[string(p.ID)] = p
	}
	used := map[string][]string{}
	for typ, m := range cfg.Models {
		used[m.Provider] = append(used[m.Provider], string(typ))
	}
	var out []Item
	seen := map[string]bool{}
	if cfg.Providers != nil {
		for id, pc := range cfg.Providers.Seq2() {
			seen[id] = true
			out = append(out, a.item(id, pc, known[id], files, used[id], fp))
		}
	}
	for id, kp := range known {
		if seen[id] {
			continue
		}
		it := Item{
			ID:          "provider:" + id,
			Name:        kp.Name,
			Kind:        KindProvider,
			Description: i18n.M("Model provider from the built-in catalog."),
			Source:      Source{Origin: "built-in catalog", Scope: "system"},
			Auth:        AuthAPIKey,
			Status:      StatusNotConfigured,
			Actions:     []Action{ActionConfigure},
			Capabilities: []i18n.Message{
				i18n.M("%d models", len(kp.Models)),
			},
		}
		if env := envName(kp.APIKey); env != "" {
			it.Remedy = i18n.M("Set %s or add a key from /model.", env)
		} else {
			it.Remedy = i18n.M("Add credentials from /model.")
		}
		it.fingerprint = fp.Fingerprint("catalog", id)
		out = append(out, it)
	}
	return out
}

func (a *ProviderAdapter) item(id string, pc config.ProviderConfig, kp catwalk.Provider, files configFiles, roles []string, fp Fingerprinter) Item {
	name := pc.Name
	if name == "" {
		name = kp.Name
	}
	if name == "" {
		name = id
	}
	it := Item{Name: name, Auth: AuthAPIKey, Status: StatusUnchecked}
	if pc.OAuthToken != nil {
		it.ID, it.Kind, it.Auth = "account:"+id, KindAccount, AuthOAuth
		it.Description = i18n.M("Subscription account signed in with OAuth.")
		if pc.OAuthToken.Email != "" {
			it.Detail = i18n.M("Signed in as %s", pc.OAuthToken.Email)
		}
		if a.Secrets != nil {
			a.Secrets.Add(pc.OAuthToken.AccessToken, pc.OAuthToken.RefreshToken)
		}
	} else {
		it.ID, it.Kind = "provider:"+id, KindProvider
		it.Description = i18n.M("Model provider using the %s API.", cmpOr(string(pc.Type), "openai"))
	}
	if a.Secrets != nil {
		a.Secrets.Add(pc.APIKey)
		a.Secrets.Add(pc.APIKeys...)
	}
	it.Source = files.locate("providers." + escapeKey(id))
	if it.Source.Origin == "" {
		if env := envName(pc.APIKeyTemplate); env != "" {
			it.Source = Source{Origin: "environment", Scope: "user", Path: "$" + env}
			it.Auth = AuthEnv
		} else {
			it.Source = Source{Origin: "built-in", Scope: "system"}
		}
	}
	models := pc.Models
	if len(models) == 0 {
		models = kp.Models
	}
	it.Capabilities = append(it.Capabilities, i18n.M("%d models", len(models)))
	if slices.ContainsFunc(models, func(m catwalk.Model) bool { return m.CanReason }) {
		it.Capabilities = append(it.Capabilities, i18n.M("reasoning"))
	}
	if slices.ContainsFunc(models, func(m catwalk.Model) bool { return m.SupportsImages }) {
		it.Capabilities = append(it.Capabilities, i18n.M("image input"))
	}
	slices.Sort(roles)
	for _, r := range roles {
		it.Capabilities = append(it.Capabilities, i18n.M("selected for the %s model", r))
	}

	hasKey := strings.TrimSpace(pc.APIKey) != "" || len(pc.APIKeys) > 0
	switch {
	case pc.Disable:
		it.Status = StatusDisabled
	case pc.OAuthToken == nil && !hasKey && !localEndpoint(pc.BaseURL):
		it.Status = StatusNotConfigured
		if env := envName(pc.APIKeyTemplate); env != "" {
			it.Remedy = i18n.M("Set %s or add a key from /model.", env)
		} else {
			it.Remedy = i18n.M("Add credentials from /model.")
		}
	}
	if it.Status != StatusNotConfigured && !pc.Disable {
		it.Actions = append(it.Actions, ActionTest)
		if it.Kind == KindAccount {
			it.CheckNote = i18n.M("Free: the token is checked locally and refreshed only when it has expired; no model is called.")
		} else if unverifiable(pc) {
			it.CheckNote = i18n.M("Free: only the key's format can be checked for this provider; verifying it would need a billed request.")
		} else {
			it.CheckNote = i18n.M("Free: lists the provider's models; no model is called.")
		}
	}
	if it.Kind == KindAccount {
		it.Actions = append(it.Actions, ActionLogin)
	} else {
		it.Actions = append(it.Actions, ActionConfigure)
	}
	if it.Source.Editable {
		if pc.Disable {
			it.Actions = append(it.Actions, ActionEnable)
		} else {
			it.Actions = append(it.Actions, ActionDisable)
		}
		if hasKey || pc.OAuthToken != nil {
			it.Actions = append(it.Actions, ActionForgetCredentials)
		}
	} else if it.Source.Path != "" && it.Source.Origin != "environment" {
		it.Remedy = i18n.Join(it.Remedy, definedIn(it.Source.Path))
	}
	var tokenShape any
	if t := pc.OAuthToken; t != nil {
		tokenShape = []any{t.AccessToken, t.RefreshToken, t.ExpiresAt}
	}
	it.fingerprint = fp.Fingerprint("provider", id, pc.Disable, pc.APIKey, pc.APIKeys, pc.BaseURL, pc.Type, tokenShape)
	return it
}

func cmpOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func definedIn(path string) i18n.Message {
	return i18n.M("Defined in %s; edit that file to change it.", path)
}

// Check implements Adapter.
func (a *ProviderAdapter) Check(ctx context.Context, item Item) CheckResult {
	id := providerID(item.ID)
	cfg := a.Backend.Config()
	pc, ok := cfg.Providers.Get(id)
	if !ok {
		return CheckResult{Status: StatusNotConfigured, Remedy: i18n.M("Add credentials from /model.")}
	}
	if item.Kind == KindAccount && pc.OAuthToken != nil {
		return a.checkAccount(ctx, item, pc)
	}
	test := a.Test
	if test == nil {
		test = func(ctx context.Context, pc config.ProviderConfig, r config.VariableResolver) error {
			return pc.TestConnectionContext(ctx, r)
		}
	}
	err := test(ctx, pc, a.Backend.Resolver())
	if ctx.Err() != nil {
		return CheckResult{}
	}
	switch {
	case err == nil && unverifiable(pc):
		return CheckResult{Status: StatusLimited, Detail: i18n.M("Key format accepted; the provider offers no free way to verify it."), Remedy: i18n.M("If requests fail, replace the key from /model.")}
	case err == nil:
		return CheckResult{Status: StatusReady, Detail: i18n.M("Credentials accepted by the provider.")}
	}
	msg := err.Error()
	if strings.Contains(msg, "401") || strings.Contains(msg, "403") || strings.Contains(strings.ToLower(msg), "invalid api key") || strings.Contains(strings.ToLower(msg), "not a valid") {
		return CheckResult{Status: StatusNeedsLogin, Err: i18n.Raw(msg), Remedy: i18n.M("The provider rejected the credentials. Replace the key from /model.")}
	}
	return CheckResult{Status: StatusConnectionError, Err: i18n.Raw(msg), Remedy: i18n.M("Check the network connection and the provider's base URL, then test again.")}
}

func (a *ProviderAdapter) checkAccount(ctx context.Context, item Item, pc config.ProviderConfig) CheckResult {
	tok := pc.OAuthToken
	if !tok.IsExpired() {
		until := time.Unix(tok.ExpiresAt, 0)
		return CheckResult{Status: StatusReady, Detail: i18n.M("Signed in; the token is valid until %s.", until.Format("2006-01-02 15:04"))}
	}
	if tok.RefreshToken == "" {
		return CheckResult{Status: StatusNeedsLogin, Err: i18n.M("The token expired and cannot be refreshed."), Remedy: i18n.M("Sign in again from /model.")}
	}
	if err := a.Backend.RefreshOAuthToken(ctx, scopeFor(item.Source), pc.ID); err != nil {
		if ctx.Err() != nil {
			return CheckResult{}
		}
		return CheckResult{Status: StatusNeedsLogin, Err: i18n.M("The token could not be refreshed: %s", err.Error()), Remedy: i18n.M("Sign in again from /model.")}
	}
	fresh, _ := a.Backend.Config().Providers.Get(pc.ID)
	detail := i18n.M("Signed in; the token was refreshed.")
	if fresh.OAuthToken != nil && fresh.OAuthToken.ExpiresAt > 0 {
		detail = i18n.M("Signed in; the token was refreshed and is valid until %s.", time.Unix(fresh.OAuthToken.ExpiresAt, 0).Format("2006-01-02 15:04"))
	}
	return CheckResult{Status: StatusReady, Detail: detail}
}

// Do implements Adapter.
func (a *ProviderAdapter) Do(_ context.Context, item Item, action Action) (i18n.Message, error) {
	id := providerID(item.ID)
	base := "providers." + escapeKey(id)
	scope := scopeFor(item.Source)
	switch action {
	case ActionConfigure, ActionLogin:
		return i18n.M("Open /model and choose %s to set it up.", item.Name), nil
	case ActionDisable, ActionEnable:
		if !item.Source.Editable {
			return i18n.Message{}, errors.New("this provider is defined outside the files Atlas edits")
		}
		if err := a.Backend.SetConfigField(scope, base+".disable", action == ActionDisable); err != nil {
			return i18n.Message{}, fmt.Errorf("save %s: %w", item.Source.Path, err)
		}
		if action == ActionDisable {
			return i18n.M("Disabled in %s. Credentials were kept.", item.Source.Path), nil
		}
		return i18n.M("Enabled in %s.", item.Source.Path), nil
	case ActionForgetCredentials:
		if !item.Source.Editable {
			return i18n.Message{}, errors.New("this provider is defined outside the files Atlas edits")
		}
		var removed []string
		for _, k := range []string{"api_key", "api_keys", "oauth"} {
			if err := a.Backend.RemoveConfigField(scope, base+"."+k); err != nil {
				return i18n.Message{}, fmt.Errorf("remove %s from %s: %w", k, item.Source.Path, err)
			}
			removed = append(removed, k)
		}
		return i18n.M("Removed %s from %s. Other provider settings were kept.", strings.Join(removed, ", "), item.Source.Path), nil
	}
	return i18n.Message{}, ErrUnsupported
}

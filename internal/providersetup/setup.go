package providersetup

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
)

// maxKeyBytes bounds a pasted key, matching `login`.
const maxKeyBytes = 16 * 1024

// Redact removes the secret, and its URL-escaped and base64 forms, from s.
// Every string that leaves this package passes through it.
func Redact(s, secret string) string {
	if secret == "" {
		return s
	}
	for _, form := range []string{
		secret,
		url.QueryEscape(secret),
		url.PathEscape(secret),
		base64.StdEncoding.EncodeToString([]byte(secret)),
	} {
		if form != "" {
			s = strings.ReplaceAll(s, form, "[redacted]")
		}
	}
	return s
}

// NormalizeKey checks a pasted key: a single non-empty token.
func NormalizeKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" || strings.ContainsAny(key, "\r\n\t ") {
		return "", errors.New("enter a single non-empty API key (no spaces or line breaks)")
	}
	if len(key) > maxKeyBytes {
		return "", fmt.Errorf("the API key exceeds %d bytes", maxKeyBytes)
	}
	return key, nil
}

// KeyRequest describes the key being asked for, in words safe to show.
type KeyRequest struct {
	ProviderID string
	Host       string
	StoredIn   string
}

// KeyPrompter asks the user for the API key out of band (a masked input).
// It must never route the value through the model.
type KeyPrompter func(ctx context.Context, req KeyRequest) (string, error)

// Store is the part of the config store Setup needs.
type Store interface {
	Config() *config.Config
	Resolve(key string) (string, error)
	SetConfigFields(scope config.Scope, kv map[string]any) error
	ConfigFilePath(scope config.Scope) (string, error)
}

// Params is the input to Setup.
type Params struct {
	ID   string
	Name string
	// Type is the API type; empty selects openai-compat.
	Type    string
	BaseURL string
	// APIKeyEnv names an environment variable holding the key. The config
	// stores "$NAME" and the key itself is never written.
	APIKeyEnv string
	// Key is a key obtained by the caller itself (the CLI reading stdin).
	// The tool never sets it.
	Key string
	// DiscoverModels lists models from the endpoint before writing.
	DiscoverModels bool
	// Models are explicit model ids; they win over discovered ones.
	Models        []string
	Scope         config.Scope
	Overwrite     bool
	AllowInsecure bool
	Transport     http.RoundTripper
}

// Result describes what was written.
type Result struct {
	ID         string
	Name       string
	Type       catwalk.Type
	BaseURL    string
	Scope      string
	ConfigFile string
	Models     []catwalk.Model
	// KeyLocation says where the key lives, never what it is.
	KeyLocation string
	Notes       []string
	Replaced    bool
	Loaded      bool
}

// Prepared is the validated, key-free part of a request. The tool uses it
// to word its permission prompt before anything else happens.
type Prepared struct {
	ID      string
	Type    catwalk.Type
	URL     *url.URL
	EnvName string
}

// Prepare validates everything that needs no network and no key.
func Prepare(p Params) (*Prepared, error) {
	if err := ValidateID(p.ID); err != nil {
		return nil, err
	}
	typ, err := ParseType(p.Type)
	if err != nil {
		return nil, err
	}
	u, err := ValidateBaseURL(p.BaseURL, p.AllowInsecure)
	if err != nil {
		return nil, err
	}
	env := ""
	if strings.TrimSpace(p.APIKeyEnv) != "" {
		if err := ValidateEnvName(p.APIKeyEnv); err != nil {
			return nil, err
		}
		env = NormalizeEnvName(p.APIKeyEnv)
	}
	for _, m := range p.Models {
		if !validModelID(strings.TrimSpace(m)) || strings.TrimSpace(m) == "" {
			return nil, fmt.Errorf("invalid model id %q", m)
		}
	}
	if len(p.Models) > MaxModels {
		return nil, fmt.Errorf("too many models (%d, limit %d)", len(p.Models), MaxModels)
	}
	if !p.DiscoverModels && len(p.Models) == 0 {
		return nil, errors.New("discover_models is false and no models were given; list at least one model id in models")
	}
	return &Prepared{ID: p.ID, Type: typ, URL: u, EnvName: env}, nil
}

// Setup validates, probes and writes a custom provider. Nothing is written
// unless every step before the write succeeded.
func Setup(ctx context.Context, store Store, p Params, prompt KeyPrompter) (res *Result, err error) {
	var key string
	defer func() {
		// Last line of defense: whatever produced an error, the key does
		// not leave through it.
		if err != nil {
			err = errors.New(Redact(err.Error(), key))
		}
	}()

	prep, err := Prepare(p)
	if err != nil {
		return nil, err
	}
	if p.Scope == config.ScopeWorkspace && prep.EnvName == "" {
		return nil, errors.New("a workspace-scoped provider is stored inside the project, where a saved key could be committed by accident; give api_key_env (an environment variable name) or use the global scope")
	}

	existing, exists := store.Config().Providers.Get(p.ID)
	if exists && !p.Overwrite {
		return nil, fmt.Errorf("a provider named %q already exists (%d models); choose another id, or set overwrite to replace it", p.ID, len(existing.Models))
	}

	scopeName := "global"
	if p.Scope == config.ScopeWorkspace {
		scopeName = "workspace"
	}
	configFile, err := store.ConfigFilePath(p.Scope)
	if err != nil {
		return nil, err
	}

	// Where the key comes from.
	apiKeyValue := ""
	keyLocation := ""
	switch {
	case p.Key != "":
		key, err = NormalizeKey(p.Key)
		if err != nil {
			return nil, err
		}
		apiKeyValue = key
		keyLocation = "saved in " + configFile
	case prep.EnvName != "":
		if v, rerr := store.Resolve("$" + prep.EnvName); rerr == nil && v != "" {
			key = v
			apiKeyValue = "$" + prep.EnvName
			keyLocation = fmt.Sprintf("environment variable %s (the config stores only the name; the variable must be set whenever Atlas runs)", prep.EnvName)
			break
		}
		fallthrough
	default:
		if prompt == nil {
			if prep.EnvName != "" {
				return nil, fmt.Errorf("environment variable %s is not set and there is no way to ask you for the key here; set %s and run again", prep.EnvName, prep.EnvName)
			}
			return nil, errors.New("no API key: give api_key_env (--api-key-env on the command line), the name of an environment variable that holds the key, and run again; this mode cannot prompt")
		}
		if p.Scope == config.ScopeWorkspace {
			return nil, fmt.Errorf("environment variable %s is not set; a key cannot be saved into a workspace config file, set the variable or use the global scope", prep.EnvName)
		}
		entered, perr := prompt(ctx, KeyRequest{ProviderID: p.ID, Host: prep.URL.Host, StoredIn: configFile})
		if perr != nil {
			return nil, fmt.Errorf("no API key was entered: %w", perr)
		}
		key, err = NormalizeKey(entered)
		if err != nil {
			return nil, err
		}
		apiKeyValue = key
		keyLocation = "saved in " + configFile + " (file mode 0600)"
	}

	baseURL := prep.URL.String()
	var models []catwalk.Model
	var notes []string

	if p.DiscoverModels {
		pr, perr := Probe(ctx, p.Transport, prep.Type, baseURL, key)
		if perr != nil {
			return nil, fmt.Errorf("nothing was written. %w", perr)
		}
		baseURL = pr.BaseURL
		models = pr.Models
		if pr.Note != "" {
			notes = append(notes, pr.Note)
		}
		if len(models) == 0 {
			notes = append(notes, "The endpoint answered but lists no models.")
		}
	} else {
		notes = append(notes, "Model discovery was skipped (discover_models is false), so the endpoint and key were not checked. Run `atlas-agent provider test "+p.ID+"` to check them.")
	}

	models = mergeModels(p.Models, models)
	if len(models) == 0 {
		return nil, errors.New("nothing was written: no models were found and none were given; pass models explicitly")
	}
	if len(models) > MaxModels {
		models = models[:MaxModels]
		notes = append(notes, fmt.Sprintf("Only the first %d models were kept.", MaxModels))
	}

	entry := map[string]any{
		"name":     cmpOr(p.Name, p.ID),
		"type":     string(prep.Type),
		"base_url": baseURL,
		"api_key":  apiKeyValue,
		"models":   models,
	}
	if !p.DiscoverModels {
		entry["discover_models"] = false
	}
	if err := store.SetConfigFields(p.Scope, map[string]any{"providers." + p.ID: entry}); err != nil {
		return nil, fmt.Errorf("nothing was written: the configuration was rejected: %w", err)
	}

	_, loaded := store.Config().Providers.Get(p.ID)
	if !loaded {
		notes = append(notes, "The file was written but the provider is not in the loaded configuration; run `atlas-agent config paths` and check for warnings in the log.")
	}
	slog.Info("Custom provider added", "provider", p.ID, "type", string(prep.Type), "host", prep.URL.Host, "models", len(models), "scope", scopeName)

	return &Result{
		ID:          p.ID,
		Name:        cmpOr(p.Name, p.ID),
		Type:        prep.Type,
		BaseURL:     baseURL,
		Scope:       scopeName,
		ConfigFile:  configFile,
		Models:      models,
		KeyLocation: keyLocation,
		Notes:       notes,
		Replaced:    exists,
		Loaded:      loaded,
	}, nil
}

// mergeModels returns the explicit ids first, then discovered models not
// already named; an explicit id keeps the discovered metadata if any.
func mergeModels(explicit []string, discovered []catwalk.Model) []catwalk.Model {
	byID := make(map[string]catwalk.Model, len(discovered))
	for _, m := range discovered {
		byID[m.ID] = m
	}
	var out []catwalk.Model
	seen := map[string]bool{}
	for _, raw := range explicit {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if m, ok := byID[id]; ok {
			out = append(out, m)
		} else {
			out = append(out, catwalk.Model{ID: id, Name: id})
		}
	}
	for _, m := range discovered {
		if !seen[m.ID] {
			seen[m.ID] = true
			out = append(out, m)
		}
	}
	return out
}

func cmpOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

// Summary renders the one-screen report. It names where the key lives and
// never what it is.
func (r *Result) Summary() string {
	var b strings.Builder
	verb := "Added"
	if r.Replaced {
		verb = "Replaced"
	}
	fmt.Fprintf(&b, "%s provider %q (%s).\n", verb, r.ID, r.Name)
	fmt.Fprintf(&b, "  type:     %s\n", r.Type)
	fmt.Fprintf(&b, "  base_url: %s\n", r.BaseURL)
	fmt.Fprintf(&b, "  scope:    %s (%s)\n", r.Scope, r.ConfigFile)
	fmt.Fprintf(&b, "  api key:  %s\n", r.KeyLocation)
	fmt.Fprintf(&b, "  models:   %d", len(r.Models))
	const show = 10
	if len(r.Models) > 0 {
		ids := make([]string, 0, show)
		for i, m := range r.Models {
			if i == show {
				break
			}
			ids = append(ids, m.ID)
		}
		fmt.Fprintf(&b, ": %s", strings.Join(ids, ", "))
		if len(r.Models) > show {
			fmt.Fprintf(&b, ", ... (%d more)", len(r.Models)-show)
		}
	}
	b.WriteString("\n")
	for _, n := range r.Notes {
		fmt.Fprintf(&b, "Note: %s\n", n)
	}
	if len(r.Models) > 0 {
		fmt.Fprintf(&b, "To use it, pick %s/%s in /model, or switch the session model (atlas_config set_model).\n", r.ID, r.Models[0].ID)
	}
	return b.String()
}

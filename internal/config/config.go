package config

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/antigravity"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/copilot"
	"github.com/invopop/jsonschema"
)

const (
	appName = "atlas"
	// legacyAppName is what this program was called before the rebrand. Every
	// on-disk name it produced is still read; see legacy.go for the rule.
	legacyAppName        = "Atlas-Agent"
	defaultDataDirectory = "." + appName
	defaultInitializeAs  = "AGENTS.md"
)

var defaultContextPaths = []string{
	".github/copilot-instructions.md",
	".cursorrules",
	".cursor/rules/",
	"CLAUDE.md",
	"CLAUDE.local.md",
	"GEMINI.md",
	"gemini.md",
	"atlas.md",
	"atlas.local.md",
	"Atlas.md",
	"Atlas.local.md",
	"ATLAS.md",
	"ATLAS.local.md",
	// Kept so a project that already carries a context file under the old
	// name goes on being read.
	"Atlas-Agent.md",
	"Atlas-Agent.local.md",
	"Atlas-Agent.md",
	"Atlas-Agent.local.md",
	"ATLAS-AGENT.md",
	"ATLAS-AGENT.local.md",
	"AGENTS.md",
	"agents.md",
	"Agents.md",
}

type SelectedModelType string

// String returns the string representation of the [SelectedModelType].
func (s SelectedModelType) String() string {
	return string(s)
}

const (
	SelectedModelTypeLarge SelectedModelType = "large"
	SelectedModelTypeSmall SelectedModelType = "small"
)

// Valid reports whether s names a model type an agent can be routed to.
func (s SelectedModelType) Valid() bool {
	return s == SelectedModelTypeLarge || s == SelectedModelTypeSmall
}

const (
	AgentCoder string = "coder"
	AgentTask  string = "task"
)

type SelectedModel struct {
	// The model id as used by the provider API.
	// Required.
	Model string `json:"model" jsonschema:"required,description=The model ID as used by the provider API,example=gpt-4o"`
	// The model provider, same as the key/id used in the providers config.
	// Required.
	Provider string `json:"provider" jsonschema:"required,description=The model provider ID that matches a key in the providers config,example=openai"`

	// Only used by models that use the openai provider and need this set.
	ReasoningEffort string `json:"reasoning_effort,omitempty" jsonschema:"description=Reasoning effort level for OpenAI models that support it,enum=low,enum=medium,enum=high"`

	// Used by anthropic models that can reason to indicate if the model should think.
	Think bool `json:"think,omitempty" jsonschema:"description=Enable thinking mode for Anthropic models that support reasoning"`

	// Overrides the default model configuration.
	MaxTokens        int64    `json:"max_tokens,omitempty" jsonschema:"description=Maximum number of tokens for model responses,maximum=200000,example=4096"`
	Temperature      *float64 `json:"temperature,omitempty" jsonschema:"description=Sampling temperature,minimum=0,maximum=1,example=0.7"`
	TopP             *float64 `json:"top_p,omitempty" jsonschema:"description=Top-p (nucleus) sampling parameter,minimum=0,maximum=1,example=0.9"`
	TopK             *int64   `json:"top_k,omitempty" jsonschema:"description=Top-k sampling parameter"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty" jsonschema:"description=Frequency penalty to reduce repetition"`
	PresencePenalty  *float64 `json:"presence_penalty,omitempty" jsonschema:"description=Presence penalty to increase topic diversity"`

	// Override provider specific options.
	ProviderOptions map[string]any `json:"provider_options,omitempty" jsonschema:"description=Additional provider-specific options for the model"`
}

type ProviderConfig struct {
	// The provider's id.
	ID string `json:"id,omitempty" jsonschema:"description=Unique identifier for the provider,example=openai"`
	// The provider's name, used for display purposes.
	Name string `json:"name,omitempty" jsonschema:"description=Human-readable name for the provider,example=OpenAI"`
	// The provider's API endpoint.
	BaseURL string `json:"base_url,omitempty" jsonschema:"description=Base URL for the provider's API,format=uri,example=https://api.openai.com/v1"`
	// The provider type, e.g. "openai", "anthropic", etc. if empty it defaults to openai.
	Type catwalk.Type `json:"type,omitempty" jsonschema:"description=Provider type that determines the API format,default=openai"`
	// The provider's API key.
	APIKey string `json:"api_key,omitempty" jsonschema:"description=API key for authentication with the provider,example=$OPENAI_API_KEY"`
	// APIKeys lists additional API keys for this same provider -- separate
	// accounts or subscriptions -- rotated in round-robin order across
	// separate sessions/model builds (session affinity: a running session
	// keeps whichever key it started with). When one key's quota runs out
	// mid-turn with no further model fallback configured, the next
	// session or turn against this provider tries the next key in line
	// instead of the one that just got rate-limited.
	APIKeys []string `json:"api_keys,omitempty" jsonschema:"description=Additional API keys for this provider (separate accounts/subscriptions)\\, rotated round-robin across sessions,example=[\"$OPENAI_API_KEY_2\"\\,\"$OPENAI_API_KEY_3\"]"`
	// The original API key template before resolution (for re-resolution on auth errors).
	APIKeyTemplate string `json:"-"`
	// OAuthToken for providers that use OAuth2 authentication.
	OAuthToken *oauth.Token `json:"oauth,omitempty" jsonschema:"description=OAuth2 token for authentication with the provider"`
	// Marks the provider as disabled.
	Disable bool `json:"disable,omitempty" jsonschema:"description=Whether this provider is disabled,default=false"`

	// Custom system prompt prefix.
	SystemPromptPrefix string `json:"system_prompt_prefix,omitempty" jsonschema:"description=Custom prefix to add to system prompts for this provider"`

	// Extra headers to send with each request to the provider. Values
	// run through shell expansion at config-load time, so $VAR and
	// $(cmd) work the same way they do in MCP headers. A header whose
	// value resolves to the empty string (unset bare $VAR under
	// lenient nounset, $(echo), or literal "") is omitted from the
	// outgoing request rather than sent as "Header:".
	ExtraHeaders map[string]string `json:"extra_headers,omitempty" jsonschema:"description=Additional HTTP headers to send with requests"`
	// ExtraBody is merged verbatim into OpenAI-compatible request
	// bodies. String values are NOT shell-expanded: this is a plain
	// JSON passthrough so that arbitrary provider-extension fields
	// (numbers, nested objects, booleans) round-trip without a
	// recursive walker guessing at intent. If you need an env-var-
	// driven value at request time, put it in extra_headers, or in
	// the provider's top-level api_key / base_url, all of which do
	// expand.
	ExtraBody map[string]any `json:"extra_body,omitempty" jsonschema:"description=Additional fields to include in request bodies\\, only works with openai-compatible providers"`

	ProviderOptions map[string]any `json:"provider_options,omitempty" jsonschema:"description=Additional provider-specific options for this provider"`

	// Used to pass extra parameters to the provider.
	ExtraParams map[string]string `json:"-"`

	// AWSAuthRefresh is a shell command run when Bedrock returns a
	// credential error. Output is discarded to avoid corrupting the TUI.
	AWSAuthRefresh string `json:"aws_auth_refresh,omitempty" jsonschema:"description=Shell command to run when AWS credentials expire (Bedrock only)."`

	// Skip cost accumulation for this provider when using subscription or flat rate billing.
	FlatRate bool `json:"flat_rate,omitempty" jsonschema:"description=Flat-rate mode for this provider"`

	// AutoDiscoverModels controls model discovery via /v1/models endpoint.
	// When Models is empty and this is nil or true, Atlas-Agent auto-discovers
	// models. When true and Models is non-empty, discovered models are
	// merged in (user-specified models take precedence). When false,
	// only explicitly listed models are used.
	AutoDiscoverModels *bool `json:"discover_models,omitempty" jsonschema:"description=Auto-discover models from /v1/models endpoint. When true with existing models they are merged (yours win),default=true"`

	// The provider models
	Models []catwalk.Model `json:"models,omitempty" jsonschema:"description=List of models available from this provider"`
}

// ToProvider converts the [ProviderConfig] to a [catwalk.Provider].
func (c *ProviderConfig) ToProvider() catwalk.Provider {
	// Convert config provider to provider.Provider format
	provider := catwalk.Provider{
		Name:   c.Name,
		ID:     catwalk.InferenceProvider(c.ID),
		Models: make([]catwalk.Model, len(c.Models)),
	}

	// Convert models
	for i, model := range c.Models {
		provider.Models[i] = catwalk.Model{
			ID:                     model.ID,
			Name:                   model.Name,
			CostPer1MIn:            model.CostPer1MIn,
			CostPer1MOut:           model.CostPer1MOut,
			CostPer1MInCached:      model.CostPer1MInCached,
			CostPer1MOutCached:     model.CostPer1MOutCached,
			ContextWindow:          model.ContextWindow,
			DefaultMaxTokens:       model.DefaultMaxTokens,
			CanReason:              model.CanReason,
			ReasoningLevels:        model.ReasoningLevels,
			DefaultReasoningEffort: model.DefaultReasoningEffort,
			SupportsImages:         model.SupportsImages,
		}
	}

	return provider
}

func (c *ProviderConfig) SetupGitHubCopilot() {
	maps.Copy(c.ExtraHeaders, copilot.Headers())
}

// SetupChatGPT configures the extra headers a ChatGPT (Codex) OAuth
// session needs against the chatgpt.com backend: the signed-in account id
// so requests are billed against that subscription, and an originator tag
// identifying this client the way the official Codex CLI does.
func (c *ProviderConfig) SetupChatGPT() {
	if c.OAuthToken == nil {
		return
	}
	if c.ExtraHeaders == nil {
		c.ExtraHeaders = map[string]string{}
	}
	if c.OAuthToken.AccountID != "" {
		c.ExtraHeaders["chatgpt-account-id"] = c.OAuthToken.AccountID
	}
	c.ExtraHeaders["originator"] = "codex_cli_rs"
}

// SetupAntigravity records the Cloud project id a signed-in Antigravity
// (Google AI Pro/Ultra) account was assigned or provisioned with, and the
// Client-Metadata/User-Agent headers its Cloud Code backend expects on
// every model request.
func (c *ProviderConfig) SetupAntigravity() {
	if c.OAuthToken == nil {
		return
	}
	if c.ExtraParams == nil {
		c.ExtraParams = map[string]string{}
	}
	c.ExtraParams["project"] = c.OAuthToken.AccountID
	if c.ExtraHeaders == nil {
		c.ExtraHeaders = map[string]string{}
	}
	maps.Copy(c.ExtraHeaders, antigravity.RequestHeaders())
}

// SetupClaude records the account id a signed-in claude.ai
// (Claude Pro/Max/Team) session was authorized against, and the
// headers the claude.ai console backend expects on every model
// request. The actual request envelope is filled in by the
// claude fantasy.Provider (see internal/deps/atlas-llm/providers/
// claude for the TODO list).
func (c *ProviderConfig) SetupClaude() {
	if c.OAuthToken == nil {
		return
	}
	if c.ExtraParams == nil {
		c.ExtraParams = map[string]string{}
	}
	if c.OAuthToken.AccountID != "" {
		c.ExtraParams["account_id"] = c.OAuthToken.AccountID
	}
	if c.ExtraHeaders == nil {
		c.ExtraHeaders = map[string]string{}
	}
	// PlanType carries the subscription tier (e.g. "pro", "max",
	// "team") that claude.ai's billing API expects. Surfaced as a
	// header so a future claude envelope can route accordingly.
	if c.OAuthToken.PlanType != "" {
		c.ExtraHeaders["x-claude-plan"] = c.OAuthToken.PlanType
	}
}

// SetupGrokWeb records the account id a signed-in grok.com (xAI
// SuperGrok) session was authorized against. The actual request
// envelope is filled in by the grokweb fantasy.Provider (see
// internal/deps/atlas-llm/providers/grokweb for the TODO list).
func (c *ProviderConfig) SetupGrokWeb() {
	if c.OAuthToken == nil {
		return
	}
	if c.ExtraParams == nil {
		c.ExtraParams = map[string]string{}
	}
	if c.OAuthToken.AccountID != "" {
		c.ExtraParams["account_id"] = c.OAuthToken.AccountID
	}
	if c.ExtraHeaders == nil {
		c.ExtraHeaders = map[string]string{}
	}
	if c.OAuthToken.PlanType != "" {
		c.ExtraHeaders["x-grok-plan"] = c.OAuthToken.PlanType
	}
}

// SetupWindsurf records the account id a signed-in Windsurf
// (Codeium Pro/Teams) session was authorized against. The actual
// request envelope is filled in by the windsurf fantasy.Provider.
func (c *ProviderConfig) SetupWindsurf() {
	if c.OAuthToken == nil {
		return
	}
	if c.ExtraParams == nil {
		c.ExtraParams = map[string]string{}
	}
	if c.OAuthToken.AccountID != "" {
		c.ExtraParams["account_id"] = c.OAuthToken.AccountID
	}
}

// SetupJetBrains records the subscription tier a signed-in JetBrains
// (Pro/Ultimate) account carries, surfaced as a header so the
// jetbrains fantasy.Provider can route to the right model tier.
func (c *ProviderConfig) SetupJetBrains() {
	if c.OAuthToken == nil {
		return
	}
	if c.ExtraHeaders == nil {
		c.ExtraHeaders = map[string]string{}
	}
	if c.OAuthToken.PlanType != "" {
		c.ExtraHeaders["x-jetbrains-plan"] = c.OAuthToken.PlanType
	}
}

// SetupAugment records the account id a signed-in Augment Code
// session was authorized against. The actual request envelope is a
// TODO in the augment fantasy.Provider.
func (c *ProviderConfig) SetupAugment() {
	if c.OAuthToken == nil {
		return
	}
	if c.ExtraParams == nil {
		c.ExtraParams = map[string]string{}
	}
	if c.OAuthToken.AccountID != "" {
		c.ExtraParams["account_id"] = c.OAuthToken.AccountID
	}
}

// SetupFactory records the account id a signed-in Factory AI
// Droids session was authorized against. The actual request
// envelope is a TODO in the factory fantasy.Provider.
func (c *ProviderConfig) SetupFactory() {
	if c.OAuthToken == nil {
		return
	}
	if c.ExtraParams == nil {
		c.ExtraParams = map[string]string{}
	}
	if c.OAuthToken.AccountID != "" {
		c.ExtraParams["account_id"] = c.OAuthToken.AccountID
	}
}

// SetupCodeRabbit records the account id a signed-in CodeRabbit
// session was authorized against. The actual request envelope is a
// TODO in the coderabbit fantasy.Provider.
func (c *ProviderConfig) SetupCodeRabbit() {
	if c.OAuthToken == nil {
		return
	}
	if c.ExtraParams == nil {
		c.ExtraParams = map[string]string{}
	}
	if c.OAuthToken.AccountID != "" {
		c.ExtraParams["account_id"] = c.OAuthToken.AccountID
	}
}

// SetupZed records the account id a signed-in Zed Pro session was
// authorized against. The actual request envelope is a TODO in the
// zed fantasy.Provider.
func (c *ProviderConfig) SetupZed() {
	if c.OAuthToken == nil {
		return
	}
	if c.ExtraParams == nil {
		c.ExtraParams = map[string]string{}
	}
	if c.OAuthToken.AccountID != "" {
		c.ExtraParams["account_id"] = c.OAuthToken.AccountID
	}
}

type MCPType string

const (
	MCPStdio MCPType = "stdio"
	MCPSSE   MCPType = "sse"
	MCPHttp  MCPType = "http"
)

type MCPConfig struct {
	Command       string            `json:"command,omitempty" jsonschema:"description=Command to execute for stdio MCP servers,example=npx"`
	Env           map[string]string `json:"env,omitempty" jsonschema:"description=Environment variables to set for the MCP server"`
	Args          []string          `json:"args,omitempty" jsonschema:"description=Arguments to pass to the MCP server command"`
	Type          MCPType           `json:"type" jsonschema:"required,description=Type of MCP connection,enum=stdio,enum=sse,enum=http,default=stdio"`
	URL           string            `json:"url,omitempty" jsonschema:"description=URL for HTTP or SSE MCP servers,format=uri,example=http://localhost:3000/mcp"`
	Disabled      bool              `json:"disabled,omitempty" jsonschema:"description=Whether this MCP server is disabled,default=false"`
	DisabledTools []string          `json:"disabled_tools,omitempty" jsonschema:"description=List of tools from this MCP server to disable,example=get-library-doc"`
	EnabledTools  []string          `json:"enabled_tools,omitempty" jsonschema:"description=Allow list of tools from this MCP server,example=get-library-doc"`
	Timeout       int               `json:"timeout,omitempty" jsonschema:"description=Timeout in seconds for MCP server connections,default=10,example=30,example=60,example=120"`

	// Sessionless marks a server that does not maintain an MCP session (it
	// never issues a Mcp-Session-Id). When true, Atlas-Agent omits the
	// tools/prompts/resources list-changed handlers: the go-sdk opens a
	// SEP-2575 "subscriptions/listen" stream whenever any of those handlers
	// is set, and sessionless streamable-HTTP servers (e.g. GitHub MCP)
	// answer that POST with 404 ("session not found"), which the SDK treats
	// as fatal. The cost is no live list-changed notifications from this
	// server.
	//
	// When nil, Atlas-Agent auto-detects a set of known sessionless servers (see
	// IsSessionless); set it explicitly to override that detection.
	Sessionless *bool `json:"sessionless,omitempty" jsonschema:"description=Mark a sessionless MCP server (no Mcp-Session-Id) so Atlas-Agent skips the subscriptions/listen stream it would otherwise reject. Leave unset to auto-detect known sessionless servers (e.g. GitHub MCP),default=false"`

	// Headers are HTTP headers for HTTP/SSE MCP servers. Values run
	// through shell expansion at MCP startup, so $VAR and $(cmd)
	// work. A header whose value resolves to the empty string (unset
	// bare $VAR under lenient nounset, $(echo), or literal "") is
	// omitted from the outgoing request rather than sent as
	// "Header:".
	Headers map[string]string `json:"headers,omitempty" jsonschema:"description=HTTP headers for HTTP/SSE MCP servers"`

	// OAuth enables the MCP OAuth 2.1 authorization flow for HTTP
	// transport servers. When true, the client uses dynamic client
	// registration and opens a browser for the user to authorize.
	// Tokens are persisted automatically. Only supported for type=http.
	OAuth bool `json:"oauth,omitempty" jsonschema:"description=Enable OAuth 2.1 authorization flow for this MCP server (HTTP transport only),default=false"`

	// OAuthClientID is an optional pre-registered OAuth client ID. Set
	// it for servers that do not support dynamic client registration
	// (e.g. GitHub, Slack) and instead issue client credentials when you
	// register an OAuth app. Values run through shell expansion, so
	// $VAR and $(cmd) work.
	OAuthClientID string `json:"oauth_client_id,omitempty" jsonschema:"description=Pre-registered OAuth client ID for servers without dynamic client registration"`

	// OAuthClientSecret is the optional secret paired with
	// OAuthClientID for confidential clients. Values run through shell
	// expansion, so $VAR and $(cmd) work.
	OAuthClientSecret string `json:"oauth_client_secret,omitempty" jsonschema:"description=Pre-registered OAuth client secret paired with oauth_client_id"`

	// OAuthCallbackPort pins the localhost port used for the OAuth
	// redirect listener. Set this when the OAuth provider requires an
	// exact-match callback URL (e.g. GitHub OAuth Apps). When omitted,
	// Atlas-Agent picks the first free port from its default range.
	OAuthCallbackPort int `json:"oauth_callback_port,omitempty" jsonschema:"description=Fixed localhost port for the OAuth callback, required by providers that enforce exact-match redirect URIs"`

	// OAuthToken is the persisted OAuth token for this server. It is
	// managed internally and stored in the global data config.
	OAuthToken *oauth.Token `json:"oauth_token,omitempty" jsonschema:"-"`
}

// isOrphanedToken reports whether this entry is a leftover OAuth token
// with no real server config.
func (m MCPConfig) isOrphanedToken() bool {
	return m.Type == "" && m.Command == "" && m.URL == "" && m.OAuthToken != nil
}

type LSPConfig struct {
	Disabled    bool              `json:"disabled,omitempty" jsonschema:"description=Whether this LSP server is disabled,default=false"`
	Command     string            `json:"command,omitempty" jsonschema:"description=Command to execute for the LSP server,example=gopls"`
	Args        []string          `json:"args,omitempty" jsonschema:"description=Arguments to pass to the LSP server command"`
	Env         map[string]string `json:"env,omitempty" jsonschema:"description=Environment variables to set to the LSP server command"`
	FileTypes   []string          `json:"filetypes,omitempty" jsonschema:"description=File types this LSP server handles,example=go,example=mod,example=rs,example=c,example=js,example=ts"`
	RootMarkers []string          `json:"root_markers,omitempty" jsonschema:"description=Files or directories that indicate the project root,example=go.mod,example=package.json,example=Cargo.toml"`
	InitOptions map[string]any    `json:"init_options,omitempty" jsonschema:"description=Initialization options passed to the LSP server during initialize request"`
	Options     map[string]any    `json:"options,omitempty" jsonschema:"description=LSP server-specific settings passed during initialization"`
	Timeout     int               `json:"timeout,omitempty" jsonschema:"description=Timeout in seconds for LSP server initialization,default=30,example=60,example=120"`
}

type TUIOptions struct {
	CompactMode bool   `json:"compact_mode,omitempty" jsonschema:"description=Enable compact mode for the TUI interface,default=false"`
	DiffMode    string `json:"diff_mode,omitempty" jsonschema:"description=Diff mode for the TUI interface,enum=unified,enum=split"`
	// Here we can add themes later or any TUI related options
	//

	Completions Completions `json:"completions,omitzero" jsonschema:"description=Completions UI options"`
	Transparent *bool       `json:"transparent,omitempty" jsonschema:"description=Enable transparent background for the TUI interface,default=false"`
	Scrollbar   string      `json:"scrollbar,omitempty" jsonschema:"description=Chat scrollbar visibility,enum=default,enum=always,enum=never,default=default"`
	BoxCorners  string      `json:"box_corners,omitempty" jsonschema:"description=Corner style for framed surfaces such as the composer and the landing cards. Leave unset to let Atlas-Agent pick based on the terminal it is running in. Which styles render depends on the terminal font: a font missing a glyph draws a replacement box in its place. 'rounded' needs U+256D-U+2570 and 'arc' needs U+25DC-U+25DF\\, both of which some fonts omit; 'sharp'\\, 'bold' and 'double' come from the widely-supported Box Drawing block.,enum=sharp,enum=rounded,enum=arc,enum=bold,enum=double,enum=bevel"`
	ExitBanner  ExitBanner  `json:"exit_banner,omitempty" jsonschema:"description=Exit banner style after quitting Atlas-Agent,enum=default,enum=compact,enum=none,default=default"`
}

// IsTransparent reports whether the TUI draws a transparent background. The
// nil receiver and the unset pointer both mean opaque, so callers can ask
// without unwrapping either.
func (t *TUIOptions) IsTransparent() bool {
	return t != nil && t.Transparent != nil && *t.Transparent
}

// Completions defines options for the completions UI.
type Completions struct {
	MaxDepth *int `json:"max_depth,omitempty" jsonschema:"description=Maximum depth for the ls tool,default=0,example=10"`
	MaxItems *int `json:"max_items,omitempty" jsonschema:"description=Maximum number of items to return for the ls tool,default=1000,example=100"`
}

// Limits returns the configured completion limits. Zero means the user has not
// pinned that limit, and callers fall back to their own built-in cap.
func (c Completions) Limits() (depth, items int) {
	return ptrValOr(c.MaxDepth, 0), ptrValOr(c.MaxItems, 0)
}

// Diff mode options.
const (
	DiffModeUnified = "unified" // Inline unified diffs
	DiffModeSplit   = "split"   // Side-by-side diffs
)

// Scrollbar visibility options.
const (
	ScrollbarDefault = "default" // Auto-hide after 2 seconds
	ScrollbarAlways  = "always"  // Always show when content exceeds viewport
	ScrollbarNever   = "never"   // Never show scrollbar
)

// ExitBanner selects what ATLAS-AGENT prints after the TUI exits.
type ExitBanner string

const (
	// ExitBannerDefault renders the full ASCII art logo with padding. It is
	// also what the zero value and any unrecognized value fall back to.
	ExitBannerDefault ExitBanner = "default"
	// ExitBannerCompact renders only the session and resume lines, with no
	// logo and no padding. With no active session it renders nothing at all,
	// so Atlas-Agent exits silently.
	ExitBannerCompact ExitBanner = "compact"
	// ExitBannerNone renders nothing.
	ExitBannerNone ExitBanner = "none"
)

type Permissions struct {
	AllowedTools []string `json:"allowed_tools,omitempty" jsonschema:"description=List of tools that don't require permission prompts,example=bash,example=view"`
}

type TrailerStyle string

const (
	TrailerStyleNone         TrailerStyle = "none"
	TrailerStyleCoAuthoredBy TrailerStyle = "co-authored-by"
	TrailerStyleAssistedBy   TrailerStyle = "assisted-by"
)

type Attribution struct {
	TrailerStyle  TrailerStyle `json:"trailer_style,omitempty" jsonschema:"description=Style of attribution trailer to add to commits,enum=none,enum=co-authored-by,enum=assisted-by,default=assisted-by"`
	CoAuthoredBy  *bool        `json:"co_authored_by,omitempty" jsonschema:"description=Deprecated: use trailer_style instead"`
	GeneratedWith bool         `json:"generated_with,omitempty" jsonschema:"description=Add Generated with Atlas-Agent line to commit messages and issues and PRs,default=true"`
}

// JSONSchemaExtend marks the co_authored_by field as deprecated in the schema.
func (Attribution) JSONSchemaExtend(schema *jsonschema.Schema) {
	if schema.Properties != nil {
		if prop, ok := schema.Properties.Get("co_authored_by"); ok {
			prop.Deprecated = true
		}
	}
}

type Options struct {
	ContextPaths         []string    `json:"context_paths,omitempty" jsonschema:"description=Paths to files containing context information for the AI,example=.cursorrules,example=ATLAS-AGENT.md"`
	GlobalContextPaths   []string    `json:"global_context_paths,omitempty" jsonschema:"description=Paths to files containing global context information for the AI,default=~/.config/Atlas-Agent/ATLAS-AGENT.md,default=~/.config/AGENTS.md"`
	SkillsPaths          []string    `json:"skills_paths,omitempty" jsonschema:"description=Paths to directories containing Agent Skills (folders with SKILL.md files),example=~/.config/atlas/skills,example=./skills"`
	SubagentsPaths       []string    `json:"subagents_paths,omitempty" jsonschema:"description=Paths to directories containing subagent definitions (name.md files with a model role),example=~/.config/atlas/agents,example=./.atlas/agents"`
	TUI                  *TUIOptions `json:"tui,omitempty" jsonschema:"description=Terminal user interface options"`
	Debug                bool        `json:"debug,omitempty" jsonschema:"description=Enable debug logging,default=false"`
	DebugLSP             bool        `json:"debug_lsp,omitempty" jsonschema:"description=Enable debug logging for LSP servers,default=false"`
	DisableAutoSummarize bool        `json:"disable_auto_summarize,omitempty" jsonschema:"description=Disable automatic conversation summarization,default=false"`
	// MaxSessionCost, if positive, refuses a new prompt once the
	// session's accumulated cost has reached it. The refusal happens
	// before the request is dispatched, so it costs nothing beyond the
	// spend already on the session.
	MaxSessionCost float64 `json:"max_session_cost,omitempty" jsonschema:"description=Refuse a new prompt once a session's accumulated cost (USD) reaches this. Zero or unset means unbounded.,example=5"`
	// AllowedDomains and BlockedDomains govern which hosts the fetch,
	// download, and agentic-fetch tools may reach. Blocked wins over
	// allowed. Matching a domain also matches its subdomains.
	AllowedDomains []string `json:"allowed_domains,omitempty" jsonschema:"description=If set\\, the only domains (and their subdomains) the fetch/download tools may reach,example=docs.example.com"`
	BlockedDomains []string `json:"blocked_domains,omitempty" jsonschema:"description=Domains (and their subdomains) the fetch/download tools may never reach\\, checked before allowed_domains,example=internal.example.com"`
	// AutoSummarizeAt is the fraction of the model's context window that
	// may be used before a turn stops to summarize. Values outside (0,1)
	// keep the built-in thresholds.
	AutoSummarizeAt float64 `json:"auto_summarize_at,omitempty" jsonschema:"description=Summarize once this fraction of the context window is used (0-1 exclusive). Unset keeps the built-in thresholds.,example=0.8"`
	// MaxProviderRetries caps how many times a failed request to the
	// provider is retried before the turn gives up. Unset keeps the
	// provider library's default; 0 disables retries.
	MaxProviderRetries *int `json:"max_provider_retries,omitempty" jsonschema:"description=How many times to retry a failed provider request. Unset keeps the default; 0 disables retries.,example=5"`
	// MaxConcurrentSubAgents caps how many agent-tool sub-agents run at
	// once. Zero or unset means no limit.
	MaxConcurrentSubAgents int `json:"max_concurrent_subagents,omitempty" jsonschema:"description=Cap how many agent-tool sub-agents run at the same time. Zero or unset means no limit.,example=3"`
	// RestrictWritesToWorkingDir refuses writes outside the working
	// directory outright, before any permission is requested. Off by
	// default: editing a file in a sibling checkout is ordinary work.
	RestrictWritesToWorkingDir bool `json:"restrict_writes_to_working_dir,omitempty" jsonschema:"description=Refuse writes outside the working directory outright,default=false"`
	// MaxDownloadBytes caps what the download tool may write. Zero or
	// unset means no cap beyond the client timeout.
	MaxDownloadBytes int64 `json:"max_download_bytes,omitempty" jsonschema:"description=Refuse a download larger than this many bytes. Zero or unset means no limit.,example=52428800"`
	// AllowedCommands and BlockedCommands adjust the bash tool's built-in
	// list of banned commands. Allowing a command also lifts the
	// subcommand blocks on it (e.g. allowing "npm" permits "npm install
	// -g"), so an allow is the more specific instruction and wins over a
	// block of the same name.
	AllowedCommands []string `json:"allowed_commands,omitempty" jsonschema:"description=Commands to lift from the bash tool's built-in block list (their subcommand blocks are lifted too),example=curl"`
	BlockedCommands []string `json:"blocked_commands,omitempty" jsonschema:"description=Extra commands the bash tool may never run - on top of the built-in block list,example=kubectl"`
	// MaxStepsPerTurn caps how many model/tool-call steps a single turn
	// may take, independent of whether it is repeating itself (that is
	// hasRepeatedToolCalls's job). Zero means unbounded.
	MaxStepsPerTurn int `json:"max_steps_per_turn,omitempty" jsonschema:"description=Stop a turn once it has taken this many model/tool-call steps\\, whether or not it is making progress. Zero or unset means unbounded.,example=50"`
	// ToolTimeout bounds a single tool call, in seconds. Zero or unset
	// means unbounded: a tool call that legitimately takes ten minutes is
	// ordinary work in a large repository.
	ToolTimeout int `json:"tool_timeout,omitempty" jsonschema:"description=Stop a single tool call after this many seconds and tell the model it timed out. Zero or unset means unbounded.,example=300"`
	// DataDirectory is where ATLAS-AGENT keeps per-project state such as
	// the SQLite database and workspace overrides. Relative paths are
	// resolved against the working directory; absolute paths are used
	// verbatim. After defaulting the stored value is always absolute.
	DataDirectory             string       `json:"data_directory,omitempty" jsonschema:"description=Directory for storing application data. Relative paths are resolved against the working directory; absolute paths are used as-is.,default=.atlas,example=.atlas"`
	DisabledTools             []string     `json:"disabled_tools,omitempty" jsonschema:"description=List of built-in tools to disable and hide from the agent,example=bash,example=sourcegraph"`
	DisableProviderAutoUpdate bool         `json:"disable_provider_auto_update,omitempty" jsonschema:"description=Disable providers auto-update. Atlas Agent ships an embedded provider catalog and uses it by default; enable this flag to opt out and use the catalog as-is,default=true"`
	DisableDefaultProviders   bool         `json:"disable_default_providers,omitempty" jsonschema:"description=Ignore all default/embedded providers. When enabled\\, providers must be fully specified in the config file with base_url\\, models\\, and api_key - no merging with defaults occurs,default=false"`
	Attribution               *Attribution `json:"attribution,omitempty" jsonschema:"description=Attribution settings for generated content"`
	DisableMetrics            bool         `json:"disable_metrics,omitempty" jsonschema:"description=Disable sending metrics,default=false"`
	InitializeAs              string       `json:"initialize_as,omitempty" jsonschema:"description=Name of the context file to create/update during project initialization,default=AGENTS.md,example=AGENTS.md,example=ATLAS-AGENT.md,example=CLAUDE.md,example=docs/LLMs.md"`
	AutoLSP                   *bool        `json:"auto_lsp,omitempty" jsonschema:"description=Automatically setup LSPs based on root markers,default=true"`
	Progress                  *bool        `json:"progress,omitempty" jsonschema:"description=Show indeterminate progress updates during long operations,default=true"`
	Notifications             string       `json:"notifications,omitempty" jsonschema:"description=Notification style to use. Options: auto (default)\\, native\\, osc\\, bell\\, disabled. Auto selects based on environment: native for local sessions\\, osc for SSH (with automatic OSC 99/777 detection).,enum=auto,enum=native,enum=osc,enum=bell,enum=disabled,default=auto"`
	DisabledSkills            []string     `json:"disabled_skills,omitempty" jsonschema:"description=List of skill names to disable and hide from the agent,example=crush-config"`
	Memory                    *Memory      `json:"memory,omitempty" jsonschema:"description=Bounds on the prose the agent carries between sessions"`
	// AgentModels overrides which model type (large or small) a built-in
	// agent uses, keyed by agent ID (coder, task). An agent not named here
	// keeps its default. Unknown agent IDs and invalid model types are
	// ignored rather than rejected, since a stale key here should not stop
	// startup over a knob nobody is relying on any more.
	AgentModels map[string]SelectedModelType `json:"agent_models,omitempty" jsonschema:"description=Override which model type (large or small) an agent uses\\, keyed by agent id,example={\"task\":\"small\"}"`
	// ModelFallbacks lists, per model role, alternate provider/model pairs
	// tried in order when the role's primary model answers with a
	// rate-limit or quota error (HTTP 429). The large-model chain applies
	// mid-turn, to the main conversation; the small-model chain applies to
	// background small-model calls that already retry against the large
	// model on failure (e.g. session title generation), inserted ahead of
	// that existing large-model fallback.
	ModelFallbacks map[SelectedModelType][]SelectedModel `json:"model_fallbacks,omitempty" jsonschema:"description=Alternate provider/model pairs to fail over to\\, in order\\, when a model role hits a 429/rate-limit response,example={\"large\":[{\"model\":\"gpt-4o-mini\",\"provider\":\"openai\"}]}"`
	// FallbackCooldown is how many seconds a model fallback chain stays on
	// the model it failed over to before the next turn resets the chain
	// back to the primary. Zero (the default) resets on every turn, which
	// is the existing behavior; a nonzero value avoids retrying a model
	// that is likely still rate-limited from the previous turn.
	FallbackCooldown int `json:"fallback_cooldown,omitempty" jsonschema:"description=How many seconds a fallback model stays active after a failover before the next turn returns to the primary model. Zero resets every turn.,example=300"`
	// ModelRoles maps a free-form role name (e.g. "frontend", "research",
	// "review") to a concrete provider/model pair, distinct from the
	// large/small model types. A subagent's Model field can reference one
	// of these by name (with or without a leading "@") so different kinds
	// of work can run on different models without changing the session's
	// primary model. A handful of names are also recognized by built-in
	// features when present: "advisor" and "escalate" (see Advisor below),
	// and "compact", which -- when set -- summarization (auto or
	// /summarize) runs on instead of the session's own model.
	ModelRoles map[string]SelectedModel `json:"model_roles,omitempty" jsonschema:"description=Named model roles a subagent's model field can reference by name\\, e.g. \"frontend\" or \"research\". A few names are recognized by built-in features when present: advisor\\, escalate\\, and compact (used to summarize with a different model than the session's own).,example={\"research\":{\"model\":\"o3\",\"provider\":\"openai\"}}"`
	// SessionMode names a mode (see internal/subagents' built-in modes,
	// plus any subagent the user has authored) whose instructions are
	// folded into the main session's own system prompt, so the agent you
	// are talking to takes on that specialty instead of delegating to it.
	// When a model role shares the mode's name, the session also switches
	// to that model. Empty -- the default -- leaves the session on its
	// ordinary coder prompt and its large model.
	SessionMode string `json:"session_mode,omitempty" jsonschema:"description=Name of the mode whose instructions are folded into the main session's system prompt\\, e.g. review or security. A model role of the same name also switches the session's model. Empty runs the ordinary coder prompt.,example=security"`
	// Advisor enables a second model that reviews each finished turn (the
	// user's prompt and the assistant's final response) with read-only
	// tools (glob, grep, ls, view) of its own, and can leave a note that
	// is injected ahead of the session's next prompt. It runs on the
	// "advisor" ModelRoles entry; with none configured, Advisor has
	// nothing to run on and is silently inert even if Enabled is true.
	Advisor *Advisor `json:"advisor,omitempty" jsonschema:"description=A second model that reviews each turn and can leave a note for the next one. Needs a \"advisor\" entry in model_roles to run on."`

	// Sandbox contains every external process the shell interpreter
	// spawns (bash tool commands, hook commands, scripts they invoke) in
	// a Windows Job Object. See the Sandbox type doc for exactly what
	// this does and does not protect against.
	Sandbox *Sandbox `json:"sandbox,omitempty" jsonschema:"description=Contain shell-spawned processes in an OS-level container. Currently Windows only (Job Objects); a no-op elsewhere."`
}

// Sandbox configures Job Object containment (internal/sandbox) for every
// external process the shell interpreter spawns. This is process
// lifetime and resource containment, not a security boundary: a
// contained process can still read and write any file it has permission
// to and reach the network normally. What it guarantees is that the
// process (and anything it spawns) cannot outlive being cut off, and
// optionally that it cannot fork past a process-count ceiling or exceed
// a per-process memory ceiling. Currently implemented for Windows only
// (Job Objects); enabling it elsewhere is silently inert.
type Sandbox struct {
	Enabled bool `json:"enabled,omitempty" jsonschema:"description=Turn on Job Object containment for shell-spawned processes. Windows only for now -- inert elsewhere. This bounds process lifetime and count/memory; it does not restrict filesystem or network access.,default=false"`
	// MaxProcesses caps how many processes may be active in the
	// container at once, as a basic fork-bomb mitigation. 0 (the zero
	// value, via MaxProcessesOrDefault) uses a generous default rather
	// than no limit at all, since "enabled" should mean something even
	// when the user hasn't tuned it.
	MaxProcesses int `json:"max_processes,omitempty" jsonschema:"description=Cap on concurrently active processes inside the container (a fork-bomb ceiling). 0 uses a generous default.,default=256,example=64"`
	// MaxMemoryMB caps the committed memory of any single contained
	// process. 0 means unlimited -- ordinary dev tools (go build, npm
	// install) can legitimately need more memory than a one-size guess
	// would allow, so unlike MaxProcesses this has no default ceiling.
	MaxMemoryMB int `json:"max_memory_mb,omitempty" jsonschema:"description=Memory ceiling (MB) for any single contained process. 0 means unlimited.,example=2048"`
}

// defaultSandboxMaxProcesses is used when Sandbox.Enabled is true but
// MaxProcesses is left at its zero value -- "enabled" should mean some
// containment, not none.
const defaultSandboxMaxProcesses = 256

// MaxProcessesOrDefault returns the configured process-count ceiling, or
// defaultSandboxMaxProcesses for an unset or non-positive value.
func (s Sandbox) MaxProcessesOrDefault() int {
	if s.MaxProcesses > 0 {
		return s.MaxProcesses
	}
	return defaultSandboxMaxProcesses
}

// MaxMemoryBytes returns the configured per-process memory ceiling in
// bytes, or 0 (unlimited) when MaxMemoryMB is unset.
func (s Sandbox) MaxMemoryBytes() uint64 {
	if s.MaxMemoryMB <= 0 {
		return 0
	}
	return uint64(s.MaxMemoryMB) * 1024 * 1024
}

// Advisor holds settings for the optional turn-reviewing second model. See
// Options.Advisor.
type Advisor struct {
	Enabled bool `json:"enabled,omitempty" jsonschema:"description=Turn the advisor on. Needs an \"advisor\" model_roles entry to actually run.,default=false"`
	// EveryNTurns reviews only every Nth finished turn instead of all of
	// them, so a chatty session does not double its model spend. 1 (the
	// zero value, via EveryNTurns()) reviews every turn.
	EveryNTurns int `json:"every_n_turns,omitempty" jsonschema:"description=Review only every Nth finished turn instead of every one. 1 (default) reviews every turn.,default=1,example=3"`
	// MinSeverity is the lowest severity that raises a
	// notify.TypeAdvisorNote notification. Every severity above NONE is
	// still queued for the session's next prompt regardless -- this only
	// controls what interrupts the user mid-session versus waiting
	// quietly for the next turn.
	MinSeverity string `json:"min_severity,omitempty" jsonschema:"description=Lowest severity (NIT / CONCERN / BLOCKER) that raises a notification. Every severity is still queued for the next prompt either way.,default=CONCERN,example=BLOCKER"`
	// AutoEscalate runs a second, deeper pass with the "escalate" model
	// role (falling back to the advisor's own model when no "escalate"
	// role is configured) whenever the advisor's severity meets
	// EscalateThreshold, replacing the advisor's one-line note with
	// whatever that second pass produces -- a fuller diagnosis, or a
	// concrete fix description, from a pass explicitly asked to try
	// harder than a quick review. It never changes the turn that was
	// reviewed or delays the user; only the note queued for the next
	// prompt differs.
	AutoEscalate bool `json:"auto_escalate,omitempty" jsonschema:"description=Run a deeper second pass (the \"escalate\" model role, or the advisor's own model if none is set) whenever the advisor's severity meets escalate_threshold, and use its output as the queued note instead of the advisor's one-liner.,default=false"`
	// EscalateThreshold is the minimum severity that triggers
	// AutoEscalate. Defaults to BLOCKER: escalation costs a second model
	// call, so it is reserved for the severity that already means
	// "should be addressed before the work continues."
	EscalateThreshold string `json:"escalate_threshold,omitempty" jsonschema:"description=Lowest severity (NIT / CONCERN / BLOCKER) that triggers auto_escalate.,default=BLOCKER,example=CONCERN"`
}

// TurnInterval returns the configured review cadence, defaulting to 1
// (every turn) for zero or negative values.
func (a Advisor) TurnInterval() int {
	if a.EveryNTurns > 0 {
		return a.EveryNTurns
	}
	return 1
}

// NotifyThreshold returns the configured minimum notify severity,
// defaulting to CONCERN (the advisor's original, pre-configurable
// behavior) for an unset or unrecognized value.
func (a Advisor) NotifyThreshold() string {
	switch strings.ToUpper(a.MinSeverity) {
	case "NIT", "CONCERN", "BLOCKER":
		return strings.ToUpper(a.MinSeverity)
	default:
		return "CONCERN"
	}
}

// EscalateSeverityThreshold returns the configured minimum severity for
// AutoEscalate, defaulting to BLOCKER for an unset or unrecognized
// value.
func (a Advisor) EscalateSeverityThreshold() string {
	switch strings.ToUpper(a.EscalateThreshold) {
	case "NIT", "CONCERN", "BLOCKER":
		return strings.ToUpper(a.EscalateThreshold)
	default:
		return "BLOCKER"
	}
}

// Memory bounds the persistent stores. The bounds matter because their
// contents are prepended to every request: raising them raises the cost of
// every turn. A negative value means unbounded.
type Memory struct {
	ProjectLimit int `json:"project_limit,omitempty" jsonschema:"description=Character limit for the project memory store (MEMORY.md). Negative means unbounded.,default=3200"`
	UserLimit    int `json:"user_limit,omitempty" jsonschema:"description=Character limit for the user profile store (USER.md). Negative means unbounded.,default=2000"`
}

type MCPs map[string]MCPConfig

type MCP struct {
	Name string    `json:"name"`
	MCP  MCPConfig `json:"mcp"`
}

func (m MCPs) Sorted() []MCP {
	sorted := make([]MCP, 0, len(m))
	for k, v := range m {
		sorted = append(sorted, MCP{
			Name: k,
			MCP:  v,
		})
	}
	slices.SortFunc(sorted, func(a, b MCP) int {
		return strings.Compare(a.Name, b.Name)
	})
	return sorted
}

type LSPs map[string]LSPConfig

type LSP struct {
	Name string    `json:"name"`
	LSP  LSPConfig `json:"lsp"`
}

func (l LSPs) Sorted() []LSP {
	sorted := make([]LSP, 0, len(l))
	for k, v := range l {
		sorted = append(sorted, LSP{
			Name: k,
			LSP:  v,
		})
	}
	slices.SortFunc(sorted, func(a, b LSP) int {
		return strings.Compare(a.Name, b.Name)
	})
	return sorted
}

// ResolvedEnv returns m.Env with every value expanded through the
// given resolver. The returned slice is of the form "KEY=value" sorted
// by key so callers get deterministic output; the receiver's Env map is
// not mutated. On the first resolution failure it returns nil and an
// error that identifies the offending key; the inner resolver error is
// already sanitized by ResolveValue and is wrapped with %w so
// errors.Is/As continues to work. Callers are expected to surface it
// (for MCP, via StateError on the status card) rather than silently
// spawn the server with an empty credential.
//
// The resolver choice matters: in server mode pass the shell resolver
// so $VAR / $(cmd) expand; in client mode pass IdentityResolver so the
// template is forwarded verbatim and expansion happens on the server.
func (m MCPConfig) ResolvedEnv(r VariableResolver) ([]string, error) {
	return resolveEnvs(m.Env, r)
}

// ResolvedArgs returns m.Args with every element expanded through the
// given resolver. A fresh slice is allocated; m.Args is never mutated.
// On the first resolution failure it returns nil and an error
// identifying the offending positional index; the inner resolver error
// is already sanitized by ResolveValue and is wrapped with %w so
// errors.Is/As continues to work.
//
// See ResolvedEnv for guidance on picking a resolver.
func (m MCPConfig) ResolvedArgs(r VariableResolver) ([]string, error) {
	if len(m.Args) == 0 {
		return nil, nil
	}
	out := make([]string, len(m.Args))
	for i, a := range m.Args {
		v, err := r.ResolveValue(a)
		if err != nil {
			return nil, fmt.Errorf("arg %d: %w", i, err)
		}
		out[i] = v
	}
	return out, nil
}

// ResolvedURL returns m.URL expanded through the given resolver. The
// receiver is not mutated. Errors from the resolver are already
// sanitized by ResolveValue and are wrapped with %w for errors.Is/As.
//
// URLs run through the same shell-expansion pipeline as the other
// fields, so a literal '$' (e.g. OData query strings containing
// $filter/$select) must be escaped as '\$' or '${DOLLAR:-$}' to avoid
// being interpreted as a variable reference. Same constraint already
// applies to command, args, env, and headers.
//
// See ResolvedEnv for guidance on picking a resolver.
func (m MCPConfig) ResolvedURL(r VariableResolver) (string, error) {
	if m.URL == "" {
		return "", nil
	}
	v, err := r.ResolveValue(m.URL)
	if err != nil {
		return "", fmt.Errorf("url: %w", err)
	}
	return v, nil
}

// knownSessionlessMCPs is the set of MCP endpoint URLs (normalized, no
// trailing slash) that are known not to maintain an MCP session — they
// never issue a Mcp-Session-Id and reject the SEP-2575
// "subscriptions/listen" stream. Add an entry when a server is confirmed to
// behave this way.
var knownSessionlessMCPs = map[string]struct{}{
	"https://api.github.com/mcp":        {},
	"https://api.githubcopilot.com/mcp": {},
}

// IsSessionless reports whether the server should be treated as sessionless.
// An explicit Sessionless value wins; when unset, the resolved URL is matched
// against knownSessionlessMCPs (trailing slash ignored). The URL is resolved
// through r so $VAR-expanded endpoints are detected too; on a resolution
// error the explicit value (or false) is used.
func (m MCPConfig) IsSessionless(r VariableResolver) bool {
	if m.Sessionless != nil {
		return *m.Sessionless
	}
	url, err := m.ResolvedURL(r)
	if err != nil {
		return false
	}
	_, ok := knownSessionlessMCPs[strings.TrimSuffix(url, "/")]
	return ok
}

// ResolvedHeaders returns m.Headers with every value expanded through
// the given resolver. A fresh map is allocated; m.Headers is never
// mutated. On the first resolution failure it returns nil and an error
// identifying the offending header name; the inner resolver error is
// already sanitized by ResolveValue and is wrapped with %w so
// errors.Is/As continues to work.
//
// A header whose value resolves to the empty string (unset bare $VAR
// under lenient nounset, $(echo), or literal "") is omitted from the
// returned map — sending "X-Auth:" with an empty value is rejected by
// some providers and the user's intent in "optional, env-gated
// header" is clearly "absent when the var isn't set."
//
// See ResolvedEnv for guidance on picking a resolver.
func (m MCPConfig) ResolvedHeaders(r VariableResolver) (map[string]string, error) {
	if len(m.Headers) == 0 {
		return map[string]string{}, nil
	}
	out := make(map[string]string, len(m.Headers))
	// Sort keys so failures are reported deterministically when more
	// than one header would fail.
	keys := make([]string, 0, len(m.Headers))
	for k := range m.Headers {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		v, err := r.ResolveValue(m.Headers[k])
		if err != nil {
			return nil, fmt.Errorf("header %s: %w", k, err)
		}
		if v == "" {
			continue
		}
		out[k] = v
	}
	return out, nil
}

// ResolvedArgs returns l.Args with every element expanded through the
// given resolver. A fresh slice is allocated; l.Args is never mutated.
// On the first resolution failure it returns nil and an error
// identifying the offending positional index; the inner resolver error
// is already sanitized by ResolveValue and is wrapped with %w so
// errors.Is/As continues to work.
//
// Empty resolved values are kept (a deliberate "empty positional arg"
// like --flag "" is sometimes valid), matching MCPConfig.ResolvedArgs.
//
// The resolver choice matters: in server mode pass the shell resolver
// so $VAR / $(cmd) expand; in client mode pass IdentityResolver so the
// template is forwarded verbatim.
func (l LSPConfig) ResolvedArgs(r VariableResolver) ([]string, error) {
	if len(l.Args) == 0 {
		return nil, nil
	}
	out := make([]string, len(l.Args))
	for i, a := range l.Args {
		v, err := r.ResolveValue(a)
		if err != nil {
			return nil, fmt.Errorf("arg %d: %w", i, err)
		}
		out[i] = v
	}
	return out, nil
}

// ResolvedEnv returns l.Env with every value expanded through the
// given resolver. A fresh map is allocated; l.Env is never mutated.
// On the first resolution failure it returns nil and an error that
// identifies the offending key; the inner resolver error is already
// sanitized by ResolveValue and is wrapped with %w so errors.Is/As
// continues to work.
//
// Empty resolved values are kept ("FOO=" is a legitimate request;
// opt out via ${VAR:+...}), matching MCPConfig.ResolvedEnv.
//
// Shape note: this returns map[string]string rather than the []string
// shape MCPConfig.ResolvedEnv uses because the consumer
// (powernap.ClientConfig.Environment in internal/lsp/client.go) takes
// a map directly — returning a []string here would only force a
// round-trip back to a map at the call site.
//
// See ResolvedArgs for guidance on picking a resolver.
func (l LSPConfig) ResolvedEnv(r VariableResolver) (map[string]string, error) {
	if len(l.Env) == 0 {
		return map[string]string{}, nil
	}
	out := make(map[string]string, len(l.Env))
	// Sort keys so failures are reported deterministically when more
	// than one value would fail.
	keys := make([]string, 0, len(l.Env))
	for k := range l.Env {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		v, err := r.ResolveValue(l.Env[k])
		if err != nil {
			return nil, fmt.Errorf("env %q: %w", k, err)
		}
		out[k] = v
	}
	return out, nil
}

type Agent struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	// This is the id of the system prompt used by the agent
	Disabled bool `json:"disabled,omitempty"`

	Model SelectedModelType `json:"model" jsonschema:"required,description=The model type to use for this agent,enum=large,enum=small,default=large"`

	// The available tools for the agent
	//  if this is nil, all tools are available
	AllowedTools []string `json:"allowed_tools,omitempty"`

	// this tells us which MCPs are available for this agent
	//  if this is empty all mcps are available
	//  the string array is the list of tools from the AllowedMCP the agent has available
	//  if the string array is nil, all tools from the AllowedMCP are available
	AllowedMCP map[string][]string `json:"allowed_mcp,omitempty"`

	// Overrides the context paths for this agent
	ContextPaths []string `json:"context_paths,omitempty"`
}

type Tools struct {
	Ls       ToolLs       `json:"ls,omitzero"`
	Grep     ToolGrep     `json:"grep,omitzero"`
	Glob     ToolGlob     `json:"glob,omitzero"`
	Bash     ToolBash     `json:"bash,omitzero"`
	View     ToolView     `json:"view,omitzero"`
	Browser  ToolBrowser  `json:"browser,omitzero"`
	Debugger ToolDebugger `json:"debugger,omitzero"`
	Teams    ToolTeams    `json:"teams,omitzero"`

	CodeIntel ToolCodeIntel `json:"code_intel,omitzero"`
	Quality   ToolQuality   `json:"quality,omitzero"`
	Git       ToolGit       `json:"git,omitzero"`
	Docs      ToolDocs      `json:"docs,omitzero"`
}

// ToolGit configures the read-only git tools -- status, log, blame and
// the like.
//
// Off by default like the other groups. The agent can already reach git
// through the shell; these tools exist because they parse git's
// machine-readable formats into compact answers, cannot modify anything,
// and so need no approval for what is a read.
type ToolGit struct {
	Enabled *bool `json:"enabled,omitempty" jsonschema:"description=Turn on the read-only git tools (git_status and friends) so the agent can inspect the working tree and history without shelling out.,default=false"`
}

// IsEnabled reports whether the git tools should be registered.
func (t ToolGit) IsEnabled() bool {
	return ptrValOr(t.Enabled, false)
}

// ToolQuality configures the code-quality and security tools --
// credential scanning, coverage, linting and the like.
//
// Off by default, on the same reasoning as the other groups: every
// registered tool costs context on every request. Worth turning on for
// any repository that is going to be pushed anywhere public.
type ToolQuality struct {
	Enabled *bool `json:"enabled,omitempty" jsonschema:"description=Turn on the code-quality and security tools (scan_secrets and friends) so the agent can check a tree for leaked credentials and other quality problems.,default=false"`
}

// IsEnabled reports whether the quality tools should be registered.
func (t ToolQuality) IsEnabled() bool {
	return ptrValOr(t.Enabled, false)
}

// ToolCodeIntel configures the static-analysis tools (dead_code and
// friends), which parse Go source with go/ast to answer structural
// questions the LSP tools cannot: what is unreferenced, what implements
// what, how packages depend on each other.
//
// Off by default. Not because they are dangerous -- every one of them is
// read-only -- but because each registered tool costs context on every
// single request, and these only earn that cost on a Go codebase the user
// is actually auditing.
type ToolCodeIntel struct {
	Enabled *bool `json:"enabled,omitempty" jsonschema:"description=Turn on the Go static-analysis tools (dead_code and friends) so the agent can find unreferenced declarations and other structural facts without compiling the tree.,default=false"`
}

// IsEnabled reports whether the code-intelligence tools should be
// registered.
func (t ToolCodeIntel) IsEnabled() bool {
	return ptrValOr(t.Enabled, false)
}

// ToolDocs configures the documentation tools (doc_index and friends),
// which read a tree's Markdown files to answer structural questions --
// what topics exist, where a section lives -- without the agent reading
// every file in full first.
//
// Off by default, the same reasoning as ToolCodeIntel: read-only, but not
// worth its context cost on a request that has nothing to do with a
// repository's documentation.
type ToolDocs struct {
	Enabled *bool `json:"enabled,omitempty" jsonschema:"description=Turn on the documentation tools (doc_index and friends) so the agent can index and search a tree's Markdown files by heading.,default=false"`
}

// IsEnabled reports whether the documentation tools should be
// registered.
func (t ToolDocs) IsEnabled() bool {
	return ptrValOr(t.Enabled, false)
}

// ToolTeams configures the team_send / team_read tools, which let
// sub-agents spawned from the same task (running in parallel or nested
// arbitrarily deep) broadcast short notes to each other via an in-memory
// mailbox. Off by default like the other opt-in tools, even though this
// one has no filesystem/network/process footprint at all -- it only
// matters once sub-agents are in play, so it stays out of the tool
// palette otherwise.
type ToolTeams struct {
	Enabled *bool `json:"enabled,omitempty" jsonschema:"description=Turn on team_send / team_read so sub-agents spawned by the same task can broadcast short notes to each other while still running.,default=false"`
}

// IsEnabled reports whether the team_send/team_read tools should be
// registered.
func (t ToolTeams) IsEnabled() bool {
	return ptrValOr(t.Enabled, false)
}

// ToolDebugger configures the debugger tool, which drives a Go program
// under Delve (`dlv dap`) over the Debug Adapter Protocol. Off by default,
// like the browser tool: it launches an external process and requires
// Delve to be installed. Scoped to Go/Delve only -- there is no claim of
// multi-language support.
type ToolDebugger struct {
	Enabled       *bool          `json:"enabled,omitempty" jsonschema:"description=Turn on the debugger tool so the agent can launch a Go program under Delve: set breakpoints / step / inspect variables. Requires a Delve (dlv) install on PATH (or dlv_path below).,default=false"`
	DlvPath       string         `json:"dlv_path,omitempty" jsonschema:"description=Path to the dlv binary. Empty uses dlv on PATH."`
	ActionTimeout *time.Duration `json:"action_timeout,omitempty" jsonschema:"description=How long a single debugger request (set breakpoints / step / evaluate / etc.) may run before it is aborted. Does not bound continue -- that waits for the program to actually stop.,default=30s,example=1m"`
}

// IsEnabled reports whether the debugger tool should be registered.
func (t ToolDebugger) IsEnabled() bool {
	return ptrValOr(t.Enabled, false)
}

// GetActionTimeout returns the user-defined per-action timeout, or its
// default.
func (t ToolDebugger) GetActionTimeout() time.Duration {
	return ptrValOr(t.ActionTimeout, 30*time.Second)
}

// ToolBrowser configures the browser tool, which drives a real
// Chrome/Chromium instance over the Chrome DevTools Protocol. Off by
// default: unlike the other tools, it launches an external browser process
// and reaches whatever URL the permission prompt allows, so turning it on
// is an explicit opt-in.
type ToolBrowser struct {
	Enabled        *bool          `json:"enabled,omitempty" jsonschema:"description=Turn on the browser tool so the agent can navigate / click / type / screenshot / run JavaScript in a real Chrome or Chromium instance. Requires a Chrome or Chromium install on PATH (or executable_path below).,default=false"`
	ExecutablePath string         `json:"executable_path,omitempty" jsonschema:"description=Path to a Chrome or Chromium binary. Empty auto-detects an installed browser."`
	Headless       *bool          `json:"headless,omitempty" jsonschema:"description=Run the browser without a visible window,default=true"`
	UseRealProfile *bool          `json:"use_real_profile,omitempty" jsonschema:"description=Browse as you: copies your own Chrome profile - cookies, saved logins, extensions - into the directory Atlas launches from, so the agent starts already signed in wherever you are. Chrome cannot open the profile it is running on and refuses remote debugging on it, so this is a copy rather than a live view: signing in on one side does not reach the other. Delete the snapshot directory to take the copy again.,default=false"`
	RealProfilePin string         `json:"real_profile_pin,omitempty" jsonschema:"description=Which Chrome profile use_real_profile copies when you have several. Empty copies the one you browsed with last - pin it if you do not want the profile you last touched deciding who the agent acts as.,example=Default"`
	UserDataDir    string         `json:"user_data_dir,omitempty" jsonschema:"description=Chrome profile directory to launch with. Cookies and logins written here survive between sessions, so the agent browses as a signed-in user instead of a blank profile. Empty uses a throwaway profile. Chrome cannot open a directory another running Chrome already holds, so point this at a directory of its own rather than at your everyday profile."`
	RemoteURL      string         `json:"remote_url,omitempty" jsonschema:"description=DevTools endpoint to drive a long-lived browser through (for example http://127.0.0.1:9222). If nothing is listening there Atlas starts one itself using user_data_dir and executable_path; that browser keeps running after Atlas exits so its logins and tabs survive. Attach to a Chrome you started yourself by giving it the same port. Chrome refuses remote debugging on your everyday profile so this is always a separate one - sign into it once and it stays signed in.,example=http://127.0.0.1:9222"`
	ActionTimeout  *time.Duration `json:"action_timeout,omitempty" jsonschema:"description=How long a single browser action (navigate / click / eval / etc.) may run before it is aborted,default=30s,example=1m"`
	IdleTimeout    *time.Duration `json:"idle_timeout,omitempty" jsonschema:"description=How long an unused browser session is kept open before it is closed automatically,default=10m,example=5m"`
}

// IsEnabled reports whether the browser tool should be registered.
func (t ToolBrowser) IsEnabled() bool {
	return ptrValOr(t.Enabled, false)
}

// UsesRealProfile reports whether new sessions launch from a copy of the
// user's own Chrome profile.
func (t ToolBrowser) UsesRealProfile() bool {
	return ptrValOr(t.UseRealProfile, false)
}

// GetRealProfilePin returns the profile name to copy, or empty for
// whichever the user browsed with last.
func (t ToolBrowser) GetRealProfilePin() string {
	return t.RealProfilePin
}

// GetUserDataDir returns the Chrome profile directory new sessions launch
// with. Empty means chromedp's own throwaway profile.
func (t ToolBrowser) GetUserDataDir() string {
	return t.UserDataDir
}

// GetRemoteURL returns the DevTools endpoint of a browser to attach to
// instead of launching one. Empty means launch.
func (t ToolBrowser) GetRemoteURL() string {
	return t.RemoteURL
}

// IsHeadless reports whether new sessions should run without a visible
// window.
func (t ToolBrowser) IsHeadless() bool {
	return ptrValOr(t.Headless, true)
}

// GetActionTimeout returns the user-defined per-action timeout, or its
// default.
func (t ToolBrowser) GetActionTimeout() time.Duration {
	return ptrValOr(t.ActionTimeout, 30*time.Second)
}

// GetIdleTimeout returns the user-defined session idle timeout, or its
// default.
func (t ToolBrowser) GetIdleTimeout() time.Duration {
	return ptrValOr(t.IdleTimeout, 10*time.Minute)
}

type ToolView struct {
	DefaultReadLimit *int  `json:"default_read_limit,omitempty" jsonschema:"description=Lines the view tool returns when the model does not say,default=200,example=500"`
	MaxLineLength    *int  `json:"max_line_length,omitempty" jsonschema:"description=How much of a single long line survives before it is cut,default=2000,example=500"`
	HashAnchors      *bool `json:"hash_anchors,omitempty" jsonschema:"description=Show a short content hash next to every line so edit can target a line by number instead of reproducing its exact text. Off by default: it adds a few characters to every line view returns in exchange for cheaper and more reliable single-line edits.,default=false"`
}

// Limits returns the user-defined read limit and line length. Zero means
// "unset": the view tool substitutes its own defaults rather than returning
// nothing.
func (t ToolView) Limits() (defaultReadLimit, maxLineLength int) {
	return ptrValOr(t.DefaultReadLimit, 0), ptrValOr(t.MaxLineLength, 0)
}

// HashAnchorsEnabled reports whether view should annotate each line with a
// content hash that edit's anchor_line/anchor_hash mode can target.
func (t ToolView) HashAnchorsEnabled() bool {
	return ptrValOr(t.HashAnchors, false)
}

type ToolBash struct {
	MaxOutputLength     *int `json:"max_output_length,omitempty" jsonschema:"description=Maximum width of the output the bash tool returns before its middle is dropped,default=30000,example=10000"`
	AutoBackgroundAfter *int `json:"auto_background_after,omitempty" jsonschema:"description=Seconds a command may run in the foreground before it becomes a background job,default=60,example=120"`
}

// Limits returns the user-defined output width and auto-background
// threshold. Zero means "unset": the bash tool substitutes its own defaults
// rather than truncating to nothing or backgrounding immediately.
func (t ToolBash) Limits() (maxOutputLength, autoBackgroundAfter int) {
	return ptrValOr(t.MaxOutputLength, 0), ptrValOr(t.AutoBackgroundAfter, 0)
}

type ToolLs struct {
	MaxDepth *int `json:"max_depth,omitempty" jsonschema:"description=Maximum depth for the ls tool,default=0,example=10"`
	MaxItems *int `json:"max_items,omitempty" jsonschema:"description=Maximum number of items to return for the ls tool,default=1000,example=100"`
}

// Limits returns the user-defined max-depth and max-items, or their defaults.
func (t ToolLs) Limits() (depth, items int) {
	return ptrValOr(t.MaxDepth, 0), ptrValOr(t.MaxItems, 0)
}

type ToolGrep struct {
	Timeout *time.Duration `json:"timeout,omitempty" jsonschema:"description=Timeout for the grep tool call,default=5s,example=10s"`
}

// GetTimeout returns the user-defined timeout or the default.
func (t ToolGrep) GetTimeout() time.Duration {
	return ptrValOr(t.Timeout, 5*time.Second)
}

type ToolGlob struct {
	Timeout *time.Duration `json:"timeout,omitempty" jsonschema:"description=Timeout for the glob tool call,default=30s,example=10s"`
}

// GetTimeout returns the user-defined timeout or the default.
func (t ToolGlob) GetTimeout() time.Duration {
	return ptrValOr(t.Timeout, 30*time.Second)
}

// HookConfig defines a user-configured shell command that fires on a hook
// event (e.g. PreToolUse). This is a pure-data struct: matcher compilation
// is owned by hooks.Runner so a JSON round-trip, merge, or reload can't
// silently drop compiled state.
type HookConfig struct {
	// Friendly display name shown in the TUI. Falls back to Command when empty.
	Name string `json:"name,omitempty" jsonschema:"description=Friendly display name shown in the TUI for this hook"`
	// Regex pattern tested against the tool name. Empty means match all.
	Matcher string `json:"matcher,omitempty" jsonschema:"description=Regex pattern tested against the tool name. Empty means match all tools."`
	// Shell command to execute.
	Command string `json:"command" jsonschema:"required,description=Shell command to execute when the hook fires"`
	// Timeout in seconds. Default 30.
	Timeout int `json:"timeout,omitempty" jsonschema:"description=Timeout in seconds for the hook command,default=30"`
}

// DisplayName returns the hook name for display purposes. It returns Name
// when set, otherwise falls back to Command.
func (h *HookConfig) DisplayName() string {
	if h.Name != "" {
		return h.Name
	}
	return h.Command
}

// TimeoutDuration returns the hook timeout as a time.Duration, defaulting
// to 30s.
func (h *HookConfig) TimeoutDuration() time.Duration {
	if h.Timeout <= 0 {
		return 30 * time.Second
	}
	return time.Duration(h.Timeout) * time.Second
}

// Config holds the configuration for Atlas-Agent.
type Config struct {
	Schema string `json:"$schema,omitempty"`

	// We currently only support large/small as values here.
	Models map[SelectedModelType]SelectedModel `json:"models,omitempty" jsonschema:"description=Model configurations for different model types,example={\"large\":{\"model\":\"gpt-4o\",\"provider\":\"openai\"}}"`

	// Recently used models stored in the data directory config.
	RecentModels map[SelectedModelType][]SelectedModel `json:"recent_models,omitempty" jsonschema:"-"`

	// The providers that are configured
	Providers *csync.Map[string, ProviderConfig] `json:"providers,omitempty" jsonschema:"description=AI provider configurations"`

	MCP MCPs `json:"mcp,omitempty" jsonschema:"description=Model Context Protocol server configurations"`

	LSP LSPs `json:"lsp,omitempty" jsonschema:"description=Language Server Protocol configurations"`

	Options *Options `json:"options,omitempty" jsonschema:"description=General application options"`

	Permissions *Permissions `json:"permissions,omitempty" jsonschema:"description=Permission settings for tool usage"`

	Tools Tools `json:"tools,omitzero" jsonschema:"description=Tool configurations"`

	Hooks map[string][]HookConfig `json:"hooks,omitempty" jsonschema:"description=User-defined shell commands that fire on hook events (e.g. PreToolUse)"`

	// Env is a map of environment variables set on startup.
	Env map[string]string `json:"env,omitempty" jsonschema:"description=Environment variables to set on startup"`

	Agents map[string]Agent `json:"-"`
}

// cloneForWrite returns a copy of c that the store's typed field mutators
// may modify without racing readers of the currently published Config.
//
// Reads of a published Config take no lock beyond the pointer load, so a
// mutator must never write through the live pointer. Instead it clones,
// mutates the clone, and atomically swaps it in. The clone gives fresh
// copies of every field a typed mutator touches in place — Models,
// RecentModels, MCP, and Options (with its nested TUI pointer). Providers
// is a *csync.Map (internally synchronized) and is shared by reference;
// the remaining fields are immutable after load from the mutators'
// standpoint and are likewise shared.
func (c *Config) cloneForWrite() *Config {
	nc := *c
	nc.Models = maps.Clone(c.Models)
	nc.RecentModels = maps.Clone(c.RecentModels)
	nc.MCP = maps.Clone(c.MCP)
	if c.Options != nil {
		opts := *c.Options
		if c.Options.TUI != nil {
			tui := *c.Options.TUI
			opts.TUI = &tui
		}
		nc.Options = &opts
	}
	return &nc
}

// ensureTUI returns c.Options.TUI, allocating Options and TUI as needed so
// callers can assign TUI fields without nil checks.
func (c *Config) ensureTUI() *TUIOptions {
	if c.Options == nil {
		c.Options = &Options{}
	}
	if c.Options.TUI == nil {
		c.Options.TUI = &TUIOptions{}
	}
	return c.Options.TUI
}

func (c *Config) EnabledProviders() []ProviderConfig {
	var enabled []ProviderConfig
	for p := range c.Providers.Seq() {
		if !p.Disable {
			enabled = append(enabled, p)
		}
	}
	return enabled
}

// IsConfigured  return true if at least one provider is configured
func (c *Config) IsConfigured() bool {
	return len(c.EnabledProviders()) > 0
}

func (c *Config) GetModel(provider, model string) *catwalk.Model {
	if providerConfig, ok := c.Providers.Get(provider); ok {
		for _, m := range providerConfig.Models {
			if m.ID == model {
				return &m
			}
		}
	}
	return nil
}

// IsModelAvailable returns true if the provider is enabled and the model
// exists in its catalog. Unlike GetModel, it rejects disabled providers.
func (c *Config) IsModelAvailable(provider, model string) bool {
	providerConfig, ok := c.Providers.Get(provider)
	if !ok || providerConfig.Disable {
		return false
	}
	for _, m := range providerConfig.Models {
		if m.ID == model {
			return true
		}
	}
	return false
}

func (c *Config) GetProviderForModel(modelType SelectedModelType) *ProviderConfig {
	model, ok := c.Models[modelType]
	if !ok {
		return nil
	}
	if providerConfig, ok := c.Providers.Get(model.Provider); ok {
		return &providerConfig
	}
	return nil
}

func (c *Config) GetModelByType(modelType SelectedModelType) *catwalk.Model {
	model, ok := c.Models[modelType]
	if !ok {
		return nil
	}
	return c.GetModel(model.Provider, model.Model)
}

// StripRoleReference strips a leading "@" from a role reference, so a
// subagent's `model: "@research"` and a bare `model: "research"` resolve to
// the same role name.
func StripRoleReference(ref string) string {
	return strings.TrimPrefix(strings.TrimSpace(ref), "@")
}

// ResolveRole resolves a role reference to a concrete provider/model pair.
// name may be "large" or "small" (the built-in model types) or any key in
// Options.ModelRoles; a leading "@" is stripped so "@research" and
// "research" mean the same thing. Reports false if name is empty or names
// nothing configured.
func (c *Config) ResolveRole(name string) (SelectedModel, bool) {
	name = StripRoleReference(name)
	if name == "" {
		return SelectedModel{}, false
	}
	switch SelectedModelType(name) {
	case SelectedModelTypeLarge, SelectedModelTypeSmall:
		model, ok := c.Models[SelectedModelType(name)]
		return model, ok
	}
	if c.Options == nil {
		return SelectedModel{}, false
	}
	model, ok := c.Options.ModelRoles[name]
	return model, ok
}

func (c *Config) LargeModel() *catwalk.Model {
	model, ok := c.Models[SelectedModelTypeLarge]
	if !ok {
		return nil
	}
	return c.GetModel(model.Provider, model.Model)
}

func (c *Config) SmallModel() *catwalk.Model {
	model, ok := c.Models[SelectedModelTypeSmall]
	if !ok {
		return nil
	}
	return c.GetModel(model.Provider, model.Model)
}

const maxRecentModelsPerType = 5

// allToolNames is the allowlist every agent's tool set is filtered
// through. A tool the coordinator builds but that is missing here is
// silently dropped: it is never offered to the model and nothing says so.
// TestEveryBuiltToolIsAllowed in internal/agent guards against that drift.
func allToolNames() []string {
	return []string{
		"agent",
		"bash",
		"atlas_info",
		"atlas_logs",
		"job_output",
		"job_kill",
		"download",
		"edit",
		"exit_plan_mode",
		"multiedit",
		"lsp_diagnostics",
		"lsp_references",
		"lsp_restart",
		"lsp_symbols",
		"lsp_definition",
		"lsp_call_hierarchy",
		"lsp_rename",
		"lsp_rename_file",
		"lsp_replace_symbol",
		"fetch",
		"agentic_fetch",
		"orchestrate",
		"delegate",
		"vibe",
		"facts",
		"inspect_file",
		"glob",
		"grep",
		"ls",
		"memory",
		"question",
		"session_search",
		"skill_manage",
		"sourcegraph",
		"todos",
		"usage",
		"view",
		"write",
		"list_mcp_resources",
		"read_mcp_resource",

		// The opt-in tool groups below (see the Tools struct and
		// coordinator.buildTools) are only ever registered when their
		// group is enabled, but still need to be listed here -- an
		// agent's AllowedTools is filtered against this list regardless
		// of whether the tool was actually built, so a name missing here
		// makes the tool silently unusable even after the user turns its
		// group on.
		"browser",
		"debugger",
		"team_send",
		"team_read",
		// CodeIntel
		"dead_code",
		"type_hierarchy",
		"import_graph",
		"impact_analysis",
		"code_metrics",
		"todo_scan",
		"api_surface",
		"anti_pattern_scan",
		"security_scan",
		"generate_docstring",
		"generate_tests",
		"semantic_code_search",
		"metric_export",
		"env_var_audit",
		// Quality
		"scan_secrets",
		"test_run",
		"coverage_report",
		"lint_run",
		"dep_audit",
		"docker_build_explain",
		"k8s_manifest_lint",
		"terraform_lint",
		"ci_cd_pipeline_debugger",
		"cloud_resource_costs",
		"log_tail",
		// Git
		"git_status",
		"git_log",
		"git_blame",
		"git_diff",
		"git_branches",
		"changelog_gen",
		"pr_describe",
		"pre_commit_guard",
		"git_conventional_commit",
		"git_conflict_resolver",
		"audit_trail",
		"git_commit_split",
		"github_pr_view",
		// Docs
		"doc_index",
	}
}

func resolveAllowedTools(allTools []string, disabledTools []string) []string {
	if disabledTools == nil {
		return allTools
	}
	// filter out disabled tools (exclude mode)
	return filterSlice(allTools, disabledTools, false)
}

func resolveReadOnlyTools(tools []string) []string {
	readOnlyTools := []string{"glob", "grep", "ls", "lsp_call_hierarchy", "lsp_definition", "lsp_symbols", "session_search", "sourcegraph", "usage", "view"}
	// filter to only include tools that are in allowedtools (include mode)
	return filterSlice(tools, readOnlyTools, true)
}

func filterSlice(data []string, mask []string, include bool) []string {
	var filtered []string
	for _, s := range data {
		// if include is true, we include items that ARE in the mask
		// if include is false, we include items that are NOT in the mask
		if include == slices.Contains(mask, s) {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

func (c *Config) SetupAgents() {
	allowedTools := resolveAllowedTools(allToolNames(), c.Options.DisabledTools)

	agents := map[string]Agent{
		AgentCoder: {
			ID:           AgentCoder,
			Name:         "Coder",
			Description:  "An agent that helps with executing coding tasks.",
			Model:        c.agentModel(AgentCoder, SelectedModelTypeLarge),
			ContextPaths: c.Options.ContextPaths,
			AllowedTools: allowedTools,
		},

		AgentTask: {
			ID:           AgentTask,
			Name:         "Task",
			Description:  "An agent that helps with searching for context and finding implementation details.",
			Model:        c.agentModel(AgentTask, SelectedModelTypeLarge),
			ContextPaths: c.Options.ContextPaths,
			AllowedTools: resolveReadOnlyTools(allowedTools),
			// NO MCPs or LSPs by default
			AllowedMCP: map[string][]string{},
		},
	}
	c.Agents = agents
}

// agentModel resolves which model type an agent should use: the override in
// Options.AgentModels for id, if one is set and names a real model type,
// otherwise def. This is where a role gets routed to a model tier -- coder
// stays on the large model by default, but a workspace that wants the task
// agent's searches on the small model sets it there instead of here.
func (c *Config) agentModel(id string, def SelectedModelType) SelectedModelType {
	if override, ok := c.Options.AgentModels[id]; ok && override.Valid() {
		return override
	}
	return def
}

func (c *ProviderConfig) TestConnection(resolver VariableResolver) error {
	var (
		providerID = catwalk.InferenceProvider(c.ID)
		testURL    = ""
		headers    = make(map[string]string)
		apiKey, _  = resolver.ResolveValue(c.APIKey)
	)

	switch providerID {
	case catwalk.InferenceProviderMiniMax, catwalk.InferenceProviderMiniMaxChina:
		// NOTE: MiniMax has no good endpoint we can use to validate the API key.
		return nil
	case catwalk.InferenceProviderAlibabaSingapore:
		// NOTE: Alibaba has no good endpoint we can use to validate the API key.
		// Let's at least check the pattern.
		if !strings.HasPrefix(apiKey, "sk-") {
			return fmt.Errorf("invalid API key format for provider %s", c.ID)
		}
		return nil
	}

	switch c.Type {
	case catwalk.TypeOpenAI, catwalk.TypeOpenAICompat, catwalk.TypeOpenRouter:
		baseURL, _ := resolver.ResolveValue(c.BaseURL)
		baseURL = cmp.Or(baseURL, "https://api.openai.com/v1")

		switch providerID {
		case catwalk.InferenceProviderOpenRouter:
			testURL = baseURL + "/credits"
		case catwalk.InferenceProviderOpenCodeGo:
			testURL = strings.Replace(baseURL, "/go", "", 1) + "/models"
		default:
			testURL = baseURL + "/models"
		}

		headers["Authorization"] = "Bearer " + apiKey
	case catwalk.TypeAnthropic:
		baseURL, _ := resolver.ResolveValue(c.BaseURL)
		baseURL = cmp.Or(baseURL, "https://api.anthropic.com/v1")

		switch providerID {
		case catwalk.InferenceKimiCoding:
			testURL = baseURL + "/v1/models"
		default:
			testURL = baseURL + "/models"
		}

		headers["x-api-key"] = apiKey
		headers["anthropic-version"] = "2023-06-01"
	case catwalk.TypeGoogle:
		baseURL, _ := resolver.ResolveValue(c.BaseURL)
		baseURL = cmp.Or(baseURL, "https://generativelanguage.googleapis.com")
		testURL = baseURL + "/v1beta/models?key=" + url.QueryEscape(apiKey)
	case catwalk.TypeBedrock:
		// NOTE: Bedrock has a `/foundation-models` endpoint that we could in
		// theory use, but apparently the authorization is region-specific,
		// so it's not so trivial.
		if strings.HasPrefix(apiKey, "ABSK") { // Bedrock API keys
			return nil
		}
		return errors.New("not a valid bedrock api key")
	case catwalk.TypeVercel:
		// NOTE: Vercel does not validate API keys on the `/models` endpoint.
		if strings.HasPrefix(apiKey, "vck_") { // Vercel API keys
			return nil
		}
		return errors.New("not a valid vercel api key")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, "GET", testURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request for provider %s: %w", c.ID, err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for k, v := range c.ExtraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to create request for provider %s: %w", c.ID, err)
	}
	defer resp.Body.Close()

	switch providerID {
	case catwalk.InferenceProviderZAI:
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("failed to connect to provider %s: %s", c.ID, resp.Status)
		}
	default:
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("failed to connect to provider %s: %s", c.ID, resp.Status)
		}
	}
	return nil
}

// resolveEnvs expands every value in envs through the given resolver
// and returns a fresh "KEY=value" slice sorted by key. The input map is
// not mutated. On the first resolution failure it returns nil and an
// error identifying the offending variable; the inner resolver error is
// already sanitized by ResolveValue and is wrapped with %w.
func resolveEnvs(envs map[string]string, r VariableResolver) ([]string, error) {
	if len(envs) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(envs))
	for k := range envs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	res := make([]string, 0, len(envs))
	for _, k := range keys {
		v, err := r.ResolveValue(envs[k])
		if err != nil {
			return nil, fmt.Errorf("env %s: %w", k, err)
		}
		res = append(res, fmt.Sprintf("%s=%s", k, v))
	}
	return res, nil
}

func ptrValOr[T any](t *T, el T) T {
	if t == nil {
		return el
	}
	return *t
}

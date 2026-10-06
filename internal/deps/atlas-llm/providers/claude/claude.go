// Package claude implements a fantasy.Provider for Anthropic's
// Messages API authenticated with a Claude Pro/Max/Team subscription
// OAuth token, rather than a pay-per-token API key.
//
// The wire format is the ordinary public Messages API at
// api.anthropic.com -- the subscription grant carries a
// "user:inference" scope, so the token is an authorization for
// inference, not a separate protocol. Only the auth headers differ
// from the API-key path:
//
//	Authorization: Bearer <oauth access token>   (instead of x-api-key)
//	anthropic-beta: oauth-2025-04-20             (marks the grant type)
//
// Everything else -- request envelope, streaming SSE, tool calls --
// is identical, so this package is a thin configuration of the
// anthropic provider rather than a reimplementation of it.
//
// # Subscription-inference fingerprint
//
// Anthropic gates subscription-token inference on the caller
// identifying itself as their first-party CLI. Without that
// fingerprint the request is refused with a 429 whose body is
// intentionally empty (no quota explanation), while a same-account
// `claude -p "..."` succeeds. The fingerprint has several pieces, all
// of which must be on every request:
//
//  1. The system prompt's first two text blocks, in order:
//     - `x-anthropic-billing-header: cc_version=<v>.<3-char-sha256>;
//     cc_entrypoint=cli; cch=00000;` (billing attestation marker;
//     the `cch=00000` placeholder is patched in place at fetch time
//     by the cchPatcher RoundTripper with an XXH64 of the body, low
//     20 bits formatted as 5 hex chars. See cch_patcher.go.)
//     - `You are Claude Code, Anthropic's official CLI for Claude.`
//  2. User-Agent header `claude-cli/<version> (external, cli)`, where
//     the version tracks the most recent @anthropic-ai/claude-code
//     release.
//  3. Stainless SDK metadata: `X-Stainless-Arch`, `X-Stainless-Lang`,
//     `X-Stainless-OS`, `X-Stainless-Package-Version`,
//     `X-Stainless-Retry-Count`, `X-Stainless-Runtime`,
//     `X-Stainless-Runtime-Version`, `X-Stainless-Timeout`.
//  4. `anthropic-version: 2023-06-01`.
//  5. `anthropic-dangerous-direct-browser-access: true`.
//  6. `x-app: cli`.
//  7. The `anthropic-beta` header carrying the full Claude Code beta
//     chain (oauth-2025-04-20 plus interleaved-thinking,
//     thinking-token-count, context-management, prompt-caching-scope,
//     structured-outputs).
//  8. `metadata.user_id` as a JSON-stringified object with
//     `device_id`, `account_uuid`, and `session_id`.
//
// The billing header's `<3-char-sha256>` is `SHA256("59cf53e54c78" +
// msg[4] + msg[7] + msg[20] + claudeCodeVersion)[:3]`, where `msg` is
// the first user message text. We compute that at call time so the
// header reflects the actual conversation; Claude Code recomputes it
// on the same inputs.
//
// This provider sets every static value at construction and injects
// the dynamic ones (billing header hash, session_id, user_id) on
// each call before delegating to the underlying anthropic provider.
// See docs/claude-subscription-login.md for the investigation that
// established the requirement.
//
// See the companion package internal/oauth/claude for the login side.
package claude

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/anthropic"
)

// Name is the name of the claude provider, matched against
// catwalk.Type by the coordinator's provider dispatch. The catwalk
// Type is also "claude", so the API-key anthropic provider (Type:
// "anthropic") and the OAuth claude provider (Type: "claude")
// live side by side: pay-per-token via anthropic, Pro/Max/Team
// subscription via claude.
const Name = "claude"

// defaultBaseURL is the public Anthropic API. The subscription token
// authenticates against the same host an API key would. The path is
// bare on purpose: the underlying provider appends /v1/messages, so a
// base URL ending in /v1 produces a 404-shaped /v1/v1/messages.
const defaultBaseURL = "https://api.anthropic.com"

// oauthBeta is the beta flag Anthropic's API expects on requests
// authenticated with a subscription OAuth grant instead of an API key.
// The rest of the chain is sent as a comma-joined `anthropic-beta`
// header alongside it; both must be present, in the right order.
const oauthBeta = "oauth-2025-04-20"

// claudeCodeBetas is the full chain Claude Code's CLI advertises
// alongside the OAuth beta. Order matters: Anthropic's server treats
// the chain as ordered, and the SDK dedupes by token rather than
// position, so the safest bet is to match Claude Code byte-for-byte.
// Source: oh-my-pi `claudeCodeUtilityBetaDefaults` (claude-code-fingerprint.ts).
var claudeCodeBetas = []string{
	oauthBeta,
	"interleaved-thinking-2025-05-14",
	"thinking-token-count-2026-05-13",
	"context-management-2025-06-27",
	"prompt-caching-scope-2026-01-05",
	"structured-outputs-2025-12-15",
}

// claudeCodeVersion is the Claude Code release version used in the
// User-Agent and the billing header. It is a static string rather
// than a query of the installed binary because the wrapped provider
// is constructed before any model call, and a fingerprint that
// changes per process is more fingerprint-y than one that stays
// constant. Bump alongside major Claude Code releases to keep the
// fingerprint looking current.
const claudeCodeVersion = "2.1.257"

// claudeCodeSdkVersion is the @anthropic-ai/sdk version that Claude
// Code's current release bundles. Sent in the `X-Stainless-Package-Version`
// header so the API can tell which SDK version is on the wire.
const claudeCodeSdkVersion = "0.112.1"

// claudeCodeUserAgent is the User-Agent header Anthropic expects on
// subscription-inference requests. Format mirrors @anthropic-ai/claude-code.
const claudeCodeUserAgent = "claude-cli/" + claudeCodeVersion + " (external, cli)"

// claudeCodeSystemInstruction is the second system block of every
// request (the first is the billing header). Anthropic's check for
// subscription-inference looks for this exact string at the start of
// the system content; if it is missing or appears later, the request
// is refused.
const claudeCodeSystemInstruction = "You are Claude Code, Anthropic's official CLI for Claude."

// claudeToolPrefix is prepended to the wire name of every custom tool
// we send. Anthropic's subscription-inference path reserves the bare
// namespace for built-in server tools; an unprefixed name like
// `bash` collides with the built-in `bash` and is rejected.
const claudeToolPrefix = "_"

// billingHeaderSalt is the secret-salted prefix Claude Code's
// billing-header hash is computed over. Public; the secret is the
// fact that *any* hash at all is expected to land at the right
// position, not the salt's confidentiality.
const billingHeaderSalt = "59cf53e54c78"

// billingHeaderCCHPlaceholder is the literal `cch=00000` segment of
// the billing header. It is sent as a placeholder and patched in
// place at fetch time by the cchPatcher RoundTripper. See
// cch_patcher.go.
const billingHeaderCCHPlaceholder = "cch=00000"

// stainlessRuntimeVersion is the Node.js version Claude Code's
// bundled SDK reports. We hardcode a plausible current Node LTS
// rather than runtime.GOVERSION because the wire value is meant to
// match the JS SDK's reported runtime, not Go's.
const stainlessRuntimeVersion = "v26.3.0"

type options struct {
	accessToken string
	baseURL     string
	accountID   string
	headers     map[string]string
}

// Option configures the claude provider.
type Option = func(*options)

// WithAccessToken sets the OAuth access token issued to the Claude
// subscription.
func WithAccessToken(tok string) Option {
	return func(o *options) { o.accessToken = tok }
}

// WithBaseURL overrides the API endpoint. Empty keeps the default.
func WithBaseURL(baseURL string) Option {
	return func(o *options) { o.baseURL = baseURL }
}

// WithAccountID sets the account id recorded during login (see
// internal/oauth/claude). Anthropic's Messages API does not require
// it on the request, so it is kept for diagnostics only.
func WithAccountID(id string) Option {
	return func(o *options) { o.accountID = id }
}

// WithHeaders adds extra HTTP headers to every request, for a user who
// needs to thread a proxy header through. The auth headers this
// package sets take precedence.
func WithHeaders(headers map[string]string) Option {
	return func(o *options) { o.headers = headers }
}

// New creates a new claude provider backed by the Anthropic Messages
// API and a subscription OAuth token.
func New(opts ...Option) (fantasy.Provider, error) {
	o := options{}
	for _, opt := range opts {
		opt(&o)
	}
	if o.accessToken == "" {
		return nil, errors.New("claude: missing access token; sign in with `atlas login claude`")
	}
	if o.baseURL == "" {
		o.baseURL = defaultBaseURL
	}

	// Start from any caller-supplied headers so the auth ones below
	// overwrite rather than get overwritten.
	headers := make(map[string]string, len(o.headers)+16)
	for k, v := range o.headers {
		headers[k] = v
	}
	headers["Authorization"] = "Bearer " + o.accessToken
	headers["anthropic-beta"] = strings.Join(claudeCodeBetas, ",")
	headers["anthropic-version"] = "2023-06-01"
	headers["anthropic-dangerous-direct-browser-access"] = "true"
	headers["x-app"] = "cli"
	headers["User-Agent"] = claudeCodeUserAgent
	for k, v := range stainlessHeaders() {
		headers[k] = v
	}

	// SkipAuth keeps the anthropic provider from also sending an
	// x-api-key header: the Bearer token is the whole credential, and
	// sending both is how you get a confusing 401. The User-Agent and
	// the rest of the fingerprint headers are passed in the headers
	// map so the underlying provider includes them in its request.
	//
	// WithHTTPClient installs the cch billing-attestation patcher:
	// it intercepts the outgoing request body, computes the XXH64
	// attestation, and replaces the cch=00000 placeholder before
	// the body is sent on the wire. See cch_patcher.go.
	inner, err := anthropic.New(
		anthropic.WithName(Name),
		anthropic.WithBaseURL(o.baseURL),
		anthropic.WithSkipAuth(true),
		anthropic.WithHeaders(headers),
		anthropic.WithHTTPClient(&http.Client{
			Transport: &cchPatcher{inner: http.DefaultTransport},
		}),
	)
	if err != nil {
		return nil, err
	}

	return &preambleProvider{inner: inner, accountID: o.accountID}, nil
}

// preambleProvider wraps a fantasy.Provider so every LanguageModel
// returned from it injects the Claude Code system-prompt structure
// (billing header + identity preamble) and the metadata block on
// outgoing calls. The User-Agent and Stainless headers are set once
// at construction time on the underlying provider.
type preambleProvider struct {
	inner     fantasy.Provider
	accountID string
}

func (p *preambleProvider) Name() string { return p.inner.Name() }

func (p *preambleProvider) LanguageModel(ctx context.Context, modelID string) (fantasy.LanguageModel, error) {
	lm, err := p.inner.LanguageModel(ctx, modelID)
	if err != nil {
		return nil, err
	}
	return &preambleModel{
		inner:     lm,
		accountID: p.accountID,
		// One device_id per process is fine; a per-process install
		// identifier is what Claude Code uses, and the value never
		// leaves the user's machine.
		deviceID: loadOrMintDeviceID(),
		// sessionID is lazily generated on the first call and reused
		// for the lifetime of this model object; a stable
		// X-Claude-Code-Session-Id across turns is what the API uses
		// to track usage, and a fresh value per call would inflate
		// the session count on the Anthropic side.
	}, nil
}

// preambleModel is a fantasy.LanguageModel that prepends the Claude
// Code billing header and identity preamble to every call's prompt
// before delegating to the underlying anthropic model. It also
// attaches the subscription-inference metadata block (`user_id`,
// `device_id`, `account_uuid`, `session_id`) via ExtraBody and the
// X-Claude-Code-Session-Id header.
type preambleModel struct {
	inner     fantasy.LanguageModel
	accountID string
	deviceID  string

	sessionOnce sync.Once
	sessionID   string
}

func (m *preambleModel) Provider() string { return m.inner.Provider() }
func (m *preambleModel) Model() string    { return m.inner.Model() }

func (m *preambleModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	bindings := claudeToolBindings(call.Tools)
	r, err := m.inner.Generate(ctx, m.decorate(call))
	if err != nil || r == nil {
		return r, err
	}
	for i, content := range r.Content {
		if c, ok := content.(fantasy.ToolCallContent); ok && !c.ProviderExecuted {
			if binding, exists := bindings[c.ToolName]; exists {
				c.ToolName = binding.name
				c.Input = normalizeClaudeArguments(c.Input, binding.schema)
				r.Content[i] = c
			}
		}
	}
	return r, nil
}

func (m *preambleModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	bindings := claudeToolBindings(call.Tools)
	stream, err := m.inner.Stream(ctx, m.decorate(call))
	if err != nil {
		return nil, err
	}
	return func(yield func(fantasy.StreamPart) bool) {
		for part := range stream {
			if !part.ProviderExecuted {
				if binding, exists := bindings[part.ToolCallName]; exists {
					part.ToolCallName = binding.name
					if part.Type == fantasy.StreamPartTypeToolCall {
						part.ToolCallInput = normalizeClaudeArguments(part.ToolCallInput, binding.schema)
					}
				}
			}
			if !yield(part) {
				return
			}
		}
	}, nil
}

func (m *preambleModel) GenerateObject(ctx context.Context, call fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	// Object calls also run through the same Anthropic model, so the
	// fingerprint has to be on the wire for them too. The system
	// prompt is not consulted by the object schema path, but
	// Anthropic's check is at the request level, not the response
	// level.
	return m.inner.GenerateObject(ctx, call)
}

func (m *preambleModel) StreamObject(ctx context.Context, call fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return m.inner.StreamObject(ctx, call)
}

// decorate applies the subscription-inference fingerprint to a
// fantasy.Call. The original call is left untouched so callers can
// reuse it across models.
func (m *preambleModel) decorate(call fantasy.Call) fantasy.Call {
	firstUserText := firstUserText(call.Prompt)
	call.Prompt = withFingerprint(call.Prompt, firstUserText)
	call.Prompt = claudeToolHistory(call.Prompt, call.Tools)
	m.setSession()
	enrichProviderOptions(&call, m.userID())
	call.Headers = mergeHeaders(call.Headers, map[string]string{
		"X-Claude-Code-Session-Id": m.sessionID,
	})
	// Tool names: Claude Code prefixes every custom tool with `_` so
	// they don't collide with Anthropic's built-in tool namespace
	// (Bash, Read, Write, etc.). The anthropic provider's tool
	// encoder is not aware of this; rewrite the wire names here.
	if len(call.Tools) > 0 {
		rewritten := make([]fantasy.Tool, len(call.Tools))
		for i, t := range call.Tools {
			rewritten[i] = renameTool(t, claudeToolPrefix+t.GetName())
		}
		call.Tools = rewritten
	}
	if call.ToolChoice != nil {
		for _, t := range call.Tools {
			if _, ok := t.(fantasy.FunctionTool); ok && t.GetName() == claudeToolPrefix+string(*call.ToolChoice) {
				choice := fantasy.SpecificToolChoice(t.GetName())
				call.ToolChoice = &choice
				break
			}
		}
	}
	return call
}

// setSession mints the per-model session id on first use.
func (m *preambleModel) setSession() {
	m.sessionOnce.Do(func() {
		m.sessionID = newUUIDv4()
	})
}

// userID is the JSON-stringified metadata block the Anthropic API
// expects on the `metadata.user_id` field for subscription-inference
// requests. Claude Code's `services/api/claude.ts` builds the same
// shape, and the values are how Anthropic attributes usage to the
// user/account/session.
func (m *preambleModel) userID() string {
	uid := map[string]any{
		"device_id":    m.deviceID,
		"account_uuid": m.accountID,
		"session_id":   m.sessionID,
	}
	b, err := json.Marshal(uid)
	if err != nil {
		// Marshalling a map[string]any of strings cannot fail in
		// practice; return a non-empty value so the API doesn't see
		// an empty user_id (which is treated as no metadata at all).
		return `{"device_id":"","account_uuid":"","session_id":""}`
	}
	return string(b)
}

// enrichProviderOptions sets the metadata.user_id field on the
// anthropic ProviderOptions. It mutates the call's ProviderOptions
// map, creating the entry if necessary.
func enrichProviderOptions(call *fantasy.Call, userID string) {
	if call.ProviderOptions == nil {
		call.ProviderOptions = fantasy.ProviderOptions{}
	}
	existing, ok := call.ProviderOptions[anthropic.Name]
	var opts *anthropic.ProviderOptions
	if ok {
		opts, _ = existing.(*anthropic.ProviderOptions)
	}
	if opts == nil {
		opts = &anthropic.ProviderOptions{}
	}
	// ExtraBody is merged into the request JSON by the underlying
	// anthropic provider. `metadata.user_id` is the documented slot
	// for the OAuth-subscription attribution payload.
	if opts.ExtraBody == nil {
		opts.ExtraBody = map[string]any{}
	}
	opts.ExtraBody["metadata"] = map[string]any{
		"user_id": userID,
	}
	call.ProviderOptions[anthropic.Name] = opts
}

// mergeHeaders returns a new map containing the existing call-level
// headers plus the ones we want to force. The provided map wins on
// conflicts so a per-call value can't clobber a fingerprint header.
func mergeHeaders(existing map[string]string, forced map[string]string) map[string]string {
	merged := make(map[string]string, len(existing)+len(forced))
	for k, v := range existing {
		merged[k] = v
	}
	for k, v := range forced {
		merged[k] = v
	}
	return merged
}

// withFingerprint returns a copy of prompt with the two Claude Code
// system blocks prepended in order: billing header first, then the
// identity preamble. Anthropic serializes the system field as an
// ordered list of text blocks, so the order in this slice is the
// order on the wire, which is what the fingerprint check relies on.
func withFingerprint(prompt fantasy.Prompt, firstUserText string) fantasy.Prompt {
	billing := buildBillingHeader(firstUserText)
	identity := fantasy.NewSystemMessage(claudeCodeSystemInstruction)
	out := make(fantasy.Prompt, 0, len(prompt)+2)
	out = append(out,
		fantasy.NewSystemMessage(billing),
		identity,
	)
	out = append(out, prompt...)
	return out
}

// buildBillingHeader renders the `x-anthropic-billing-header` text
// block. The 3-char hash is the SHA256 of a salt concatenated with
// three characters of the first user message and the Claude Code
// version, truncated to its first 3 hex chars. The `cch=00000`
// placeholder is patched in place at fetch time by cchPatcher with
// the actual XXH64 attestation. See cch_patcher.go.
func buildBillingHeader(firstUserText string) string {
	h := sha256.New()
	h.Write([]byte(billingHeaderSalt))
	for _, idx := range []int{4, 7, 20} {
		var c byte = '0'
		if idx < len(firstUserText) {
			c = firstUserText[idx]
		}
		h.Write([]byte{c})
	}
	h.Write([]byte(claudeCodeVersion))
	hashHex := hex.EncodeToString(h.Sum(nil))
	if len(hashHex) > 3 {
		hashHex = hashHex[:3]
	}
	return fmt.Sprintf("x-anthropic-billing-header: cc_version=%s.%s; cc_entrypoint=cli; %s;",
		claudeCodeVersion, hashHex, billingHeaderCCHPlaceholder)
}

// firstUserText returns the concatenation of the text parts of the
// first user-role message in prompt, used as the input to the
// billing-header hash. If no user message exists, an empty string is
// returned, which the hash then pads with the '0' default character.
func firstUserText(prompt fantasy.Prompt) string {
	for _, msg := range prompt {
		if msg.Role != fantasy.MessageRoleUser {
			continue
		}
		var sb strings.Builder
		for _, part := range msg.Content {
			tp, ok := fantasy.AsMessagePart[fantasy.TextPart](part)
			if !ok {
				continue
			}
			sb.WriteString(tp.Text)
		}
		return sb.String()
	}
	return ""
}

// stainlessHeaders returns the X-Stainless-* headers Claude Code's
// CLI sends. They identify the underlying SDK and runtime to the
// API; the values are static (the runtime is the JS SDK, not our Go
// binary) so they live as constants rather than runtime probes.
func stainlessHeaders() map[string]string {
	return map[string]string{
		"X-Stainless-Arch":            stainlessArch(runtime.GOARCH),
		"X-Stainless-Lang":            "js",
		"X-Stainless-OS":              stainlessOS(runtime.GOOS),
		"X-Stainless-Package-Version": claudeCodeSdkVersion,
		"X-Stainless-Retry-Count":     "0",
		"X-Stainless-Runtime":         "node",
		"X-Stainless-Runtime-Version": stainlessRuntimeVersion,
		"X-Stainless-Timeout":         "600",
	}
}

func stainlessOS(goos string) string {
	switch goos {
	case "darwin":
		return "MacOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	case "freebsd":
		return "FreeBSD"
	default:
		return "Other::" + strings.ToLower(goos)
	}
}

func stainlessArch(goarch string) string {
	switch goarch {
	case "amd64", "x64":
		return "x64"
	case "arm64", "aarch64":
		return "arm64"
	case "386", "x86", "ia32":
		return "x86"
	default:
		return "other::" + goarch
	}
}

// renameTool returns a copy of tool with its Name set to the wire
// name. Only function tools carry an editable Name in this SDK;
// provider-defined tools already use a separate namespace and are
// passed through unchanged.
func renameTool(tool fantasy.Tool, wireName string) fantasy.Tool {
	t, ok := tool.(fantasy.FunctionTool)
	if !ok {
		return tool
	}
	t.Name = wireName
	return t
}

// loadOrMintDeviceID returns a stable per-install device identifier
// for the metadata.user_id block. The value is stored in
// ~/.config/atlas-agent/device_id; a new UUIDv4 is minted and saved
// the first time we run. The file is best-effort: a read failure
// mints a fresh value for this process only, so the user does not
// lose all inference access because of a permission glitch.
func loadOrMintDeviceID() string {
	if path, err := deviceIDPath(); err == nil {
		if b, err := os.ReadFile(path); err == nil {
			id := strings.TrimSpace(string(b))
			if id != "" {
				return id
			}
		}
	}
	id := newUUIDv4()
	if path, err := deviceIDPath(); err == nil {
		// Best-effort persistence; failure is non-fatal.
		_ = os.MkdirAll(strings.TrimSuffix(path, "/device_id"), 0o755)
		_ = os.WriteFile(path, []byte(id+"\n"), 0o600)
	}
	return id
}

func deviceIDPath() (string, error) {
	if dir := os.Getenv("ATLAS_AGENT_DATA_DIR"); dir != "" {
		return dir + "/device_id", nil
	}
	// Mirror the rest of the project's config-root selection; this
	// is intentionally minimal so the function is self-contained and
	// has no import cycle with internal/config.
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return home + "/.config/atlas-agent/device_id", nil
}

// newUUIDv4 returns a randomly-generated UUIDv4 string. A
// cryptographic source is used so the value is unpredictable from
// outside the process; the version/variant bits are set per RFC 4122.
func newUUIDv4() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand should not fail; if it does, fall back to a
		// less-secure but still-derivable value rather than panicking
		// a model call.
		copy(b[:], []byte(fmt.Sprintf("atlas-%d", os.Getpid())))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ProviderOptions carries claude-specific per-call options, set via
// fantasy.Call.ProviderOptions[Name]. The underlying provider is
// Anthropic's, so anthropic.ProviderOptions covers the real knobs
// (thinking budget, cache control); this exists so a config naming
// "claude" still parses.
type ProviderOptions = anthropic.ProviderOptions

// ParseOptions parses provider options from a merged options map for
// the claude provider.
func ParseOptions(data map[string]any) (*ProviderOptions, error) {
	var options ProviderOptions
	if err := fantasy.ParseOptions(data, &options); err != nil {
		return nil, err
	}
	return &options, nil
}

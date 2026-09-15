// Package muse implements a fantasy.Provider for Meta's Messages
// API authenticated with a Muse Code subscription's minted Model
// API key, rather than a pay-per-token dashboard key.
//
// The wire format is Meta's Anthropic-compatible Messages API at
// api.meta.ai: requests shaped for Anthropic's /v1/messages run
// unchanged, authenticated with the subscription credential instead of
// x-api-key, so this package is a thin configuration of the
// anthropic provider rather than a reimplementation of it. Meta
// requires max_tokens on every request; the embedded catalog sets
// it on each Muse Spark model so the wire never omits it.
//
// See the companion package internal/oauth/muse for the login side.
package muse

import (
	"errors"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/anthropic"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Name is the name of the muse provider, matched against
// catwalk.Type by the coordinator's provider dispatch.
const Name = "muse"

// defaultBaseURL is the Meta Model API host. The path is bare on
// purpose: the underlying provider appends /v1/messages, so a base
// URL ending in /v1 produces a 404-shaped /v1/v1/messages.
const defaultBaseURL = "https://api.meta.ai"

type options struct {
	accessToken string
	baseURL     string
	headers     map[string]string
	httpClient  option.HTTPClient
}

// Option configures the muse provider.
type Option = func(*options)

// WithAccessToken sets the minted Model API key issued to the Muse
// Code subscription.
func WithAccessToken(tok string) Option {
	return func(o *options) { o.accessToken = tok }
}

// WithBaseURL overrides the API endpoint. Empty keeps the default.
func WithBaseURL(baseURL string) Option {
	return func(o *options) { o.baseURL = baseURL }
}

// WithHeaders adds extra HTTP headers to every request, for a user
// who needs to thread a proxy header through. The auth header this
// package sets takes precedence.
func WithHeaders(headers map[string]string) Option {
	return func(o *options) { o.headers = headers }
}

// WithHTTPClient overrides the HTTP client, used for debug logging.
func WithHTTPClient(client option.HTTPClient) Option {
	return func(o *options) { o.httpClient = client }
}

// New creates a new muse provider backed by Meta's Messages API and
// a subscription Model API key.
func New(opts ...Option) (fantasy.Provider, error) {
	o := options{}
	for _, opt := range opts {
		opt(&o)
	}
	if o.accessToken == "" {
		return nil, errors.New("muse: missing access token; sign in with `atlas login muse`")
	}
	if o.baseURL == "" {
		o.baseURL = defaultBaseURL
	}

	// Start from any caller-supplied headers so the auth one below
	// overwrites rather than gets overwritten.
	headers := make(map[string]string, len(o.headers)+1)
	for k, v := range o.headers {
		headers[k] = v
	}
	headers["Authorization"] = "Bearer " + o.accessToken

	// SkipAuth keeps the anthropic provider from also sending an
	// x-api-key header: the subscription credential travels alone, and
	// sending both is how you get a confusing 401.
	anthropicOpts := []anthropic.Option{
		anthropic.WithName(Name),
		anthropic.WithBaseURL(o.baseURL),
		anthropic.WithSkipAuth(true),
		anthropic.WithHeaders(headers),
	}
	if o.httpClient != nil {
		anthropicOpts = append(anthropicOpts, anthropic.WithHTTPClient(o.httpClient))
	}
	return anthropic.New(anthropicOpts...)
}

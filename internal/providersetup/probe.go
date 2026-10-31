package providersetup

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
)

// Limits on what a probe accepts from an endpoint.
const (
	probeTimeout     = 15 * time.Second
	maxResponseBytes = 4 << 20
	// MaxModels is the most models one provider may list. A larger list is
	// refused rather than truncated, so the user is never handed a silently
	// partial catalog.
	MaxModels        = 500
	maxRedirects     = 3
	errorSnippetSize = 200
)

// errCrossHostRedirect is returned when an endpoint redirects somewhere
// the API key must not follow.
var errCrossHostRedirect = errors.New("redirected to a different host")

// ProbeResult is what a successful model-listing request found.
type ProbeResult struct {
	// BaseURL is the base URL that worked; it differs from the input when
	// the /v1 fallback was needed.
	BaseURL string
	Models  []catwalk.Model
	Note    string
}

// newHTTPClient builds the client probes use. It follows redirects only
// within the same host (and never from https down to http), because the
// key travels in a header the standard library does not strip on a
// cross-host redirect for anything but Authorization.
func newHTTPClient(rt http.RoundTripper) *http.Client {
	if rt == nil {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		tr.Proxy = http.ProxyFromEnvironment
		rt = tr
	}
	return &http.Client{
		Transport: rt,
		Timeout:   probeTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return errors.New("too many redirects")
			}
			first := via[0].URL
			if !strings.EqualFold(req.URL.Host, first.Host) {
				return fmt.Errorf("%w (%s)", errCrossHostRedirect, req.URL.Host)
			}
			if first.Scheme == "https" && req.URL.Scheme != "https" {
				return fmt.Errorf("%w: https downgraded to http", errCrossHostRedirect)
			}
			return nil
		},
	}
}

// Probe lists the models the endpoint offers, authenticating the way the
// provider type expects. It tries base_url+"/models" and, when that is a
// 404 and the URL has no /v1, base_url+"/v1/models" once.
func Probe(ctx context.Context, rt http.RoundTripper, typ catwalk.Type, baseURL, apiKey string) (*ProbeResult, error) {
	client := newHTTPClient(rt)

	tried := []string{}
	try := func(base string) ([]byte, int, string, error) {
		endpoint, headers, ok := config.ModelsEndpoint(typ, base, apiKey)
		if !ok {
			return nil, 0, "", fmt.Errorf("provider type %q has no model-listing route", typ)
		}
		tried = append(tried, endpoint)
		return fetch(ctx, client, endpoint, headers, apiKey)
	}

	body, status, snippet, err := try(baseURL)
	if err != nil {
		return nil, err
	}
	result := &ProbeResult{BaseURL: baseURL}

	if status == http.StatusNotFound && typ != catwalk.TypeGoogle && !strings.HasSuffix(baseURL, "/v1") {
		fallback := baseURL + "/v1"
		fbBody, fbStatus, fbSnippet, fbErr := try(fallback)
		if fbErr != nil {
			return nil, fbErr
		}
		if fbStatus == http.StatusOK {
			body, status, snippet = fbBody, fbStatus, fbSnippet
			result.BaseURL = fallback
			result.Note = fmt.Sprintf("%s has no /models route but %s does; the base URL was corrected to %s.", baseURL, fallback+"/models", fallback)
		} else if fbStatus != http.StatusNotFound {
			// The /v1 route exists but refused us (for example a bad
			// key), which is more informative than the first 404.
			body, status, snippet = fbBody, fbStatus, fbSnippet
			result.BaseURL = fallback
		}
	}

	if status != http.StatusOK {
		return nil, statusError(status, tried, snippet)
	}

	models, err := parseModels(typ, body)
	if err != nil {
		return nil, fmt.Errorf("%s answered 200 but the body is not a %s model list: %w", tried[len(tried)-1], typ, err)
	}
	if len(models) > MaxModels {
		return nil, fmt.Errorf("%s lists %d models, more than the limit of %d; pass the few you need as models with discover_models false", tried[len(tried)-1], len(models), MaxModels)
	}
	result.Models = models
	return result, nil
}

// fetch performs one GET and returns the (size-capped) body, the status
// code and a short, sanitized snippet of the body for error messages.
func fetch(ctx context.Context, client *http.Client, endpoint string, headers map[string]string, apiKey string) ([]byte, int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, "", fmt.Errorf("could not build the request: %s", Redact(err.Error(), apiKey))
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, "", networkError(err, endpoint, apiKey)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, 0, "", fmt.Errorf("reading the response from %s failed: %s", endpoint, Redact(err.Error(), apiKey))
	}
	if len(data) > maxResponseBytes {
		return nil, 0, "", fmt.Errorf("%s returned more than %d MB; refusing to read it. Is this really a model-list endpoint?", endpoint, maxResponseBytes>>20)
	}
	return data, resp.StatusCode, snippetOf(data, apiKey), nil
}

// snippetOf makes a short single-line excerpt of an error body that is
// safe to show: control characters removed, the key redacted.
func snippetOf(data []byte, apiKey string) string {
	s := Redact(string(data), apiKey)
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > errorSnippetSize {
		s = s[:errorSnippetSize] + "..."
	}
	return s
}

func statusError(status int, tried []string, snippet string) error {
	where := tried[len(tried)-1]
	var msg string
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		msg = fmt.Sprintf("the provider rejected the API key (HTTP %d from %s): the key is wrong, expired, or lacks access to the model list", status, where)
	case status == http.StatusNotFound:
		msg = fmt.Sprintf("HTTP 404: no model list at %s: the base URL is probably wrong (tried: %s). Check the provider's API documentation for the right base URL", where, strings.Join(tried, ", "))
	case status == http.StatusTooManyRequests:
		msg = fmt.Sprintf("the provider is rate limiting requests (HTTP 429 from %s); try again shortly", where)
	case status >= 500:
		msg = fmt.Sprintf("the provider failed (HTTP %d from %s); try again later", status, where)
	default:
		msg = fmt.Sprintf("unexpected HTTP %d from %s", status, where)
	}
	if snippet != "" {
		msg += ". Response: " + snippet
	}
	return errors.New(msg)
}

func networkError(err error, endpoint, apiKey string) error {
	host := endpoint
	if u, perr := url.Parse(endpoint); perr == nil {
		host = u.Host
	}
	var (
		dnsErr  *net.DNSError
		tlsErr  *tls.CertificateVerificationError
		timeout interface{ Timeout() bool }
	)
	switch {
	case errors.Is(err, errCrossHostRedirect):
		return fmt.Errorf("%s redirected to another host, and the API key is never sent to a host you did not name (%s). Use the redirect target as base_url if you trust it", host, Redact(err.Error(), apiKey))
	case errors.As(err, &dnsErr):
		return fmt.Errorf("could not resolve the host %q: check the spelling of base_url and your network", host)
	case errors.As(err, &tlsErr):
		return fmt.Errorf("the TLS certificate of %q could not be verified: %s", host, Redact(tlsErr.Error(), apiKey))
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &timeout) && timeout.Timeout():
		return fmt.Errorf("%s did not answer within %d seconds", host, int(probeTimeout.Seconds()))
	case errors.Is(err, context.Canceled):
		return errors.New("cancelled")
	}
	return fmt.Errorf("could not reach %s: %s", host, Redact(err.Error(), apiKey))
}

// --- Parsing ---

type listEntry struct {
	// OpenAI / Anthropic style.
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	// Google style.
	Name             string   `json:"name"`
	GoogleDisplay    string   `json:"displayName"`
	InputTokenLimit  int64    `json:"inputTokenLimit"`
	OutputTokenLimit int64    `json:"outputTokenLimit"`
	Methods          []string `json:"supportedGenerationMethods"`
	// Context and output limits, when the endpoint states them. Names
	// differ by server; none is invented when absent.
	MaxInputTokens  int64 `json:"max_input_tokens"`
	MaxTokens       int64 `json:"max_tokens"`
	ContextWindow   int64 `json:"context_window"`
	ContextLength   int64 `json:"context_length"`
	MaxModelLen     int64 `json:"max_model_len"`
	MaxOutputTokens int64 `json:"max_output_tokens"`
	MaxCompletion   int64 `json:"max_completion_tokens"`
}

func parseModels(typ catwalk.Type, body []byte) ([]catwalk.Model, error) {
	var envelope struct {
		Data   []listEntry `json:"data"`
		Models []listEntry `json:"models"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, errors.New("not valid JSON of the expected shape")
	}
	entries := envelope.Data
	if typ == catwalk.TypeGoogle {
		entries = envelope.Models
	}
	if entries == nil {
		return nil, errors.New("no model list found in the response")
	}

	seen := map[string]bool{}
	models := make([]catwalk.Model, 0, len(entries))
	for _, e := range entries {
		id, display := e.ID, e.DisplayName
		if typ == catwalk.TypeGoogle {
			if len(e.Methods) > 0 && !containsFold(e.Methods, "generateContent") {
				continue
			}
			id = strings.TrimPrefix(e.Name, "models/")
			display = e.GoogleDisplay
		}
		id = strings.TrimSpace(id)
		if id == "" || seen[id] || !validModelID(id) {
			continue
		}
		seen[id] = true

		m := catwalk.Model{ID: id, Name: id}
		if display != "" && len(display) <= 200 {
			m.Name = display
		}
		m.ContextWindow = firstPositive(e.MaxInputTokens, e.ContextWindow, e.ContextLength, e.MaxModelLen, e.InputTokenLimit)
		m.DefaultMaxTokens = firstPositive(e.MaxOutputTokens, e.MaxCompletion, e.OutputTokenLimit)
		if typ == catwalk.TypeAnthropic && m.DefaultMaxTokens == 0 {
			m.DefaultMaxTokens = firstPositive(e.MaxTokens)
		}
		models = append(models, m)
	}
	return models, nil
}

func validModelID(id string) bool {
	if len(id) > 200 {
		return false
	}
	for _, r := range id {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func containsFold(list []string, want string) bool {
	for _, s := range list {
		if strings.EqualFold(s, want) {
			return true
		}
	}
	return false
}

func firstPositive(vals ...int64) int64 {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}

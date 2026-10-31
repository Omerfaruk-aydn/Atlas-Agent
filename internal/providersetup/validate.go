// Package providersetup adds a custom model provider to the configuration
// in one validated step: check the inputs, ask for the API key without
// letting it reach the model, probe the endpoint, discover its models and
// only then write.
//
// Both the atlas_config tool and the `provider add` command call Setup, so
// the two cannot drift apart on what counts as a valid provider.
package providersetup

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/embedded"
)

// Provider API types a custom provider can use. These are the types the
// agent coordinator can build a client for and that have a known
// model-listing route.
var supportedTypes = []catwalk.Type{
	catwalk.TypeOpenAICompat, catwalk.TypeOpenAI, catwalk.TypeAnthropic, catwalk.TypeGoogle,
}

var (
	idPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	envNamePatten = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
)

// SupportedTypes lists the accepted provider types for messages.
func SupportedTypes() []string {
	out := make([]string, len(supportedTypes))
	for i, t := range supportedTypes {
		out[i] = string(t)
	}
	return out
}

// ParseType normalizes a type name; empty selects openai-compat.
func ParseType(raw string) (catwalk.Type, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return catwalk.TypeOpenAICompat, nil
	}
	for _, t := range supportedTypes {
		if string(t) == raw {
			return t, nil
		}
	}
	return "", fmt.Errorf("unsupported provider type %q; use one of: %s", raw, strings.Join(SupportedTypes(), ", "))
}

// ValidateID checks a provider id. Ids become config keys and path
// segments, so only lowercase letters, digits, '-' and '_' are allowed.
func ValidateID(id string) error {
	if id == "" {
		return errors.New("id is required")
	}
	if !idPattern.MatchString(id) {
		return fmt.Errorf("invalid id %q: use only lowercase letters, digits, '-' and '_' (start with a letter or digit, at most 64 characters); no dots, spaces or uppercase", id)
	}
	for _, p := range embedded.GetAll() {
		if strings.EqualFold(string(p.ID), id) {
			return fmt.Errorf("id %q is reserved for a built-in provider; pick another, for example %q", id, id+"-custom")
		}
	}
	return nil
}

// ValidateEnvName checks the name of an environment variable.
func ValidateEnvName(name string) error {
	name = strings.TrimPrefix(strings.TrimSpace(name), "$")
	if !envNamePatten.MatchString(name) {
		return fmt.Errorf("invalid api_key_env %q: give the variable's name (letters, digits, '_'), not its value", name)
	}
	return nil
}

// NormalizeEnvName strips a leading '$' and whitespace.
func NormalizeEnvName(name string) string {
	return strings.TrimPrefix(strings.TrimSpace(name), "$")
}

// isLoopbackHost reports whether host is a local development address.
func isLoopbackHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// ValidateBaseURL parses and checks the base URL the API key will be sent
// to, and returns it without a trailing slash.
func ValidateBaseURL(raw string, allowInsecure bool) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("base_url is required: find the provider's API base URL in its documentation (do not guess it)")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("base_url is not a valid URL: %v", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !isLoopbackHost(u.Hostname()) && !allowInsecure {
			return nil, fmt.Errorf("base_url %q uses plain http, which would send the API key unencrypted; use https (plain http is accepted only for localhost unless allow_insecure is set)", u.Redacted())
		}
	case "":
		return nil, errors.New("base_url needs a scheme such as https://")
	default:
		return nil, fmt.Errorf("base_url scheme %q is not allowed; use https", u.Scheme)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Hostname() == "" {
		return nil, errors.New("base_url has no host")
	}
	if u.User != nil {
		return nil, errors.New("base_url must not contain credentials (user:password@); the API key is supplied separately")
	}
	if u.Fragment != "" || strings.Contains(raw, "#") {
		return nil, errors.New("base_url must not contain a #fragment")
	}
	if u.RawQuery != "" || strings.Contains(raw, "?") {
		return nil, errors.New("base_url must not contain a ?query; it is the API root, for example https://api.example.com/v1")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u, nil
}

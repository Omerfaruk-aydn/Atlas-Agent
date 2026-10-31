package config

import (
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
)

// ModelsEndpoint returns the model-listing URL and authentication headers
// for a provider of the given API type, the same request
// [ProviderConfig.TestConnection] makes for a generic provider of that
// type. ok is false for types that have no model-listing route this
// function knows about.
//
// The API key is only ever put in a header, never in the URL, so a
// transport error that quotes the URL cannot quote the key. (Google also
// accepts ?key=; TestConnection still uses that form, the path is the
// same.)
func ModelsEndpoint(t catwalk.Type, baseURL, apiKey string) (endpoint string, headers map[string]string, ok bool) {
	baseURL = strings.TrimRight(baseURL, "/")
	switch t {
	case catwalk.TypeOpenAI, catwalk.TypeOpenAICompat, catwalk.TypeOpenRouter:
		return baseURL + "/models", map[string]string{"Authorization": "Bearer " + apiKey}, true
	case catwalk.TypeAnthropic:
		return baseURL + "/models", map[string]string{
			"x-api-key":         apiKey,
			"anthropic-version": "2023-06-01",
		}, true
	case catwalk.TypeGoogle:
		return baseURL + "/v1beta/models", map[string]string{"x-goog-api-key": apiKey}, true
	}
	return "", nil, false
}

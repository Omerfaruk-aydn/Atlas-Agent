package openai

import (
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/internal/httpheaders"
	"github.com/openai/openai-go/v3/option"
)

// callUARequestOptions returns per-request options that override the
// client-level User-Agent header when the Call carries agent-level UA
// settings.
//
// When noDefaultUA is true the SDK's own User-Agent is preserved and no
// override is applied (needed for providers like OpenRouter, which reject
// User-Agent headers they don't expect).
func callUARequestOptions(call fantasy.Call) []option.RequestOption {
	if ua, ok := httpheaders.CallUserAgent(call.UserAgent); ok {
		return []option.RequestOption{option.WithHeader("User-Agent", ua)}
	}
	return nil
}

// objectCallUARequestOptions returns per-request options that override the
// client-level User-Agent header when the ObjectCall carries agent-level UA
// settings.
func objectCallUARequestOptions(call fantasy.ObjectCall) []option.RequestOption {
	if ua, ok := httpheaders.CallUserAgent(call.UserAgent); ok {
		return []option.RequestOption{option.WithHeader("User-Agent", ua)}
	}
	return nil
}

func callHeadersRequestOptions(call fantasy.Call) []option.RequestOption {
	headers, ok := httpheaders.CallHeaders(call.Headers)
	if !ok {
		return nil
	}
	opts := make([]option.RequestOption, 0, len(headers))
	for k, v := range headers {
		opts = append(opts, option.WithHeader(k, v))
	}
	return opts
}

func objectCallHeadersRequestOptions(call fantasy.ObjectCall) []option.RequestOption {
	headers, ok := httpheaders.CallHeaders(call.Headers)
	if !ok {
		return nil
	}
	opts := make([]option.RequestOption, 0, len(headers))
	for k, v := range headers {
		opts = append(opts, option.WithHeader(k, v))
	}
	return opts
}

// sessionCacheKey derives a prompt cache key from the session affinity
// header the agent sends with every call of a conversation. OpenAI routes a
// request to a machine by its prefix and the cache key; without a stable
// key, requests of one conversation land on different machines and miss
// the cache that earlier steps of the same conversation filled. The
// official Codex client sends its conversation id for the same reason.
func sessionCacheKey(headers map[string]string) (string, bool) {
	for k, v := range headers {
		if strings.EqualFold(k, "x-session-affinity") && v != "" {
			return v, true
		}
	}
	return "", false
}

// supportsPromptCacheKey reports whether the endpoint is OpenAI's own or the
// ChatGPT Codex backend. Other OpenAI-compatible servers may reject
// parameters they do not know, so they never get a key unless configured.
func supportsPromptCacheKey(baseURL, provider string) bool {
	return isChatGPTBackend(baseURL, provider) || baseURL == "" || strings.Contains(baseURL, "api.openai.com")
}

// chatGPTAffinityRequestOptions adds the session id headers the ChatGPT Codex
// backend routes prompt caching by. A prompt_cache_key in the body alone does
// not reach a warm cache there: clients that sent only the key read a few
// percent of their input from cache, those that also sent the header about
// ninety-nine. The value matches the cache key, as the official client does.
// A header the caller set itself is left alone.
func chatGPTAffinityRequestOptions(baseURL, provider string, call fantasy.Call) []option.RequestOption {
	if !isChatGPTBackend(baseURL, provider) {
		return nil
	}
	key, ok := sessionCacheKey(call.Headers)
	if !ok {
		return nil
	}
	for k := range call.Headers {
		if strings.EqualFold(k, "session_id") || strings.EqualFold(k, "session-id") {
			return nil
		}
	}
	return []option.RequestOption{option.WithHeader("session_id", key), option.WithHeader("session-id", key)}
}

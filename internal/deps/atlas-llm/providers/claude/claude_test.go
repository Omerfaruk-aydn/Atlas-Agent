package claude

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestNewRequiresAnAccessToken(t *testing.T) {
	_, err := New()

	require.Error(t, err)
	require.Contains(t, err.Error(), "atlas login claude", "the error should say how to get a token, not just that one is missing")
}

func TestProviderReportsItsOwnName(t *testing.T) {
	p, err := New(WithAccessToken("tok"))

	require.NoError(t, err)
	require.Equal(t, Name, p.Name(), "the provider must identify as claude, not anthropic, so config and model roles resolve to the right entry")
}

// A subscription grant authenticates with a Bearer token and the OAuth
// beta flag. Sending x-api-key alongside it is how you get a confusing
// 401, so SkipAuth must keep the API-key header off the wire entirely.
func TestRequestsCarryBearerAuthAndNoAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "unrelated-api-key")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "unrelated-auth-token")
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p, err := New(WithAccessToken("tok-abc"), WithBaseURL(srv.URL))
	require.NoError(t, err)

	model, err := p.LanguageModel(context.Background(), "claude-sonnet-5")
	require.NoError(t, err)

	_, err = model.Generate(context.Background(), fantasy.Call{
		Prompt: []fantasy.Message{
			fantasy.NewUserMessage("hello"),
		},
	})
	require.NoError(t, err)

	require.Equal(t, "Bearer tok-abc", got.Get("Authorization"))
	beta := got.Get("anthropic-beta")
	require.Contains(t, beta, oauthBeta, "the OAuth beta must be on the wire")
	require.Empty(t, got.Get("x-api-key"), "the Bearer token is the whole credential")
}

// Caller-supplied headers are for proxies and the like; they must not
// be able to clobber the credential.
func TestAuthHeadersWinOverCallerHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p, err := New(
		WithAccessToken("real-token"),
		WithBaseURL(srv.URL),
		WithHeaders(map[string]string{
			"Authorization": "Bearer someone-elses-token",
			"X-Proxy-Tag":   "keep-me",
		}),
	)
	require.NoError(t, err)

	model, err := p.LanguageModel(context.Background(), "claude-sonnet-5")
	require.NoError(t, err)
	_, err = model.Generate(context.Background(), fantasy.Call{
		Prompt: []fantasy.Message{
			fantasy.NewUserMessage("hello"),
		},
	})
	require.NoError(t, err)

	require.Equal(t, "Bearer real-token", got.Get("Authorization"))
	require.Equal(t, "keep-me", got.Get("X-Proxy-Tag"), "unrelated caller headers still go through")
}

// Subscription-inference fingerprint: User-Agent must identify as
// Claude Code so the API does not 429 the request. The version must
// match a current Claude Code release; bump it when bumping the
// fingerprint constants.
func TestRequestsCarryClaudeCodeUserAgent(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p, err := New(WithAccessToken("tok"), WithBaseURL(srv.URL))
	require.NoError(t, err)

	model, err := p.LanguageModel(context.Background(), "claude-sonnet-5")
	require.NoError(t, err)
	_, err = model.Generate(context.Background(), fantasy.Call{
		Prompt: []fantasy.Message{
			fantasy.NewUserMessage("hello"),
		},
	})
	require.NoError(t, err)

	require.Equal(t, claudeCodeUserAgent, got.Get("User-Agent"))
}

// Subscription-inference fingerprint: the system prompt's structure
// must be billing-header-first, identity-preamble-second, then
// caller content. The two-block ordering is what Anthropic's
// subscription check looks for; a preamble at system[0] is no longer
// sufficient on its own.
func TestRequestsInjectFingerprintSystemBlocksInOrder(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p, err := New(WithAccessToken("tok"), WithBaseURL(srv.URL))
	require.NoError(t, err)

	model, err := p.LanguageModel(context.Background(), "claude-sonnet-5")
	require.NoError(t, err)
	_, err = model.Generate(context.Background(), fantasy.Call{
		Prompt: []fantasy.Message{
			fantasy.NewUserMessage("hello"),
		},
	})
	require.NoError(t, err)

	system, ok := gotBody["system"].([]any)
	require.True(t, ok, "anthropic provider must serialize the system prompt as a top-level field")
	require.GreaterOrEqual(t, len(system), 2, "system[0]=billing, system[1]=preamble must both be on the wire")

	billing, ok := system[0].(map[string]any)
	require.True(t, ok)
	billingText, ok := billing["text"].(string)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(billingText, "x-anthropic-billing-header: cc_version="+claudeCodeVersion+"."),
		"system[0] must be the billing header block, got: %q", billingText)
	require.Contains(t, billingText, "cc_entrypoint=cli")
	// The cch segment is patched in place at fetch time by the
	// cchPatcher RoundTripper; the test is using a real HTTP server,
	// so the wire body has already been patched. The placeholder is
	// therefore gone and a 5-hex-char attestation has taken its
	// place. See cch_patcher_test.go for the unit-level coverage of
	// the patching itself.
	require.NotContains(t, billingText, billingHeaderCCHPlaceholder,
		"the cch segment must be patched on the wire, not the placeholder, got: %q", billingText)
	require.Regexp(t, `cch=[0-9a-f]{5}`, billingText,
		"the cch segment must be a 5-hex-char XXH64 attestation, got: %q", billingText)

	preamble, ok := system[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, claudeCodeSystemInstruction, preamble["text"],
		"system[1] must be the Claude Code identity preamble")
}

// If the caller already supplied a system message, the fingerprint
// blocks must still lead: system[0]=billing, system[1]=preamble,
// system[2]=caller. A fingerprint that lands behind caller content
// is silently broken.
func TestFingerprintBeatsCallerSystemMessage(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p, err := New(WithAccessToken("tok"), WithBaseURL(srv.URL))
	require.NoError(t, err)

	model, err := p.LanguageModel(context.Background(), "claude-sonnet-5")
	require.NoError(t, err)
	_, err = model.Generate(context.Background(), fantasy.Call{
		Prompt: []fantasy.Message{
			fantasy.NewSystemMessage("you are a helpful assistant"),
			fantasy.NewUserMessage("hello"),
		},
	})
	require.NoError(t, err)

	system, ok := gotBody["system"].([]any)
	require.True(t, ok)
	require.GreaterOrEqual(t, len(system), 3, "billing + preamble + caller system = 3 system blocks")

	billing, ok := system[0].(map[string]any)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(billing["text"].(string), "x-anthropic-billing-header:"))

	preamble, ok := system[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, claudeCodeSystemInstruction, preamble["text"])

	caller, ok := system[2].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "you are a helpful assistant", caller["text"])
}

// withFingerprint must not mutate the caller's prompt slice; callers
// may reuse a Call across models.
func TestWithFingerprintDoesNotMutateInput(t *testing.T) {
	original := fantasy.Prompt{
		fantasy.NewSystemMessage("caller system"),
		fantasy.NewUserMessage("hello"),
	}
	originalSnapshot := make(fantasy.Prompt, len(original))
	copy(originalSnapshot, original)

	_ = withFingerprint(original, "hello")

	require.Len(t, original, len(originalSnapshot), "withFingerprint must not append to the caller's slice")
	for i, msg := range original {
		require.Equal(t, originalSnapshot[i], msg, "withFingerprint must not reorder the caller's slice")
	}
}

// Subscription-inference fingerprint: the static Claude Code header
// block must be on every request. Anthropic's check looks at
// `anthropic-version`, `x-app`, `anthropic-dangerous-direct-browser-access`,
// and the full `X-Stainless-*` family.
func TestRequestsCarryClaudeCodeStaticHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p, err := New(WithAccessToken("tok"), WithBaseURL(srv.URL))
	require.NoError(t, err)

	model, err := p.LanguageModel(context.Background(), "claude-sonnet-5")
	require.NoError(t, err)
	_, err = model.Generate(context.Background(), fantasy.Call{
		Prompt: []fantasy.Message{
			fantasy.NewUserMessage("hello"),
		},
	})
	require.NoError(t, err)

	require.Equal(t, "2023-06-01", got.Get("anthropic-version"))
	require.Equal(t, "true", got.Get("anthropic-dangerous-direct-browser-access"))
	require.Equal(t, "cli", got.Get("x-app"))
	require.Equal(t, "js", got.Get("X-Stainless-Lang"))
	require.Equal(t, "node", got.Get("X-Stainless-Runtime"))
	require.Equal(t, "0", got.Get("X-Stainless-Retry-Count"))
	require.Equal(t, claudeCodeSdkVersion, got.Get("X-Stainless-Package-Version"))
	require.NotEmpty(t, got.Get("X-Stainless-OS"))
	require.NotEmpty(t, got.Get("X-Stainless-Arch"))
	require.NotEmpty(t, got.Get("X-Stainless-Runtime-Version"))
	require.NotEmpty(t, got.Get("X-Stainless-Timeout"))
}

// Subscription-inference fingerprint: every request must carry
// `metadata.user_id` as a JSON object with device_id, account_uuid,
// and session_id. Without that block the API does not know how to
// attribute the call.
func TestRequestsCarryMetadataUserID(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p, err := New(WithAccessToken("tok"), WithBaseURL(srv.URL), WithAccountID("acct-1"))
	require.NoError(t, err)

	model, err := p.LanguageModel(context.Background(), "claude-sonnet-5")
	require.NoError(t, err)
	_, err = model.Generate(context.Background(), fantasy.Call{
		Prompt: []fantasy.Message{
			fantasy.NewUserMessage("hello"),
		},
	})
	require.NoError(t, err)

	metadata, ok := gotBody["metadata"].(map[string]any)
	require.True(t, ok, "metadata block must be on the wire")
	userID, ok := metadata["user_id"].(string)
	require.True(t, ok, "user_id must be a JSON string")
	var uid map[string]any
	require.NoError(t, json.Unmarshal([]byte(userID), &uid))
	require.Equal(t, "acct-1", uid["account_uuid"])
	require.NotEmpty(t, uid["device_id"])
	require.NotEmpty(t, uid["session_id"])
}

// Subscription-inference fingerprint: the X-Claude-Code-Session-Id
// header is what lets the API track a multi-turn session. It must be
// stable across calls on the same model and absent of any per-turn
// entropy that would inflate the backend session count.
func TestSessionIDStableAcrossCalls(t *testing.T) {
	var first, second string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Claude-Code-Session-Id") {
		case "":
		default:
			if first == "" {
				first = r.Header.Get("X-Claude-Code-Session-Id")
			} else {
				second = r.Header.Get("X-Claude-Code-Session-Id")
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p, err := New(WithAccessToken("tok"), WithBaseURL(srv.URL))
	require.NoError(t, err)

	model, err := p.LanguageModel(context.Background(), "claude-sonnet-5")
	require.NoError(t, err)
	_, err = model.Generate(context.Background(), fantasy.Call{
		Prompt: []fantasy.Message{
			fantasy.NewUserMessage("hello"),
		},
	})
	require.NoError(t, err)
	_, err = model.Generate(context.Background(), fantasy.Call{
		Prompt: []fantasy.Message{
			fantasy.NewUserMessage("hello again"),
		},
	})
	require.NoError(t, err)

	require.NotEmpty(t, first, "first call must carry a session id")
	require.Equal(t, first, second, "session id must be stable across calls on the same model")
}

// Subscription-inference fingerprint: custom tool names must be
// prefixed with `_` so they do not collide with Anthropic's built-in
// tool namespace (Bash, Read, Write, etc.) on the subscription path.
func TestCustomToolNamesArePrefixed(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p, err := New(WithAccessToken("tok"), WithBaseURL(srv.URL))
	require.NoError(t, err)

	model, err := p.LanguageModel(context.Background(), "claude-sonnet-5")
	require.NoError(t, err)
	_, err = model.Generate(context.Background(), fantasy.Call{
		Prompt: []fantasy.Message{
			fantasy.NewUserMessage("hello"),
		},
		Tools: []fantasy.Tool{
			fantasy.FunctionTool{
				Name:        "view",
				Description: "view a file",
				InputSchema: map[string]any{"type": "object"},
			},
		},
	})
	require.NoError(t, err)

	tools, ok := gotBody["tools"].([]any)
	require.True(t, ok, "tools must be on the wire")
	require.NotEmpty(t, tools)
	first, ok := tools[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "_view", first["name"], "tool names must be prefixed with `_`")
}

// buildBillingHeader must produce a 3-char hex hash derived from the
// first user message and the Claude Code version. The hash must
// change when either input changes, otherwise the billing header is
// not actually fingerprinting anything.
func TestBillingHeaderHashIsContentSensitive(t *testing.T) {
	a := buildBillingHeader("hello world, this is a test") // pos 4=o, 7=o, 20=space
	b := buildBillingHeader("hellX world, this is a test") // pos 4=X (was 'o')
	c := buildBillingHeader("hello world, this is a test")
	require.NotEqual(t, a, b, "hash must change when first user message changes")
	require.Equal(t, a, c, "hash must be stable for the same input")

	// Pull the 3-char hash out of the rendered header and confirm it
	// is a valid hex string.
	require.True(t, strings.Contains(a, "cc_version="+claudeCodeVersion+"."),
		"the version segment must be in the header, got: %q", a)
	parts := strings.SplitN(a, "cc_version="+claudeCodeVersion+".", 2)
	require.Equal(t, 2, len(parts), "the version segment must be parseable, got: %q", a)
	rest := parts[1]
	// Hash is followed by "; cc_entrypoint=..." or ";" at end. Strip
	// off the trailing semicolon-separated segment.
	hash := strings.SplitN(rest, ";", 2)[0]
	require.Len(t, hash, 3, "the hash must be exactly 3 chars, got: %q", hash)
	for _, c := range hash {
		require.True(t,
			(c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
			"hash char %q must be lowercase hex", c)
	}
}

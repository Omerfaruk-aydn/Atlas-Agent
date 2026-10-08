package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/anthropic"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/google"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/openai"
	"github.com/stretchr/testify/require"
)

func cdpParamsSchema(t *testing.T) map[string]any {
	t.Helper()
	prepared := fantasy.PrepareFunctionTool(newBrowserTool(&mockPermissionService{}, t.TempDir(), newFakeBrowserSessions(), browserDescription(false)))
	props := prepared.InputSchema["properties"].(map[string]any)
	return props["cdp_params"].(map[string]any)
}

func TestBrowserCDPParamsModelVisibleSchemaIsObject(t *testing.T) {
	t.Parallel()
	node := cdpParamsSchema(t)
	require.Equal(t, "object", node["type"])
	require.NotContains(t, node, "items", "a byte-slice schema would advertise an integer array")
	require.Contains(t, node["description"], "JSON object")
}

// captureProviderRequest sends one call through a real provider client to a
// local server and returns the request body the provider would receive.
func captureProviderRequest(t *testing.T, newModel func(baseURL string) fantasy.LanguageModel) map[string]any {
	t.Helper()
	var mu sync.Mutex
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		mu.Lock()
		if body == nil {
			body = data
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"captured","type":"invalid_request_error","code":400,"status":"INVALID_ARGUMENT"}}`))
	}))
	t.Cleanup(srv.Close)
	tool := fantasy.PrepareFunctionTool(newBrowserTool(&mockPermissionService{}, t.TempDir(), newFakeBrowserSessions(), browserDescription(false)))
	model := newModel(srv.URL)
	_, _ = model.Generate(context.Background(), fantasy.Call{
		Prompt: fantasy.Prompt{fantasy.NewUserMessage("hi")},
		Tools:  []fantasy.Tool{tool},
	})
	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, body, "provider sent no request")
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	return decoded
}

func TestBrowserCDPParamsProviderSchemasAreObjects(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	t.Run("anthropic", func(t *testing.T) {
		t.Parallel()
		body := captureProviderRequest(t, func(base string) fantasy.LanguageModel {
			p, err := anthropic.New(anthropic.WithBaseURL(base), anthropic.WithAPIKey("test"))
			require.NoError(t, err)
			m, err := p.LanguageModel(ctx, "claude-test")
			require.NoError(t, err)
			return m
		})
		tool := body["tools"].([]any)[0].(map[string]any)
		cdp := tool["input_schema"].(map[string]any)["properties"].(map[string]any)["cdp_params"].(map[string]any)
		require.Equal(t, "object", cdp["type"])
		require.NotContains(t, cdp, "items")
	})
	t.Run("openai", func(t *testing.T) {
		t.Parallel()
		body := captureProviderRequest(t, func(base string) fantasy.LanguageModel {
			p, err := openai.New(openai.WithBaseURL(base), openai.WithAPIKey("test"))
			require.NoError(t, err)
			m, err := p.LanguageModel(ctx, "gpt-test")
			require.NoError(t, err)
			return m
		})
		tool := findOpenAITool(t, body)
		cdp := tool["properties"].(map[string]any)["cdp_params"].(map[string]any)
		require.Equal(t, "object", cdp["type"])
		require.NotContains(t, cdp, "items")
	})
	t.Run("google", func(t *testing.T) {
		t.Parallel()
		body := captureProviderRequest(t, func(base string) fantasy.LanguageModel {
			p, err := google.New(google.WithBaseURL(base), google.WithGeminiAPIKey("test"))
			require.NoError(t, err)
			m, err := p.LanguageModel(ctx, "gemini-test")
			require.NoError(t, err)
			return m
		})
		decl := body["tools"].([]any)[0].(map[string]any)["functionDeclarations"].([]any)[0].(map[string]any)
		cdp := decl["parameters"].(map[string]any)["properties"].(map[string]any)["cdp_params"].(map[string]any)
		require.Equal(t, "OBJECT", strings.ToUpper(cdp["type"].(string)))
		require.NotContains(t, cdp, "items")
	})
}

// findOpenAITool accepts both the chat completions and responses shapes.
func findOpenAITool(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	tool := body["tools"].([]any)[0].(map[string]any)
	if fn, ok := tool["function"].(map[string]any); ok {
		return fn["parameters"].(map[string]any)
	}
	return tool["parameters"].(map[string]any)
}

func TestDecodeCDPParamsContract(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		raw     string
		want    map[string]any
		wantErr bool
	}{
		{name: "object", raw: `{"type":"mouseMoved","x":10,"y":20}`, want: map[string]any{"type": "mouseMoved", "x": 10.0, "y": 20.0}},
		{name: "omitted", raw: ``},
		{name: "null", raw: `null`},
		{name: "one string layer", raw: `"{\"type\":\"mousePressed\",\"x\":5,\"y\":6,\"button\":\"left\"}"`, want: map[string]any{"type": "mousePressed", "x": 5.0, "y": 6.0, "button": "left"}},
		{name: "two string layers", raw: `"\"{\\\"x\\\":1}\""`, wantErr: true},
		{name: "invalid json in string", raw: `"{\"x\":1"`, wantErr: true},
		{name: "plain string", raw: `"click at 10,20"`, wantErr: true},
		{name: "array", raw: `[1,2]`, wantErr: true},
		{name: "array in string", raw: `"[1,2]"`, wantErr: true},
		{name: "number", raw: `42`, wantErr: true},
		{name: "bool", raw: `true`, wantErr: true},
		{name: "trailing data in string", raw: `"{\"x\":1} {\"y\":2}"`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := decodeCDPParams(BrowserCDPParams(tc.raw))
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "cdp_params must be a JSON object")
				require.NotContains(t, err.Error(), "10,20", "the received value must not be echoed")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestBrowserToolCDPDeliversObjectAndStringCompatParams(t *testing.T) {
	t.Parallel()
	for name, input := range map[string]string{
		"object": `{"action":"cdp","cdp_method":"Input.dispatchMouseEvent","cdp_params":{"type":"mouseMoved","x":390,"y":844}}`,
		"string": `{"action":"cdp","cdp_method":"Input.dispatchMouseEvent","cdp_params":"{\"type\":\"mouseMoved\",\"x\":390,\"y\":844}"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sessions := newFakeBrowserSessions()
			sessions.sessions["test-session"] = &fakeBrowserSession{cdpResult: map[string]any{}}
			resp := runBrowserToolRaw(t, sessions, input)
			require.False(t, resp.IsError, resp.Content)
			f := sessions.sessions["test-session"]
			require.Equal(t, "Input.dispatchMouseEvent", f.cdpMethod)
			require.Equal(t, map[string]any{"type": "mouseMoved", "x": 390.0, "y": 844.0}, f.cdpParams)
		})
	}
}

func TestBrowserToolCDPRejectsWrongShapesWithoutSending(t *testing.T) {
	t.Parallel()
	for name, params := range map[string]string{
		"array":         `[{"type":"mouseMoved"}]`,
		"scalar":        `7`,
		"text":          `"move the mouse"`,
		"broken string": `"{\"type\":\"mouseMoved\""`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sessions := newFakeBrowserSessions()
			sessions.sessions["test-session"] = &fakeBrowserSession{}
			resp := runBrowserToolRaw(t, sessions, `{"action":"cdp","cdp_method":"Input.dispatchMouseEvent","cdp_params":`+params+`}`)
			require.True(t, resp.IsError)
			require.Contains(t, resp.Content, "invalid_cdp_params")
			require.Contains(t, resp.Content, "No CDP command sent")
			require.Empty(t, sessions.sessions["test-session"].cdpMethod, "nothing may reach the backend")
		})
	}
}

func runBrowserToolRaw(t *testing.T, sessions *fakeBrowserSessions, input string) fantasy.ToolResponse {
	t.Helper()
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "test-session")
	resp, err := newBrowserTool(&mockPermissionService{}, t.TempDir(), sessions, browserDescription(false)).Run(ctx, fantasy.ToolCall{ID: "raw-call", Name: BrowserToolName, Input: input})
	require.NoError(t, err)
	return resp
}

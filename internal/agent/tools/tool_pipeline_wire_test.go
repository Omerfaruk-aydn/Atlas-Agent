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

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/anthropic"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/claude"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/google"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/muse"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/openai"
	"github.com/stretchr/testify/require"
)

// captureToolRequest sends one generation request carrying tool_pipeline to
// a local server and returns the decoded request body exactly as the
// provider serialized it. The server answers with an error; only the request
// matters.
func captureToolRequest(t *testing.T, build func(baseURL string) (fantasy.Provider, error), model string) map[string]any {
	t.Helper()
	var (
		mu   sync.Mutex
		body []byte
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		mu.Lock()
		if body == nil && json.Valid(data) && strings.Contains(string(data), "tool_pipeline") {
			body = data
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"capture only","code":400,"status":"INVALID_ARGUMENT"}}`))
	}))
	t.Cleanup(server.Close)
	provider, err := build(server.URL)
	require.NoError(t, err)
	lm, err := provider.LanguageModel(t.Context(), model)
	require.NoError(t, err)
	tool := fantasy.PrepareFunctionTool(NewToolPipeline(nil))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, _ = lm.Generate(ctx, fantasy.Call{
		Prompt: fantasy.Prompt{fantasy.NewUserMessage("observe the calculator")},
		Tools:  []fantasy.Tool{tool},
	})
	mu.Lock()
	defer mu.Unlock()
	require.NotNil(t, body, "the provider sent no request carrying tool_pipeline")
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	return decoded
}

// findToolSchema locates the tool_pipeline parameter schema in any of the
// supported wire layouts.
func findToolSchema(node any) map[string]any {
	switch value := node.(type) {
	case map[string]any:
		name, _ := value["name"].(string)
		if strings.HasSuffix(name, "tool_pipeline") {
			for _, key := range []string{"input_schema", "parameters"} {
				if schema, ok := value[key].(map[string]any); ok {
					return schema
				}
			}
		}
		for _, child := range value {
			if found := findToolSchema(child); found != nil {
				return found
			}
		}
	case []any:
		for _, child := range value {
			if found := findToolSchema(child); found != nil {
				return found
			}
		}
	}
	return nil
}

// schemaTypes maps every reachable schema path to its JSON type. Gemini
// spells types in upper case; the comparison is case-insensitive. A node
// whose description says it was bounded for depth ends the walk.
func schemaTypes(node map[string]any, path string, out map[string]string) {
	kind, _ := node["type"].(string)
	out[path] = strings.ToLower(kind)
	if description, _ := node["description"].(string); strings.Contains(description, "validated locally") {
		return
	}
	if properties, ok := node["properties"].(map[string]any); ok {
		for name, child := range properties {
			if childMap, ok := child.(map[string]any); ok {
				schemaTypes(childMap, path+"."+name, out)
			}
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		schemaTypes(items, path+"[]", out)
	}
}

// TestPipelineWireSchemaKeepsObjectTypes proves that every provider
// conversion sends desktop, browser, flow and each nested node with the same
// JSON type the decoder expects. Session 69361c0a's string-encoded desktop
// therefore was not caused by a schema conversion that advertised a string.
func TestPipelineWireSchemaKeepsObjectTypes(t *testing.T) {
	t.Parallel()
	prepared := fantasy.PrepareFunctionTool(NewToolPipeline(nil))
	data, err := json.Marshal(prepared.InputSchema)
	require.NoError(t, err)
	var local map[string]any
	require.NoError(t, json.Unmarshal(data, &local))
	want := map[string]string{}
	schemaTypes(local, "", want)
	for _, field := range []string{".desktop", ".browser", ".flow", ".steps[].arguments"} {
		require.Equal(t, "object", want[field], field)
	}
	for path, kind := range want {
		require.NotEmpty(t, kind, "the local schema has a typeless node at %s; Gemini would send it as STRING", path)
	}
	// Flow values, file checks and rename conflict policies are advertised
	// with the types the decoder reads.
	valueFields := map[string]string{
		".desktop.flow.nodes[].capture":                "object",
		".desktop.flow.nodes[].capture.number":         "object",
		".desktop.flow.nodes[].file":                   "object",
		".desktop.flow.nodes[].file.condition":         "string",
		".desktop.flow.nodes[].rename.on_conflict":     "string",
		".desktop.rename.on_conflict":                  "string",
		".desktop.flow.nodes[].capture.number.decimal": "string",
	}
	for path, kind := range valueFields {
		require.Equal(t, kind, want[path], path)
	}

	providers := map[string]struct {
		model string
		build func(string) (fantasy.Provider, error)
	}{
		"anthropic": {"claude-test", func(url string) (fantasy.Provider, error) {
			return anthropic.New(anthropic.WithBaseURL(url), anthropic.WithAPIKey("test"))
		}},
		"claude-subscription": {"claude-haiku-5-5", func(url string) (fantasy.Provider, error) {
			return claude.New(claude.WithBaseURL(url), claude.WithAccessToken("test"))
		}},
		"muse-depth-bounded": {"muse-test", func(url string) (fantasy.Provider, error) {
			return muse.New(muse.WithBaseURL(url), muse.WithAccessToken("test"))
		}},
		"openai-chat": {"gpt-test", func(url string) (fantasy.Provider, error) {
			return openai.New(openai.WithBaseURL(url), openai.WithAPIKey("test"))
		}},
		"openai-responses": {"gpt-test", func(url string) (fantasy.Provider, error) {
			return openai.New(openai.WithBaseURL(url), openai.WithAPIKey("test"), openai.WithUseResponsesAPI())
		}},
		"gemini": {"gemini-test", func(url string) (fantasy.Provider, error) {
			return google.New(google.WithBaseURL(url), google.WithGeminiAPIKey("test"))
		}},
	}
	for name, provider := range providers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			body := captureToolRequest(t, provider.build, provider.model)
			wire := findToolSchema(body)
			require.NotNil(t, wire, "tool_pipeline schema missing from the %s request", name)
			got := map[string]string{}
			schemaTypes(wire, "", got)
			require.Equal(t, "object", got[".desktop"], "desktop must reach %s as an object", name)
			for path, kind := range got {
				require.Equal(t, want[path], kind, "%s changed the JSON type at %s", name, path)
			}
			// A depth-bounded provider may stop at an ancestor it validates
			// locally; otherwise each new field reaches the model.
			for path := range valueFields {
				if _, ok := got[path]; ok {
					continue
				}
				bounded := false
				for prefix := path; strings.Contains(prefix, "."); prefix = prefix[:strings.LastIndex(prefix, ".")] {
					if _, ok := got[prefix]; ok {
						bounded = true
						break
					}
				}
				require.True(t, bounded, "%s dropped %s without a bounded ancestor", name, path)
			}
		})
	}
}

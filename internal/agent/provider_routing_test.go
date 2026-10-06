package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/anthropic"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/google"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/muse"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/openai"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/openaicompat"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/embedded"
	"github.com/stretchr/testify/require"
)

func TestDirectCloudAndMetaCatalogResponsesRequests(t *testing.T) {
	for _, providerID := range []string{"bedrock-openai", "meta-api"} {
		t.Run(providerID, func(t *testing.T) {
			coord := hermeticSubagentCoordinator(t)
			var catalog catwalk.Provider
			for _, p := range embedded.GetAll() {
				if string(p.ID) == providerID {
					catalog = p
				}
			}
			require.NotEmpty(t, catalog.Models)
			var selected catwalk.Model
			for _, m := range catalog.Models {
				if m.ID == catalog.DefaultLargeModelID {
					selected = m
				}
				require.NotEqual(t, "global.openai.gpt-6.1-sol", m.ID)
			}
			requests := make(chan map[string]any, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer test-key" {
					http.Error(w, "incorrect protocol or credentials", http.StatusBadRequest)
					return
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, "invalid request", http.StatusBadRequest)
					return
				}
				requests <- body
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"response-1","object":"response","status":"completed","output":[{"id":"message-1","type":"message","role":"assistant","content":[{"type":"output_text","text":"OK","annotations":[]}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
			}))
			defer server.Close()
			cfg := config.ProviderConfig{ID: providerID, Type: catalog.Type, BaseURL: server.URL + "/v1", APIKey: "test-key"}
			provider, err := coord.buildProvider(cfg, config.SelectedModel{Model: selected.ID}, false)
			require.NoError(t, err)
			lm, err := provider.LanguageModel(t.Context(), selected.ID)
			require.NoError(t, err)
			_, err = lm.Generate(t.Context(), fantasy.Call{Prompt: fantasy.Prompt{{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "Hello"}}}}, ProviderOptions: getProviderOptions(Model{CatwalkCfg: selected}, cfg)})
			require.NoError(t, err)
			request := <-requests
			require.Equal(t, selected.ID, request["model"])
			require.Equal(t, selected.DefaultReasoningEffort, request["reasoning"].(map[string]any)["effort"])
		})
	}
}

func TestVertexReasoningOptionsFollowModelProtocol(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"gemini-3.8-flash", "claude-opus-5-5"} {
		model := Model{CatwalkCfg: catwalk.Model{ID: id, CanReason: true, ReasoningLevels: []string{"medium", "high"}, DefaultReasoningEffort: "medium"}}
		opts := getProviderOptions(model, config.ProviderConfig{ID: "vertexai", Type: catwalk.TypeVertexAI})
		if id == "gemini-3.8-flash" {
			require.Equal(t, "medium", *opts[google.Name].(*google.ProviderOptions).ThinkingConfig.ThinkingLevel)
		} else {
			require.Contains(t, opts, anthropic.Name)
		}
	}
}

func TestOpenCodeGoogleRequestPath(t *testing.T) {
	coord := hermeticSubagentCoordinator(t)
	paths := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"OK"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`))
	}))
	defer server.Close()
	provider, err := coord.buildProvider(config.ProviderConfig{
		ID: "opencode-zen", Type: catwalk.TypeOpenAICompat,
		APIKey: "test-key", BaseURL: server.URL + "/zen/v1",
	}, config.SelectedModel{Model: "gemini-3.8-flash"}, false)
	require.NoError(t, err)
	lm, err := provider.LanguageModel(context.Background(), "gemini-3.8-flash")
	require.NoError(t, err)
	_, err = lm.Generate(context.Background(), fantasy.Call{Prompt: fantasy.Prompt{
		{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "Hello"}}},
	}})
	require.NoError(t, err)
	require.Equal(t, "/zen/v1/models/gemini-3.8-flash:generateContent", <-paths)
}

func TestUnavailableProviderRejectsBeforeResolvingCredentials(t *testing.T) {
	coord := &coordinator{}
	for _, typ := range []catwalk.Type{catwalk.TypeGrokWeb, catwalk.TypeWindsurf, catwalk.TypeJetBrains, catwalk.TypeAugment, catwalk.TypeFactory, catwalk.TypeCodeRabbit, catwalk.TypeZed} {
		_, err := coord.buildProvider(config.ProviderConfig{ID: string(typ), Type: typ}, config.SelectedModel{}, false)
		require.ErrorContains(t, err, "model calls are not implemented")
	}
	for _, id := range []string{"amp", "bolt", "phind", "codex-ide"} {
		_, err := coord.buildProvider(config.ProviderConfig{ID: id, Type: catwalk.TypeOpenAICompat}, config.SelectedModel{}, false)
		require.ErrorContains(t, err, "unavailable")
	}
}

func TestMiniMaxMPlanRequest(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "another-provider-key")
	coord := hermeticSubagentCoordinator(t)
	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/anthropic/v1/messages" || r.Header.Get("Authorization") != "Bearer test-plan-key" {
			http.Error(w, "incorrect plan path or authorization", http.StatusBadRequest)
			return
		}
		if r.Header.Get("X-Api-Key") != "" {
			http.Error(w, "unrelated provider credential leaked", http.StatusBadRequest)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		requests <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_test","type":"message","role":"assistant","model":"MiniMax-M3.1-Flash-Preview","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()
	pc := config.ProviderConfig{ID: "minimax-m-plan", Type: catwalk.TypeAnthropic, APIKey: "test-plan-key", BaseURL: server.URL + "/anthropic"}
	provider, err := coord.buildProvider(pc, config.SelectedModel{Model: "MiniMax-M3.1-Flash-Preview"}, false)
	require.NoError(t, err)
	require.Equal(t, "another-provider-key", os.Getenv("ANTHROPIC_API_KEY"), "choosing a plan must preserve other provider credentials")
	lm, err := provider.LanguageModel(context.Background(), "MiniMax-M3.1-Flash-Preview")
	require.NoError(t, err)
	model := Model{CatwalkCfg: catwalk.Model{ID: "MiniMax-M3.1-Flash-Preview", CanReason: true, ReasoningLevels: []string{"low", "medium", "high", "xhigh", "max"}, DefaultReasoningEffort: "max"}}
	_, err = lm.Generate(context.Background(), fantasy.Call{
		ProviderOptions: getProviderOptions(model, pc),
		Prompt:          fantasy.Prompt{{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "Hello"}}}},
	})
	require.NoError(t, err)
	request := <-requests
	require.Equal(t, "max", request["output_config"].(map[string]any)["effort"])
	require.Equal(t, "adaptive", request["thinking"].(map[string]any)["type"])
	require.NotContains(t, request["thinking"], "display", "Claude-only display controls must not be sent to MiniMax")
}

func TestXiaomiThinkingOptions(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"xiaomi", "xiaomi-token-plan-cn", "xiaomi-token-plan-sgp", "xiaomi-token-plan-ams"} {
		for _, think := range []bool{true, false} {
			model := Model{
				CatwalkCfg: catwalk.Model{ID: "mimo-v2.6-pro", CanReason: true},
				ModelCfg:   config.SelectedModel{Think: think},
			}
			cfg := config.ProviderConfig{ID: id, Type: catwalk.TypeOpenAICompat}
			options := getProviderOptions(model, cfg)[openaicompat.Name].(*openaicompat.ProviderOptions)
			require.Nil(t, options.ReasoningEffort)
			want := "disabled"
			if think {
				want = "enabled"
			}
			require.Equal(t, want, options.ExtraBody["thinking"].(map[string]any)["type"])
		}
	}
}

func TestXiaomiExplicitThinkingOptions(t *testing.T) {
	t.Parallel()
	model := Model{CatwalkCfg: catwalk.Model{ID: "mimo-v2.6-pro", CanReason: true}}
	cfg := config.ProviderConfig{
		ID: "xiaomi", Type: catwalk.TypeOpenAICompat,
		ProviderOptions: map[string]any{"extra_body": map[string]any{
			"thinking": map[string]any{"type": "enabled"}, "custom": true,
		}},
	}
	options := getProviderOptions(model, cfg)[openaicompat.Name].(*openaicompat.ProviderOptions)
	require.Equal(t, "enabled", options.ExtraBody["thinking"].(map[string]any)["type"])
	require.Equal(t, true, options.ExtraBody["custom"])
}

func TestXiaomiSelectedEffortOverridesLegacyThinkingToggle(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{"xiaomi", "xiaomi-token-plan-cn", "xiaomi-token-plan-sgp", "xiaomi-token-plan-ams"} {
		for _, effort := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"} {
			model := Model{CatwalkCfg: catwalk.Model{ID: "mimo-v2.6-pro", CanReason: true, ReasoningLevels: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}}, ModelCfg: config.SelectedModel{Think: true, ReasoningEffort: effort}}
			options := getProviderOptions(model, config.ProviderConfig{ID: provider, Type: catwalk.TypeOpenAICompat})[openaicompat.Name].(*openaicompat.ProviderOptions)
			require.Nil(t, options.ReasoningEffort, "Chat Completions uses thinking.type")
			want := "enabled"
			if effort == "none" {
				want = "disabled"
			}
			require.Equal(t, want, options.ExtraBody["thinking"].(map[string]any)["type"], "%s/%s", provider, effort)
		}
	}
}

func TestXiaomiEffortReachesChatCompletionRequest(t *testing.T) {
	for _, providerID := range []string{"xiaomi", "xiaomi-token-plan-cn", "xiaomi-token-plan-sgp", "xiaomi-token-plan-ams"} {
		t.Run(providerID, func(t *testing.T) {
			coord := hermeticSubagentCoordinator(t)
			var selected catwalk.Model
			for _, provider := range embedded.GetAll() {
				if string(provider.ID) == providerID {
					for _, model := range provider.Models {
						if model.ID == "mimo-v2.6-pro" {
							selected = model
						}
					}
				}
			}
			require.NotEmpty(t, selected.ID)
			requests := make(chan map[string]any, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/chat/completions" {
					http.Error(w, "incorrect path", http.StatusBadRequest)
					return
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, "invalid body", http.StatusBadRequest)
					return
				}
				requests <- body
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"fixture","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
			}))
			defer server.Close()
			cfg := config.ProviderConfig{ID: providerID, Type: catwalk.TypeOpenAICompat, BaseURL: server.URL + "/v1", APIKey: "test-key"}
			provider, err := coord.buildProvider(cfg, config.SelectedModel{Model: selected.ID}, false)
			require.NoError(t, err)
			lm, err := provider.LanguageModel(t.Context(), selected.ID)
			require.NoError(t, err)
			for _, effort := range []string{"none", "high"} {
				_, err = lm.Generate(t.Context(), fantasy.Call{Prompt: fantasy.Prompt{{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "Hello"}}}}, ProviderOptions: getProviderOptions(Model{CatwalkCfg: selected, ModelCfg: config.SelectedModel{Think: true, ReasoningEffort: effort}}, cfg)})
				require.NoError(t, err)
				request := <-requests
				want := "enabled"
				if effort == "none" {
					want = "disabled"
				}
				require.Equal(t, want, request["thinking"].(map[string]any)["type"])
				require.NotContains(t, request, "reasoning_effort")
			}
		})
	}
}

func TestAlibabaPlanThinkingUsesEnableThinking(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{"alibaba-coding-cn", "alibaba-token-plan-sgp", "alibaba-token-plan-cn", "alibaba-token-plan-team-sgp", "alibaba-token-plan-team-cn"} {
		modelIDs := []string{"qwen3.8-max", "deepseek-v4.1-flash", "glm-5.3"}
		if provider == "alibaba-coding-cn" {
			modelIDs = []string{"qwen3.6-plus", "deepseek-v4-pro", "glm-5.2"}
		}
		for _, modelID := range modelIDs {
			model := Model{CatwalkCfg: catwalk.Model{ID: modelID, CanReason: true}, ModelCfg: config.SelectedModel{Think: true}}
			opts := getProviderOptions(model, config.ProviderConfig{ID: provider, Type: catwalk.TypeOpenAICompat})[openaicompat.Name].(*openaicompat.ProviderOptions)
			require.Equal(t, true, opts.ExtraBody["enable_thinking"], "%s/%s", provider, modelID)
			require.NotContains(t, opts.ExtraBody, "thinking")
		}
	}
}

func TestAlibabaGLM53CannotDisableRequiredThinking(t *testing.T) {
	t.Parallel()
	model := Model{CatwalkCfg: catwalk.Model{ID: "glm-5.3", CanReason: true}, ModelCfg: config.SelectedModel{ProviderOptions: map[string]any{"extra_body": map[string]any{"enable_thinking": false}}}}
	options := getProviderOptions(model, config.ProviderConfig{ID: "alibaba-token-plan-cn", Type: catwalk.TypeOpenAICompat})
	require.Equal(t, true, options[openaicompat.Name].(*openaicompat.ProviderOptions).ExtraBody["enable_thinking"])
}

func TestOpenCodeModelProtocols(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		provider string
		model    string
		want     catwalk.Type
	}{
		{"opencode-zen", "claude-sonnet-5-5", catwalk.TypeAnthropic},
		{"opencode-zen", "gpt-6.1-sol", catwalk.TypeOpenAI},
		{"opencode-go", "gpt-6-luna", catwalk.TypeOpenAI},
		{"opencode-go", "grok-4.7", catwalk.TypeOpenAI},
		{"opencode-go", "muse-spark-1.3-contributor", catwalk.TypeOpenAI},
		{"opencode-zen", "muse-spark-1.3-contributor-free", catwalk.TypeOpenAI},
		{"opencode-zen", "gemini-3.8-flash", catwalk.TypeGoogle},
		{"opencode-go", "qwen3.8-max", catwalk.TypeAnthropic},
		{"opencode-zen", "qwen3.8-max", catwalk.TypeOpenAICompat},
		{"opencode-go", "minimax-m3", catwalk.TypeAnthropic},
		{"opencode-zen", "minimax-m3", catwalk.TypeOpenAICompat},
		{"opencode-go", "mimo-v2.6-pro", catwalk.TypeOpenAICompat},
	} {
		require.Equal(t, tc.want, opencodeModelType(tc.provider, tc.model), tc.model)
	}
	model := Model{
		CatwalkCfg: catwalk.Model{ID: "claude-sonnet-5-5", CanReason: true, ReasoningLevels: []string{"high"}},
	}
	options := getProviderOptions(model, config.ProviderConfig{ID: "opencode-zen", Type: catwalk.TypeOpenAICompat})
	require.Contains(t, options, anthropic.Name, "options must match the selected transport")
}

func TestOpenCodeResponsesReasoningRequest(t *testing.T) {
	for _, modelID := range []string{"grok-4.7", "muse-spark-1.3-contributor", "muse-spark-1.3-contributor-free"} {
		t.Run(modelID, func(t *testing.T) {
			coord := hermeticSubagentCoordinator(t)
			requests := make(chan map[string]any, 1)
			sessions := make(chan string, 3)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/zen/v1/responses" {
					http.Error(w, "incorrect model protocol", http.StatusBadRequest)
					return
				}
				if r.Header.Get("x-opencode-session") == "" || !strings.HasPrefix(r.UserAgent(), "ATLAS-AGENT/") {
					http.Error(w, "missing coding agent identity", http.StatusBadRequest)
					return
				}
				sessions <- r.Header.Get("x-opencode-session")
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, "invalid request", http.StatusBadRequest)
					return
				}
				requests <- body
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"resp_test","object":"response","status":"completed","output":[{"type":"message","id":"msg_test","role":"assistant","status":"completed","content":[{"type":"output_text","text":"OK","annotations":[]}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
			}))
			defer server.Close()
			pc := config.ProviderConfig{ID: "opencode-zen", Type: catwalk.TypeOpenAICompat, APIKey: "test-key", BaseURL: server.URL + "/zen/v1"}
			provider, err := coord.buildProvider(pc, config.SelectedModel{Model: modelID}, false)
			require.NoError(t, err)
			lm, err := provider.LanguageModel(t.Context(), modelID)
			require.NoError(t, err)
			model := Model{CatwalkCfg: catwalk.Model{ID: modelID, CanReason: true, ReasoningLevels: []string{"low", "high"}}, ModelCfg: config.SelectedModel{ReasoningEffort: "low"}}
			opts := getProviderOptions(model, pc)
			require.Contains(t, opts, openai.Name)
			_, err = lm.Generate(t.Context(), fantasy.Call{
				ProviderOptions: opts,
				Prompt:          fantasy.Prompt{{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "Hello"}}}},
			})
			require.NoError(t, err)
			request := <-requests
			reasoning, ok := request["reasoning"].(map[string]any)
			require.True(t, ok, "selected reasoning effort must reach the API")
			require.Equal(t, "low", reasoning["effort"])
			firstSession := <-sessions
			for _, headers := range []map[string]string{nil, {"x-opencode-session": "conversation-override"}} {
				_, err = lm.Generate(t.Context(), fantasy.Call{Headers: headers, Prompt: fantasy.Prompt{fantasy.NewUserMessage("Hello again")}})
				require.NoError(t, err)
				<-requests
				if headers == nil {
					require.Equal(t, firstSession, <-sessions)
				} else {
					require.Equal(t, "conversation-override", <-sessions)
				}
			}
		})
	}
}

func TestClaudeAccountReasoningEffort(t *testing.T) {
	t.Parallel()
	model := Model{CatwalkCfg: catwalk.Model{ID: "claude-opus-5-5", CanReason: true, ReasoningLevels: []string{"low", "medium", "high", "xhigh", "max"}, DefaultReasoningEffort: "medium"}}
	pc := config.ProviderConfig{ID: "claude", Type: catwalk.TypeClaude}
	for _, effort := range []string{"", "low", "max"} {
		model.ModelCfg.ReasoningEffort = effort
		opts := getProviderOptions(model, pc)
		require.Contains(t, opts, anthropic.Name)
		want := effort
		if want == "" {
			want = "medium"
		}
		require.Equal(t, want, string(*opts[anthropic.Name].(*anthropic.ProviderOptions).Effort))
	}
}

func TestOpenCodeSessionAffinityHeader(t *testing.T) {
	t.Parallel()
	headers := sessionHeaders("session-one")
	require.NotEmpty(t, headers["x-opencode-session"])
	require.Equal(t, headers["x-session-id"], headers["x-opencode-session"])
	require.Equal(t, headers, sessionHeaders("session-one"))
	require.NotEqual(t, headers["x-opencode-session"], sessionHeaders("session-two")["x-opencode-session"])
}

func TestCopilotResponsesOptionsPreserveReasoning(t *testing.T) {
	t.Parallel()
	pc := config.ProviderConfig{ID: "copilot", Type: catwalk.TypeOpenAICompat}
	model := Model{CatwalkCfg: catwalk.Model{ID: "gpt-6.1-sol", CanReason: true, ReasoningLevels: []string{"low", "high"}}, ModelCfg: config.SelectedModel{ReasoningEffort: "low"}}
	opts := getProviderOptions(model, pc)
	require.Contains(t, opts, openai.Name)
	require.Equal(t, "low", string(*opts[openai.Name].(*openai.ResponsesProviderOptions).ReasoningEffort))
	model.CatwalkCfg.ID = "claude-sonnet-5-5"
	require.Contains(t, getProviderOptions(model, pc), openaicompat.Name, "the chat-completions route keeps its own option type")
}

func TestMuseAccountEffortReachesMessagesAPI(t *testing.T) {
	t.Parallel()
	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("Authorization") != "Bearer test-model-key" {
			http.Error(w, "incorrect account request", http.StatusBadRequest)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		requests <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_test","type":"message","role":"assistant","model":"muse-spark-1.3","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()
	provider, err := muse.New(muse.WithAccessToken("test-model-key"), muse.WithBaseURL(server.URL))
	require.NoError(t, err)
	lm, err := provider.LanguageModel(t.Context(), "muse-spark-1.3")
	require.NoError(t, err)
	model := Model{CatwalkCfg: catwalk.Model{ID: "muse-spark-1.3", CanReason: true, ReasoningLevels: []string{"low", "medium", "high", "xhigh"}}, ModelCfg: config.SelectedModel{ReasoningEffort: "low"}}
	_, err = lm.Generate(t.Context(), fantasy.Call{
		MaxOutputTokens: new(int64(4096)),
		ProviderOptions: getProviderOptions(model, config.ProviderConfig{ID: "muse", Type: catwalk.TypeMuse}),
		Prompt:          fantasy.Prompt{{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "Hello"}}}},
	})
	require.NoError(t, err)
	request := <-requests
	output, ok := request["output_config"].(map[string]any)
	require.True(t, ok, "effort must reach the native Messages API")
	require.Equal(t, "low", output["effort"])
	require.Equal(t, "adaptive", request["thinking"].(map[string]any)["type"])
}

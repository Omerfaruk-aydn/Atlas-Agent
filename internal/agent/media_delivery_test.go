package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/anthropic"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/google"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/providers/openai"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/stretchr/testify/require"
)

const viewportCaption = `Viewport: {"viewport_id":"vp-delivery","image_width":1600,"image_height":765,"image_scale":0.8333333333333334}`

func mediaDeliveryMessages(text, data string) []fantasy.Message {
	return []fantasy.Message{{Role: fantasy.MessageRoleTool, Content: []fantasy.MessagePart{
		fantasy.ToolResultPart{ToolCallID: "screenshot-call", Output: fantasy.ToolResultOutputContentMedia{Text: text, Data: data, MediaType: "image/png"}},
	}}}
}

func TestMediaDeliveryPreservesCaptionAcrossProviderFamilies(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{"anthropic", "claude", "bedrock", "bedrock-europe", "openai", "gemini", "antigravity", "xiaomi", "deepseek", "opencode", "meta-api", "custom-compatible"} {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			a := &sessionAgent{}
			data := base64.StdEncoding.EncodeToString([]byte("image bytes"))
			messages := a.workaroundProviderMediaLimitations(mediaDeliveryMessages(viewportCaption, data), Model{
				ModelCfg: config.SelectedModel{Provider: provider}, CatwalkCfg: catwalk.Model{SupportsImages: true},
			})
			part := messages[0].Content[0].(fantasy.ToolResultPart)
			require.Equal(t, "screenshot-call", part.ToolCallID)
			switch out := part.Output.(type) {
			case fantasy.ToolResultOutputContentMedia:
				require.Equal(t, viewportCaption, out.Text)
				require.Equal(t, data, out.Data)
			case fantasy.ToolResultOutputContentText:
				require.Contains(t, out.Text, viewportCaption)
				require.Len(t, messages, 2)
				file := messages[1].Content[1].(fantasy.FilePart)
				require.Equal(t, []byte("image bytes"), file.Data)
				require.Equal(t, "tool-result-screenshot-call", file.Filename)
			default:
				t.Fatalf("unexpected tool output %T", out)
			}
			if provider == "claude" {
				require.Len(t, messages, 1, "Claude supports native tool-result images")
				_, native := part.Output.(fantasy.ToolResultOutputContentMedia)
				require.True(t, native)
			}
			// Preparing an already prepared history must not duplicate media.
			require.Equal(t, messages, a.workaroundProviderMediaLimitations(messages, Model{
				ModelCfg: config.SelectedModel{Provider: provider}, CatwalkCfg: catwalk.Model{SupportsImages: true},
			}))
		})
	}
}

func TestMediaDeliveryRetainsTextWhenImagesAreUnavailable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		data    string
		vision  bool
		warning string
	}{
		{"text-only", base64.StdEncoding.EncodeToString([]byte("image")), false, "not supported"},
		{"invalid-image", "not valid base64!", true, "could not be decoded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			messages := (&sessionAgent{}).workaroundProviderMediaLimitations(mediaDeliveryMessages(viewportCaption, tc.data), Model{
				ModelCfg: config.SelectedModel{Provider: "openai"}, CatwalkCfg: catwalk.Model{SupportsImages: tc.vision},
			})
			require.Len(t, messages, 1)
			out := messages[0].Content[0].(fantasy.ToolResultPart).Output.(fantasy.ToolResultOutputContentText)
			require.Contains(t, out.Text, viewportCaption)
			require.Contains(t, out.Text, tc.warning)
		})
	}
}

// TestMediaDeliveryWireRequests checks the actual JSON sent by provider
// clients after a screenshot is stored, reloaded and prepared again.
func TestMediaDeliveryWireRequests(t *testing.T) {
	t.Parallel()
	for _, family := range []string{"anthropic", "openai", "google"} {
		t.Run(family, func(t *testing.T) {
			t.Parallel()
			captured := make(chan []byte, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				select {
				case captured <- body:
				default:
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"captured","type":"invalid_request_error","code":400,"status":"INVALID_ARGUMENT"}}`))
			}))
			defer server.Close()
			var provider fantasy.Provider
			var err error
			switch family {
			case "anthropic":
				provider, err = anthropic.New(anthropic.WithBaseURL(server.URL), anthropic.WithAPIKey("test"))
			case "openai":
				provider, err = openai.New(openai.WithBaseURL(server.URL), openai.WithAPIKey("test"))
			case "google":
				provider, err = google.New(google.WithBaseURL(server.URL), google.WithGeminiAPIKey("test"))
			}
			require.NoError(t, err)
			model, err := provider.LanguageModel(t.Context(), "test-model")
			require.NoError(t, err)
			a := &sessionAgent{}
			data := base64.StdEncoding.EncodeToString([]byte("image bytes"))
			stored := a.convertToToolResult(fantasy.ToolResultContent{ToolCallID: "screenshot-call", ToolName: "browser", Result: fantasy.ToolResultOutputContentMedia{Text: viewportCaption, Data: data, MediaType: "image/png"}})
			reloaded := message.Message{Role: message.Tool, Parts: []message.ContentPart{stored}}
			selected := family
			if family == "google" {
				selected = "gemini"
			}
			prepared := a.workaroundProviderMediaLimitations(reloaded.ToAIMessage(), Model{
				ModelCfg: config.SelectedModel{Provider: selected}, CatwalkCfg: catwalk.Model{SupportsImages: true},
			})
			prompt := fantasy.Prompt{
				fantasy.NewUserMessage("Inspect the canvas"),
				{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{fantasy.ToolCallPart{ToolCallID: "screenshot-call", ToolName: "browser", Input: `{"action":"screenshot"}`}}},
			}
			prompt = append(prompt, prepared...)
			_, _ = model.Generate(context.Background(), fantasy.Call{Prompt: prompt})
			select {
			case body := <-captured:
				var decoded any
				require.NoError(t, json.Unmarshal(body, &decoded))
				// Decode the wire JSON so escaped quotes cannot hide a caption
				// loss or accidentally count a metadata-only field as evidence.
				require.True(t, wireContainsString(decoded, viewportCaption), string(body))
				require.Contains(t, string(body), data, "the image must travel with its caption")
			default:
				t.Fatal("provider sent no request")
			}
		})
	}
}

func wireContainsString(value any, text string) bool {
	switch value := value.(type) {
	case string:
		return strings.Contains(value, text)
	case map[string]any:
		for _, child := range value {
			if wireContainsString(child, text) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if wireContainsString(child, text) {
				return true
			}
		}
	}
	return false
}

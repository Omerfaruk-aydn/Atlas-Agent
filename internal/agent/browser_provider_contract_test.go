package agent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// requestToolSchema returns the parameter schema of one advertised tool as
// the provider format carries it.
func requestToolSchema(t *testing.T, format wireFormat, body string, name string) map[string]any {
	t.Helper()
	var probe struct {
		Tools []struct {
			Name        string         `json:"name"`
			InputSchema map[string]any `json:"input_schema"`
			Function    struct {
				Name       string         `json:"name"`
				Parameters map[string]any `json:"parameters"`
			} `json:"function"`
			FunctionDeclarations []struct {
				Name                 string         `json:"name"`
				Parameters           map[string]any `json:"parameters"`
				ParametersJSONSchema map[string]any `json:"parametersJsonSchema"`
			} `json:"functionDeclarations"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &probe))
	for _, tool := range probe.Tools {
		switch format {
		case wireOpenAI:
			if tool.Function.Name == name {
				return tool.Function.Parameters
			}
		case wireAnthropic:
			if tool.Name == name {
				return tool.InputSchema
			}
		case wireGemini:
			for _, decl := range tool.FunctionDeclarations {
				if decl.Name == name {
					if decl.Parameters != nil {
						return decl.Parameters
					}
					return decl.ParametersJSONSchema
				}
			}
		}
	}
	t.Fatalf("Tool %s is not advertised", name)
	return nil
}

// A browser call that breaks the value contract is refused the same way in
// every provider format: the schema the provider sees carries value, and the
// refusal that goes back to the model names the code and says no input was
// sent. These are recorded wire fixtures, not real provider responses.
func TestBrowserContractAcrossProviderFormats(t *testing.T) {
	for _, format := range []wireFormat{wireOpenAI, wireAnthropic, wireGemini} {
		t.Run(string(format), func(t *testing.T) {
			first, second := "call_1", "call_2"
			if format == wireAnthropic {
				first, second = "toolu_1", "toolu_2"
			}
			h := newPayloadCoordinator(t, format, "", automationTools,
				toolCallReply(format, first, "browser", map[string]any{"action": "set_field", "advanced": map[string]any{"ref": "e3"}}),
				toolCallReply(format, second, "browser", map[string]any{"action": "set_field", "ref": "e3", "valeu": "Ada"}),
				textReply(format, "stopped"),
			)
			_, err := h.coord.Run(t.Context(), newSession(t, h), "Tarayıcıda açık formdaki ad alanını doldur")
			require.NoError(t, err)

			steps := stepTools(t, h)
			require.Len(t, steps, 3)
			require.Contains(t, steps[0], "browser")
			require.Contains(t, steps[0], "tool_pipeline")

			schema := requestToolSchema(t, format, stepBody(t, h, 0), "browser")
			properties, ok := schema["properties"].(map[string]any)
			require.True(t, ok, "browser schema has properties")
			require.Contains(t, properties, "value")
			require.Contains(t, properties, "advanced")
			pipeline := requestToolSchema(t, format, stepBody(t, h, 0), "tool_pipeline")
			pipelineProperties, ok := pipeline["properties"].(map[string]any)
			require.True(t, ok, "pipeline schema has properties")
			require.NotContains(t, pipelineProperties, "desktop", "browser work gets the browser-scoped pipeline")

			missing := stepBody(t, h, 1)
			require.Contains(t, missing, "missing_value")
			require.Contains(t, missing, `\"input_sent\":false`)
			unknown := stepBody(t, h, 2)
			require.Contains(t, unknown, "valeu")
			require.Contains(t, unknown, "unknown_field")
		})
	}
}

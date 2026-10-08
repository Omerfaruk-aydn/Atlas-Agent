package agent

import (
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

// toolPayload is what one advertised tool costs in a request body, in bytes
// of the provider's own JSON. Bytes are measured; tokens are not estimated.
type toolPayload struct {
	Name        string `json:"name"`
	Description int    `json:"description_bytes"`
	Schema      int    `json:"schema_bytes"`
}

type payloadReport struct {
	Format    wireFormat    `json:"format"`
	Prompt    string        `json:"prompt"`
	BodyBytes int           `json:"body_bytes"`
	Tools     []toolPayload `json:"tools"`
}

func measureTools(format wireFormat, body map[string]any) []toolPayload {
	var out []toolPayload
	size := func(v any) int {
		if v == nil {
			return 0
		}
		if s, ok := v.(string); ok {
			return len(s)
		}
		data, _ := json.Marshal(v)
		return len(data)
	}
	tools, _ := body["tools"].([]any)
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		switch format {
		case wireOpenAI:
			fn, _ := tool["function"].(map[string]any)
			out = append(out, toolPayload{Name: fn["name"].(string), Description: size(fn["description"]), Schema: size(fn["parameters"])})
		case wireAnthropic:
			out = append(out, toolPayload{Name: tool["name"].(string), Description: size(tool["description"]), Schema: size(tool["input_schema"])})
		case wireGemini:
			decls, _ := tool["functionDeclarations"].([]any)
			for _, d := range decls {
				decl, _ := d.(map[string]any)
				out = append(out, toolPayload{Name: decl["name"].(string), Description: size(decl["description"]), Schema: size(decl["parameters"])})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Description+out[i].Schema > out[j].Description+out[j].Schema })
	return out
}

// TestBrowserDesignRequestPayload records the first request a browser
// design task sends in each wire format. The browser tool and the pipeline
// must both be callable on the first step: a model cannot group steps with a
// tool whose schema it was never given. ATLAS_PAYLOAD_REPORT=path writes the
// measured sizes as JSON.
func TestBrowserDesignRequestPayload(t *testing.T) {
	prompt := "Browser Use ile Figma'da yeni bir dosya oluştur ve ilk mobil ekranı tasarla"
	var reports []payloadReport
	for _, format := range []wireFormat{wireOpenAI, wireAnthropic, wireGemini} {
		h := newPayloadCoordinator(t, format, "", automationTools)
		_, err := h.coord.Run(t.Context(), newSession(t, h), prompt)
		require.NoError(t, err)
		requests := h.recorder.stepRequests()
		require.NotEmpty(t, requests)
		data, err := json.Marshal(requests[0])
		require.NoError(t, err)
		tools := measureTools(format, requests[0])
		reports = append(reports, payloadReport{Format: format, Prompt: prompt, BodyBytes: len(data), Tools: tools})
	}
	measured := reports
	for _, format := range []wireFormat{wireOpenAI, wireAnthropic, wireGemini} {
		// Desktop work advertises computer and tool_pipeline; measured for
		// comparison only.
		desktop := "Masaüstünde Not Defteri penceresini aç"
		h := newPayloadCoordinator(t, format, "", automationTools)
		_, err := h.coord.Run(t.Context(), newSession(t, h), desktop)
		require.NoError(t, err)
		requests := h.recorder.stepRequests()
		require.NotEmpty(t, requests)
		data, err := json.Marshal(requests[0])
		require.NoError(t, err)
		measured = append(measured, payloadReport{Format: format, Prompt: desktop, BodyBytes: len(data), Tools: measureTools(format, requests[0])})
	}
	if path := os.Getenv("ATLAS_PAYLOAD_REPORT"); path != "" {
		data, err := json.MarshalIndent(measured, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, data, 0o644))
	}
	for _, report := range reports {
		names := map[string]bool{}
		for _, tool := range report.Tools {
			names[tool.Name] = true
		}
		require.True(t, names["browser"], "%s: browser schema on the first step", report.Format)
		require.True(t, names["tool_pipeline"], "%s: tool_pipeline schema on the first step of browser work", report.Format)
	}
}

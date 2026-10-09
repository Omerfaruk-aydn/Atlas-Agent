package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// sessionStringEncodedDesktop is the tool_pipeline input of session
// 69361c0a: desktop arrived as a string, and the encoded object is followed
// by one stray "}".
const sessionStringEncodedDesktop = `{"desktop": "{\"mode\":\"sequence\",\"result\":\"checkpoints\",\"steps\":[{\"input\":{\"action\":\"type\",\"text\":\"1234*56=\",\"automation\":{\"window_id\":\"984086\"}},\"checkpoint\":{\"window_id\":\"984086\",\"element_id\":\"42:12716200:4:10\",\"condition\":\"text\",\"expected\":\"Ekran değeri 69.104\",\"wait_ms\":3000}},{\"input\":{\"action\":\"type\",\"text\":\"/8=\",\"automation\":{\"window_id\":\"984086\"}},\"checkpoint\":{\"window_id\":\"984086\",\"element_id\":\"42:12716200:4:10\",\"condition\":\"text\",\"expected\":\"Ekran değeri 8.638\",\"wait_ms\":3000}},{\"input\":{\"action\":\"type\",\"text\":\"+362=\",\"automation\":{\"window_id\":\"984086\"}},\"checkpoint\":{\"window_id\":\"984086\",\"element_id\":\"42:12716200:4:10\",\"condition\":\"text\",\"expected\":\"Ekran değeri 9.000\",\"wait_ms\":3000}}]}}"}`

// recordingInvoke counts every child call a pipeline dispatches. Any count
// above zero means a tool call left the pipeline.
type recordingInvoke struct {
	mu    sync.Mutex
	calls []fantasy.ToolCall
	reply func(fantasy.ToolCall) fantasy.ToolResponse
}

func (r *recordingInvoke) invoke(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	r.mu.Lock()
	r.calls = append(r.calls, call)
	r.mu.Unlock()
	if r.reply != nil {
		return r.reply(call), nil
	}
	return fantasy.NewTextResponse(`{"result":[]}`), nil
}

func (r *recordingInvoke) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func runPipelineInput(t *testing.T, input string, rec *recordingInvoke) fantasy.ToolResponse {
	t.Helper()
	resp, err := NewToolPipeline(rec.invoke).Run(t.Context(), fantasy.ToolCall{ID: "call", Name: ToolPipelineName, Input: input})
	require.NoError(t, err)
	return resp
}

func pipelineContractOf(t *testing.T, resp fantasy.ToolResponse) computer.ContractError {
	t.Helper()
	_, document, ok := strings.Cut(resp.Content, "Tool contract: ")
	require.True(t, ok, resp.Content)
	var contract computer.ContractError
	require.NoError(t, json.Unmarshal([]byte(document), &contract), resp.Content)
	return contract
}

// The real session input is malformed after decoding (trailing "}"), so it
// is rejected with a structured error naming the field and type, and
// nothing is dispatched. Before, the model saw only a Go type error.
func TestSessionStringEncodedDesktopIsRejectedStructurally(t *testing.T) {
	t.Parallel()
	rec := &recordingInvoke{}
	resp := runPipelineInput(t, sessionStringEncodedDesktop, rec)
	require.True(t, resp.IsError)
	require.True(t, strings.HasPrefix(resp.Content, "invalid_argument_type:"), resp.Content)
	require.NotContains(t, resp.Content, "jsontext")
	contract := pipelineContractOf(t, resp)
	require.Equal(t, "desktop", contract.Field)
	require.Equal(t, "object", contract.ExpectedType)
	require.False(t, contract.InputSent)
	require.Equal(t, "none", contract.Effect)
	require.Contains(t, contract.Message, "trailing content")
	var example map[string]any
	require.NoError(t, json.Unmarshal([]byte(contract.Example), &example), "the example is valid JSON")
	_, isObject := example["desktop"].(map[string]any)
	require.True(t, isObject, "the example shows desktop as an object")
	require.Zero(t, rec.count(), "no child call was dispatched")
}

// One string layer around exactly one object is decoded once, validated
// through the ordinary decoder and runtime, executed and reported.
func TestSingleStringEncodedObjectIsNormalizedAndReported(t *testing.T) {
	t.Parallel()
	object := `{"mode":"sequence","result":"checkpoints","steps":[{"input":{"action":"type","text":"1234*56=","automation":{"window_id":"984086"}},"checkpoint":{"window_id":"984086","element_id":"42:1","condition":"text","expected":"Ekran değeri 69.104"}}]}`
	encoded, err := json.Marshal(object)
	require.NoError(t, err)
	var direct, normalized PipelineParams
	require.NoError(t, json.Unmarshal([]byte(`{"desktop":`+object+`}`), &direct))
	require.NoError(t, json.Unmarshal([]byte(`{"desktop":`+string(encoded)+`}`), &normalized))
	require.Equal(t, direct.Desktop, normalized.Desktop, "the decoded plan equals the object form")
	require.Empty(t, direct.normalized)
	require.Equal(t, []PipelineNormalization{{Field: "desktop", From: "json_string", To: "object"}}, normalized.normalized)

	// Generic steps: arguments is a known object field too. The child call
	// receives the decoded object, and the result says it was normalized.
	rec := &recordingInvoke{}
	arguments, err := json.Marshal(`{"action":"windows"}`)
	require.NoError(t, err)
	resp := runPipelineInput(t, `{"steps":[{"id":"list","tool":"computer","arguments":`+string(arguments)+`}]}`, rec)
	require.False(t, resp.IsError, resp.Content)
	require.Equal(t, 1, rec.count())
	require.JSONEq(t, `{"action":"windows"}`, rec.calls[0].Input)
	var result struct {
		Normalization []PipelineNormalization `json:"argument_normalization"`
		Results       []PipelineResult        `json:"results"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &result), resp.Content)
	require.Equal(t, "steps[0].arguments", result.Normalization[0].Field)
	require.Len(t, result.Results, 1)
}

// Everything except one string layer around one object is refused before any
// dispatch: free text, arrays, null, a second layer, malformed JSON, trailing
// content, oversized strings and non-object types.
func TestPipelineObjectFieldsRejectEverythingElse(t *testing.T) {
	t.Parallel()
	quote := func(s string) string {
		data, _ := json.Marshal(s)
		return string(data)
	}
	twice := quote(quote(`{"mode":"observe","window_id":"1"}`))
	cases := map[string]string{
		"free text":         `{"desktop":"observe the calculator"}`,
		"array in string":   `{"desktop":` + quote(`[{"mode":"observe"}]`) + `}`,
		"null in string":    `{"desktop":"null"}`,
		"second layer":      `{"desktop":` + twice + `}`,
		"malformed":         `{"desktop":` + quote(`{"mode":"observe",`) + `}`,
		"trailing object":   `{"desktop":` + quote(`{"mode":"observe"}{"mode":"act"}`) + `}`,
		"trailing text":     `{"desktop":` + quote(`{"mode":"observe"} please`) + `}`,
		"array value":       `{"desktop":[{"mode":"observe"}]}`,
		"number value":      `{"desktop":7}`,
		"boolean value":     `{"browser":true}`,
		"flow free text":    `{"flow":"run my flow"}`,
		"arguments text":    `{"steps":[{"id":"a","tool":"computer","arguments":"windows"}]}`,
		"arguments trailer": `{"steps":[{"id":"a","tool":"computer","arguments":` + quote(`{"action":"windows"}}`) + `}]}`,
		"oversized":         `{"desktop":` + quote(`{"mode":"observe","window_id":"`+strings.Repeat("1", maxEncodedObjectBytes)+`"}`) + `}`,
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rec := &recordingInvoke{}
			resp := runPipelineInput(t, input, rec)
			require.True(t, resp.IsError, resp.Content)
			require.True(t, strings.HasPrefix(resp.Content, "invalid_argument_type:"), resp.Content)
			contract := pipelineContractOf(t, resp)
			require.Equal(t, "object", contract.ExpectedType)
			require.False(t, contract.InputSent)
			require.Equal(t, "none", contract.Effect)
			require.NotEmpty(t, contract.Example)
			require.Zero(t, rec.count(), "no child call was dispatched")
		})
	}
}

// Normalization never weakens the decoded object's own validation: unknown
// fields and wrong nesting inside a normalized desktop are still refused,
// and ordinary text fields are never parsed as JSON.
func TestNormalizedObjectKeepsValidationAndTextFields(t *testing.T) {
	t.Parallel()
	quote := func(s string) string {
		data, _ := json.Marshal(s)
		return string(data)
	}
	rec := &recordingInvoke{}
	resp := runPipelineInput(t, `{"desktop":`+quote(`{"mode":"observe","window_id":"1","action":"click"}`)+`}`, rec)
	require.True(t, resp.IsError)
	require.True(t, strings.HasPrefix(resp.Content, "unknown_field:"), resp.Content)
	require.Zero(t, rec.count())

	resp = runPipelineInput(t, `{"desktop":`+quote(`{"desktop":{"mode":"observe"}}`)+`}`, rec)
	require.True(t, strings.HasPrefix(resp.Content, "invalid_nesting:"), resp.Content)
	require.Zero(t, rec.count())

	// A text argument that happens to look like JSON stays text.
	resp = runPipelineInput(t, `{"steps":[{"id":"t","tool":"computer","arguments":{"action":"type","text":"{\"a\":1}","automation":{"window_id":"1"}}}]}`, rec)
	require.False(t, resp.IsError, resp.Content)
	require.Equal(t, 1, rec.count())
	var sent map[string]any
	require.NoError(t, json.Unmarshal([]byte(rec.calls[0].Input), &sent))
	require.Equal(t, `{"a":1}`, sent["text"])
	require.NotContains(t, resp.Content, "argument_normalization")
}

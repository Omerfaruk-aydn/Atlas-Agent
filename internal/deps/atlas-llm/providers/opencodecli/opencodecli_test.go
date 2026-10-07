package opencodecli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func testCall() fantasy.Call {
	return fantasy.Call{Prompt: fantasy.Prompt{fantasy.NewUserMessage("Find a file")}, Tools: []fantasy.Tool{fantasy.FunctionTool{Name: "view", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"}, "additionalProperties": false}}}}
}

func TestReplyValidation(t *testing.T) {
	t.Parallel()
	call := testCall()
	r, err := decodeReply(`{"text":"Inspecting","tool_calls":[{"name":"view","arguments":{"path":"a.go"}}]}`, call)
	require.NoError(t, err)
	require.Equal(t, fantasy.FinishReasonToolCalls, r.FinishReason)
	require.False(t, r.Content.ToolCalls()[0].ProviderExecuted)
	for _, reply := range []string{
		`{"text":"","tool_calls":[{"name":"bash","arguments":{}}]}`,
		`{"text":"","tool_calls":[{"name":"view","arguments":{}}]}`,
		`{"text":"","tool_calls":[{"name":"view","arguments":{"path":123}}]}`,
		`{"text":"","tool_calls":[{"name":"view","arguments":"{}"}]}`,
		`{"text":"OK","tool_calls":[]} {"text":"other"}`,
		`{"text":"","tool_calls":[]}`,
		`{"text":"OK","tool_calls":[],"execute":true}`,
	} {
		_, err := decodeReply(reply, call)
		require.Error(t, err, reply)
	}
	none := fantasy.ToolChoiceNone
	call.ToolChoice = &none
	_, err = decodeReply(`{"text":"","tool_calls":[{"name":"view","arguments":{"path":"a"}}]}`, call)
	require.Error(t, err)
	required := fantasy.ToolChoiceRequired
	call.ToolChoice = &required
	_, err = decodeReply(`{"text":"Done","tool_calls":[]}`, call)
	require.Error(t, err)
}

func TestCLIEvents(t *testing.T) {
	t.Parallel()
	result, err := parseEvents(strings.NewReader("{\"type\":\"text\",\"part\":{\"text\":\"hello\"}}\n{\"type\":\"step_finish\",\"part\":{\"reason\":\"stop\",\"tokens\":{\"input\":10,\"output\":2,\"total\":12}}}\n"))
	require.NoError(t, err)
	require.Equal(t, "hello", result.text)
	require.EqualValues(t, 12, result.usage.TotalTokens)
	for _, events := range []string{`{"type":"tool_use"}`, `{"type":"error","error":{"message":"denied"}}`, `{"type":"text","part":{"text":"partial"}}`, `not-json`, `{"type":"step_finish","part":{"reason":"length"}}`} {
		_, err := parseEvents(strings.NewReader(events))
		require.Error(t, err)
	}
}

func TestIsolatedConfiguration(t *testing.T) {
	t.Parallel()
	env := bridgeEnvironment([]string{"OPENCODE_CONFIG_CONTENT=unsafe", "xdg_config_home=unsafe", "KEEP=value"}, "isolated", "isolated/config.json", bridgeConfig())
	joined := strings.Join(env, "\n")
	require.NotContains(t, joined, "unsafe")
	require.Contains(t, joined, "KEEP=value")
	require.Contains(t, joined, `"*":"ask"`)
	prompt, err := requestPrompt(testCall())
	require.NoError(t, err)
	require.Contains(t, prompt, "Find a file")
	require.Contains(t, prompt, "input_schema")
	call := testCall()
	call.Prompt[0].Content = append(call.Prompt[0].Content, fantasy.FilePart{Data: []byte("image"), MediaType: "image/png"})
	_, err = requestPrompt(call)
	require.ErrorContains(t, err, "attachments")
}

func TestLiveOpenCodeCLI(t *testing.T) {
	if os.Getenv("ATLAS_TEST_OPENCODE_CLI") != "1" {
		t.Skip("Opt-in live free-model test")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	p, err := New(Options{})
	require.NoError(t, err)
	m, err := p.LanguageModel(ctx, "mimo-v2.6-flash-free")
	require.NoError(t, err)
	r, err := m.Generate(ctx, fantasy.Call{Prompt: fantasy.Prompt{fantasy.NewUserMessage("Reply only OK; do not use tools.")}})
	require.NoError(t, err)
	require.Equal(t, "OK", strings.TrimSpace(r.Content.Text()))
	call := testCall()
	choice := fantasy.SpecificToolChoice("view")
	call.ToolChoice = &choice
	call.Prompt = fantasy.Prompt{fantasy.NewUserMessage("Request the supplied view tool with path demo.go. Do not claim you already read it.")}
	r, err = m.Generate(ctx, call)
	require.NoError(t, err)
	require.Len(t, r.Content.ToolCalls(), 1)
	require.JSONEq(t, `{"path":"demo.go"}`, r.Content.ToolCalls()[0].Input)

	count := 0
	type echoInput struct {
		Value string `json:"value"`
	}
	tool := fantasy.NewAgentTool("atlas_echo", "Return an authoritative test value", func(_ context.Context, input echoInput, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
		count++
		return fantasy.NewTextResponse("verified-" + input.Value), nil
	})
	agent := fantasy.NewAgent(m, fantasy.WithTools(tool), fantasy.WithStopConditions(fantasy.StepCountIs(3)))
	agentResult, err := agent.Generate(ctx, fantasy.AgentCall{Prompt: "Call atlas_echo once with value bridge-test. After its result, reply only with the actual returned value. Do not invent the result."})
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Contains(t, agentResult.Response.Content.Text(), "verified-bridge-test")
}

func TestLiveNativePermissionsAreRejected(t *testing.T) {
	if os.Getenv("ATLAS_TEST_OPENCODE_CLI") != "1" {
		t.Skip("Opt-in live permission isolation test")
	}
	marker := filepath.Join(t.TempDir(), "native-marker.txt")
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	_, err := runCLI(ctx, Options{}, "mimo-v2.6-flash-free", "Use your own native write tool to create the file "+marker+" with text native-marker. Do not answer without calling that native tool. Do not output a bridge JSON object.")
	require.Error(t, err)
	_, statErr := os.Stat(marker)
	require.True(t, os.IsNotExist(statErr), "Native write must not bypass Atlas")
}

func TestLiveCancellation(t *testing.T) {
	if os.Getenv("ATLAS_TEST_OPENCODE_CLI") != "1" {
		t.Skip("Opt-in live cancellation test")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := runCLI(ctx, Options{}, "mimo-v2.6-flash-free", "Reply OK")
	require.Error(t, err)
	require.Less(t, time.Since(started), 5*time.Second)
}

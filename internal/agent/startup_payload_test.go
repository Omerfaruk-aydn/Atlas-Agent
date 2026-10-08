package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

// wireFormat is one provider API family the mock server speaks.
type wireFormat string

const (
	wireOpenAI    wireFormat = "openai-compat"
	wireAnthropic wireFormat = "anthropic"
	wireGemini    wireFormat = "google"
)

type recordedRequest struct {
	Path string
	Body []byte
}

// requestRecorder is a local model endpoint. Requests that carry tools are
// conversation steps and consume scripted replies in order; requests
// without tools (title generation) get a plain text reply.
type requestRecorder struct {
	format   wireFormat
	mu       sync.Mutex
	requests []recordedRequest
	replies  []string
	// failures maps a conversation-step index to an HTTP status returned
	// once for that step before the scripted reply.
	failures map[int]int
	steps    int
	// untooled replies to requests without tools, such as title generation.
	untooled string
	// untooledDelay holds those replies back, e.g. to make title
	// generation finish after the conversation step.
	untooledDelay time.Duration
}

func (r *requestRecorder) handler(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	r.requests = append(r.requests, recordedRequest{Path: req.URL.Path, Body: body})
	reply := ""
	if len(requestToolNames(r.format, body)) > 0 {
		if status, ok := r.failures[r.steps]; ok {
			delete(r.failures, r.steps)
			r.mu.Unlock()
			w.Header().Set("retry-after-ms", "1")
			http.Error(w, `{"error":{"message":"temporary","type":"overloaded_error"}}`, status)
			return
		}
		r.steps++
		if len(r.replies) > 0 {
			reply, r.replies = r.replies[0], r.replies[1:]
		}
	}
	untooled, delay := r.untooled, r.untooledDelay
	r.mu.Unlock()
	if reply == "" {
		time.Sleep(delay)
		reply = untooled
	}
	if reply == "" {
		reply = textReply(r.format, "Merhaba")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, reply)
}

// stepRequests returns the bodies of conversation steps, in order.
func (r *requestRecorder) stepRequests() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []map[string]any
	for _, request := range r.requests {
		if len(requestToolNames(r.format, request.Body)) == 0 {
			continue
		}
		var body map[string]any
		if json.Unmarshal(request.Body, &body) == nil {
			out = append(out, body)
		}
	}
	return out
}

func (r *requestRecorder) stepPaths() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, request := range r.requests {
		if len(requestToolNames(r.format, request.Body)) > 0 {
			out = append(out, request.Path)
		}
	}
	return out
}

// requestToolNames extracts advertised tool names from any supported wire
// format.
func requestToolNames(format wireFormat, body []byte) []string {
	var probe struct {
		Tools []struct {
			Name     string `json:"name"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
			FunctionDeclarations []struct {
				Name string `json:"name"`
			} `json:"functionDeclarations"`
		} `json:"tools"`
	}
	if json.Unmarshal(body, &probe) != nil {
		return nil
	}
	var names []string
	for _, tool := range probe.Tools {
		switch format {
		case wireOpenAI:
			names = append(names, tool.Function.Name)
		case wireAnthropic:
			names = append(names, tool.Name)
		case wireGemini:
			for _, decl := range tool.FunctionDeclarations {
				names = append(names, decl.Name)
			}
		}
	}
	return names
}

func sse(events ...any) string {
	var b strings.Builder
	for _, event := range events {
		data, _ := json.Marshal(event)
		if typed, ok := event.(map[string]any); ok {
			if name, ok := typed["type"].(string); ok && strings.Contains(name, "_") {
				fmt.Fprintf(&b, "event: %s\n", name)
			}
		}
		fmt.Fprintf(&b, "data: %s\n\n", data)
	}
	return b.String()
}

func textReply(format wireFormat, text string) string {
	switch format {
	case wireAnthropic:
		return sse(
			map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_1", "type": "message", "role": "assistant", "content": []any{}, "model": "mock-model", "usage": map[string]any{"input_tokens": 10, "output_tokens": 1}}},
			map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}},
			map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": text}},
			map[string]any{"type": "content_block_stop", "index": 0},
			map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": 2}},
			map[string]any{"type": "message_stop"},
		)
	case wireGemini:
		return sse(map[string]any{
			"candidates":    []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": text}}}, "finishReason": "STOP"}},
			"usageMetadata": map[string]any{"promptTokenCount": 10, "candidatesTokenCount": 1, "totalTokenCount": 11},
		})
	default:
		return openAIText(text)
	}
}

// toolCallReply scripts one model step that calls a tool. Anthropic replies
// may carry a signed thinking block, which must survive into the next
// request's history.
func toolCallReply(format wireFormat, id, name string, args any, signature ...string) string {
	input, _ := json.Marshal(args)
	switch format {
	case wireAnthropic:
		events := []any{map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_" + id, "type": "message", "role": "assistant", "content": []any{}, "model": "mock-model", "usage": map[string]any{"input_tokens": 10, "output_tokens": 1}}}}
		index := 0
		if len(signature) > 0 {
			events = append(events,
				map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "thinking", "thinking": "", "signature": ""}},
				map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "thinking_delta", "thinking": "Need the tool."}},
				map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "signature_delta", "signature": signature[0]}},
				map[string]any{"type": "content_block_stop", "index": 0},
			)
			index = 1
		}
		events = append(events,
			map[string]any{"type": "content_block_start", "index": index, "content_block": map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{}}},
			map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(input)}},
			map[string]any{"type": "content_block_stop", "index": index},
			map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use"}, "usage": map[string]any{"output_tokens": 5}},
			map[string]any{"type": "message_stop"},
		)
		return sse(events...)
	case wireGemini:
		return sse(map[string]any{
			"candidates":    []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{map[string]any{"functionCall": map[string]any{"name": name, "args": args}}}}, "finishReason": "STOP"}},
			"usageMetadata": map[string]any{"promptTokenCount": 10, "candidatesTokenCount": 1, "totalTokenCount": 11},
		})
	default:
		return openAIToolCall(id, name, args)
	}
}

func openAIText(text string) string {
	chunk, _ := json.Marshal(map[string]any{"id": "c1", "object": "chat.completion.chunk", "created": 1, "model": "mock-model", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": text}}}})
	done, _ := json.Marshal(map[string]any{"id": "c1", "object": "chat.completion.chunk", "created": 1, "model": "mock-model", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 1, "total_tokens": 11}})
	return "data: " + string(chunk) + "\n\ndata: " + string(done) + "\n\ndata: [DONE]\n\n"
}

func openAIToolCall(id, name string, args any) string {
	input, _ := json.Marshal(args)
	chunk, _ := json.Marshal(map[string]any{"id": "c1", "object": "chat.completion.chunk", "created": 1, "model": "mock-model", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": id, "type": "function", "function": map[string]any{"name": name, "arguments": string(input)}}}}}}})
	done, _ := json.Marshal(map[string]any{"id": "c1", "object": "chat.completion.chunk", "created": 1, "model": "mock-model", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 1, "total_tokens": 11}})
	return "data: " + string(chunk) + "\n\ndata: " + string(done) + "\n\ndata: [DONE]\n\n"
}

type payloadHarness struct {
	coord    Coordinator
	recorder *requestRecorder
	env      fakeEnv
	cfg      *config.ConfigStore
}

// newPayloadCoordinator wires the production coordinator, the full default
// coder palette and the real system prompt against a local recording
// server. options is spliced into the "options" object; extra is spliced
// into the top-level config object.
func newPayloadCoordinator(t *testing.T, format wireFormat, options, extra string, replies ...string) payloadHarness {
	t.Helper()
	for _, name := range []string{"GLOBAL_CONFIG", "GLOBAL_DATA", "CACHE_DIR"} {
		t.Setenv("ATLAS_AGENT_"+name, t.TempDir())
	}
	recorder := &requestRecorder{format: format, replies: replies, failures: map[int]int{}}
	server := httptest.NewServer(http.HandlerFunc(recorder.handler))
	t.Cleanup(server.Close)
	env := testEnv(t)
	baseURL := server.URL
	if format == wireOpenAI {
		baseURL += "/v1"
	}
	atlasJSON := `{
  "options": {"disable_default_providers": true, "disable_provider_auto_update": true` + options + `},
  "providers": {
    "mock": {"id": "mock", "name": "Mock", "type": "` + string(format) + `", "base_url": "` + baseURL + `", "api_key": "test-key",
      "models": [{"id": "mock-model", "name": "Mock", "context_window": 200000, "default_max_tokens": 1024}]},
    "backup": {"id": "backup", "name": "Backup", "type": "` + string(format) + `", "base_url": "` + strings.Replace(baseURL, "/v1", "/backup/v1", 1) + `", "api_key": "test-key",
      "models": [{"id": "mock-model", "name": "Backup", "context_window": 200000, "default_max_tokens": 1024}]}},
  "models": {"large": {"provider": "mock", "model": "mock-model"},
             "small": {"provider": "mock", "model": "mock-model"}}` + extra + `
}`
	require.NoError(t, os.WriteFile(filepath.Join(env.workingDir, "atlas.json"), []byte(atlasJSON), 0o644))
	cfg, err := config.Init(env.workingDir, "", false)
	require.NoError(t, err)
	cfg.SetupAgents()
	coord, err := NewCoordinator(t.Context(), CoordinatorOptions{
		Config:      cfg,
		Sessions:    env.sessions,
		Messages:    env.messages,
		Permissions: permission.NewPermissionService(env.workingDir, true, nil),
		History:     env.history,
		FileTracker: *env.filetracker,
	})
	require.NoError(t, err)
	t.Cleanup(coord.CancelAll)
	return payloadHarness{coord: coord, recorder: recorder, env: env, cfg: cfg}
}

type toolSize struct {
	Name  string `json:"name"`
	Bytes int    `json:"bytes"`
}

type payloadMeasurement struct {
	BodyBytes   int            `json:"body_bytes"`
	SystemBytes int            `json:"system_bytes"`
	ToolsBytes  int            `json:"tools_bytes"`
	ToolCount   int            `json:"tool_count"`
	Largest     []toolSize     `json:"largest_tools"`
	ToolNames   []string       `json:"tool_names"`
	Sections    map[string]int `json:"system_sections"`
}

var systemSections = []string{"available_skills", "engineering_workflow", "prompt_recipe", "project_context", "user_preferences", "execution_status", "deferred_tools", "tool_protocol"}

// measurePayload sizes an OpenAI-format request body as sent on the wire.
func measurePayload(t *testing.T, body map[string]any) payloadMeasurement {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	m := payloadMeasurement{BodyBytes: len(raw), Sections: map[string]int{}}
	for _, msg := range body["messages"].([]any) {
		msg := msg.(map[string]any)
		if msg["role"] != "system" {
			continue
		}
		data, _ := json.Marshal(msg["content"])
		m.SystemBytes += len(data)
		text, _ := msg["content"].(string)
		for _, section := range systemSections {
			start := strings.Index(text, "<"+section)
			end := strings.Index(text, "</"+section+">")
			if start >= 0 && end > start {
				m.Sections[section] += end - start
			}
		}
	}
	for _, tool := range body["tools"].([]any) {
		data, _ := json.Marshal(tool)
		name := tool.(map[string]any)["function"].(map[string]any)["name"].(string)
		m.ToolsBytes += len(data)
		m.ToolCount++
		m.Largest = append(m.Largest, toolSize{name, len(data)})
		m.ToolNames = append(m.ToolNames, name)
	}
	slices.SortFunc(m.Largest, func(a, b toolSize) int { return b.Bytes - a.Bytes })
	m.Largest = m.Largest[:min(len(m.Largest), 12)]
	return m
}

const automationTools = `, "tools": {"browser": {"enabled": true}, "computer": {"enabled": true}}`

func runGreeting(t *testing.T, options, extra string) payloadMeasurement {
	t.Helper()
	h := newPayloadCoordinator(t, wireOpenAI, options, extra)
	sess, err := h.env.sessions.Create(t.Context(), "payload")
	require.NoError(t, err)
	_, err = h.coord.Run(t.Context(), sess.ID, "merhaba")
	require.NoError(t, err)
	requests := h.recorder.stepRequests()
	require.Len(t, requests, 1)
	return measurePayload(t, requests[0])
}

// TestStartupPayloadReport writes the before/after measurement used in the
// delivery report. It runs only on request.
func TestStartupPayloadReport(t *testing.T) {
	out := os.Getenv("ATLAS_PAYLOAD_REPORT")
	if out == "" {
		t.Skip("set ATLAS_PAYLOAD_REPORT to write a payload report")
	}
	report := map[string]payloadMeasurement{}
	for name, c := range map[string][2]string{
		"default": {"", ""}, "explicit_false": {`, "defer_tool_schemas": false`, ""}, "explicit_true": {`, "defer_tool_schemas": true`, ""},
		"automation_default": {"", automationTools}, "automation_explicit_false": {`, "defer_tool_schemas": false`, automationTools},
	} {
		report[name] = runGreeting(t, c[0], c[1])
	}
	data, err := json.MarshalIndent(report, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(out, data, 0o644))
}

// Package webtest provides fixtures for testing the web client against a
// real Atlas server without a paid model: a scripted OpenAI-compatible
// model endpoint and the config that points Atlas at it.
//
// The fake model is driven by markers in the user's prompt, so a test
// can ask the real agent to call a real tool:
//
//	[[ask]]          call the question tool with a free-text question
//	[[write NAME]]   call the write tool to create NAME (needs permission)
//	[[bash]]         call the bash tool with a harmless echo
//	[[slow]]         stream a long answer slowly, for cancellation
//	[[big]]          answer with a large markdown document
//	[[orchestrate]]  play the orchestrator mode with two background workers
//
// Anything else is echoed back as markdown. After a tool result the model
// answers once with a summary that quotes the result.
package webtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// FakeLLM is a scripted OpenAI-compatible chat completions server.
type FakeLLM struct {
	*httptest.Server
	mu       sync.Mutex
	requests []map[string]any
	calls    atomic.Int64
	// SlowDelay is the delay between chunks of a [[slow]] answer.
	SlowDelay time.Duration
}

// NewFakeLLM starts the fake model server.
func NewFakeLLM() *FakeLLM {
	f := &FakeLLM{SlowDelay: 150 * time.Millisecond}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}

// Requests returns the decoded request bodies received so far.
func (f *FakeLLM) Requests() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.requests...)
}

// Calls counts chat completion requests.
func (f *FakeLLM) Calls() int64 { return f.calls.Load() }

type chatMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"tool_call_id"`
	ToolCalls  []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tool_calls"`
}

type chatRequest struct {
	Model    string           `json:"model"`
	Stream   bool             `json:"stream"`
	Messages []chatMessage    `json:"messages"`
	Tools    []map[string]any `json:"tools"`
}

// text returns a message's text, whichever content shape it uses.
func (m chatMessage) text() string {
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(m.Content, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			b.WriteString(p.Text)
		}
		return b.String()
	}
	return ""
}

func (m chatMessage) hasImage() bool {
	var parts []struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(m.Content, &parts) != nil {
		return false
	}
	for _, p := range parts {
		if p.Type == "image_url" {
			return true
		}
	}
	return false
}

var writeMarker = regexp.MustCompile(`\[\[write ([^\]\s]+)\]\]`)

func (f *FakeLLM) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"fake-model","object":"model"},{"id":"fake-small","object":"model"}]}`))
			return
		}
		http.NotFound(w, r)
		return
	}
	f.calls.Add(1)
	var raw map[string]any
	var req chatRequest
	body := json.NewDecoder(r.Body)
	if err := body.Decode(&raw); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	enc, _ := json.Marshal(raw)
	_ = json.Unmarshal(enc, &req)
	f.mu.Lock()
	f.requests = append(f.requests, raw)
	f.mu.Unlock()

	if strings.Contains(string(enc), "[[error]]") {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Should-Retry", "false")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"scripted regression failure","type":"invalid_request_error"}}`))
		return
	}
	reply := f.script(req)
	if req.Stream {
		f.stream(w, r, req.Model, reply)
		return
	}
	f.respond(w, req.Model, reply)
}

type scriptedReply struct {
	text     []string
	delay    time.Duration
	toolName string
	toolArgs string
}

func (f *FakeLLM) script(req chatRequest) scriptedReply {
	if len(req.Tools) == 0 {
		// Auxiliary calls (titles, summaries) have no tools.
		return scriptedReply{text: []string{"Fake session title"}}
	}
	// A review (the reviewer is handed <changed_files>) answers with one
	// finding in the format the reviewer is told to use.
	for _, m := range req.Messages {
		if m.Role == "user" && strings.Contains(m.text(), "<changed_files>") {
			if strings.Contains(m.text(), "[[review-slow]]") {
				return scriptedReply{text: []string{"Reading... "}, delay: 30 * time.Second}
			}
			return scriptedReply{text: []string{"I read the diff.\n\n```json\n" + reviewFindingsJSON + "\n```"}}
		}
	}
	if reply, ok := orchestrateScript(req); ok {
		return reply
	}
	var lastUser string
	var userHadImage bool
	lastUserIndex := -1
	for i := len(req.Messages) - 1; i >= 0; i-- {
		m := req.Messages[i]
		if m.Role != "user" {
			continue
		}
		text := strings.TrimSpace(m.text())
		if strings.HasPrefix(text, "<") {
			continue
		}
		lastUser = strings.TrimPrefix(text, "User steering for the active turn: ")
		lastUserIndex = i
		userHadImage = m.hasImage()
		break
	}
	for i := len(req.Messages) - 1; i > lastUserIndex; i-- {
		if req.Messages[i].Role != "tool" {
			continue
		}
		result := req.Messages[i].text()
		if len(result) > 300 {
			result = result[:300]
		}
		return scriptedReply{text: []string{"Tool finished. Result: ", "`" + strings.ReplaceAll(result, "`", "'") + "`"}}
	}
	wantedTool := ""
	switch {
	case strings.Contains(lastUser, "[[ask]]"):
		wantedTool = "question"
	case writeMarker.MatchString(lastUser):
		wantedTool = "write"
	case strings.Contains(lastUser, "[[bash]]"), strings.Contains(lastUser, "[[permission]]"):
		wantedTool = "bash"
	}
	if wantedTool != "" && !toolOffered(req.Tools, wantedTool) {
		args, _ := json.Marshal(map[string]any{"names": []string{wantedTool}})
		return scriptedReply{toolName: "tool_search", toolArgs: string(args)}
	}
	switch {
	case strings.Contains(lastUser, "[[ask]]"):
		args, _ := json.Marshal(map[string]any{"questions": []map[string]any{{
			"type":        "free_text",
			"label":       "Name",
			"purpose":     "information",
			"question":    "What should the file be called?",
			"description": "Pick any short name.",
		}}})
		return scriptedReply{toolName: "question", toolArgs: string(args)}
	case writeMarker.MatchString(lastUser):
		name := writeMarker.FindStringSubmatch(lastUser)[1]
		args, _ := json.Marshal(map[string]string{"file_path": name, "content": "written by the fake model\n"})
		return scriptedReply{toolName: "write", toolArgs: string(args)}
	case strings.Contains(lastUser, "[[permission]]"):
		args, _ := json.Marshal(map[string]string{"description": "Request explicit permission", "command": "rm -f .atlas-e2e-permission-probe"})
		return scriptedReply{toolName: "bash", toolArgs: string(args)}
	case strings.Contains(lastUser, "[[bash]]"):
		args, _ := json.Marshal(map[string]string{"description": "Print a marker", "command": "echo pipeline-output"})
		return scriptedReply{toolName: "bash", toolArgs: string(args)}
	case strings.Contains(lastUser, "[[slow]]"):
		chunks := make([]string, 80)
		for i := range chunks {
			chunks[i] = fmt.Sprintf("tick %d ", i)
		}
		return scriptedReply{text: chunks, delay: f.SlowDelay}
	case strings.Contains(lastUser, "[[big]]"):
		return scriptedReply{text: bigDocument()}
	}
	echo := "Echo: " + lastUser
	if userHadImage {
		echo += "\n\n(image received)"
	}
	return scriptedReply{text: []string{echo[:len(echo)/2], echo[len(echo)/2:]}}
}

var workerMarker = regexp.MustCompile(`\[\[worker (\w+)\]\]`)

// OrchestratorWorkers are the workers an [[orchestrate]] prompt starts.
var OrchestratorWorkers = []string{"alpha", "beta"}

// WorkerDelay is how long a scripted worker takes over its final answer,
// so the orchestrator is still waiting for it when it first tries to stop.
var WorkerDelay = 700 * time.Millisecond

// orchestrateScript plays the orchestrator mode end to end. The session
// whose user prompt holds [[orchestrate]] starts one background worker per
// name in OrchestratorWorkers, then tries to finish before collecting them
// (the harness must send it back), then collects them one at a time with
// agent_result and reports. A session whose prompt holds [[worker NAME]]
// posts on the team board when it can and answers slowly.
func orchestrateScript(req chatRequest) (scriptedReply, bool) {
	call := func(name string, args map[string]any) (scriptedReply, bool) {
		if !toolOffered(req.Tools, name) {
			raw, _ := json.Marshal(map[string]any{"names": []string{name}})
			return scriptedReply{toolName: "tool_search", toolArgs: string(raw)}, true
		}
		raw, _ := json.Marshal(args)
		return scriptedReply{toolName: name, toolArgs: string(raw)}, true
	}
	trigger := -1
	for i, m := range req.Messages {
		if m.Role == "user" && strings.Contains(m.text(), "[[orchestrate]]") {
			trigger = i
			break
		}
	}
	if trigger >= 0 {
		var agents, collected int
		sentBack := false
		lastResult := ""
		for _, m := range req.Messages[trigger+1:] {
			for _, c := range m.ToolCalls {
				switch c.Function.Name {
				case "agent":
					agents++
				case "agent_result":
					collected++
				}
			}
			if m.Role == "tool" {
				lastResult = m.text()
			}
			if m.Role != "tool" && strings.Contains(m.text(), "Do not finish yet") {
				sentBack = true
			}
		}
		switch {
		case agents < len(OrchestratorWorkers):
			name := OrchestratorWorkers[agents]
			return call("agent", map[string]any{
				"agent_name":        "backend",
				"prompt":            "[[worker " + name + "]] Post your status on the team board for @lead, then report.",
				"run_in_background": true,
			})
		case !sentBack:
			return scriptedReply{text: []string{"Both workers are started."}}, true
		case collected < len(OrchestratorWorkers):
			return call("agent_result", map[string]any{"wait": true, "wait_timeout": 30})
		default:
			if len(lastResult) > 200 {
				lastResult = lastResult[:200]
			}
			return scriptedReply{text: []string{"Orchestration complete. ", "Last worker: " + lastResult}}, true
		}
	}
	for _, m := range req.Messages {
		if m.Role != "user" {
			continue
		}
		match := workerMarker.FindStringSubmatch(m.text())
		if match == nil {
			continue
		}
		posted, searched := false, false
		for _, other := range req.Messages {
			for _, c := range other.ToolCalls {
				posted = posted || c.Function.Name == "team_send"
				searched = searched || c.Function.Name == "tool_search"
			}
		}
		// Load the board tool once if its schema is deferred; give up
		// posting if it is not there at all.
		if !posted && (toolOffered(req.Tools, "team_send") || !searched) {
			return call("team_send", map[string]any{"text": "@lead worker " + match[1] + " is done"})
		}
		return scriptedReply{text: []string{"Worker " + match[1] + " ", "finished its piece."}, delay: WorkerDelay}, true
	}
	return scriptedReply{}, false
}

func toolOffered(tools []map[string]any, name string) bool {
	for _, tool := range tools {
		function, _ := tool["function"].(map[string]any)
		if function["name"] == name {
			return true
		}
	}
	return false
}

func bigDocument() []string {
	var out []string
	out = append(out, "# Large answer\n\n| Column | Value |\n|---|---|\n| a | 1 |\n| b | 2 |\n\n")
	for i := range 400 {
		out = append(out, fmt.Sprintf("Paragraph %d with **bold** text and a [link](https://example.com/%d).\n\n```go\nfunc f%d() int { return %d }\n```\n\n", i, i, i, i))
	}
	return out
}

func (f *FakeLLM) stream(w http.ResponseWriter, r *http.Request, model string, reply scriptedReply) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	send := func(v any) bool {
		raw, _ := json.Marshal(v)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
			return false
		}
		if flusher != nil {
			flusher.Flush()
		}
		return true
	}
	chunk := func(delta map[string]any, finish any) map[string]any {
		return map[string]any{
			"id": "chatcmpl-fake", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model,
			"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}},
		}
	}
	if !send(chunk(map[string]any{"role": "assistant", "content": ""}, nil)) {
		return
	}
	finish := "stop"
	if reply.toolName != "" {
		finish = "tool_calls"
		if !send(chunk(map[string]any{"tool_calls": []map[string]any{{
			"index": 0, "id": fmt.Sprintf("call_%d", f.calls.Load()), "type": "function",
			"function": map[string]any{"name": reply.toolName, "arguments": reply.toolArgs},
		}}}, nil)) {
			return
		}
	}
	for _, t := range reply.text {
		if reply.delay > 0 {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(reply.delay):
			}
		}
		if !send(chunk(map[string]any{"content": t}, nil)) {
			return
		}
	}
	final := chunk(map[string]any{}, finish)
	final["usage"] = map[string]any{"prompt_tokens": 1200, "completion_tokens": 80, "total_tokens": 1280}
	send(final)
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func (f *FakeLLM) respond(w http.ResponseWriter, model string, reply scriptedReply) {
	msg := map[string]any{"role": "assistant", "content": strings.Join(reply.text, "")}
	finish := "stop"
	if reply.toolName != "" {
		finish = "tool_calls"
		msg["tool_calls"] = []map[string]any{{
			"id": fmt.Sprintf("call_%d", f.calls.Load()), "type": "function",
			"function": map[string]any{"name": reply.toolName, "arguments": reply.toolArgs},
		}}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": "chatcmpl-fake", "object": "chat.completion", "created": time.Now().Unix(), "model": model,
		"choices": []map[string]any{{"index": 0, "message": msg, "finish_reason": finish}},
		"usage":   map[string]any{"prompt_tokens": 1200, "completion_tokens": 80, "total_tokens": 1280},
	})
}

// Environment is an isolated Atlas configuration pointing at a fake
// model: config, data and cache directories, and the variables that
// select them.
type Environment struct {
	Root string
	Env  map[string]string
}

// NewEnvironment writes an isolated global config under root that uses
// the fake model at llmURL for every model role.
func NewEnvironment(root, llmURL string) (*Environment, error) {
	dirs := map[string]string{
		"config": filepath.Join(root, "config"),
		"data":   filepath.Join(root, "data"),
		"cache":  filepath.Join(root, "cache"),
		"home":   filepath.Join(root, "home"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
	}
	model := func(id, name string) map[string]any {
		return map[string]any{
			"id": id, "name": name, "context_window": 128000, "default_max_tokens": 4096,
			"can_reason": true, "reasoning_levels": []string{"low", "medium", "high"},
			"default_reasoning_effort": "medium", "supports_attachments": true,
			"cost_per_1m_in": 1.0, "cost_per_1m_out": 2.0,
		}
	}
	cfg := map[string]any{
		"providers": map[string]any{
			"fake": map[string]any{
				"name":            "Fake Provider",
				"type":            "openai-compat",
				"base_url":        llmURL + "/v1",
				"api_key":         "fake-secret-key-never-in-browser",
				"discover_models": false,
				"models":          []any{model("fake-model", "Fake Model"), model("fake-small", "Fake Small")},
			},
		},
		"models": map[string]any{
			"large": map[string]any{"provider": "fake", "model": "fake-model"},
			"small": map[string]any{"provider": "fake", "model": "fake-small"},
		},
		"options": map[string]any{
			"disable_metrics":              true,
			"disable_provider_auto_update": true,
			"disable_default_providers":    true,
		},
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dirs["config"], "atlas.json"), raw, 0o600); err != nil {
		return nil, err
	}
	return &Environment{Root: root, Env: map[string]string{
		"ATLAS_AGENT_GLOBAL_CONFIG":   dirs["config"],
		"ATLAS_AGENT_GLOBAL_DATA":     dirs["data"],
		"ATLAS_AGENT_CACHE_DIR":       dirs["cache"],
		"ATLAS_AGENT_DISABLE_METRICS": "1",
		"XDG_CONFIG_HOME":             dirs["home"],
		"XDG_DATA_HOME":               dirs["data"],
		"XDG_CACHE_HOME":              dirs["cache"],
	}}, nil
}

// reviewFindingsJSON is what the fake reviewer reports: one real finding and
// one the reviewer calls a "nit".
const reviewFindingsJSON = `{"summary":"One problem found.","findings":[{"severity":"nit","title":"Naming","file":"main.go","line":2,"explanation":"Unclear name.","suggestion":"Rename it."},{"severity":"high","title":"Empty input panics","file":"main.go","line":7,"explanation":"An empty slice indexes element 0.","suggestion":"Check the length first."}]}`

package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func stepTools(t *testing.T, h payloadHarness) [][]string {
	t.Helper()
	var out [][]string
	for _, body := range h.recorder.stepRequests() {
		data, err := json.Marshal(body)
		require.NoError(t, err)
		out = append(out, requestToolNames(h.recorder.format, data))
	}
	return out
}

func stepBody(t *testing.T, h payloadHarness, step int) string {
	t.Helper()
	requests := h.recorder.stepRequests()
	require.Greater(t, len(requests), step)
	data, err := json.Marshal(requests[step])
	require.NoError(t, err)
	return string(data)
}

func newSession(t *testing.T, h payloadHarness) string {
	t.Helper()
	sess, err := h.env.sessions.Create(t.Context(), "deferral")
	require.NoError(t, err)
	return sess.ID
}

// A: a greeting carries the core palette and a short capability index, not
// the pipeline, browser or computer schemas, and the measured request is
// several times smaller than with every schema sent.
func TestGreetingSendsCorePaletteOnly(t *testing.T) {
	deferred := runGreeting(t, "", automationTools)
	full := runGreeting(t, `, "defer_tool_schemas": false`, automationTools)

	for _, name := range []string{"tool_pipeline", "browser", "computer", "agent", "workflow"} {
		require.NotContains(t, deferred.ToolNames, name)
		require.Contains(t, full.ToolNames, name, "explicit false keeps every schema")
	}
	for _, name := range []string{"tool_search", "bash", "view", "edit", "write", "grep", "glob", "ls"} {
		require.Contains(t, deferred.ToolNames, name)
	}
	require.Positive(t, deferred.Sections["deferred_tools"])
	require.Positive(t, deferred.Sections["tool_protocol"], "compact routing guidance remains available with core tools")
	require.Positive(t, full.Sections["tool_protocol"], "explicit false keeps the desktop procedures")
	require.Less(t, deferred.BodyBytes*4, full.BodyBytes, "deferred=%d full=%d", deferred.BodyBytes, full.BodyBytes)
}

// B: discovery, then reading, editing and running a check across steps.
// The discovered tool is advertised on the very next request.
func TestCodingFlowLoadsDiscoveredToolNextStep(t *testing.T) {
	h := newPayloadCoordinator(t, wireOpenAI, "", "",
		toolCallReply(wireOpenAI, "c1", "tool_search", map[string]any{"names": []string{"project_map"}}),
		toolCallReply(wireOpenAI, "c2", "view", map[string]any{"file_path": "main.go"}),
		toolCallReply(wireOpenAI, "c3", "edit", map[string]any{"file_path": "main.go", "old_string": "old", "new_string": "new"}),
		toolCallReply(wireOpenAI, "c4", "bash", map[string]any{"command": "echo verified", "description": "Run check"}),
		textReply(wireOpenAI, "done"),
	)
	require.NoError(t, os.WriteFile(filepath.Join(h.env.workingDir, "main.go"), []byte("package main // old\n"), 0o644))
	_, err := h.coord.Run(t.Context(), newSession(t, h), "main.go dosyasını düzelt ve doğrula")
	require.NoError(t, err)

	steps := stepTools(t, h)
	require.Len(t, steps, 5)
	require.NotContains(t, steps[0], "project_map")
	for _, step := range steps[1:] {
		require.Contains(t, step, "project_map", "a discovery stays loaded for the rest of the work")
	}
	data, err := os.ReadFile(filepath.Join(h.env.workingDir, "main.go"))
	require.NoError(t, err)
	require.Contains(t, string(data), "new")
	require.Contains(t, stepBody(t, h, 4), "verified")
}

// A model that calls an authorized tool without loading it gets a bounded,
// explicit refusal; the tool is not run and is loaded for the next step.
func TestUnloadedToolCallIsLoadedNotExecuted(t *testing.T) {
	h := newPayloadCoordinator(t, wireOpenAI, "", "",
		toolCallReply(wireOpenAI, "c1", "worktree", map[string]any{"action": "create", "name": "guessed"}),
		textReply(wireOpenAI, "ok"),
	)
	_, err := h.coord.Run(t.Context(), newSession(t, h), "make a worktree")
	require.NoError(t, err)
	steps := stepTools(t, h)
	require.Len(t, steps, 2)
	require.NotContains(t, steps[0], "worktree", "stubs are never advertised")
	require.Contains(t, steps[1], "worktree")
	require.Contains(t, stepBody(t, h, 1), "NOT executed")
	entries, err := os.ReadDir(h.env.workingDir)
	require.NoError(t, err)
	for _, entry := range entries {
		require.NotContains(t, entry.Name(), "guessed")
	}
}

// C: a browser request preloads only the browser schema; protocols for
// desktop work stay out.
func TestBrowserRequestPreloadsBrowser(t *testing.T) {
	h := newPayloadCoordinator(t, wireOpenAI, "", automationTools)
	_, err := h.coord.Run(t.Context(), newSession(t, h), "Tarayıcıda example.com sayfasını aç ve başlığı doğrula")
	require.NoError(t, err)
	steps := stepTools(t, h)
	require.Contains(t, steps[0], "browser")
	require.NotContains(t, steps[0], "computer")
	require.NotContains(t, stepBody(t, h, 0), `tool_protocol name=\"desktop\"`)
}

// D: a desktop request loads computer and tool_pipeline together with the
// desktop procedures (window identity, stale observation and checkpoint
// rules), and the pipeline keeps its flow/sequence/adaptive recipes.
func TestDesktopRequestLoadsComputerPipelineAndProtocol(t *testing.T) {
	h := newPayloadCoordinator(t, wireOpenAI, "", automationTools)
	_, err := h.coord.Run(t.Context(), newSession(t, h), "Masaüstünde Not Defteri penceresini aç")
	require.NoError(t, err)
	steps := stepTools(t, h)
	require.Contains(t, steps[0], "computer")
	require.Contains(t, steps[0], "tool_pipeline")
	body := stepBody(t, h, 0)
	require.Contains(t, body, `tool_protocol name=\"desktop\"`)
	require.Contains(t, body, "Never reuse the parent handle for a dialog")
	for _, mode := range []string{"flow", "sequence", "adaptive", "transition", "rename"} {
		require.Contains(t, body, `\"`+mode+`\"`)
	}
}

// E/F/G: a session switches from chat to browser work; the browser stays
// loaded on the next turn, disappears once the user disables it, and never
// leaks into another session.
func TestTaskSwitchPermissionChangeAndSessionIsolation(t *testing.T) {
	h := newPayloadCoordinator(t, wireOpenAI, "", automationTools,
		textReply(wireOpenAI, "Merhaba"),
		toolCallReply(wireOpenAI, "c1", "tool_search", map[string]any{"query": "browser screenshot"}),
		textReply(wireOpenAI, "loaded"),
		textReply(wireOpenAI, "still here"),
	)
	session := newSession(t, h)
	_, err := h.coord.Run(t.Context(), session, "merhaba")
	require.NoError(t, err)
	_, err = h.coord.Run(t.Context(), session, "şimdi bir web sayfasını kontrol et")
	require.NoError(t, err)
	_, err = h.coord.Run(t.Context(), session, "devam")
	require.NoError(t, err)
	steps := stepTools(t, h)
	require.Len(t, steps, 4)
	require.NotContains(t, steps[0], "browser")
	require.NotContains(t, steps[1], "browser")
	require.Contains(t, steps[2], "browser")
	require.Contains(t, steps[3], "browser", "discoveries persist across turns of the same session")

	other := newSession(t, h)
	_, err = h.coord.Run(t.Context(), other, "devam")
	require.NoError(t, err)
	require.NotContains(t, stepTools(t, h)[4], "browser")

	disabled := false
	h.cfg.Config().Tools.Browser.Enabled = &disabled
	_, err = h.coord.Run(t.Context(), session, "devam")
	require.NoError(t, err)
	require.NotContains(t, stepTools(t, h)[5], "browser", "a stale discovery cannot restore a disabled tool")
}

// H: the same discovery round trip in Anthropic and Gemini wire formats,
// including a signed Anthropic thinking block that must be replayed
// unchanged with the tool history.
func TestDeferredToolsAcrossProviderFormats(t *testing.T) {
	for _, format := range []wireFormat{wireAnthropic, wireGemini} {
		t.Run(string(format), func(t *testing.T) {
			var first string
			if format == wireAnthropic {
				first = toolCallReply(format, "toolu_1", "tool_search", map[string]any{"names": []string{"project_map"}}, "sig-123")
			} else {
				first = toolCallReply(format, "call_1", "tool_search", map[string]any{"names": []string{"project_map"}})
			}
			h := newPayloadCoordinator(t, format, "", "", first, textReply(format, "done"))
			_, err := h.coord.Run(t.Context(), newSession(t, h), "proje haritasını çıkar")
			require.NoError(t, err)
			steps := stepTools(t, h)
			require.Len(t, steps, 2)
			require.NotContains(t, steps[0], "project_map")
			require.Contains(t, steps[1], "project_map")
			require.NotContains(t, steps[0], "tool_pipeline")
			second := stepBody(t, h, 1)
			require.Contains(t, second, "tool_search", "the earlier tool call stays in history")
			if format == wireAnthropic {
				require.Contains(t, second, "sig-123")
			}
		})
	}
}

// H: a retried step and a model fallback send the palette selected for that
// step, not the full or an empty one.
func TestRetryAndFallbackKeepTheStepPalette(t *testing.T) {
	t.Run("retry", func(t *testing.T) {
		h := newPayloadCoordinator(t, wireOpenAI, "", "",
			toolCallReply(wireOpenAI, "c1", "tool_search", map[string]any{"names": []string{"project_map"}}),
			textReply(wireOpenAI, "done"),
		)
		h.recorder.failures[1] = http.StatusServiceUnavailable
		_, err := h.coord.Run(t.Context(), newSession(t, h), "harita")
		require.NoError(t, err)
		var attempts [][]string
		for _, request := range h.recorder.requests {
			if names := requestToolNames(wireOpenAI, request.Body); len(names) > 0 {
				attempts = append(attempts, names)
			}
		}
		require.Len(t, attempts, 3)
		require.Equal(t, attempts[1], attempts[2], "the retry resends the same palette")
		require.Contains(t, attempts[2], "project_map")
	})
	t.Run("fallback", func(t *testing.T) {
		h := newPayloadCoordinator(t, wireOpenAI, `, "model_fallbacks": {"large": [{"provider": "backup", "model": "mock-model"}]}`, "",
			toolCallReply(wireOpenAI, "c1", "tool_search", map[string]any{"names": []string{"project_map"}}),
			textReply(wireOpenAI, "done"),
		)
		h.recorder.failures[1] = http.StatusTooManyRequests
		_, err := h.coord.Run(t.Context(), newSession(t, h), "harita")
		require.NoError(t, err)
		paths := h.recorder.stepPaths()
		require.True(t, strings.HasPrefix(paths[len(paths)-1], "/backup/"), "%v", paths)
		steps := stepTools(t, h)
		require.Contains(t, steps[len(steps)-1], "project_map")
	})
}

func TestDeferToolSchemasSettingIsTriState(t *testing.T) {
	t.Parallel()
	enabled, disabled := true, false
	require.True(t, (&config.Options{}).DeferToolSchemasEnabled(), "unset means on")
	require.True(t, (&config.Options{DeferToolSchemas: &enabled}).DeferToolSchemasEnabled())
	require.False(t, (&config.Options{DeferToolSchemas: &disabled}).DeferToolSchemasEnabled(), "explicit false is kept")
}

func TestCapabilityIndexIsStableAndExcludesCore(t *testing.T) {
	t.Parallel()
	d := &toolDeferral{pinned: []string{"todos"}}
	palette := []string{"bash", "browser", "computer", "todos", "tool_pipeline", "tool_search", "mcp_github_create_issue"}
	index := d.capabilityIndex(fixturePalette(palette))
	require.Contains(t, index, "- browser: browser")
	require.Contains(t, index, "- desktop: computer")
	require.Contains(t, index, "- mcp: mcp_github_create_issue")
	require.NotContains(t, index, "bash")
	require.NotContains(t, index, "todos")
	require.Equal(t, index, d.capabilityIndex(fixturePalette(palette)))
	require.Empty(t, (*toolDeferral)(nil).capabilityIndex(fixturePalette(palette)))
	require.True(t, strings.Contains(index, "tool_search"))
}

func fixturePalette(names []string) []fantasy.AgentTool {
	out := make([]fantasy.AgentTool, 0, len(names))
	for _, name := range names {
		if name == tools.ToolSearchToolName {
			out = append(out, tools.NewToolSearchTool(tools.ToolSearchOptions{}))
			continue
		}
		out = append(out, fantasy.NewAgentTool(name, "fixture "+name, func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.ToolResponse{}, nil
		}))
	}
	return out
}

package webui

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
	"github.com/stretchr/testify/require"
)

// The orchestrator mode, chosen from the web UI, runs a real team through
// the server: two background workers post on the board, the orchestrator is
// sent back when it tries to stop before collecting them, then collects both
// and reports. Its own requests never offer edit or shell tools.
func TestAcceptance_OrchestratorModeRunsATeam(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)

	type mode struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	var settings struct {
		Modes []mode `json:"modes"`
	}
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, wsPath(ws.ID, "/settings"), nil, &settings))
	idx := slices.IndexFunc(settings.Modes, func(m mode) bool { return m.Name == "orchestrator" })
	require.GreaterOrEqual(t, idx, 0, "the mode picker offers the orchestrator")
	require.Contains(t, settings.Modes[idx].Description, "team")

	var applied struct {
		SessionMode string `json:"session_mode"`
	}
	require.Equal(t, http.StatusOK, b.json(http.MethodPost, wsPath(ws.ID, "/session-mode"), map[string]string{"mode": "orchestrator"}, &applied))
	require.Equal(t, "orchestrator", applied.SessionMode)

	ev := b.events(ws.ID, 0)
	sess := b.newSession(ws.ID)
	runID := b.prompt(ws.ID, sess.ID, "[[orchestrate]] Split this into two pieces.")
	ev.waitFor(t, 2*time.Minute, isRun(runID))
	h.waitIdle(ws.ID, sess.ID, time.Minute)

	msgs := b.messages(ws.ID, sess.ID)
	var calls []string
	for _, m := range msgs {
		for _, p := range m.Parts {
			if call, ok := p.(proto.ToolCall); ok {
				calls = append(calls, call.Name)
			}
		}
	}
	require.Equal(t, 2, count(calls, "agent"), "two workers were started: %v", calls)
	require.Equal(t, 2, count(calls, "agent_result"), "each worker was collected once: %v", calls)
	text := assistantText(msgs)
	require.Contains(t, text, "Orchestration complete.")
	require.Contains(t, text, "finished its piece", "the report quotes a worker's answer")

	// The orchestrator tried to stop early and the harness sent it back.
	// Tool schemas may be deferred, so the offered list holds the core
	// tools and the rest load through tool_search; what matters is that
	// nothing that edits or runs commands is ever offered to it.
	sentBack, posted := false, false
	var orchestratorTools []string
	for _, req := range h.llm.Requests() {
		raw, _ := json.Marshal(req["messages"])
		names := offeredTools(req)
		switch {
		case strings.Contains(string(raw), "[[orchestrate]]"):
			orchestratorTools = append(orchestratorTools, names...)
			sentBack = sentBack || strings.Contains(string(raw), "Do not finish yet")
		case strings.Contains(string(raw), "[[worker "):
			posted = posted || strings.Contains(string(raw), "Sent (seq")
		}
	}
	require.True(t, sentBack, "finishing with workers still running is sent back")
	require.Contains(t, orchestratorTools, "agent")
	require.Contains(t, orchestratorTools, "agent_result")
	for _, banned := range []string{"edit", "multiedit", "write", "bash"} {
		require.NotContains(t, orchestratorTools, banned)
	}
	require.True(t, posted, "workers get the team board without the tools.teams switch")
}

func count(names []string, want string) int {
	n := 0
	for _, name := range names {
		if name == want {
			n++
		}
	}
	return n
}

func offeredTools(req map[string]any) []string {
	tools, _ := req["tools"].([]any)
	var names []string
	for _, tool := range tools {
		entry, _ := tool.(map[string]any)
		function, _ := entry["function"].(map[string]any)
		if name, ok := function["name"].(string); ok {
			names = append(names, name)
		}
	}
	return names
}

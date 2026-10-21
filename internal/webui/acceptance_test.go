package webui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Acceptance scenarios for the web client against a real Atlas server.
// Every run goes through the production agent engine, tools, permission
// and question services; only the model is scripted (see webtest). The
// numbers refer to the acceptance list in docs/web-ui.md.

// Scenarios 1 and 4: a project opened in the browser is a real server
// workspace, and a prompt sent from the browser runs in the Atlas agent.
func TestAcceptance_WebOpensProjectAndRunsAgent(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	require.True(t, ws.Configured)
	require.True(t, ws.AgentReady, "the gateway initializes an unready agent")

	list, err := h.upstream.ListWorkspaces(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, ws.ID, list[0].ID)

	// Opening the same project again, or from a second tab, reuses the
	// workspace instead of creating a backend per tab.
	again := h.signIn().open(h.project)
	require.Equal(t, ws.ID, again.ID)

	ev := b.events(ws.ID, 0)
	sess := b.newSession(ws.ID)
	b.prompt(ws.ID, sess.ID, "hello from the browser")
	ev.waitFor(t, 30*time.Second, isStatus(sess.ID, true))
	ev.waitFor(t, 30*time.Second, isRunComplete(sess.ID))
	h.waitIdle(ws.ID, sess.ID, 30*time.Second)

	msgs := b.messages(ws.ID, sess.ID)
	require.Contains(t, assistantText(msgs), "Echo: hello from the browser")
	require.Equal(t, proto.FinishReasonEndTurn, lastFinish(msgs))
	ev.waitFor(t, 10*time.Second, isStatus(sess.ID, false))
}

// Scenario 2: a session created in the browser is visible to a terminal
// client joined to the same server, under the same workspace ID.
func TestAcceptance_WebSessionVisibleToCLI(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	sess := b.newSession(ws.ID)

	cli, cliWS, _ := h.cli(context.Background(), h.project)
	require.Equal(t, ws.ID, cliWS.ID, "the terminal joins the workspace the browser opened")
	sessions, err := cli.ListSessions(context.Background(), cliWS.ID)
	require.NoError(t, err)
	require.True(t, containsSession(sessions, sess.ID))

	// A rename from the browser keeps usage counters the server tracks.
	var renamed proto.Session
	require.Equal(t, http.StatusOK, b.json(http.MethodPatch, wsPath(ws.ID, "/sessions/", sess.ID), map[string]string{"title": "Renamed in browser"}, &renamed))
	require.Equal(t, "Renamed in browser", renamed.Title)
	got, err := cli.GetSession(context.Background(), cliWS.ID, sess.ID)
	require.NoError(t, err)
	require.Equal(t, "Renamed in browser", got.Title)
}

func containsSession(list []proto.Session, id string) bool {
	for _, s := range list {
		if s.ID == id {
			return true
		}
	}
	return false
}

// Scenario 3: a run the terminal starts streams to the browser, with its
// state.
func TestAcceptance_CLIRunStreamsToWeb(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	ev := b.events(ws.ID, 0)

	cli, cliWS, _ := h.cli(context.Background(), h.project)
	sess, err := cli.CreateSession(context.Background(), cliWS.ID, "From terminal")
	require.NoError(t, err)
	ev.waitFor(t, 10*time.Second, func(e hubEvent) bool {
		return e.Type == "session" && innerPayload[proto.Session](t, e).ID == sess.ID
	})
	require.NoError(t, cli.SendMessage(context.Background(), cliWS.ID, sess.ID, uuid.NewString(), "typed in the terminal"))

	ev.waitFor(t, 30*time.Second, isStatus(sess.ID, true))
	ev.waitFor(t, 30*time.Second, func(e hubEvent) bool {
		if e.Type != "message" {
			return false
		}
		m := innerPayload[proto.Message](t, e)
		return m.SessionID == sess.ID && m.Role == proto.Assistant && strings.Contains(assistantText([]proto.Message{m}), "typed in the terminal")
	})
	ev.waitFor(t, 30*time.Second, isStatus(sess.ID, false))
}

// Scenario 5: tool calls and their results reach the browser from the
// server's own events, and the snapshot agrees.
func TestAcceptance_ToolResultsFromBackendEvents(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	ev := b.events(ws.ID, 0)
	sess := b.newSession(ws.ID)
	b.prompt(ws.ID, sess.ID, "[[bash]]")

	ev.waitFor(t, 30*time.Second, func(e hubEvent) bool {
		if e.Type != "message" {
			return false
		}
		m := innerPayload[proto.Message](t, e)
		for _, p := range m.Parts {
			if r, ok := p.(proto.ToolResult); ok && r.Name == "bash" && strings.Contains(r.Content, "pipeline-output") {
				return true
			}
		}
		return false
	})
	ev.waitFor(t, 30*time.Second, isRunComplete(sess.ID))
	h.waitIdle(ws.ID, sess.ID, 30*time.Second)
	msgs := b.messages(ws.ID, sess.ID)
	var call, result bool
	for _, m := range msgs {
		for _, p := range m.Parts {
			switch v := p.(type) {
			case proto.ToolCall:
				call = call || v.Name == "bash"
			case proto.ToolResult:
				result = result || (v.Name == "bash" && strings.Contains(v.Content, "pipeline-output"))
			}
		}
	}
	require.True(t, call)
	require.True(t, result)
	require.Contains(t, assistantText(msgs), "pipeline-output")
}

// Scenario 6: a question asked by the agent is answered from the browser
// and the run continues with the answer.
func TestAcceptance_QuestionAnsweredFromWeb(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	ev := b.events(ws.ID, 0)
	sess := b.newSession(ws.ID)
	b.prompt(ws.ID, sess.ID, "[[ask]]")

	qev := ev.waitFor(t, 30*time.Second, func(e hubEvent) bool { return e.Type == "question_batch_request" })
	q := innerPayload[proto.QuestionRequest](t, qev)
	require.Equal(t, sess.ID, q.SessionID)

	// A reload finds the same pending question.
	var pending struct {
		Questions []proto.QuestionRequest `json:"questions"`
	}
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, wsPath(ws.ID, "/pending"), nil, &pending))
	require.Len(t, pending.Questions, 1)
	require.Equal(t, q.ID, pending.Questions[0].ID)

	var res struct {
		Resolved bool `json:"resolved"`
	}
	answer := map[string]any{"responses": []map[string]any{{"request_id": q.Questions[0].ID, "fill_in_text": "atlas-notes"}}}
	require.Equal(t, http.StatusOK, b.json(http.MethodPost, wsPath(ws.ID, "/questions/", q.ID, "/answer"), answer, &res))
	require.True(t, res.Resolved)
	// A double click sends the answer twice; only the first resolves.
	require.Equal(t, http.StatusOK, b.json(http.MethodPost, wsPath(ws.ID, "/questions/", q.ID, "/answer"), answer, &res))
	require.False(t, res.Resolved)

	ev.waitFor(t, 10*time.Second, func(e hubEvent) bool {
		return e.Type == "question_batch_notification" && innerPayload[proto.QuestionNotification](t, e).BatchID == q.ID
	})
	ev.waitFor(t, 30*time.Second, isRunComplete(sess.ID))
	h.waitIdle(ws.ID, sess.ID, 30*time.Second)
	require.Contains(t, assistantText(b.messages(ws.ID, sess.ID)), "atlas-notes")
}

// Scenario 7: a permission request answered by another client closes in
// the browser, and the browser's late answer is a no-op.
func TestAcceptance_PermissionAnsweredElsewhereClosesInWeb(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	ev := b.events(ws.ID, 0)
	cli, cliWS, _ := h.cli(context.Background(), h.project)
	sess := b.newSession(ws.ID)
	b.prompt(ws.ID, sess.ID, "[[write granted.txt]]")

	pev := ev.waitFor(t, 30*time.Second, func(e hubEvent) bool { return e.Type == "permission_request" })
	perm := innerPayload[proto.PermissionRequest](t, pev)
	require.Equal(t, "write", perm.ToolName)

	resolved, err := cli.GrantPermission(context.Background(), cliWS.ID, proto.PermissionGrant{Permission: perm, Action: proto.PermissionAllow})
	require.NoError(t, err)
	require.True(t, resolved)

	ev.waitFor(t, 10*time.Second, func(e hubEvent) bool {
		if e.Type != "permission_notification" {
			return false
		}
		n := innerPayload[proto.PermissionNotification](t, e)
		return n.ToolCallID == perm.ToolCallID && n.Granted
	})
	var res struct {
		Resolved bool `json:"resolved"`
	}
	require.Equal(t, http.StatusOK, b.json(http.MethodPost, wsPath(ws.ID, "/permissions/", perm.ID), map[string]string{"action": "deny"}, &res))
	require.False(t, res.Resolved, "the browser cannot overturn a decision made elsewhere")

	ev.waitFor(t, 30*time.Second, isRunComplete(sess.ID))
	h.waitIdle(ws.ID, sess.ID, 30*time.Second)
	content, err := os.ReadFile(filepath.Join(h.project, "granted.txt"))
	require.NoError(t, err)
	require.Equal(t, "written by the fake model\n", string(content))
	var pending struct {
		Permissions []proto.PermissionRequest `json:"permissions"`
	}
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, wsPath(ws.ID, "/pending"), nil, &pending))
	require.Empty(t, pending.Permissions)
}

// Scenarios 8 and 15: a reload reconnects to the running task without
// duplicating it, cancelling it, or losing or repeating events.
func TestAcceptance_ReloadResumesWithoutDuplicates(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	first := b.events(ws.ID, 0)
	sess := b.newSession(ws.ID)

	runID := uuid.NewString()
	body := map[string]any{"prompt": "[[slow]]", "run_id": runID}
	var out struct {
		Duplicate bool `json:"duplicate"`
	}
	require.Equal(t, http.StatusAccepted, b.json(http.MethodPost, wsPath(ws.ID, "/sessions/", sess.ID, "/prompt"), body, &out))
	require.False(t, out.Duplicate)
	first.waitFor(t, 30*time.Second, func(e hubEvent) bool {
		return e.Type == "message" && strings.Contains(assistantText([]proto.Message{innerPayload[proto.Message](t, e)}), "tick 5")
	})
	callsBefore := h.llm.Calls()

	// The page reloads: the stream drops and the lost acknowledgement is
	// retried with the same run ID.
	seen := first.snapshot()
	last := seen[len(seen)-1].Seq
	first.close()
	require.Equal(t, http.StatusAccepted, b.json(http.MethodPost, wsPath(ws.ID, "/sessions/", sess.ID, "/prompt"), body, &out))
	require.True(t, out.Duplicate, "a retried run ID starts nothing")

	resumed := b.events(ws.ID, last)
	resumed.waitFor(t, 60*time.Second, isRunComplete(sess.ID))
	h.waitIdle(ws.ID, sess.ID, 30*time.Second)

	all := append(seen, resumed.snapshot()...)
	for i := 1; i < len(all); i++ {
		require.Greater(t, all[i].Seq, all[i-1].Seq, "events neither repeat nor go backwards across the reload")
	}
	for i := 1; i < len(all); i++ {
		require.Equal(t, all[i-1].Seq+1, all[i].Seq, "no event is lost across the reload")
	}
	msgs := b.messages(ws.ID, sess.ID)
	require.Equal(t, proto.FinishReasonEndTurn, lastFinish(msgs), "the reload did not cancel the run")
	require.Contains(t, assistantText(msgs), "tick 79")
	require.Equal(t, 1, countUserMessages(msgs), "the retry did not queue a second prompt")
	require.LessOrEqual(t, h.llm.Calls()-callsBefore, int64(1), "no second model run started")
}

func countUserMessages(msgs []proto.Message) int {
	n := 0
	for _, m := range msgs {
		if m.Role == proto.User {
			n++
		}
	}
	return n
}

// Scenario 9: two tabs see the same events in the same order.
func TestAcceptance_TwoTabsConsistent(t *testing.T) {
	h := newHarness(t)
	tab1 := h.signIn()
	tab2 := h.signIn()
	ws := tab1.open(h.project)
	e1 := tab1.events(ws.ID, 0)
	e2 := tab2.events(ws.ID, 0)
	sess := tab1.newSession(ws.ID)
	tab1.prompt(ws.ID, sess.ID, "[[bash]]")
	e1.waitFor(t, 30*time.Second, isRunComplete(sess.ID))
	e2.waitFor(t, 30*time.Second, isRunComplete(sess.ID))
	h.waitIdle(ws.ID, sess.ID, 30*time.Second)
	e1.waitFor(t, 10*time.Second, isStatus(sess.ID, false))
	e2.waitFor(t, 10*time.Second, isStatus(sess.ID, false))

	a, b := e1.snapshot(), e2.snapshot()
	n := min(len(a), len(b))
	require.Positive(t, n)
	for i := range n {
		require.Equal(t, a[i].Seq, b[i].Seq)
		require.Equal(t, a[i].Type, b[i].Type)
		require.JSONEq(t, string(a[i].Payload), string(b[i].Payload))
	}
	require.Equal(t, tab1.messages(ws.ID, sess.ID), tab2.messages(ws.ID, sess.ID))
}

// Scenario 10: stop cancels the run of the session it names and no other.
func TestAcceptance_StopTargetsOneSession(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	ev := b.events(ws.ID, 0)
	s1 := b.newSession(ws.ID)
	s2 := b.newSession(ws.ID)
	b.prompt(ws.ID, s1.ID, "[[slow]] one")
	b.prompt(ws.ID, s2.ID, "[[slow]] two")
	ev.waitFor(t, 30*time.Second, isStatus(s1.ID, true))
	ev.waitFor(t, 30*time.Second, isStatus(s2.ID, true))

	require.Equal(t, http.StatusOK, b.json(http.MethodPost, wsPath(ws.ID, "/sessions/", s1.ID, "/cancel"), nil, nil))
	done := innerPayload[proto.RunComplete](t, ev.waitFor(t, 30*time.Second, isRunComplete(s1.ID)))
	require.True(t, done.Cancelled, "the stopped run ends as cancelled")
	h.waitIdle(ws.ID, s1.ID, 30*time.Second)
	require.NotEqual(t, proto.FinishReasonEndTurn, lastFinish(b.messages(ws.ID, s1.ID)))

	s, err := h.upstream.GetSession(context.Background(), ws.ID, s2.ID)
	require.NoError(t, err)
	require.True(t, s.IsBusy, "the other session keeps running")
	h.waitIdle(ws.ID, s2.ID, 60*time.Second)
	require.Equal(t, proto.FinishReasonEndTurn, lastFinish(b.messages(ws.ID, s2.ID)))
}

// Scenario 11: closing a tab, or the whole web UI, does not stop a run
// another client is driving.
func TestAcceptance_ClosingWebKeepsCLIRun(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	tab := b.events(ws.ID, 0)
	cli, cliWS, _ := h.cli(context.Background(), h.project)
	sess, err := cli.CreateSession(context.Background(), cliWS.ID, "terminal")
	require.NoError(t, err)
	require.NoError(t, cli.SendMessage(context.Background(), cliWS.ID, sess.ID, uuid.NewString(), "[[slow]]"))
	tab.waitFor(t, 30*time.Second, isStatus(sess.ID, true))

	tab.close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, h.gw.Shutdown(ctx))

	h.waitIdle(cliWS.ID, sess.ID, 60*time.Second)
	msgs, err := cli.ListMessages(context.Background(), cliWS.ID, sess.ID)
	require.NoError(t, err)
	require.Equal(t, proto.FinishReasonEndTurn, lastFinish(msgs))
	list, err := cli.ListWorkspaces(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1, "the terminal's workspace outlives the web UI")
}

// Scenario 12: model, effort and mode changes are applied by the server,
// not just recorded by the browser.
func TestAcceptance_ModelEffortAndModesApplied(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)

	var cat catalog
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, wsPath(ws.ID, "/models"), nil, &cat))
	require.Len(t, cat.Providers, 1)
	require.Equal(t, "fake", cat.Providers[0].ID)
	require.Equal(t, "fake-model", cat.Active.Model)

	status, _ := b.raw(http.MethodPost, wsPath(ws.ID, "/models"), map[string]any{"type": "large", "provider": "fake", "model": "fake-small", "reasoning_effort": "extreme", "scope": "workspace"})
	require.Equal(t, http.StatusBadRequest, status, "an effort the model does not offer is refused")

	require.Equal(t, http.StatusOK, b.json(http.MethodPost, wsPath(ws.ID, "/models"), map[string]any{"type": "large", "provider": "fake", "model": "fake-small", "reasoning_effort": "high", "scope": "workspace"}, &cat))
	require.Equal(t, "fake-small", cat.Active.Model)
	require.Equal(t, "high", cat.Active.ReasoningEffort)
	info, err := h.upstream.GetAgentInfo(context.Background(), ws.ID)
	require.NoError(t, err)
	require.Equal(t, "fake-small", info.ModelCfg.Model)

	ev := b.events(ws.ID, 0)
	sess := b.newSession(ws.ID)
	b.run(ev, ws.ID, sess.ID, "which model?")
	req := lastAgentRequest(t, h)
	require.Equal(t, "fake-small", req["model"])
	require.Equal(t, "high", req["reasoning_effort"])

	// Plan mode is enforced by the server's permission service: the
	// write is denied without anyone being asked.
	var mode proto.PermissionModeRequest
	require.Equal(t, http.StatusOK, b.json(http.MethodPost, wsPath(ws.ID, "/permission-mode"), map[string]string{"mode": "plan"}, &mode))
	require.Equal(t, "plan", mode.Mode)
	got, err := h.upstream.GetPermissionMode(context.Background(), ws.ID)
	require.NoError(t, err)
	require.Equal(t, "plan", got)
	b.run(ev, ws.ID, sess.ID, "[[write planned.txt]]")
	require.NoFileExists(t, filepath.Join(h.project, "planned.txt"))

	// A session mode joins the system prompt of the next run.
	var settings struct {
		Modes []struct {
			Name string `json:"name"`
		} `json:"modes"`
	}
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, wsPath(ws.ID, "/settings"), nil, &settings))
	require.NotEmpty(t, settings.Modes)
	chosen := settings.Modes[0].Name
	var applied struct {
		SessionMode string `json:"session_mode"`
	}
	require.Equal(t, http.StatusOK, b.json(http.MethodPost, wsPath(ws.ID, "/session-mode"), map[string]string{"mode": chosen}, &applied))
	require.Equal(t, chosen, applied.SessionMode)
	b.run(ev, ws.ID, sess.ID, "in a mode now")
	raw, err := json.Marshal(lastAgentRequest(t, h)["messages"])
	require.NoError(t, err)
	require.Contains(t, string(raw), `\u003cmode name=\"`+chosen)
	status, _ = b.raw(http.MethodPost, wsPath(ws.ID, "/session-mode"), map[string]string{"mode": "no-such-mode"})
	require.Equal(t, http.StatusBadRequest, status)
}

// lastAgentRequest returns the newest model request that carried tools,
// which is the coder agent's own (titles and summaries carry none).
func lastAgentRequest(t *testing.T, h *harness) map[string]any {
	t.Helper()
	reqs := h.llm.Requests()
	for i := len(reqs) - 1; i >= 0; i-- {
		if tools, ok := reqs[i]["tools"].([]any); ok && len(tools) > 0 {
			return reqs[i]
		}
	}
	t.Fatal("no agent request reached the model")
	return nil
}

// Scenario 13: images and files travel the server's own attachment path
// into the model request.
func TestAcceptance_AttachmentsReachTheModel(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	ev := b.events(ws.ID, 0)
	sess := b.newSession(ws.ID)
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGNgYGD4DwABBAEAwS2OUAAAAABJRU5ErkJggg==")
	runID := uuid.NewString()
	body := map[string]any{
		"prompt": "look at these",
		"run_id": runID,
		"attachments": []map[string]any{
			{"file_name": "pixel.png", "mime_type": "image/png", "content": base64.StdEncoding.EncodeToString(png)},
			{"file_name": `C:\secret\dir\notes.txt`, "mime_type": "text/plain", "content": base64.StdEncoding.EncodeToString([]byte("attached text body"))},
		},
	}
	require.Equal(t, http.StatusAccepted, b.json(http.MethodPost, wsPath(ws.ID, "/sessions/", sess.ID, "/prompt"), body, nil))
	ev.waitFor(t, 60*time.Second, isRun(runID))
	h.waitIdle(ws.ID, sess.ID, 30*time.Second)

	raw, err := json.Marshal(lastAgentRequest(t, h)["messages"])
	require.NoError(t, err)
	require.Contains(t, string(raw), "data:image/png;base64,")
	require.Contains(t, string(raw), "attached text body")
	require.Contains(t, assistantText(b.messages(ws.ID, sess.ID)), "(image received)")

	var binary bool
	for _, m := range b.messages(ws.ID, sess.ID) {
		for _, p := range m.Parts {
			if bc, ok := p.(proto.BinaryContent); ok && bc.MIMEType == "image/png" {
				binary = true
				require.Equal(t, "pixel.png", bc.Path, "only the file name crosses from the browser")
			}
		}
	}
	require.True(t, binary)

	bad := map[string]any{"prompt": "x", "run_id": uuid.NewString(), "attachments": []map[string]any{{"file_name": "a.exe", "mime_type": "application/x-msdownload", "content": "AA=="}}}
	status, _ := b.raw(http.MethodPost, wsPath(ws.ID, "/sessions/", sess.ID, "/prompt"), bad)
	require.Equal(t, http.StatusUnsupportedMediaType, status)
	huge := map[string]any{"prompt": "x", "run_id": uuid.NewString(), "attachments": []map[string]any{{"file_name": "big.png", "mime_type": "image/png", "content": base64.StdEncoding.EncodeToString(make([]byte, maxAttachmentBytes+1))}}}
	status, _ = b.raw(http.MethodPost, wsPath(ws.ID, "/sessions/", sess.ID, "/prompt"), huge)
	require.Equal(t, http.StatusRequestEntityTooLarge, status)
}

// Scenario 14: the file view and the change history show what is on disk.
func TestAcceptance_FilesAndHistoryMatchDisk(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	require.NoError(t, os.WriteFile(filepath.Join(h.project, "existing.md"), []byte("# Existing\n"), 0o600))
	var mode proto.PermissionModeRequest
	require.Equal(t, http.StatusOK, b.json(http.MethodPost, wsPath(ws.ID, "/permission-mode"), map[string]string{"mode": "bypass"}, &mode))
	ev := b.events(ws.ID, 0)
	sess := b.newSession(ws.ID)
	b.run(ev, ws.ID, sess.ID, "[[write created.txt]]")

	var listing proto.DirectoryListing
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, wsPath(ws.ID, "/files"), nil, &listing))
	names := map[string]bool{}
	for _, e := range listing.Entries {
		names[e.Name] = true
	}
	require.True(t, names["created.txt"])
	require.True(t, names["existing.md"])

	var file proto.FileContent
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, wsPath(ws.ID, "/files/content?path=created.txt"), nil, &file))
	disk, err := os.ReadFile(filepath.Join(h.project, "created.txt"))
	require.NoError(t, err)
	require.Equal(t, string(disk), file.Content)

	var history []proto.File
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, wsPath(ws.ID, "/sessions/", sess.ID, "/history"), nil, &history))
	var latest string
	var versions int
	for _, f := range history {
		if strings.HasSuffix(filepath.ToSlash(f.Path), "created.txt") {
			latest = f.Content
			versions++
			// The wire form is proto.File: snake_case fields, not the
			// untagged history.File the endpoint used to leak.
			require.Equal(t, sess.ID, f.SessionID)
			require.NotZero(t, f.CreatedAt)
		}
	}
	require.Equal(t, string(disk), latest)
	require.GreaterOrEqual(t, versions, 2, "a baseline and the written version, for the diff")

	status, _ := b.raw(http.MethodGet, wsPath(ws.ID, "/files/content?path=../outside.txt"), nil)
	require.Equal(t, http.StatusForbidden, status)
}

// Scenario 16: one workspace's sessions and events never show up in
// another's.
func TestAcceptance_WorkspacesAreIsolated(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	a := b.open(h.project)
	other := b.open(t.TempDir())
	require.NotEqual(t, a.ID, other.ID)
	evOther := b.events(other.ID, 0)
	evA := b.events(a.ID, 0)

	sa := b.newSession(a.ID)
	b.run(evA, a.ID, sa.ID, "only in A")

	var otherSessions []proto.Session
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, wsPath(other.ID, "/sessions"), nil, &otherSessions))
	require.False(t, containsSession(otherSessions, sa.ID))
	status, _ := b.raw(http.MethodGet, wsPath(other.ID, "/sessions/", sa.ID, "/messages"), nil)
	if status == http.StatusOK {
		require.Empty(t, b.messages(other.ID, sa.ID))
	}
	for _, e := range evOther.snapshot() {
		require.NotContains(t, string(e.Payload), sa.ID, "an event of workspace A reached workspace B's stream")
	}
	status, _ = b.raw(http.MethodGet, wsPath(uuid.NewString(), "/sessions"), nil)
	require.Equal(t, http.StatusNotFound, status)
}

// Scenario 17: requests without a session, from another origin, without
// the CSRF token or with a foreign Host header are refused.
func TestAcceptance_RejectsUnauthorizedRequests(t *testing.T) {
	h := newHarness(t)
	anon := &http.Client{}
	rsp, err := get(anon, h.base+"/api/bootstrap")
	require.NoError(t, err)
	rsp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, rsp.StatusCode)

	// A launch code works once.
	link := h.gw.LaunchURL()
	b := h.signIn()
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	rsp, err = get(noRedirect, link)
	require.NoError(t, err)
	rsp.Body.Close()
	require.Equal(t, http.StatusSeeOther, rsp.StatusCode)
	rsp, err = get(noRedirect, link)
	require.NoError(t, err)
	rsp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, rsp.StatusCode)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, h.base+"/api/workspaces", strings.NewReader(`{"path":"x"}`))
	req.Header.Set("Origin", "http://evil.example")
	req.Header.Set(csrfHeader, b.csrf)
	rsp, err = b.http.Do(req)
	require.NoError(t, err)
	rsp.Body.Close()
	require.Equal(t, http.StatusForbidden, rsp.StatusCode)

	req, _ = http.NewRequestWithContext(context.Background(), http.MethodPost, h.base+"/api/workspaces", strings.NewReader(`{"path":"x"}`))
	req.Header.Set("Origin", h.base)
	rsp, err = b.http.Do(req)
	require.NoError(t, err)
	rsp.Body.Close()
	require.Equal(t, http.StatusForbidden, rsp.StatusCode, "missing CSRF token")

	req, _ = http.NewRequestWithContext(context.Background(), http.MethodGet, h.base+"/api/bootstrap", nil)
	req.Host = "attacker.example:" + h.gw.Port()
	rsp, err = b.http.Do(req)
	require.NoError(t, err)
	rsp.Body.Close()
	require.Equal(t, http.StatusMisdirectedRequest, rsp.StatusCode, "DNS rebinding Host refused")

	// The API never hands out provider keys or the server's environment.
	ws := b.open(h.project)
	for _, p := range []string{"/api/bootstrap", wsPath(ws.ID), wsPath(ws.ID, "/models"), wsPath(ws.ID, "/settings"), "/api/workspaces"} {
		_, body := b.raw(http.MethodGet, p, nil)
		require.NotContains(t, body, "fake-secret-key", p)
		require.NotContains(t, body, "ATLAS_AGENT_GLOBAL_CONFIG", p)
		require.NotContains(t, body, `"env"`, p)
	}

	_, err = h.gw.Listen("0.0.0.0:0")
	require.Error(t, err, "the gateway only listens on loopback")
}

// Scenario 18: a long conversation with a large answer stays responsive:
// the answer streams through, and API calls answer promptly meanwhile.
func TestAcceptance_LargeOutputStaysResponsive(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	ev := b.events(ws.ID, 0)
	sess := b.newSession(ws.ID)
	for range 5 {
		b.run(ev, ws.ID, sess.ID, "[[big]]")
	}
	start := time.Now()
	msgs := b.messages(ws.ID, sess.ID)
	require.Less(t, time.Since(start), 5*time.Second)
	require.Greater(t, len(assistantText(msgs)), 5*40_000)
	start = time.Now()
	var st map[string]any
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, wsPath(ws.ID, "/status"), nil, &st))
	require.Less(t, time.Since(start), time.Second)
}

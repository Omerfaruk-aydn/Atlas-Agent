package mobile

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestPhoneCancelWithdrawsPairingFromTheComputer(t *testing.T) {
	t.Parallel()
	g, _ := newGateway(t)
	p := newPhone(t, g)
	u, _ := g.PairingURL()
	nonce := u[strings.Index(u, "=")+1:]
	resp, out := p.do("POST", "/api/pair", map[string]string{"nonce": nonce, "name": "Pixel"}, nil)
	require.Equal(t, 200, resp.StatusCode, out)
	require.NotEmpty(t, out["computer_os"])
	require.Equal(t, "Pixel", out["name"])
	id, poll := out["pending_id"].(string), out["poll"].(string)
	require.Len(t, g.Auth.PendingList(), 1)

	// The poll secret is the proof of ownership; the ID alone is not enough.
	resp, out = p.do("POST", "/api/pair/cancel", map[string]string{"pending_id": id, "poll": "wrong"}, nil)
	require.Equal(t, 409, resp.StatusCode)
	require.Equal(t, "not_pending", code(out))
	require.Len(t, g.Auth.PendingList(), 1)

	resp, _ = p.do("POST", "/api/pair/cancel", map[string]string{"pending_id": id, "poll": poll}, map[string]string{"Origin": "https://evil.example"})
	require.Equal(t, 403, resp.StatusCode, "a foreign page cannot cancel")

	resp, out = p.do("POST", "/api/pair/cancel", map[string]string{"pending_id": id, "poll": poll}, nil)
	require.Equal(t, 200, resp.StatusCode, out)
	require.Equal(t, "cancelled", out["state"])
	require.Empty(t, g.Auth.PendingList(), "the computer no longer offers the request")
	_, err := g.Auth.Decide(id, true)
	require.ErrorIs(t, err, ErrNotPending, "a withdrawn request cannot be accepted")

	resp, out = p.do("POST", "/api/pair/poll", map[string]string{"pending_id": id, "poll": poll}, nil)
	require.Equal(t, 200, resp.StatusCode)
	require.Equal(t, "cancelled", out["state"])
	resp, _ = p.do("GET", "/api/me", nil, nil)
	require.Equal(t, 401, resp.StatusCode)

	resp, out = p.do("POST", "/api/pair/cancel", map[string]string{"pending_id": id, "poll": poll}, nil)
	require.Equal(t, 409, resp.StatusCode, "cancel is not repeated")
	require.Equal(t, "not_pending", code(out))
}

func TestPhoneCannotCancelADecidedPairing(t *testing.T) {
	t.Parallel()
	g, _ := newGateway(t)
	u, _ := g.PairingURL()
	pp, poll, err := g.Auth.Redeem(u[strings.Index(u, "=")+1:], "Pixel", "test", "127.0.0.1")
	require.NoError(t, err)
	_, err = g.Auth.Decide(pp.ID, true)
	require.NoError(t, err)
	require.ErrorIs(t, g.Auth.Cancel(pp.ID, poll), ErrNotPending)
	state, _, _, err := g.Auth.Poll(pp.ID, poll)
	require.NoError(t, err)
	require.Equal(t, "accepted", state)
}

func TestPairedPhoneSeesComputerAndTaskSteps(t *testing.T) {
	t.Parallel()
	g, _ := newGateway(t)
	p := newPhone(t, g)
	require.Equal(t, "accepted", p.pair(true))
	_, out := p.do("GET", "/api/me", nil, nil)
	computer, ok := out["computer"].(map[string]any)
	require.True(t, ok, out)
	require.NotEmpty(t, computer["os"])
	device := out["device"].(map[string]any)
	require.NotZero(t, device["paired_at"])
}

func TestTruncateUTF8KeepsWholeRunes(t *testing.T) {
	t.Parallel()
	require.Equal(t, "kısa", truncateUTF8("kısa", 10))
	s := truncateUTF8(strings.Repeat("ğ", 10), 5)
	require.True(t, utf8.ValidString(s))
	require.Equal(t, "ğğ…", s)
}

func TestTodoViewsCarryRealStepsWithinLimits(t *testing.T) {
	t.Parallel()
	s := session.Session{ID: "s1", Todos: []session.Todo{
		{Content: "Read the code", ActiveForm: "Reading the code", Status: session.TodoStatusCompleted},
		{Content: strings.Repeat("x", 1000), ActiveForm: "Writing", Status: session.TodoStatusInProgress},
	}}
	out := todoViews(s)
	require.Len(t, out, 2)
	require.Equal(t, "Read the code", out[0]["content"])
	require.Equal(t, string(session.TodoStatusCompleted), out[0]["status"])
	require.LessOrEqual(t, len(out[1]["content"]), maxTodoBytes+len("…"))
	require.Empty(t, todoViews(session.Session{}))
}

func TestToolCallViewsExposeOnlyNameAndPath(t *testing.T) {
	t.Parallel()
	m := message.Message{ID: "m1", SessionID: "s1", Role: message.Assistant}
	m.AddToolCall(message.ToolCall{ID: "a", Name: "edit", Input: `{"file_path":"internal/x.go","old_string":"secret body"}`, Finished: true})
	m.AddToolCall(message.ToolCall{ID: "b", Name: "bash", Input: `{"command":"go test ./..."}`})
	out := toolCallViews(m)
	require.Len(t, out, 2)
	require.Equal(t, map[string]any{"name": "edit", "finished": true, "path": "internal/x.go"}, out[0])
	require.Equal(t, map[string]any{"name": "bash", "finished": false}, out[1], "tool input other than a path stays on the computer")
}

func TestPermissionDetailsShowWhatReallyChanges(t *testing.T) {
	t.Parallel()
	edit := permission.PermissionRequest{SessionID: "s1", ToolName: "edit", Path: "D:/p", Params: map[string]any{
		"file_path":   "D:/p/app.css",
		"old_content": "a {\n  padding: 6px;\n}\n",
		"new_content": "a {\n  padding: 8px 16px;\n}\n",
	}}
	v := map[string]any{}
	permissionDetails(edit, v)
	require.Equal(t, "D:/p/app.css", v["file_path"])
	require.Contains(t, v["diff"], "+  padding: 8px 16px;")
	require.Contains(t, v["diff"], "-  padding: 6px;")
	require.Equal(t, 1, v["additions"])
	require.Equal(t, 1, v["removals"])
	require.NotContains(t, v, "command")

	// A tool that supplies no content gets no invented diff.
	v = map[string]any{}
	permissionDetails(permission.PermissionRequest{SessionID: "s1", ToolName: "write", Params: map[string]any{"file_path": "a.txt"}}, v)
	require.Equal(t, "a.txt", v["file_path"])
	require.NotContains(t, v, "diff")

	v = map[string]any{}
	permissionDetails(permission.PermissionRequest{SessionID: "s1", ToolName: "bash", Params: map[string]any{"command": "go test ./... " + strings.Repeat("y", 5000)}}, v)
	require.Contains(t, v["command"], "go test")
	require.LessOrEqual(t, len(v["command"].(string)), maxCommandBytes+len("…"))
	require.NotContains(t, v, "diff")
	require.NotContains(t, v, "file_path")

	// Params that are not an object are ignored rather than failing.
	v = map[string]any{}
	permissionDetails(permission.PermissionRequest{Params: "plain"}, v)
	require.Empty(t, v)
}

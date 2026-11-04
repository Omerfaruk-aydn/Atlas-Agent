package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
	"github.com/stretchr/testify/require"
)

// The composer's /auto-compact and /auto-compact-at write the server's
// config, and the settings read back what the server holds.
func TestCompactionSettingsAreWrittenToTheServer(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)

	type settings struct {
		AutoCompact   bool `json:"auto_compact"`
		AutoCompactAt int  `json:"auto_compact_at"`
	}
	read := func() settings {
		var s settings
		require.Equal(t, http.StatusOK, b.json(http.MethodGet, wsPath(ws.ID, "/settings"), nil, &s))
		return s
	}

	require.Equal(t, settings{AutoCompact: true}, read(), "automatic summarizing is on until it is turned off")

	require.Equal(t, http.StatusOK, b.json(http.MethodPost, wsPath(ws.ID, "/compaction"), map[string]any{"enabled": false}, nil))
	require.False(t, read().AutoCompact)

	require.Equal(t, http.StatusOK, b.json(http.MethodPost, wsPath(ws.ID, "/compaction"), map[string]any{"enabled": true, "percent": 80}, nil))
	require.Equal(t, settings{AutoCompact: true, AutoCompactAt: 80}, read())

	require.Equal(t, http.StatusOK, b.json(http.MethodPost, wsPath(ws.ID, "/compaction"), map[string]any{"percent": 0}, nil))
	require.Equal(t, settings{AutoCompact: true}, read(), "0 goes back to the built-in thresholds")

	for name, body := range map[string]map[string]any{
		"nothing to change": {},
		"too high":          {"percent": 100},
		"below one":         {"percent": 0.5},
		"negative":          {"percent": -5},
	} {
		status, _ := b.raw(http.MethodPost, wsPath(ws.ID, "/compaction"), body)
		require.Equal(t, http.StatusBadRequest, status, name)
	}
	require.Equal(t, settings{AutoCompact: true}, read(), "a refused request changes nothing")
}

// A goal is relayed to the server's coordinator. A session with no goal
// reports none, clearing it is harmless, and an oversized goal is refused
// before it reaches the server.
func TestGoalIsRelayedToTheServer(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	sess := b.newSession(ws.ID)
	goalPath := wsPath(ws.ID, "/sessions/"+sess.ID+"/goal")

	var status struct {
		Active bool   `json:"active"`
		Goal   string `json:"goal"`
	}
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, goalPath, nil, &status))
	require.False(t, status.Active)

	require.Equal(t, http.StatusOK, b.json(http.MethodPost, goalPath, map[string]string{"goal": ""}, nil), "clearing a goal that is not set")

	code, _ := b.raw(http.MethodPost, goalPath, map[string]string{"goal": strings.Repeat("x", maxGoalLen+1)})
	require.Equal(t, http.StatusBadRequest, code)
}

// A message written while a turn runs goes into that turn, as in the
// terminal, rather than waiting as a queued turn of its own. It needs no
// run ID, and the agent answers it.
func TestFollowUpGoesIntoTheRunningTurn(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	sess := b.newSession(ws.ID)
	promptPath := wsPath(ws.ID, "/sessions/"+sess.ID+"/prompt")

	b.prompt(ws.ID, sess.ID, "[[slow]]")
	require.Eventually(t, func() bool {
		s, err := h.upstream.GetSession(t.Context(), ws.ID, sess.ID)
		return err == nil && s.IsBusy
	}, 30*time.Second, 50*time.Millisecond)

	require.Equal(t, http.StatusAccepted, b.json(http.MethodPost, promptPath, map[string]any{"prompt": "steer the turn", "follow_up": true}, nil))
	require.Eventually(t, func() bool {
		for _, m := range b.messages(ws.ID, sess.ID) {
			if m.Role == "assistant" && strings.Contains(fmt.Sprint(m.Parts), "Echo: steer the turn") {
				return true
			}
		}
		return false
	}, 60*time.Second, 200*time.Millisecond, "the follow-up is answered")

	status, _ := b.raw(http.MethodPost, promptPath, map[string]any{"prompt": "no run id"})
	require.Equal(t, http.StatusBadRequest, status, "an ordinary prompt still needs a run ID")
}

// A server from an older build has no such route: that is told apart from a
// message that does not exist, so the app can say what to do about it.
func TestRewindBeforeTellsAnOldServerFromAMissingMessage(t *testing.T) {
	require.True(t, routeMissing(&upstreamError{Status: http.StatusNotFound, Message: "404 page not found"}))
	require.False(t, routeMissing(&upstreamError{Status: http.StatusNotFound, Message: "session not found"}))
	require.False(t, routeMissing(&upstreamError{Status: http.StatusBadGateway, Message: "page not found"}))

	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	sess := b.newSession(ws.ID)
	path := wsPath(ws.ID, "/sessions/"+sess.ID+"/rewind/before")

	status, _ := b.raw(http.MethodPost, path, map[string]any{})
	require.Equal(t, http.StatusBadRequest, status)
	status, _ = b.raw(http.MethodPost, path, map[string]any{"up_to_message_id": "missing"})
	require.NotEqual(t, http.StatusNotImplemented, status, "this server has the route")
	require.GreaterOrEqual(t, status, 400)
}

// Editing a message: the chat branches from just before it, the new version
// goes out there, and the original chat keeps what it had.
func TestEditedMessageBranchesFromJustBeforeIt(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	ev := b.events(ws.ID, 0)
	sess := b.newSession(ws.ID)
	b.run(ev, ws.ID, sess.ID, "first question")
	h.waitIdle(ws.ID, sess.ID, 30*time.Second)
	b.run(ev, ws.ID, sess.ID, "second question")
	h.waitIdle(ws.ID, sess.ID, 30*time.Second)

	source := b.messages(ws.ID, sess.ID)
	target := ""
	for _, m := range source {
		if string(m.Role) == "user" {
			target = m.ID
		}
	}
	require.NotEmpty(t, target)

	var result proto.RewindResult
	path := wsPath(ws.ID, "/sessions/", sess.ID, "/rewind/before")
	require.Equal(t, http.StatusOK, b.json(http.MethodPost, path, map[string]any{"up_to_message_id": target}, &result))

	text := func(sid string) string {
		raw, err := json.Marshal(b.messages(ws.ID, sid))
		require.NoError(t, err)
		return string(raw)
	}
	require.Contains(t, text(result.Session.ID), "first question")
	require.NotContains(t, text(result.Session.ID), "second question")

	b.run(ev, ws.ID, result.Session.ID, "edited question")
	h.waitIdle(ws.ID, result.Session.ID, 30*time.Second)
	require.Contains(t, text(result.Session.ID), "edited question")
	require.NotContains(t, text(sess.ID), "edited question")
	require.Len(t, b.messages(ws.ID, sess.ID), len(source))
}

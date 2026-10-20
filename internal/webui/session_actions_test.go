package webui

import (
	"net/http"
	"strings"
	"testing"

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

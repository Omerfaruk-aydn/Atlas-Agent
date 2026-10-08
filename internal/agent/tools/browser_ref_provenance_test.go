package tools

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBrowserFreshScopedRefsAreUsable(t *testing.T) {
	t.Parallel()
	sess := newObservingSession()
	h := newBrowserHarness(t, sess)
	obs := h.run(`{"action":"observe","advanced":{"mode":"scope","scope":"#panel"}}`)
	require.False(t, obs.IsError)
	require.Contains(t, obs.Content, "e1")
	r := h.run(`{"action":"set_field","ref":"e1","advanced":{"value":"300","commit":"blur"}}`)
	require.False(t, r.IsError, r.Content)
	require.Len(t, sess.fields, 1)
	// A scoped observation does not grant refs that it did not contain.
	unseen := h.run(`{"action":"set_field","ref":"e2","advanced":{"value":"16"}}`)
	require.True(t, unseen.IsError)
	require.Contains(t, unseen.Content, "stale_ref")
}

func TestBrowserRefsExpireWhenContextChanges(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"document", "tab", "connection"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			sess := newObservingSession()
			h := newBrowserHarness(t, sess)
			require.False(t, h.run(`{"action":"observe","advanced":{"mode":"scope","scope":"#panel"}}`).IsError)
			switch change {
			case "document":
				sess.doc = "doc-reloaded"
			case "tab":
				sess.tab = "other-tab"
			case "connection":
				sess.instance = "reconnected"
			}
			stale := h.run(`{"action":"set_field","ref":"e1","advanced":{"value":"300"}}`)
			require.True(t, stale.IsError)
			require.Empty(t, sess.fields)
			require.False(t, h.run(`{"action":"observe","advanced":{"mode":"scope","scope":"#panel"}}`).IsError)
			require.False(t, h.run(`{"action":"set_field","ref":"e1","advanced":{"value":"300"}}`).IsError)
		})
	}
}

func TestBrowserRefsRequireKnownDocumentIdentity(t *testing.T) {
	t.Parallel()
	sess := newObservingSession()
	h := newBrowserHarness(t, sess)
	require.False(t, h.run(`{"action":"observe"}`).IsError)
	sess.doc = ""
	require.True(t, h.run(`{"action":"set_field","ref":"e1","advanced":{"value":"300"}}`).IsError)
	require.True(t, h.run(`{"action":"observe"}`).IsError)
	require.Empty(t, sess.fields)
}

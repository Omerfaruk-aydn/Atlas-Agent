package browser

import (
	"context"
	"errors"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/stretchr/testify/require"
)

// disconnectingSession is a fake whose browser can be made to go away, as
// when the user closes the Chrome window between turns.
type disconnectingSession struct {
	fakeSession
	gone bool
}

func (d *disconnectingSession) disconnected() bool { return d.gone }

func TestManagerReplacesSessionWhoseBrowserWentAway(t *testing.T) {
	t.Parallel()
	created := 0
	var last *disconnectingSession
	m := newManager(Options{}, func(Options) (Session, error) {
		created++
		last = &disconnectingSession{}
		return last, nil
	})

	s1, err := m.Session("sess-1")
	require.NoError(t, err)
	first := last
	first.gone = true

	s2, err := m.Session("sess-1")
	require.NoError(t, err)
	require.NotSame(t, s1, s2, "a disconnected session must not be handed out again")
	require.True(t, first.closed, "the dead session is released")
	require.Equal(t, 2, created)

	s3, err := m.Session("sess-1")
	require.NoError(t, err)
	require.Same(t, s2, s3, "a live session is still reused")
}

func TestManagerKeepsLiveSessionsWithoutIdleTimeout(t *testing.T) {
	t.Parallel()
	created := 0
	m := newManager(Options{}, func(Options) (Session, error) {
		created++
		return &disconnectingSession{}, nil
	})
	_, err := m.Session("sess-1")
	require.NoError(t, err)
	_, err = m.Session("sess-1")
	require.NoError(t, err)
	require.Equal(t, 1, created)
}

func TestChromedpSessionReportsLostBrowser(t *testing.T) {
	t.Parallel()
	dead, cancel := context.WithCancel(context.Background())
	cancel()
	s := &chromedpSession{ctx: dead, rootCtx: dead}
	require.True(t, s.disconnected())

	err := s.run(chromedp.Evaluate("1", nil))
	require.ErrorIs(t, err, ErrBrowserDisconnected)
	require.Contains(t, err.Error(), "next browser action starts a fresh browser session")
}

func TestChromedpSessionKeepsCallerCancellation(t *testing.T) {
	t.Parallel()
	dead, cancelBrowser := context.WithCancel(context.Background())
	cancelBrowser()
	call, cancelCall := context.WithCancel(context.Background())
	cancelCall()
	s := &chromedpSession{ctx: dead, rootCtx: dead}
	restore := s.BindContext(call)
	defer restore()

	err := s.run(chromedp.Evaluate("1", nil))
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, errors.Is(err, ErrBrowserDisconnected), "a stopped turn is not a browser failure")
}

func TestChromedpSessionWithLiveContextIsConnected(t *testing.T) {
	t.Parallel()
	s := &chromedpSession{ctx: t.Context(), rootCtx: t.Context()}
	require.False(t, s.disconnected())
}

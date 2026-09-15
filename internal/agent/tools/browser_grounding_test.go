package tools

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/stretchr/testify/require"
)

// errBrowserGroundingBoom simulates a backend failure in tests.
var errBrowserGroundingBoom = errors.New("boom")

func TestBrowserToolNavigateAppendsFreshSnapshot(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	sess, err := sessions.Session("test-session")
	require.NoError(t, err)
	sess.(*fakeBrowserSession).snapshot = []browser.SnapshotElement{
		{Ref: "e1", Role: "link", Tag: "a", Name: "Home"},
	}
	resp := runBrowserTool(t, sessions, &mockPermissionService{},
		BrowserParams{Action: "navigate", URL: "https://example.com"})
	require.False(t, resp.IsError)
	require.Contains(t, resp.Content, "Navigated to https://example.com")
	require.Contains(t, resp.Content, "Interactive elements now:")
	require.Contains(t, resp.Content, `[e1] link "Home"`)
}

func TestBrowserToolCapsAttachedSnapshot(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	sess, err := sessions.Session("test-session")
	require.NoError(t, err)
	els := make([]browser.SnapshotElement, 0, 120)
	for i := 1; i <= 120; i++ {
		els = append(els, browser.SnapshotElement{
			Ref:  "e" + strconv.Itoa(i),
			Role: "button",
			Tag:  "button",
		})
	}
	sess.(*fakeBrowserSession).snapshot = els
	resp := runBrowserTool(t, sessions, &mockPermissionService{},
		BrowserParams{Action: "navigate", URL: "https://example.com"})
	require.False(t, resp.IsError)
	require.Equal(t, 100, strings.Count(resp.Content, "(button)"))
}

func TestBrowserToolNotesSnapshotFailure(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	sess, err := sessions.Session("test-session")
	require.NoError(t, err)
	sess.(*fakeBrowserSession).snapshotErr = errBrowserGroundingBoom
	resp := runBrowserTool(t, sessions, &mockPermissionService{},
		BrowserParams{Action: "back"})
	require.False(t, resp.IsError)
	require.Contains(t, resp.Content, "Fresh snapshot unavailable")
}

func TestBrowserToolClickFallsBackToCoordinates(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	sess, err := sessions.Session("test-session")
	require.NoError(t, err)
	fake := sess.(*fakeBrowserSession)
	fake.clickErr = errBrowserGroundingBoom
	fake.snapshot = []browser.SnapshotElement{{
		Ref: "e3", Role: "button", Tag: "button", Name: "Save",
		Rect: browser.ElementRect{X: 10, Y: 20, Width: 100, Height: 50},
	}}
	resp := runBrowserTool(t, sessions, &mockPermissionService{},
		BrowserParams{Action: "click", Ref: "e3"})
	require.False(t, resp.IsError)
	require.Equal(t, 60.0, fake.clickAtX)
	require.Equal(t, 45.0, fake.clickAtY)
	require.Contains(t, resp.Content, "coordinate fallback")
}

func TestBrowserToolClickReportsGoneRef(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	sess, err := sessions.Session("test-session")
	require.NoError(t, err)
	fake := sess.(*fakeBrowserSession)
	fake.clickErr = errBrowserGroundingBoom
	fake.snapshot = []browser.SnapshotElement{
		{Ref: "e9", Role: "link", Tag: "a"},
	}
	resp := runBrowserTool(t, sessions, &mockPermissionService{},
		BrowserParams{Action: "click", Ref: "e3"})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "gone from the page")
}

func TestBrowserToolClickWithoutRefKeepsBackendError(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	sess, err := sessions.Session("test-session")
	require.NoError(t, err)
	sess.(*fakeBrowserSession).clickErr = errBrowserGroundingBoom
	resp := runBrowserTool(t, sessions, &mockPermissionService{},
		BrowserParams{Action: "click", Selector: "#gone"})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "click failed")
	require.NotContains(t, resp.Content, "fallback")
}

func TestBrowserToolScreenshotReturnsAnnotatedImage(t *testing.T) {
	t.Parallel()
	sessions := newFakeBrowserSessions()
	sess, err := sessions.Session("test-session")
	require.NoError(t, err)
	sess.(*fakeBrowserSession).annotated = []byte("annotated-png")
	resp := runBrowserTool(t, sessions, &mockPermissionService{},
		BrowserParams{Action: "screenshot"})
	require.False(t, resp.IsError)
	require.Equal(t, "image", resp.Type)
	require.Equal(t, []byte("annotated-png"), resp.Data)
	require.Contains(t, resp.Content, "Numbered boxes")
}

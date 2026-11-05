package dots

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/stretchr/testify/require"
)

func TestMarkerAndHeld(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.False(t, IsWorkspace(dir))
	require.NoError(t, writeMarker(Dot{ID: "dot_test", Workspace: dir}))
	require.True(t, IsWorkspace(dir))
	require.NoError(t, ErrIfHeld(dir))

	require.NoError(t, os.MkdirAll(screenDir(dir), 0o700))
	require.NoError(t, writeEndpoint(dir, Endpoint{HTTP: "http://127.0.0.1:1", PID: os.Getpid(), TakenOver: true, Width: ScreenWidth, Height: ScreenHeight}))
	// The pid is alive, but nothing answers the debugging port, so a held
	// flag on a dead screen does not block the dot.
	require.NoError(t, ErrIfHeld(dir))
}

func TestScreenNavigatesAndCaptures(t *testing.T) {
	if browser.FindChrome() == "" {
		t.Skip("Chrome is not installed")
	}
	dir := t.TempDir()
	require.NoError(t, writeMarker(Dot{ID: "dot_screen", Workspace: dir}))
	t.Cleanup(func() { _ = Stop(dir) })

	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><title>Atlas Dot Screen</title><h1>Merhaba</h1>"))
	}))
	t.Cleanup(page.Close)

	ctx := context.Background()
	_, err := Ensure(ctx, dir)
	require.NoError(t, err)
	require.NoError(t, OpenURL(ctx, dir, page.URL))

	deadline := time.Now().Add(8 * time.Second)
	var status ScreenStatus
	for {
		status = Status(ctx, dir)
		if status.Title == "Atlas Dot Screen" {
			break
		}
		if time.Now().After(deadline) {
			ep, _ := readEndpoint(dir)
			req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, ep.HTTP+"/json/list", nil)
			if reqErr != nil {
				t.Fatalf("title = %q url = %q list= err=%v", status.Title, status.URL, reqErr)
			}
			raw, err := http.DefaultClient.Do(req)
			body := ""
			if err == nil {
				b, _ := io.ReadAll(raw.Body)
				raw.Body.Close()
				body = string(b)
			}
			t.Fatalf("title = %q url = %q list=%s err=%v", status.Title, status.URL, body, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.True(t, status.Running)
	require.Equal(t, IsolationChrome, status.Isolation)
	require.NotContains(t, status.URL, "chrome://")

	png, err := Capture(ctx, dir)
	require.NoError(t, err)
	require.Greater(t, len(png), 32)

	_, err = SetTakenOver(dir, true)
	require.NoError(t, err)
	err = Act(ctx, dir, Pointer{Kind: "click", X: 10, Y: 10, Button: "left"}, false)
	require.ErrorIs(t, err, ErrHeld)
	require.NoError(t, Act(ctx, dir, Pointer{Kind: "click", X: 12, Y: 12, Button: "left"}, true))
}

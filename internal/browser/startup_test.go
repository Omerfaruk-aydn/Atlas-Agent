package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestInitialURLIsLaunchScoped(t *testing.T) {
	t.Parallel()
	var launched []Options
	m := newManager(Options{}, func(opts Options) (Session, error) {
		launched = append(launched, opts)
		return &fakeSession{}, nil
	})
	defer m.CloseAll()
	first, err := m.SessionContextURL(t.Context(), "chat", "https://example.com/task")
	require.NoError(t, err)
	reused, err := m.SessionContextURL(t.Context(), "chat", "https://example.com/next")
	require.NoError(t, err)
	require.Same(t, first, reused)
	_, err = m.SessionContext(t.Context(), "other")
	require.NoError(t, err)
	require.Len(t, launched, 2)
	require.Equal(t, "https://example.com/task", launched[0].initialURL)
	require.Empty(t, launched[1].initialURL)
	require.Empty(t, m.opts.initialURL)
	for _, invalid := range []string{"about:blank", "file:///test", "https:///missing", "https://user:password@example.com"} {
		_, err := m.SessionContextURL(t.Context(), "invalid", invalid)
		require.Error(t, err)
	}
	require.Len(t, launched, 2)
}

func TestInitialDestinationRealChrome(t *testing.T) {
	if testing.Short() {
		t.Skip("Real browser excluded in short mode")
	}
	exe := findChrome()
	if exe == "" {
		t.Skip("No installed Chrome")
	}
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprintf("remote_%t", remote), func(t *testing.T) {
			var loads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/start" {
					http.Redirect(w, r, "/task", http.StatusFound)
					return
				}
				if r.URL.Path == "/task" {
					loads.Add(1)
				}
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, `<title>Task destination</title><script>console.log('startup-reporting')</script>`)
			}))
			defer server.Close()
			opts := Options{
				ExecutablePath: exe, UserDataDir: t.TempDir(), Headless: true,
				OverlayDisabled: true, ActionTimeout: 30 * time.Second, initialURL: server.URL + "/start",
			}
			if remote {
				listener, err := net.Listen("tcp4", "127.0.0.1:0")
				require.NoError(t, err)
				opts.RemoteURL = "http://" + listener.Addr().String()
				require.NoError(t, listener.Close())
				t.Cleanup(func() {
					client := &http.Client{Timeout: time.Second}
					response, err := client.Get(opts.RemoteURL + "/json/version")
					if err != nil {
						return
					}
					var version struct{ WebSocketDebuggerURL string }
					err = json.NewDecoder(response.Body).Decode(&version)
					response.Body.Close()
					if err != nil {
						t.Error(err)
						return
					}
					cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					conn, _, err := websocket.DefaultDialer.DialContext(cleanupCtx, version.WebSocketDebuggerURL, nil)
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.Close()
					require.NoError(t, conn.WriteJSON(map[string]any{"id": 1, "method": "Browser.close"}))
					require.Eventually(t, func() bool { return !remoteAlive(opts.RemoteURL) }, 5*time.Second, 50*time.Millisecond)
					time.Sleep(500 * time.Millisecond)
				})
			}
			sess, err := newChromedpSessionContext(t.Context(), opts)
			require.NoError(t, err)
			driver := sess.(*chromedpSession)
			defer sess.Close()

			got, err := sess.URL()
			require.NoError(t, err)
			require.Equal(t, server.URL+"/task", got)
			require.EqualValues(t, 1, loads.Load())
			require.NoError(t, sess.Navigate(opts.initialURL))
			require.EqualValues(t, 1, loads.Load(), "First tool action must not reload the startup destination")
			if !remote {
				require.Eventually(t, func() bool {
					for _, entry := range sess.ConsoleLogs() {
						if entry.Text == "startup-reporting" {
							return true
						}
					}
					return false
				}, time.Second, 10*time.Millisecond)
			}
			var pages []*target.Info
			require.NoError(t, chromedp.Run(driver.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
				var err error
				pages, err = target.GetTargets().Do(ctx)
				return err
			})))
			pageCount := 0
			for _, info := range pages {
				if info.Type == "page" {
					pageCount++
					require.Equal(t, server.URL+"/task", info.URL, "No launcher-owned blank tab may remain")
				}
			}
			require.Equal(t, 1, pageCount)
			require.NoError(t, sess.Navigate(server.URL+"/other"))
			got, err = sess.URL()
			require.NoError(t, err)
			require.Equal(t, server.URL+"/other", got)
			require.NoError(t, sess.Navigate(opts.initialURL))
			require.EqualValues(t, 2, loads.Load(), "Later explicit navigation must still run")
		})
	}
}

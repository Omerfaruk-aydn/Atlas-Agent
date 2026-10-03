package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAdvancedBrowserRealFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("Real browser excluded in short mode")
	}
	exe := ""
	for _, p := range []string{"C:/Program Files/Google/Chrome/Application/chrome.exe", "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe", "/usr/bin/chromium", "/usr/bin/google-chrome"} {
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			exe = p
			break
		}
	}
	if exe == "" {
		t.Skip("No installed browser; no real browser proof")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/download" {
			w.Header().Set("Content-Disposition", `attachment; filename="report.txt"`)
			fmt.Fprint(w, "verified report")
			return
		}
		if r.URL.Path == "/frame" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<button aria-label="Inside" onclick="this.textContent='Done'">Inside</button>`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><input aria-label="Email"><input id="upload" type="file"><button aria-label="Save" onclick="document.querySelector('#result').textContent='Saved'">Save</button><p id="result"></p><iframe src="/frame"></iframe><div id="shadow"></div><a id="download" href="/download">Download</a><script>const s=document.querySelector('#shadow').attachShadow({mode:'open'});s.innerHTML='<button aria-label="Shadow" onclick="this.textContent=\'Clicked\'">Shadow</button>';</script>`)
	}))
	defer server.Close()
	session, err := newChromedpSession(Options{ExecutablePath: exe, Headless: true, UserDataDir: t.TempDir(), ActionTimeout: 10 * time.Second})
	require.NoError(t, err)
	defer session.Close()
	require.NoError(t, session.Navigate(server.URL))
	driver := session.(AdvancedSession)
	run := func(p Request) json.RawMessage {
		t.Helper()
		data, _, err := driver.Advanced(t.Context(), p)
		require.NoError(t, err)
		return data
	}
	require.Contains(t, string(run(Request{Action: "find", Role: "textbox", Name: "Email"})), `"count":1`)
	run(Request{Action: "semantic_type", Role: "textbox", Name: "Email", Text: "fixture@example.test"})
	require.Contains(t, string(run(Request{Action: "assert", Role: "textbox", Name: "Email", Condition: "value", Expected: "fixture@example.test"})), `"passed":true`)
	run(Request{Action: "semantic_click", Role: "button", Name: "Save"})
	run(Request{Action: "assert", Selector: "#result", Condition: "text", Expected: "Saved"})
	run(Request{Action: "semantic_click", Role: "button", Name: "Shadow"})
	run(Request{Action: "assert", Role: "button", Name: "Shadow", Condition: "text", Expected: "Clicked"})
	data, _, err := driver.Advanced(t.Context(), Request{Action: "frames"})
	require.NoError(t, err)
	var tree struct {
		FrameTree struct {
			ChildFrames []struct {
				Frame struct {
					ID string `json:"id"`
				} `json:"frame"`
			} `json:"childFrames"`
		} `json:"frameTree"`
	}
	require.NoError(t, json.Unmarshal(data, &tree))
	require.NotEmpty(t, tree.FrameTree.ChildFrames)
	frameID := tree.FrameTree.ChildFrames[0].Frame.ID
	run(Request{Action: "semantic_click", FrameID: frameID, Role: "button", Name: "Inside"})
	run(Request{Action: "assert", FrameID: frameID, Role: "button", Name: "Inside", Condition: "text", Expected: "Done"})
	_, image, err := driver.Advanced(t.Context(), Request{Action: "capture_region", Width: 200, Height: 100})
	require.NoError(t, err)
	require.NotEmpty(t, image)
	upload := filepath.Join(t.TempDir(), "upload.txt")
	require.NoError(t, os.WriteFile(upload, []byte("fixture"), 0o600))
	run(Request{Action: "upload", Selector: "#upload", Paths: []string{upload}})
	downloadDir := t.TempDir()
	run(Request{Action: "download_start", Paths: []string{downloadDir}})
	started := time.Now().Add(-time.Second)
	require.NoError(t, session.Click("#download"))
	run(Request{Action: "download_wait", Paths: []string{filepath.Join(downloadDir, "report.txt")}, NewerThan: started})
	require.NotEmpty(t, run(Request{Action: "network"}))
	original := run(Request{Action: "tabs"})
	var tabs struct {
		Active string `json:"active_tab"`
	}
	require.NoError(t, json.Unmarshal(original, &tabs))
	newTab := run(Request{Action: "tab_new"})
	require.NoError(t, session.Navigate(server.URL))
	run(Request{Action: "assert", Selector: "#result", Condition: "visible"})
	run(Request{Action: "tab_select", TabID: tabs.Active})
	var added struct {
		Active string `json:"active_tab"`
	}
	require.NoError(t, json.Unmarshal(newTab, &added))
	run(Request{Action: "tab_close", TabID: added.Active})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err = driver.Advanced(ctx, Request{Action: "find", Selector: "#result"})
	require.Error(t, err)
}

func TestNetworkRedactionAndBound(t *testing.T) {
	t.Parallel()
	s := &chromedpSession{}
	for range 250 {
		s.appendNetwork(NetworkEntry{URL: redactedURL("https://user:secret@example.test/path?token=secret#code")})
	}
	require.Len(t, s.network, 200)
	require.Equal(t, "https://example.test/path", s.network[0].URL)
}

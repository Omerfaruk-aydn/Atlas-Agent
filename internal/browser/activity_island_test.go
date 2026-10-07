package browser

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	"github.com/stretchr/testify/require"
)

// The page overlay is reachable by page scripts, so it only announces a
// pending request; answering happens in Atlas.
func TestPageOverlayAnnouncesWaitingWithoutAnswerControls(t *testing.T) {
	exe := ""
	for _, p := range []string{"C:/Program Files/Google/Chrome/Application/chrome.exe", "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe", "/usr/bin/chromium", "/usr/bin/google-chrome"} {
		if _, err := os.Stat(p); err == nil {
			exe = p
			break
		}
	}
	if exe == "" {
		t.Skip("Chrome/Chromium not installed")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><button id="go">Go</button></body></html>`))
	}))
	defer server.Close()
	session, err := newChromedpSession(Options{ExecutablePath: exe, Headless: true, UserDataDir: t.TempDir(), ActionTimeout: 10 * time.Second})
	require.NoError(t, err)
	defer session.Close()
	s := session.(*chromedpSession)
	s.activityEnabled = true
	require.NoError(t, s.Navigate(server.URL))

	ctx, end := activity.StartFlow(t.Context(), "page-wait")
	defer end()
	ctx = activity.WithLanguage(ctx, "tr")
	s.BeginActivity(ctx, "page-wait", "click", "#go")()
	pending := activity.AwaitPrompt(ctx, activity.Prompt{Kind: activity.KindQuestion, ID: "q", Questions: []activity.PromptQuestion{{ID: "q1", Type: activity.QuestionYesNo, Text: "Devam?"}}}, func(activity.PromptResponse) bool { return true })
	last := ""
	read := func() string {
		v, err := s.Eval(`(()=>{const b=window[Symbol.for('atlas.agent.activity.v1')]?.edges.parentNode.querySelector('.control-banner');return b?JSON.stringify({waiting:b.classList.contains('waiting'),text:b.textContent,buttons:b.querySelectorAll('button,input,textarea,[role=button]').length}):''})()`)
		var decoded string
		if json.Unmarshal([]byte(v), &decoded) == nil {
			v = decoded
		}
		last = v
		if err != nil {
			last = err.Error()
		}
		return v
	}
	waitFor(t, &last, func() bool {
		v := read()
		return v != "" && containsAll(v, `"waiting":true`, "Yanıtını bekliyor", "Terminalden yanıtla", `"buttons":0`)
	})
	pending.Done(activity.OutcomeAnswered)
	waitFor(t, &last, func() bool {
		v := read()
		return containsAll(v, `"waiting":false`, "Devam ediyor") && !containsAll(v, "Terminalden yanıtla")
	})
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

func waitFor(t *testing.T, last *string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("Overlay state not reached; last read: %s", *last)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

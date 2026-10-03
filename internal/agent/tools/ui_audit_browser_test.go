package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/experiments"
	"github.com/stretchr/testify/require"
)

func TestAuditScriptsAgainstRealBrowser(t *testing.T) {
	executable := os.Getenv("ATLAS_TEST_BROWSER")
	if executable == "" {
		t.Skip("Set ATLAS_TEST_BROWSER to an installed Chromium executable")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><button id="empty"></button><input id="unlabelled"><button id="named">Continue</button></body></html>`))
	}))
	t.Cleanup(server.Close)
	manager := browser.GetManager(browser.Options{ExecutablePath: executable, Headless: true, UserDataDir: t.TempDir(), ActionTimeout: 15 * time.Second})
	session, err := manager.Session("audit-fixture")
	require.NoError(t, err)
	t.Cleanup(func() { manager.Close("audit-fixture") })
	require.NoError(t, session.Navigate(server.URL))
	output, err := session.Eval(basicAccessibilityScript)
	require.NoError(t, err)
	var audit struct {
		Engine     string `json:"engine"`
		Partial    bool   `json:"partial"`
		Violations []struct {
			Rule string `json:"rule"`
		} `json:"violations"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &audit))
	require.Equal(t, "bounded-dom-checks", audit.Engine)
	require.True(t, audit.Partial)
	require.Len(t, audit.Violations, 3)
	output, err = session.Eval(interactionAuditScript)
	require.NoError(t, err)
	require.Contains(t, output, "unlabelled")
	before, err := session.Screenshot(false)
	require.NoError(t, err)
	_, err = session.Eval(`document.body.style.backgroundColor='rgb(255,0,0)'`)
	require.NoError(t, err)
	after, err := session.Screenshot(false)
	require.NoError(t, err)
	diff, err := experiments.CompareImages(t.Context(), before, after, 0)
	require.NoError(t, err)
	require.Greater(t, diff.ChangedRatio, 0.5)
}

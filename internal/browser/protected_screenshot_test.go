package browser

import (
	"bytes"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProtectedScreenshotPreservesPageAndMasksCredentials(t *testing.T) {
	exe := findChrome()
	if exe == "" {
		t.Skip("No installed browser")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<style>body{margin:0;background:white;height:1800px}#safe{position:absolute;left:10px;top:10px;width:180px;height:30px;background:#00ff00}input{position:absolute;left:10px;top:60px;width:180px;height:30px}#plain{position:absolute;left:10px;top:110px;width:200px;height:30px;background:#ff0000}#below{position:absolute;left:10px;top:1200px;width:180px;height:30px}</style><div id="safe">Normal page content</div><input id="pw" value="Synthetic-Privacy-77!" type="text"><div id="plain">Synthetic-Privacy-77!</div><input id="below" value="Synthetic-Privacy-77!">`))
	}))
	defer server.Close()
	driver, err := newChromedpSession(Options{ExecutablePath: exe, Headless: true, UserDataDir: t.TempDir(), OverlayDisabled: true, ActionTimeout: 15 * time.Second})
	require.NoError(t, err)
	defer driver.Close()
	s := driver.(*chromedpSession)
	require.NoError(t, s.Navigate(server.URL))
	redact := func(text string) string { return strings.ReplaceAll(text, "Synthetic-Privacy-77!", "[REDACTED]") }
	for _, full := range []bool{false, true} {
		data, masks, err := s.ProtectedScreenshot(t.Context(), full, false, redact)
		require.NoError(t, err)
		require.GreaterOrEqual(t, masks, 2)
		img, err := png.Decode(bytes.NewReader(data))
		require.NoError(t, err)
		require.Equal(t, color.RGBA{R: 42, G: 44, B: 52, A: 255}, color.RGBAModel.Convert(img.At(80, 75)))
		green := color.RGBAModel.Convert(img.At(170, 25)).(color.RGBA)
		require.Equal(t, uint8(255), green.G, "ordinary page content remains visible")
		if full {
			require.Greater(t, img.Bounds().Dy(), 1230)
			require.Equal(t, color.RGBA{R: 42, G: 44, B: 52, A: 255}, color.RGBAModel.Convert(img.At(80, 1215)))
		}
	}
	unchanged, err := s.Eval(`document.querySelector('#pw').value === 'Synthetic-Privacy-77!' && document.querySelector('#plain').textContent === 'Synthetic-Privacy-77!'`)
	require.NoError(t, err)
	require.Equal(t, "true", unchanged, "privacy masking does not change the actual form or DOM text")
	_, _, err = s.ProtectedScreenshot(t.Context(), false, true, redact)
	require.NoError(t, err, "annotations remain supported")
	data, viewport, _, err := s.ProtectedViewportScreenshot(t.Context(), redact)
	require.NoError(t, err)
	require.NotEmpty(t, data)
	require.NotEmpty(t, viewport.ID, "stable protected capture preserves pointer grants")
	_, err = s.Eval(`globalThis.privacyTicker = setInterval(() => document.querySelector('#safe').textContent = String(performance.now()), 1); true`)
	require.NoError(t, err)
	data, _, err = s.ProtectedScreenshot(t.Context(), false, false, redact)
	require.ErrorContains(t, err, "stale_layout")
	require.Empty(t, data, "unstable layouts must not emit unverified pixels")
	_, _ = s.Eval(`clearInterval(globalThis.privacyTicker); true`)
}

func TestExternalDialogClosureClearsBrowserState(t *testing.T) {
	exe := findChrome()
	if exe == "" {
		t.Skip("No installed browser")
	}
	driver, err := newChromedpSession(Options{ExecutablePath: exe, Headless: true, UserDataDir: t.TempDir(), OverlayDisabled: true, ActionTimeout: 10 * time.Second})
	require.NoError(t, err)
	defer driver.Close()
	s := driver.(*chromedpSession)
	_, err = s.Eval(`setTimeout(() => alert('Synthetic native dialog'), 0); true`)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(s.PendingDialogs()) == 1 }, 3*time.Second, 10*time.Millisecond)
	_, err = s.RawCDP("Page.handleJavaScriptDialog", map[string]any{"accept": true})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(s.PendingDialogs()) == 0 }, 3*time.Second, 10*time.Millisecond)
}

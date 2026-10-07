package browser

import (
	"testing"
	"time"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"

	"github.com/stretchr/testify/require"
)

func TestWorkflowRequestPreflight(t *testing.T) {
	t.Parallel()
	for _, p := range []Request{
		{Action: "semantic_click", Role: "button", Name: "Save", Scope: "#profile"},
		{Action: "wait_for", Condition: "ready"},
		{Action: "wait_for", Condition: "network_idle"},
		{Action: "assert", Selector: "#check", Condition: "checked", Expected: "true"},
		{Action: "assert", Selector: ".row", Condition: "count", Expected: "3"},
	} {
		require.NoError(t, ValidateWorkflowRequest(p))
	}
	for _, p := range []Request{
		{Action: "semantic_click"},
		{Action: "assert", Condition: "sleep"},
		{Action: "upload", Paths: []string{"file.txt"}},
		{Action: "tab_select"},
		{Action: "wait_for", Condition: "ready", TimeoutMS: 30001},
		{Action: "find", Role: "button", ExpectedOrigin: "file:///private"},
	} {
		require.Error(t, ValidateWorkflowRequest(p))
	}
}

func TestBrowserOriginCanonicalization(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"https://Example.test:443/path", "https://example.test"} {
		origin, err := browserOrigin(raw)
		require.NoError(t, err)
		require.Equal(t, "https://example.test", origin)
	}
	_, err := browserOrigin("https://user:secret@example.test")
	require.Error(t, err)
	_, err = browserOrigin("javascript:alert(1)")
	require.Error(t, err)
}

func TestDownloadEvidenceRejectsOtherTabsAndUnfinishedTransfers(t *testing.T) {
	t.Parallel()
	s := &chromedpSession{downloadFrames: map[string]bool{"owned": true}, downloads: map[string]browserDownload{}}
	s.handleDownloadEvent(&cdpbrowser.EventDownloadWillBegin{FrameID: cdp.FrameID("other"), GUID: "bad", SuggestedFilename: "report.txt"})
	require.Empty(t, s.downloads)
	s.handleDownloadEvent(&cdpbrowser.EventDownloadWillBegin{FrameID: cdp.FrameID("owned"), GUID: "good", SuggestedFilename: "report.txt"})
	p := Request{Paths: []string{"report.txt"}, NewerThan: time.Now().Add(-time.Second)}
	got, err := s.completedDownload(p)
	require.NoError(t, err)
	require.Empty(t, got.ID)
	s.handleDownloadEvent(&cdpbrowser.EventDownloadProgress{GUID: "good", State: cdpbrowser.DownloadProgressStateCompleted})
	got, err = s.completedDownload(p)
	require.NoError(t, err)
	require.Equal(t, "good", got.ID)
	s.handleDownloadEvent(&cdpbrowser.EventDownloadProgress{GUID: "good", State: cdpbrowser.DownloadProgressStateCanceled})
	_, err = s.completedDownload(p)
	require.ErrorContains(t, err, "download_canceled")
}

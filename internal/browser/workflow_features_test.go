package browser

import (
	"context"
	"strconv"
	"testing"
	"time"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
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

func TestResponseHeadersDoNotEndNetworkWait(t *testing.T) {
	t.Parallel()
	s := &chromedpSession{}
	s.startRequest("loading")
	s.appendNetwork(NetworkEntry{RequestID: "loading", Status: 200})
	require.Contains(t, s.requests, "loading")
	s.handleTargetEvent(&network.EventLoadingFinished{RequestID: network.RequestID("loading")})
	require.NotContains(t, s.requests, "loading")
}

func TestNetworkTrackingOverflowIsExplicit(t *testing.T) {
	t.Parallel()
	s := &chromedpSession{}
	for i := 0; i < 501; i++ {
		s.startRequest(strconv.Itoa(i))
	}
	require.Len(t, s.requests, 500)
	require.True(t, s.requestTrackingIncomplete, "Dropped requests must not become false idle evidence")
}

func TestMissingFrameAndCanceledPoll(t *testing.T) {
	t.Parallel()
	tree := map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "main", "loaderId": "one"}, "childFrames": []any{map[string]any{"frame": map[string]any{"id": "child", "loaderId": "two"}}}}}
	require.Equal(t, "two", browserFrameInfo(tree, "child")["loaderId"])
	require.Nil(t, browserFrameInfo(tree, "gone"))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, browserPoll(ctx), context.Canceled)
}

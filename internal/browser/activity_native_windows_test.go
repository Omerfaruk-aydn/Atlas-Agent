//go:build windows

package browser

import (
	"math"
	"testing"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/stretchr/testify/require"
)

func TestNativeBrowserWindowMatchingAcceptsDIPAndPhysicalBounds(t *testing.T) {
	t.Parallel()
	rect := browserDesktopRect{200, 100, 2200, 1300}
	require.Zero(t, browserBoundsScore(rect, &cdpbrowser.Bounds{Left: 200, Top: 100, Width: 2000, Height: 1200}, 2))
	require.Zero(t, browserBoundsScore(rect, &cdpbrowser.Bounds{Left: 100, Top: 50, Width: 1000, Height: 600}, 2))
	require.Greater(t, browserBoundsScore(rect, &cdpbrowser.Bounds{Left: 500, Top: 100, Width: 2000, Height: 1200}, 2), float64(64))
	require.True(t, math.IsInf(browserBoundsScore(rect, nil, 1), 1))
}

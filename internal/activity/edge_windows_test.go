//go:build windows

package activity

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMonitorResizeFailureRetainsPreviousSurfaces(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r := newPlatformRenderer().(*windowsRenderer)
	defer func() {
		for i := range r.edges {
			r.edges[i].close()
		}
	}()
	area := overlayRect{0, 0, 320, 180}
	require.True(t, r.replaceEdges(area, 1, (*nativeEdgeSurface).create))
	previous := r.edges
	calls := 0
	var temporaryDC uintptr
	require.False(t, r.replaceEdges(overlayRect{-640, 0, 0, 360}, 1.5, func(s *nativeEdgeSurface, model *edgePixels) bool {
		calls++
		if calls == 2 {
			return false
		}
		ok := s.create(model)
		temporaryDC = s.dc
		return ok
	}))
	require.Equal(t, area, r.edgeArea)
	require.Equal(t, float64(1), r.edgeScale)
	for i := range r.edges {
		require.Same(t, previous[i].model, r.edges[i].model)
		require.Equal(t, previous[i].dc, r.edges[i].dc)
		bitmap, _, _ := overlayGDI.NewProc("GetCurrentObject").Call(r.edges[i].dc, 7)
		require.Equal(t, r.edges[i].bitmap, bitmap)
	}
	bitmap, _, _ := overlayGDI.NewProc("GetCurrentObject").Call(temporaryDC, 7)
	require.Zero(t, bitmap)
}

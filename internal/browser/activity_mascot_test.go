package browser

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"

	"github.com/stretchr/testify/require"
)

func TestUnpremultiplyRestoresStraightAlpha(t *testing.T) {
	t.Parallel()
	pix := []byte{64, 32, 0, 128, 10, 20, 30, 255, 0, 0, 0, 0}
	unpremultiply(pix)
	require.Equal(t, []byte{128, 64, 0, 128, 10, 20, 30, 255, 0, 0, 0, 0}, pix)
}

func TestPageLookAimsAtVerifiedTarget(t *testing.T) {
	t.Parallel()
	view := [3]float64{2, 1000, 800}
	require.Equal(t, activity.Look{Y: -.25}, pageLook(activity.Event{}, view))
	look := pageLook(activity.Event{Point: true, PointerRevision: 1, PointerX: 1000, PointerY: 800}, view)
	require.InDelta(t, 1, look.X, 1e-9)
	require.Less(t, look.Y, -.9)
}

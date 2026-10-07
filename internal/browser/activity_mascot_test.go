package browser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnpremultiplyRestoresStraightAlpha(t *testing.T) {
	t.Parallel()
	pix := []byte{64, 32, 0, 128, 10, 20, 30, 255, 0, 0, 0, 0}
	unpremultiply(pix)
	require.Equal(t, []byte{128, 64, 0, 128, 10, 20, 30, 255, 0, 0, 0, 0}, pix)
}

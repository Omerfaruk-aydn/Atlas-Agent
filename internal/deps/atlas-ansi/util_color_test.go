package ansi

import (
	"image/color"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestXParseColorRejectsInvalidComponents(t *testing.T) {
	t.Parallel()
	require.Equal(t, color.RGBA{R: 0xff, G: 0x80, B: 1, A: 0xff}, XParseColor("rgb:ffff/8080/01"))
	for _, value := range []string{"rgb:10000/00/00", "rgb:zz/00/00", "rgb:/00/00", "rgba:00/00/00/10000"} {
		require.Nil(t, XParseColor(value), value)
	}
}

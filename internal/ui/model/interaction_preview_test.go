package model

import (
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/interaction"
	"github.com/stretchr/testify/require"
)

func TestInteractionPreviewTerminalBounds(t *testing.T) {
	t.Parallel()
	preview := &interaction.Preview{Width: 60, Height: 24, Pixels: make([]uint32, 60*24), Path: "capture.png"}
	for i := range preview.Pixels {
		preview.Pixels[i] = 0x12ABEF
	}
	for _, size := range [][2]int{{80, 24}, {40, 12}, {8, 2}, {1, 1}, {0, 0}} {
		text := renderInteractionPreview(preview, size[0], size[1])
		if size[0] == 0 {
			require.Empty(t, text)
			continue
		}
		lines := strings.Split(text, "\n")
		require.LessOrEqual(t, len(lines), size[1])
		for _, line := range lines {
			require.LessOrEqual(t, ansi.StringWidth(line), size[0])
		}
	}
	preview.Width = 10000
	require.Contains(t, renderInteractionPreview(preview, 80, 24), "Invalid preview")
}

package codemap

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlaceholdersBecomeComputedNames(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"direction-%d.png":        "direction-${x}.png",
		"frame_%03d.jpg":          "frame_${x}.jpg",
		"icons/{name}.svg":        "icons/${x}.svg",
		"tiles/{z:02}/x.png":      "tiles/${x}/x.png",
		"textures/${body.id}.png": "textures/${body.id}.png",
		"textures/${id}.png":      "textures/${id}.png",
		"plain/100%.png":          "plain/100%.png",
	} {
		require.Equal(t, want, placeholders(in), in)
	}
}

package design

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func svgOf(t *testing.T, d *Doc, ids ...string) string {
	t.Helper()
	out, err := SVG(d, d.Pages[0], ids)
	require.NoError(t, err)
	return out
}

func TestBooleanGroupsDrawAsMasks(t *testing.T) {
	t.Parallel()
	kids := `{"id":"a","type":"ellipse","x":0,"y":0,"width":60,"height":60},{"id":"b","type":"ellipse","x":30,"y":0,"width":60,"height":60}`
	for op, want := range map[string]string{"union": "<mask", "subtract": `fill="#000"`, "intersect": `mask="url(#`, "exclude": "difference"} {
		d := docFrom(t, `[{"id":"g","type":"group","bool_op":"`+op+`","fills":[{"type":"solid","color":"#FF0000"}],"children":[`+kids+`]}]`)
		out := svgOf(t, d, "g")
		require.Contains(t, out, want, op)
		require.Contains(t, out, `fill="#FF0000"`, op)
	}
	// A plain group has no masks.
	d := docFrom(t, `[{"id":"g","type":"group","children":[`+kids+`]}]`)
	require.NotContains(t, svgOf(t, d, "g"), "<mask")
	// Only groups combine; the field is dropped elsewhere.
	d = docFrom(t, `[{"id":"r","type":"rect","bool_op":"union","width":5,"height":5}]`)
	require.Empty(t, d.Find("r").Node.BoolOp)
}

func TestMaskClipsSiblingsAbove(t *testing.T) {
	t.Parallel()
	d := docFrom(t, `[{"id":"f","type":"frame","width":200,"height":200,"children":[
		{"id":"low","type":"rect","width":200,"height":200},
		{"id":"m","type":"ellipse","mask":true,"x":50,"y":50,"width":80,"height":80},
		{"id":"top","type":"rect","width":200,"height":200,"fills":[{"type":"solid","color":"#00FF00"}]}]}]`)
	out := svgOf(t, d, "f")
	require.Contains(t, out, "<clipPath")
	// The mask is not drawn; the node below it is, outside the clip.
	require.Equal(t, 1, strings.Count(out, "<ellipse"), "only the clip path holds the ellipse")
}

func TestImagePaintFillsAShape(t *testing.T) {
	t.Parallel()
	d := docFrom(t, `[{"id":"r","type":"rect","width":100,"height":50,"fills":[{"type":"image","src":"data:image/png;base64,AAAA","fit":"contain"},{"type":"image"}]}]`)
	require.Len(t, d.Find("r").Node.Fills, 1, "an image paint without a source is dropped")
	out := svgOf(t, d, "r")
	require.Contains(t, out, "<pattern")
	require.Contains(t, out, `preserveAspectRatio="xMidYMid meet"`)
}

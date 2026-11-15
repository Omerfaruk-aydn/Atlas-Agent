package design

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSectionsSlicesGuidesAndCropSurviveParse(t *testing.T) {
	t.Parallel()
	raw := `{"format":"atlas-design","version":1,"name":"x","pages":[{"id":"p","name":"P","background":"#FFFFFF",
	"guides":[{"axis":"x","at":120},{"axis":"q","at":5},{"axis":"y","at":-40}],
	"children":[
	 {"id":"s","type":"section","name":"Flow","x":0,"y":0,"width":600,"height":400,"children":[{"id":"f","type":"frame","name":"A","x":10,"y":10,"width":100,"height":100}]},
	 {"id":"sl","type":"slice","name":"Hero","x":0,"y":0,"width":50,"height":50,"fills":[{"type":"solid","color":"#FF0000"}],"children":[{"id":"z","type":"rect","name":"no","width":1,"height":1}]},
	 {"id":"i","type":"image","name":"Pic","x":0,"y":0,"width":100,"height":50,"src":"a.png","crop":{"zoom":2,"x":0.2,"y":-9}},
	 {"id":"r","type":"rect","name":"R","width":80,"height":80,"crop":{"zoom":3},"fills":[{"type":"angular","angle":30,"stops":[{"offset":0,"color":"#FF0000"},{"offset":1,"color":"#0000FF"}]}]}
	]}]}`
	doc, err := Parse([]byte(raw))
	require.NoError(t, err)
	page := doc.Pages[0]
	require.Equal(t, []Guide{{Axis: "x", At: 120}, {Axis: "y", At: -40}}, page.Guides)

	section := page.Children[0]
	require.Equal(t, TypeSection, section.Type)
	require.Len(t, section.Children, 1)
	require.False(t, *section.Clip, "a section does not clip")

	slice := page.Children[1]
	require.Empty(t, slice.Children, "a slice holds nothing")

	image := page.Children[2]
	require.NotNil(t, image.Crop)
	require.InDelta(t, 2, image.Crop.Zoom, 0)
	require.InDelta(t, 5.0, image.Crop.Y+5, 20, "offsets are clamped")
	require.Nil(t, page.Children[3].Crop, "only images crop")

	svg, err := SVG(doc, page, nil)
	require.NoError(t, err)
	require.Contains(t, svg, "<pattern", "an angular gradient is drawn as wedges")
	require.GreaterOrEqual(t, strings.Count(svg, "<path d=\"M"), angularWedges)
	require.NotContains(t, svg, `fill="#FF0000"/>`+"</g>"+"<g", "the slice is not drawn")
}

func TestStopColorAtBlendsBetweenStops(t *testing.T) {
	t.Parallel()
	rgb, alpha := stopColorAt([]Stop{{Offset: 0, Color: "#000000"}, {Offset: 1, Color: "#FFFFFF"}}, 0.5)
	require.Equal(t, "#808080", rgb)
	require.InDelta(t, 1, alpha, 0)
}

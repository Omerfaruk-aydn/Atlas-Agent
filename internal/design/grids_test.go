package design

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLayoutGridsSurviveParseOnFramesOnly(t *testing.T) {
	t.Parallel()
	raw := `{"format":"atlas-design","version":1,"name":"g","pages":[{"id":"p","name":"P","children":[
	 {"id":"f","type":"frame","name":"F","x":0,"y":0,"width":390,"height":844,"grids":[
	  {"pattern":"columns","count":4,"gutter":16,"margin":16,"color":"#FF00001A"},
	  {"pattern":"grid","size":0,"count":9},
	  {"pattern":"bogus","count":-3,"align":"sideways","color":"red"}]},
	 {"id":"r","type":"rect","name":"R","x":0,"y":0,"width":10,"height":10,"grids":[{"pattern":"rows"}]}]}]}`
	doc, err := Parse([]byte(raw))
	require.NoError(t, err)
	f := doc.Pages[0].Children[0]
	require.Len(t, f.Grids, 3)
	require.Equal(t, LayoutGrid{Pattern: "columns", Count: 4, Gutter: 16, Margin: 16, Align: "stretch", Color: "#FF00001A"}, f.Grids[0])
	require.Equal(t, LayoutGrid{Pattern: "grid", Size: 8}, f.Grids[1], "a square grid keeps only its cell")
	require.Equal(t, LayoutGrid{Pattern: "columns", Count: 5, Align: "stretch"}, f.Grids[2], "bad values fall back")
	require.Nil(t, doc.Pages[0].Children[1].Grids, "only frames and components hold grids")
}

package design

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func docFrom(t *testing.T, js string) *Doc {
	t.Helper()
	d, err := Parse([]byte(`{"pages":[{"id":"p","children":` + js + `}]}`))
	require.NoError(t, err)
	return d
}

func applyOps(t *testing.T, d *Doc, js string) *Doc {
	t.Helper()
	var ops []Op
	require.NoError(t, json.Unmarshal([]byte(js), &ops))
	out, _, err := Apply(d, "", ops)
	require.NoError(t, err)
	return out
}

func TestAutoLayoutColumnRowAndFill(t *testing.T) {
	t.Parallel()
	d := docFrom(t, `[{"id":"f","type":"frame","width":400,"height":300,"layout":{"dir":"column","gap":10,"pad":20},"children":[
		{"id":"a","type":"rect","width":50,"height":30},{"id":"b","type":"rect","width":80,"height":40}]}]`)
	ResolveLayout(d, nil)
	require.Equal(t, [2]float64{20, 20}, [2]float64{d.Find("a").Node.X, d.Find("a").Node.Y})
	require.Equal(t, [2]float64{20, 60}, [2]float64{d.Find("b").Node.X, d.Find("b").Node.Y})

	d = docFrom(t, `[{"id":"f","type":"frame","width":400,"height":300,"layout":{"dir":"row","gap":10},"children":[
		{"id":"a","type":"rect","width":100,"height":30},
		{"id":"b","type":"rect","width":10,"height":30,"sizing_x":"fill"},
		{"id":"c","type":"rect","width":10,"height":30,"sizing_x":"fill"}]}]`)
	ResolveLayout(d, nil)
	require.InDelta(t, 140, d.Find("b").Node.Width, 1e-9)
	require.InDelta(t, 260, d.Find("c").Node.X, 1e-9)
}

func TestAutoLayoutHugCenterBetween(t *testing.T) {
	t.Parallel()
	d := docFrom(t, `[{"id":"f","type":"frame","width":1,"height":1,"sizing_x":"hug","sizing_y":"hug","layout":{"dir":"row","gap":8,"pad":[4,6]},"children":[
		{"id":"a","type":"rect","width":50,"height":30},{"id":"b","type":"rect","width":70,"height":20}]}]`)
	ResolveLayout(d, nil)
	require.InDelta(t, 140, d.Find("f").Node.Width, 1e-9)
	require.InDelta(t, 38, d.Find("f").Node.Height, 1e-9)

	d = docFrom(t, `[{"id":"f","type":"frame","width":400,"height":300,"layout":{"dir":"row","main":"between","pad":[0,10,0,10]},"children":[
		{"id":"a","type":"rect","width":100,"height":50},{"id":"b","type":"rect","width":100,"height":50},{"id":"c","type":"rect","width":100,"height":50}]}]`)
	ResolveLayout(d, nil)
	require.InDelta(t, 10, d.Find("a").Node.X, 1e-9)
	require.InDelta(t, 150, d.Find("b").Node.X, 1e-9)
	require.InDelta(t, 290, d.Find("c").Node.X, 1e-9)
}

func TestConstraintsFollowAResize(t *testing.T) {
	t.Parallel()
	before := docFrom(t, `[{"id":"f","type":"frame","width":200,"height":100,"children":[
		{"id":"r","type":"rect","x":170,"y":5,"width":20,"height":20,"constraints":{"x":"right"}},
		{"id":"s","type":"rect","x":10,"width":100,"height":20,"constraints":{"x":"stretch"}},
		{"id":"k","type":"rect","x":100,"y":10,"width":50,"height":50,"constraints":{"x":"scale","y":"scale"}},
		{"id":"n","type":"rect","x":170,"width":20,"height":20}]}]`)
	after := applyOps(t, before, `[{"op":"update","id":"f","props":{"width":400,"height":200}}]`)
	require.InDelta(t, 370, after.Find("r").Node.X, 1e-9)
	require.InDelta(t, 300, after.Find("s").Node.Width, 1e-9)
	require.InDelta(t, 200, after.Find("k").Node.X, 1e-9)
	require.InDelta(t, 100, after.Find("k").Node.Height, 1e-9)
	require.InDelta(t, 170, after.Find("n").Node.X, 1e-9)
}

func TestApplyKeepsLayoutAfterEdits(t *testing.T) {
	t.Parallel()
	d := docFrom(t, `[{"id":"f","type":"frame","width":300,"height":200,"layout":{"dir":"column","gap":4}}]`)
	out := applyOps(t, d, `[{"op":"add","parent":"f","node":{"id":"a","type":"rect","width":10,"height":10}},{"op":"add","parent":"f","node":{"id":"b","type":"rect","width":10,"height":10}}]`)
	require.InDelta(t, 14, out.Find("b").Node.Y, 1e-9)
	var pad Pad
	require.NoError(t, json.Unmarshal([]byte(`[3,5]`), &pad))
	require.Equal(t, Pad{3, 5, 3, 5}, pad)
	require.Error(t, json.Unmarshal([]byte(`[1,2,3]`), &pad))
}

func mustOps(t *testing.T, js string) []Op {
	t.Helper()
	var ops []Op
	require.NoError(t, json.Unmarshal([]byte(js), &ops))
	return ops
}

func TestInteractionsAreSanitized(t *testing.T) {
	t.Parallel()
	d := docFrom(t, `[{"id":"a","type":"rect","scroll":"both","flow_start":true,"interactions":[
		{"trigger":"click","action":"navigate"},
		{"trigger":"wave","action":"back"},
		{"trigger":"click","action":"url","url":"ftp://x"},
		{"trigger":"click","action":"url","url":"https://example.com","duration":99999},
		{"trigger":"key","key":"Enter","action":"back","easing":"bounce"}]},
		{"id":"f","type":"frame","scroll":"vertical","flow_start":true,"width":10,"height":10}]`)
	a := d.Find("a").Node
	require.Len(t, a.Interactions, 2)
	require.Equal(t, "https://example.com", a.Interactions[0].URL)
	require.InDelta(t, 10000, *a.Interactions[0].Duration, 1e-9)
	require.Equal(t, "Enter", a.Interactions[1].Key)
	require.Empty(t, a.Interactions[1].Easing)
	require.Empty(t, a.Scroll)
	require.False(t, a.FlowStart)
	f := d.Find("f").Node
	require.Equal(t, "vertical", f.Scroll)
	require.True(t, f.FlowStart)
	// Prototype fields survive an update op.
	out := applyOps(t, d, `[{"op":"update","id":"f","props":{"interactions":[{"trigger":"click","action":"navigate","to":"a"}]}}]`)
	require.Len(t, out.Find("f").Node.Interactions, 1)
}

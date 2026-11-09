package design

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const cardDoc = `[
 {"id":"card","type":"component","set":"Button","variant":{"state":"default"},"x":0,"y":0,"width":100,"height":40,"children":[
   {"id":"bg","type":"rect","width":100,"height":40,"fills":[{"type":"solid","color":"#111111"}]},
   {"id":"label","type":"text","text":"Go","font":{"family":"Inter","size":16,"weight":500},"fills":[{"type":"solid","color":"#FFFFFF"}]}]},
 {"id":"card-hover","type":"component","set":"Button","variant":{"state":"hover"},"x":200,"y":0,"width":100,"height":40,"children":[
   {"id":"bg2","type":"rect","width":100,"height":40,"fills":[{"type":"solid","color":"#2222FF"}]}]},
 {"id":"a","type":"instance","component_id":"card","x":0,"y":100,"width":100,"height":40},
 {"id":"b","type":"instance","component_id":"card","variant":{"state":"hover"},"x":0,"y":200,"width":100,"height":40},
 {"id":"c","type":"instance","component_id":"card","x":0,"y":300,"width":100,"height":40,"overrides":{"label":{"text":"Buy","fills":[{"type":"solid","color":"#FF0000"}]},"bg":{"visible":false}}}]`

func TestResolvedVariantsAndOverrides(t *testing.T) {
	t.Parallel()
	d := docFrom(t, cardDoc)
	r := Resolved(d)
	a := r.Find("a").Node
	require.Len(t, a.Children, 2)
	b := r.Find("b").Node
	require.Len(t, b.Children, 1, "the hover variant has its own layers")
	require.Equal(t, "#2222FF", b.Children[0].Fills[0].Color)
	c := r.Find("c").Node
	require.Equal(t, "Buy", c.Children[1].TextValue())
	require.Equal(t, "#FF0000", c.Children[1].Fills[0].Color)
	require.False(t, c.Children[0].IsVisible())
	// The source document is untouched.
	require.Empty(t, d.Find("a").Node.Children)
	require.Equal(t, "Go", d.Find("label").Node.TextValue())
}

func TestStylesAndVariablesThroughOps(t *testing.T) {
	t.Parallel()
	d := docFrom(t, `[{"id":"box","type":"rect","width":50,"height":50},{"id":"t","type":"text","text":"Hi"}]`)
	out, res, err := Apply(d, "", mustOps(t, `[
	 {"op":"set_modes","modes":["Light","Dark"]},
	 {"op":"set_variable","id":"brand","name":"Brand","props":{"values":{"Light":"#0055FF","Dark":"#88AAFF"}}},
	 {"op":"set_style","kind":"fill","id":"primary","name":"Primary","props":{"fills":[{"type":"solid","color":"#000000","variable":"brand"}]}},
	 {"op":"set_style","kind":"text","id":"h1","name":"Heading","props":{"font":{"size":32,"weight":700},"line_height":40}},
	 {"op":"update","id":"box","props":{"fill_style":"primary"}},
	 {"op":"update","id":"t","props":{"text_style":"h1"}}]`))
	require.NoError(t, err)
	require.Empty(t, res.Created)
	require.Equal(t, "Light", out.Variables.Mode)
	r := Resolved(out)
	require.Equal(t, "#0055FF", r.Find("box").Node.Fills[0].Color)
	require.InDelta(t, 32, r.Find("t").Node.Font.Size, 1e-9)
	require.Equal(t, "Inter", r.Find("t").Node.Font.Family)

	dark, _, err := Apply(out, "", mustOps(t, `[{"op":"set_modes","mode":"Dark"}]`))
	require.NoError(t, err)
	require.Equal(t, "#88AAFF", Resolved(dark).Find("box").Node.Fills[0].Color)

	svg, err := SVG(dark, dark.Pages[0], []string{"box"})
	require.NoError(t, err)
	require.Contains(t, strings.ToUpper(svg), "#88AAFF")
}

func TestTokenOpsRejectBadInput(t *testing.T) {
	t.Parallel()
	d := docFrom(t, `[]`)
	for _, js := range []string{
		`[{"op":"set_style","kind":"blob","id":"x"}]`,
		`[{"op":"set_style","kind":"fill","id":"x","props":{"fills":[]}}]`,
		`[{"op":"set_variable","id":"v","props":{"values":{"Nope":"#000000"}}}]`,
		`[{"op":"set_variable","id":"v","props":{"values":{"Default":"red"}}}]`,
		`[{"op":"delete_style","kind":"fill","id":"missing"}]`,
		`[{"op":"set_modes","mode":"Ghost"}]`,
	} {
		_, _, err := Apply(d, "", mustOps(t, js))
		require.Error(t, err, js)
	}
	// A new style or variable without an id gets one.
	_, res, err := Apply(d, "", mustOps(t, `[{"op":"set_variable","name":"Ink","props":{"values":{}}}]`))
	require.NoError(t, err)
	require.Len(t, res.Created, 1)
}

func TestSanitizeDropsBrokenTokens(t *testing.T) {
	t.Parallel()
	d, err := Parse([]byte(`{"pages":[{"id":"p","children":[{"id":"x","type":"rect","fill_style":"bad id","overrides":{"y":{}}}]}],
	 "styles":{"fills":{"ok":{"name":"Ok","fills":[{"type":"solid","color":"#123456"}]},"bad":{"fills":[]}}},
	 "variables":{"modes":["A","A",""],"mode":"Z","items":{"v":{"values":{"A":"nope"}}}}}`))
	require.NoError(t, err)
	require.Empty(t, d.Find("x").Node.FillStyle)
	require.Nil(t, d.Find("x").Node.Overrides)
	require.Len(t, d.Styles.Fills, 1)
	require.Equal(t, []string{"A"}, d.Variables.Modes)
	require.Equal(t, "A", d.Variables.Mode)
	require.Equal(t, "#000000", d.Variables.Items["v"].Values["A"])
}

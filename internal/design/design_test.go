package design

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func sample(t *testing.T) *Doc {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "app", "test", "fixtures", "design-sample.json"))
	require.NoError(t, err)
	doc, err := Parse(data)
	require.NoError(t, err)
	return doc
}

func TestParseReadsTheAppFormat(t *testing.T) {
	t.Parallel()
	doc := sample(t)
	require.Len(t, doc.Pages, 1)
	// The unknown "sparkle" node is dropped; everything else stays.
	require.Equal(t, 7, doc.Count())
	title := doc.Find("title").Node
	require.Equal(t, "Build faster", title.TextValue())
	require.Equal(t, 700, title.Font.Weight)
	require.InDelta(t, 76.8, *title.LineHeight, 1e-9)
	require.InDelta(t, -1, *title.LetterSpacing, 1e-9)
	cta := doc.Find("cta").Node
	require.True(t, cta.Radius.Each)
	require.Equal(t, "linear", cta.Fills[0].Type)
	require.Equal(t, "#00000033", cta.Strokes[0].Color)
	require.Equal(t, [2]float64{24, 24}, *doc.Find("logo").Node.ViewBox)
	require.Equal(t, "card", doc.Find("card-1").Node.ComponentID)

	// Written back, the field names are the app's.
	data, err := Marshal(doc)
	require.NoError(t, err)
	for _, key := range []string{`"line_height"`, `"letter_spacing"`, `"auto_resize"`, `"view_box"`, `"component_id"`, `"radius": [`, `"radius": 16`} {
		require.Contains(t, string(data), key)
	}
	again, err := Parse(data)
	require.NoError(t, err)
	second, err := Marshal(again)
	require.NoError(t, err)
	require.Equal(t, string(data), string(second), "sanitizing is idempotent")
}

func TestSanitizeRepairs(t *testing.T) {
	t.Parallel()
	doc, err := Parse([]byte(`{"pages":[{"id":"p","children":[
		{"id":"a","type":"rect","width":-5,"rotation":450,"fills":[{"type":"solid","color":"red"},{"color":"#abc"}]},
		{"id":"a","type":"text","text":"x","font":{"size":0,"weight":950}},
		{"id":"bad id!","type":"group","children":[{"type":"ellipse"}]}]}]}`))
	require.NoError(t, err)
	kids := doc.Pages[0].Children
	require.Len(t, kids, 3)
	require.Zero(t, kids[0].Width)
	require.InDelta(t, 90, kids[0].Rotation, 1e-9)
	require.Equal(t, []Paint{{Type: "solid", Color: "#000000"}, {Type: "solid", Color: "#ABC"}}, kids[0].Fills)
	require.NotEqual(t, "a", kids[1].ID, "a repeated id is replaced")
	require.Equal(t, 900, kids[1].Font.Weight)
	require.InDelta(t, 16, kids[1].Font.Size, 1e-9)
	require.Equal(t, "width", kids[1].AutoResize)
	require.True(t, ValidID(kids[2].ID))
	require.Nil(t, kids[2].Clip, "groups never clip")

	_, err = Parse([]byte(`{"format":"other"}`))
	require.Error(t, err)
}

func TestApplyBuildsAndEdits(t *testing.T) {
	t.Parallel()
	doc := New("Landing")
	ops := []Op{
		{Op: "add", Node: json.RawMessage(`{"id":"hero","type":"frame","name":"Hero","width":1440,"height":800,"children":[
			{"id":"h1","type":"text","x":120,"y":200,"text":"Hello","font":{"size":48,"weight":700}},
			{"id":"btn","type":"rect","x":120,"y":320,"width":160,"height":48,"radius":12}]}`)},
		{Op: "update", ID: "btn", Props: map[string]json.RawMessage{"fills": json.RawMessage(`[{"type":"solid","color":"#5b5bf7"}]`)}},
		{Op: "update", ID: "h1", Props: map[string]json.RawMessage{"font": json.RawMessage(`{"family":"Georgia"}`)}},
		{Op: "add", Parent: "hero", Index: new(int), Node: json.RawMessage(`{"id":"bg","type":"rect","width":1440,"height":800,"fills":[]}`)},
		{Op: "add_page", Name: "Mobile"},
	}
	next, res, err := Apply(doc, "", ops)
	require.NoError(t, err)
	require.Equal(t, "hero", res.Created[0])
	require.Len(t, next.Pages, 2)
	require.Empty(t, doc.Pages[0].Children, "the input document is left alone")

	hero := next.Find("hero").Node
	require.Equal(t, "#FFFFFF", hero.Fills[0].Color, "a frame defaults to white")
	require.Equal(t, "bg", hero.Children[0].ID, "index 0 is the back")
	require.Empty(t, next.Find("bg").Node.Fills)
	btn := next.Find("btn").Node
	require.Equal(t, "#5B5BF7", btn.Fills[0].Color)
	h1 := next.Find("h1").Node
	require.Equal(t, "Georgia", h1.Font.Family)
	require.Equal(t, 700, h1.Font.Weight, "font merges")
	require.InDelta(t, 5*48*glyphWidth, h1.Width, 1)
	require.InDelta(t, 57.6, h1.Height, 1)

	_, _, err = Apply(next, "", []Op{{Op: "add", Node: json.RawMessage(`{"id":"btn","type":"rect"}`)}})
	require.ErrorContains(t, err, "already used")
	_, _, err = Apply(next, "", []Op{{Op: "update", ID: "btn", Props: map[string]json.RawMessage{"type": json.RawMessage(`"text"`)}}})
	require.ErrorContains(t, err, "cannot be updated")
	_, _, err = Apply(next, "", []Op{{Op: "add", Parent: "btn", Node: json.RawMessage(`{"type":"rect"}`)}})
	require.ErrorContains(t, err, "only frames, groups and components")

	// A failing op leaves nothing of the batch behind.
	_, _, err = Apply(next, "", []Op{{Op: "delete", ID: "btn"}, {Op: "delete", ID: "nope"}})
	require.Error(t, err)
	require.NotNil(t, next.Find("btn"))
}

func TestMoveKeepsPagePosition(t *testing.T) {
	t.Parallel()
	doc, _, err := Apply(New(""), "", []Op{
		{Op: "add", Node: json.RawMessage(`{"id":"f","type":"frame","x":100,"y":50,"width":400,"height":400}`)},
		{Op: "add", Node: json.RawMessage(`{"id":"r","type":"rect","x":150,"y":80,"width":10,"height":10}`)},
		{Op: "move", ID: "r", Parent: "f"},
	})
	require.NoError(t, err)
	r := doc.Find("r")
	require.Equal(t, "f", r.Parent.ID)
	require.InDelta(t, 50, r.Node.X, 1e-9)
	require.InDelta(t, 30, r.Node.Y, 1e-9)
	_, _, err = Apply(doc, "", []Op{{Op: "move", ID: "f", Parent: "r"}})
	require.Error(t, err)
}

func TestGroupsFitTheirChildren(t *testing.T) {
	t.Parallel()
	doc, _, err := Apply(New(""), "", []Op{
		{Op: "add", Node: json.RawMessage(`{"id":"g","type":"group","x":10,"y":10,"children":[
			{"type":"rect","x":20,"y":30,"width":10,"height":10},{"type":"rect","x":50,"y":40,"width":10,"height":20}]}`)},
	})
	require.NoError(t, err)
	g := doc.Find("g").Node
	require.InDelta(t, 30, g.X, 1e-9)
	require.InDelta(t, 40, g.Y, 1e-9)
	require.InDelta(t, 40, g.Width, 1e-9)
	require.InDelta(t, 30, g.Height, 1e-9)
	require.InDelta(t, 0, g.Children[0].X, 1e-9)
}

func TestComponentsAndInstances(t *testing.T) {
	t.Parallel()
	doc, res, err := Apply(New(""), "", []Op{
		{Op: "add", Node: json.RawMessage(`{"id":"card","type":"frame","width":200,"height":100,"children":[{"type":"ellipse","width":20,"height":20}]}`)},
		{Op: "make_component", ID: "card"},
		{Op: "add", Node: json.RawMessage(`{"type":"instance","component_id":"card","x":300}`)},
	})
	require.NoError(t, err)
	inst := doc.Find(res.Created[2]).Node
	require.Equal(t, TypeInstance, inst.Type)
	require.InDelta(t, 200, inst.Width, 1e-9, "an instance takes its component's size")
	_, _, err = Apply(doc, "", []Op{{Op: "add", Node: json.RawMessage(`{"type":"instance","component_id":"nope"}`)}})
	require.ErrorContains(t, err, "component_id")
}

func TestTextOverflowNote(t *testing.T) {
	t.Parallel()
	_, res, err := Apply(New(""), "", []Op{{Op: "add", Node: json.RawMessage(`{"type":"text","auto_resize":"none","width":40,"height":20,"text":"a long sentence that wraps"}`)}})
	require.NoError(t, err)
	require.Len(t, res.Notes, 1)
	require.Contains(t, res.Notes[0], "auto_resize")
}

func TestStoreSaveConflictAndList(t *testing.T) {
	t.Parallel()
	store := NewStore(t.TempDir())
	files, err := store.List()
	require.NoError(t, err)
	require.Empty(t, files)

	rev, err := store.Save("home", New("Home"), "", true)
	require.NoError(t, err)
	_, err = store.Save("home", New("Home"), "", true)
	require.ErrorContains(t, err, "already exists")

	doc, got, err := store.Load("home")
	require.NoError(t, err)
	require.Equal(t, rev, got)
	require.Equal(t, "Home", doc.Name)

	doc.Name = "Home 2"
	rev2, err := store.Save("home", doc, rev, false)
	require.NoError(t, err)
	require.NotEqual(t, rev, rev2, "every write changes the revision")
	_, err = store.Save("home", doc, rev, false)
	require.True(t, errors.Is(err, ErrConflict))

	files, err = store.List()
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, "home", files[0].Name)

	_, err = store.Path("../evil")
	require.Error(t, err)
	require.False(t, ValidName("Upper"))
	require.Equal(t, "acilis-sayfasi", Slug("Açılış Sayfası"))
}

func TestOutlineAndSVG(t *testing.T) {
	t.Parallel()
	doc := sample(t)
	out := Outline(doc, 0)
	require.Contains(t, out, `frame "Desktop" id=desk x=0 y=0 w=1440 h=900 fill=#FFFFFF`)
	require.Contains(t, out, `text "Title" id=title`)
	require.Contains(t, out, `text="Build faster"`)
	require.Contains(t, out, `instance "Card" id=card-1`)
	require.Contains(t, Outline(doc, 2), "outline cut at 2 lines")

	svg, err := SVG(doc, doc.Pages[0], nil)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(svg, "<?xml"))
	for _, want := range []string{`viewBox="0 0 1920 900"`, `fill="#F5F5F5"`, `>Build faster</tspan>`, `<linearGradient`, `<filter`, `<clipPath`, `stroke="#000000" stroke-width="1" stroke-opacity="0.2"`, `scale(1 1)`} {
		if want == `scale(1 1)` {
			require.NotContains(t, svg, want)
			continue
		}
		require.Contains(t, svg, want)
	}
	// The path is scaled from its 24 box to 48.
	require.Contains(t, svg, `d="M 0 0 L 48 0 L 48 48 Z"`)

	one, err := SVG(doc, doc.Pages[0], []string{"cta"})
	require.NoError(t, err)
	require.Contains(t, one, `viewBox="0 0 160 48"`)
	_, err = SVG(doc, doc.Pages[0], []string{"nope"})
	require.Error(t, err)
}

func TestScalePath(t *testing.T) {
	t.Parallel()
	require.Equal(t, "M 0 0 h 20 v 30 A 2 3 0 0 1 4 6 z", ScalePath("M0 0h10v10A1 1 0 0 1 2 2z", 2, 3))
	require.Equal(t, "M1 1", ScalePath("M1 1", 1, 1))
}

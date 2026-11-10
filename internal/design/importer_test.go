package design

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const importSample = `<!doctype html><html><head><title>Landing</title>
<style>
:root { --brand: #6366f1; }
.card { display: flex; flex-direction: column; gap: 12px; padding: 24px; background: #fff; border: 1px solid #e5e7eb; border-radius: 12px; box-shadow: 0 4px 12px rgba(0,0,0,.1); }
.row { display: flex; justify-content: space-between; align-items: center; }
.btn { background: var(--brand); color: white; padding: 10px 20px; border-radius: 8px; }
h1 { font-size: 40px; }
</style></head><body>
<div class="card" style="width: 400px">
  <h1>Hello</h1>
  <p style="color: #666; line-height: 1.5">Some <b>body</b> text here.</p>
  <div class="row"><span>Price</span><button class="btn">Buy</button></div>
  <div style="display:none">hidden</div>
</div></body></html>`

func findNamed(n *Node, name string) *Node {
	if n.Name == name {
		return n
	}
	for _, c := range n.Children {
		if f := findNamed(c, name); f != nil {
			return f
		}
	}
	return nil
}

func TestImportHTMLBuildsLayout(t *testing.T) {
	t.Parallel()
	root, err := ImportHTML(importSample, ImportOptions{Width: 800})
	require.NoError(t, err)
	require.Equal(t, TypeFrame, root.Type)
	require.Equal(t, "Landing", root.Name)
	require.InDelta(t, 800, root.Width, 1e-9)

	card := findNamed(root, "div.card")
	require.NotNil(t, card)
	require.InDelta(t, 400, card.Width, 1e-9)
	require.Equal(t, "column", card.Layout.Dir)
	require.InDelta(t, 12, card.Layout.Gap, 1e-9)
	require.Equal(t, Pad{24, 24, 24, 24}, card.Layout.Pad)
	require.Equal(t, "#FFFFFF", card.Fills[0].Color)
	require.Equal(t, "#E5E7EB", card.Strokes[0].Color)
	require.InDelta(t, 12, card.Radius.Corners[0], 1e-9)
	require.Len(t, card.Effects, 1)
	require.InDelta(t, 4, *card.Effects[0].Y, 1e-9)
	require.Len(t, card.Children, 3, "the hidden element is dropped")

	h1 := findNamed(root, "h1")
	require.Equal(t, "Hello", *h1.Text)
	require.InDelta(t, 40, h1.Font.Size, 1e-9)
	require.Equal(t, 700, h1.Font.Weight)

	p := findNamed(root, "p")
	require.Equal(t, "Some body text here.", *p.Text)
	require.InDelta(t, 24, *p.LineHeight, 1e-9)
	require.Equal(t, "#666666", p.Fills[0].Color)

	row := findNamed(root, "div.row")
	require.Equal(t, "row", row.Layout.Dir)
	require.Equal(t, "between", row.Layout.Main)
	require.Equal(t, "center", row.Layout.Cross)
	btn := findNamed(root, "button.btn")
	require.Equal(t, "#6366F1", btn.Fills[0].Color, "custom properties resolve")
	require.Equal(t, "hug", btn.SizingX, "row children hug")
	require.Equal(t, "#FFFFFF", btn.Children[0].Fills[0].Color)
}

func TestImportedTreeAddsToADocument(t *testing.T) {
	t.Parallel()
	root, err := ImportHTML(importSample, ImportOptions{})
	require.NoError(t, err)
	raw, err := Marshal(&Doc{Format: Format, Version: Version, Pages: []*Page{{ID: "p", Name: "P", Children: []*Node{root}}}})
	require.NoError(t, err)
	doc, err := Parse(raw)
	require.NoError(t, err)
	require.NotEmpty(t, doc.Pages[0].Children[0].Children, "the sanitizer keeps the imported tree")
}

func TestImportHTMLGradientsAndLimits(t *testing.T) {
	t.Parallel()
	root, err := ImportHTML(`<body><section style="background: linear-gradient(90deg, #f00 0%, #00f 100%); height: 80px"></section></body>`, ImportOptions{})
	require.NoError(t, err)
	g := findNamed(root, "section")
	require.NotNil(t, g)
	require.Equal(t, "linear", g.Fills[0].Type)
	require.InDelta(t, 90, *g.Fills[0].Angle, 1e-9)
	require.Len(t, g.Fills[0].Stops, 2)

	_, err = ImportHTML(string(make([]byte, 3<<20)), ImportOptions{})
	require.Error(t, err)
	empty, err := ImportHTML(`<body></body>`, ImportOptions{})
	require.NoError(t, err)
	require.Equal(t, TypeFrame, empty.Type)
}

package design

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestThemesAreComplete(t *testing.T) {
	t.Parallel()
	themes := Themes()
	require.GreaterOrEqual(t, len(themes), 11)
	for _, th := range themes {
		full, err := LookupTheme(th.ID, "")
		require.NoError(t, err, th.ID)
		for _, token := range ColorTokens {
			pair, ok := full.Colors[token]
			require.True(t, ok, "%s lacks colour %s", th.ID, token)
			require.True(t, IsColor(pair[0]) && IsColor(pair[1]), "%s %s: %v", th.ID, token, pair)
		}
		for _, slot := range TypeSlots {
			tok, ok := full.Type[slot]
			require.True(t, ok, "%s lacks type %s", th.ID, slot)
			require.Positive(t, tok.Size)
			require.NotEmpty(t, tok.Family)
		}
		// Body text reads on the page and on cards in both modes.
		for mode := range 2 {
			if th.DarkOnly && mode == 0 {
				continue
			}
			for _, bg := range []string{"bg", "card"} {
				ratio := Contrast(full.Colors["text"][mode], full.Colors[bg][mode])
				require.GreaterOrEqual(t, ratio, 4.5, "%s text on %s in mode %d is %.2f:1", th.ID, bg, mode, ratio)
			}
		}
	}
}

func TestAccentSwitchesToARadixHue(t *testing.T) {
	t.Parallel()
	th, err := LookupTheme("zinc", "crimson")
	require.NoError(t, err)
	require.NotEqual(t, "#18181B", th.Colors["accent"][0])
	require.Equal(t, "#FFFFFF", th.Colors["on-accent"][0])
	amber, err := LookupTheme("zinc", "amber")
	require.NoError(t, err)
	require.NotEqual(t, "#FFFFFF", amber.Colors["on-accent"][0], "bright accents take dark text")
	_, err = LookupTheme("zinc", "slate")
	require.Error(t, err)
	_, err = LookupTheme("nope", "")
	require.ErrorContains(t, err, "zinc")
}

func TestEveryBlockBuildsInEveryTheme(t *testing.T) {
	t.Parallel()
	for _, th := range Themes() {
		for _, info := range Blocks("") {
			theme, err := LookupTheme(th.ID, "")
			require.NoError(t, err)
			for mode := range 2 {
				n, err := BuildBlock(info.ID, theme, mode, nil, "", map[string]bool{})
				require.NoError(t, err, "%s in %s", info.ID, th.ID)
				doc := New("kit")
				doc.Pages[0].Children = []*Node{n}
				ResolveLayout(doc, nil)
				require.Positive(t, n.Width, info.ID)
				require.Positive(t, n.Height, info.ID)
				Walk([]*Node{n}, func(c *Node, _ *Node, _ int) bool {
					require.True(t, ValidID(c.ID), c.ID)
					for _, f := range c.Fills {
						if f.Type == "solid" && f.Variable == "" {
							require.Equal(t, "#FFFFFF", f.Color, "%s/%s: kit colours come from the theme", info.ID, c.Name)
						}
					}
					return true
				})
			}
		}
	}
}

func TestBlocksListTheirSlots(t *testing.T) {
	t.Parallel()
	all := Blocks("")
	require.GreaterOrEqual(t, len(all), 45)
	platforms := map[string]int{}
	for _, b := range all {
		platforms[b.Platform]++
		require.NotEmpty(t, b.Title)
		require.NotEmpty(t, b.Description)
	}
	require.GreaterOrEqual(t, platforms["web"], 20)
	require.GreaterOrEqual(t, platforms["ios"], 10)
	require.GreaterOrEqual(t, platforms["android"], 10)
	hero := Blocks("web")[1]
	require.Equal(t, "web/hero", hero.ID)
	require.Contains(t, hero.Slots, "title")
	require.InDelta(t, webWidth, hero.Width, 0.5)
}

func TestInsertSetsAThemeAndFillsSlots(t *testing.T) {
	t.Parallel()
	props := map[string]json.RawMessage{"title": json.RawMessage(`"Plan trips together"`), "links": json.RawMessage(`["Trips","Guides"]`)}
	doc, res, err := Apply(New("site"), "", []Op{
		{Op: "insert", Block: "web/navbar", ID: "nav", Props: map[string]json.RawMessage{"links": props["links"]}},
		{Op: "insert", Block: "web/hero", Props: map[string]json.RawMessage{"title": props["title"]}},
	})
	require.NoError(t, err)
	require.Equal(t, "zinc", doc.Theme.ID)
	require.Equal(t, "nav", res.Created[0])
	require.Equal(t, "hero", res.Created[1])
	require.NotEmpty(t, res.Notes)
	hero := doc.Find("hero").Node
	require.Greater(t, hero.X, 0.0, "a second block lands beside the first")
	found := false
	Walk([]*Node{hero}, func(n *Node, _ *Node, _ int) bool {
		found = found || n.TextValue() == "Plan trips together"
		return true
	})
	require.True(t, found)
	links := 0
	Walk([]*Node{doc.Find("nav").Node}, func(n *Node, _ *Node, _ int) bool {
		if n.Name == "Link" {
			links++
		}
		return true
	})
	require.Equal(t, 2, links)

	// An unknown slot is an error that lists the real ones.
	_, _, err = Apply(doc, "", []Op{{Op: "insert", Block: "web/hero", Props: map[string]json.RawMessage{"headline": json.RawMessage(`"x"`)}}})
	require.ErrorContains(t, err, "title")
	_, _, err = Apply(doc, "", []Op{{Op: "insert", Block: "web/heroo"}})
	require.ErrorContains(t, err, "web/hero")
}

func TestSectionsFillAPageStack(t *testing.T) {
	t.Parallel()
	page := json.RawMessage(`{"id":"page","type":"frame","name":"Landing","width":1280,"height":100,"sizing_y":"hug","layout":{"dir":"column","gap":0,"pad":0,"main":"start","cross":"start"}}`)
	doc, _, err := Apply(New("site"), "", []Op{
		{Op: "add", Node: page},
		{Op: "insert", Block: "web/navbar", Parent: "page"},
		{Op: "insert", Block: "web/features", Parent: "page"},
	})
	require.NoError(t, err)
	p := doc.Find("page").Node
	require.Len(t, p.Children, 2)
	for _, c := range p.Children {
		require.InDelta(t, 1280, c.Width, 0.5, c.Name)
	}
	require.InDelta(t, p.Children[0].Height+p.Children[1].Height, p.Height, 0.5)
}

func TestSetThemeRestylesWhatIsThere(t *testing.T) {
	t.Parallel()
	doc, _, err := Apply(New("site"), "", []Op{{Op: "set_theme", Name: "zinc"}, {Op: "insert", Block: "web/hero"}})
	require.NoError(t, err)
	doc, _, err = Apply(doc, "", []Op{{Op: "set_theme", Name: "butter", Accent: "grass", Mode: "Dark"}})
	require.NoError(t, err)
	require.Equal(t, "Dark", doc.Variables.Mode)
	butter, err := LookupTheme("butter", "grass")
	require.NoError(t, err)
	Walk(doc.Pages[0].Children, func(n *Node, _ *Node, _ int) bool {
		if n.TextStyle == "t-display" {
			require.Equal(t, "Outfit", n.Font.Family)
		}
		for _, f := range n.Fills {
			if f.Variable == "c-accent" {
				require.Equal(t, butter.Colors["accent"][1], f.Color)
			}
		}
		return true
	})
	_, _, err = Apply(doc, "", []Op{{Op: "set_theme", Name: "zinc", Mode: "Sepia"}})
	require.Error(t, err)
}

func TestIconOpsAndSearch(t *testing.T) {
	t.Parallel()
	require.Greater(t, IconCount(), 1500)
	hits := SearchIcons("arrow right", 5)
	require.NotEmpty(t, hits)
	require.Equal(t, "arrow-right", hits[0].Name)
	require.NotEmpty(t, SearchIcons("cart", 5))
	_, err := LookupIcon("hart")
	require.ErrorContains(t, err, "close matches")

	doc, _, err := Apply(New("icons"), "", []Op{{Op: "add", Node: json.RawMessage(`{"id":"h","type":"path","icon":"heart","width":20}`)}})
	require.NoError(t, err)
	h := doc.Find("h").Node
	require.Equal(t, TypePath, h.Type)
	require.InDelta(t, 20, h.Height, 0.01)
	require.Empty(t, h.Fills)
	require.Equal(t, "round", h.Strokes[0].Cap)
	svg, err := SVG(doc, doc.Pages[0], nil)
	require.NoError(t, err)
	require.Contains(t, svg, `stroke-linecap="round"`)
}

// TestKitPreview writes every block as SVG when ATLAS_KIT_PREVIEW names a
// folder, so a person can look at them.
func TestKitPreview(t *testing.T) {
	dir := os.Getenv("ATLAS_KIT_PREVIEW")
	if dir == "" {
		t.Skip("set ATLAS_KIT_PREVIEW to a folder to write previews")
	}
	themes := strings.Split(os.Getenv("ATLAS_KIT_THEMES"), ",")
	if themes[0] == "" {
		themes = []string{"zinc", "ios", "material"}
	}
	for _, id := range themes {
		th, err := LookupTheme(id, "")
		require.NoError(t, err)
		doc := New(id)
		require.NoError(t, doc.setTheme(Op{Name: id}))
		x := 0.0
		for _, info := range Blocks(th.Platform) {
			n, err := BuildBlock(info.ID, th, 0, nil, "", doc.ids())
			require.NoError(t, err)
			n.X = x
			doc.Pages[0].Children = append(doc.Pages[0].Children, n)
			ResolveLayout(doc, nil)
			x += n.Width + 80
			one := New(info.ID)
			one.Styles, one.Variables, one.Theme = doc.Styles, doc.Variables, doc.Theme
			c := *n
			c.X, c.Y = 0, 0
			one.Pages[0].Children = []*Node{&c}
			svg, err := SVG(one, one.Pages[0], nil)
			require.NoError(t, err)
			name := id + "-" + strings.ReplaceAll(info.ID, "/", "-") + ".svg"
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(svg), 0o644))
		}
	}
}

func TestBlocksPassTheirOwnLint(t *testing.T) {
	t.Parallel()
	for _, th := range Themes() {
		for _, info := range Blocks("") {
			doc := New("lint")
			_, _, err := Apply(doc, "", nil)
			require.NoError(t, err)
			doc, _, err = Apply(doc, "", []Op{{Op: "set_theme", Name: th.ID}, {Op: "insert", Block: info.ID, ID: "b"}})
			require.NoError(t, err, info.ID)
			doc.Pages[0].Background = doc.Variables.Items["c-bg"].Values[doc.Variables.Mode]
			report, err := Lint(doc, "", "b")
			require.NoError(t, err)
			for _, is := range report.Issues {
				require.NotEqual(t, "error", is.Severity, "%s in %s: %s", info.ID, th.ID, is.Message)
				require.NotContains(t, []string{"spacing", "clipped", "alignment", "emoji", "placeholder"}, is.Rule, "%s in %s: %s", info.ID, th.ID, is.Message)
			}
		}
	}
}

func TestLintFindsSlop(t *testing.T) {
	t.Parallel()
	bad := json.RawMessage(`{"id":"s","type":"frame","name":"Screen","width":390,"height":844,"fills":[{"type":"linear","stops":[{"offset":0,"color":"#7C3AED"},{"offset":1,"color":"#2563EB"}]}],"children":[
	 {"id":"t","type":"text","text":"🚀 Lorem ipsum dolor sit amet","x":20,"y":20,"font":{"family":"Inter","size":9,"weight":400},"fills":[{"type":"solid","color":"#8B5CF6"}]},
	 {"id":"b","type":"frame","name":"Buy button","x":21,"y":80,"width":120,"height":30,"layout":{"dir":"row","gap":7,"pad":[5,10,5,10],"main":"center","cross":"center"}},
	 {"id":"o","type":"rect","x":300,"y":800,"width":200,"height":100}
	]}`)
	doc, _, err := Apply(New("bad"), "", []Op{{Op: "add", Node: bad}})
	require.NoError(t, err)
	report, err := Lint(doc, "", "")
	require.NoError(t, err)
	rules := map[string]bool{}
	for _, is := range report.Issues {
		rules[is.Rule] = true
	}
	for _, want := range []string{"emoji", "placeholder", "small-text", "spacing", "touch-target", "clipped", "alignment", "gradient"} {
		require.True(t, rules[want], "expected a %s issue in %s", want, report.Text(40))
	}
	require.Less(t, report.Score, 80)
	require.Contains(t, report.Text(3), "more")
	require.InDelta(t, 21, Contrast("#000000", "#FFFFFF"), 0.01)
	require.InDelta(t, 1, Contrast("#FFFFFF", "#FFFFFF"), 0.01)
}

// The COP31 Companion example (kit/examples) is a 13-screen app built only
// from the kit. It must keep applying and keep linting well, so a change to a
// block that breaks a real composition shows up here.
func TestCOP31ExampleAppStaysClean(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("kit", "examples", "cop31-ops.json"))
	require.NoError(t, err)
	var ops []Op
	require.NoError(t, json.Unmarshal(data, &ops))
	doc, _, err := Apply(New("COP31 Companion"), "", ops)
	require.NoError(t, err)
	require.Len(t, doc.Pages[0].Children, 13)
	for _, screen := range doc.Pages[0].Children {
		require.InDelta(t, 390, screen.Width, 0.5, screen.Name)
		require.InDelta(t, 844, screen.Height, 0.5, screen.Name)
		report, err := Lint(doc, "", screen.ID)
		require.NoError(t, err)
		require.GreaterOrEqual(t, report.Score, 90, "%s: %s", screen.Name, report.Text(10))
		for _, is := range report.Issues {
			require.NotEqual(t, "error", is.Severity, "%s: %s", screen.Name, is.Message)
		}
	}
}

func TestSetBlockPropsKeepsIDsAndTweaks(t *testing.T) {
	t.Parallel()
	doc, _, err := Apply(New("props"), "", []Op{
		{Op: "set_theme", Name: "zinc"},
		{Op: "insert", Block: "web/hero", ID: "hero"},
	})
	require.NoError(t, err)
	root := doc.Find("hero").Node
	require.NotNil(t, root.Kit)
	require.Equal(t, "web/hero", root.Kit.ID)
	var layer *Node
	Walk([]*Node{root}, func(n *Node, _ *Node, _ int) bool {
		if layer == nil && n.ID != root.ID && n.Type != TypeText && len(n.Fills) > 0 {
			layer = n
		}
		return true
	})
	require.NotNil(t, layer)
	doc, _, err = Apply(doc, "", []Op{{Op: "update", ID: layer.ID, Props: map[string]json.RawMessage{
		"fills": json.RawMessage(`[{"type":"solid","color":"#FF00AA"}]`),
	}}})
	require.NoError(t, err)
	doc, _, err = Apply(doc, "", []Op{{Op: "set_block_props", ID: "hero", Props: map[string]json.RawMessage{
		"title": json.RawMessage(`"Measured title"`),
	}}})
	require.NoError(t, err)
	root = doc.Find("hero").Node
	require.Equal(t, "hero", root.ID)
	require.JSONEq(t, `"Measured title"`, string(root.Kit.Props["title"]))
	kept := doc.Find(layer.ID)
	require.NotNil(t, kept)
	require.Equal(t, "#FF00AA", kept.Node.Fills[0].Color)
	found := false
	Walk([]*Node{root}, func(n *Node, _ *Node, _ int) bool {
		if n.Type == TypeText && strings.Contains(n.TextValue(), "Measured title") {
			found = true
		}
		return true
	})
	require.True(t, found)
}

func TestQRBadgeIsAPath(t *testing.T) {
	t.Parallel()
	doc, _, err := Apply(New("qr"), "", []Op{
		{Op: "set_theme", Name: "ios"},
		{Op: "insert", Block: "ios/badge", ID: "badge"},
	})
	require.NoError(t, err)
	var qr *Node
	Walk(doc.Pages[0].Children, func(n *Node, _ *Node, _ int) bool {
		if n.Name == "QR code" {
			qr = n
		}
		return qr == nil
	})
	require.NotNil(t, qr)
	n := 0
	Walk([]*Node{qr}, func(node *Node, _ *Node, _ int) bool {
		n++
		return true
	})
	require.Less(t, n, 20, "the QR code should be one path plus the finder squares, not a rect per module")
	require.Equal(t, TypePath, qr.Children[0].Type)
}

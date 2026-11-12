package design

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAutofixSnapsOddSpacing(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"id":"f","type":"frame","name":"Row","width":200,"height":40,"layout":{"dir":"row","gap":5,"pad":[1,8,8,8],"main":"start","cross":"start"}}`)
	doc, _, err := Apply(New("fix"), "", []Op{{Op: "add", Node: raw}})
	require.NoError(t, err)
	ops, err := Autofix(doc, "", "")
	require.NoError(t, err)
	require.NotEmpty(t, ops)
	doc, _, err = Apply(doc, "", ops)
	require.NoError(t, err)
	lay := doc.Find("f").Node.Layout
	require.Equal(t, 0, int(lay.Gap)%2)
	require.Equal(t, 0, int(lay.Pad[0])%2)
}

func TestLinkScreensWiresTabs(t *testing.T) {
	t.Parallel()
	home := json.RawMessage(`{"id":"home","type":"frame","name":"Home","width":390,"height":844,"children":[
	  {"id":"tab","type":"frame","name":"Tab","width":72,"height":48,"children":[
	    {"id":"label","type":"text","name":"Label","text":"Map","font":{"family":"Inter","size":12,"weight":500}}
	  ]}
	]}`)
	maps := json.RawMessage(`{"id":"map","type":"frame","name":"Map","x":500,"width":390,"height":844,"children":[
	  {"id":"back","type":"frame","name":"Back","width":44,"height":44}
	]}`)
	doc, _, err := Apply(New("flow"), "", []Op{{Op: "add", Node: home}, {Op: "add", Node: maps}, {Op: "link_screens"}})
	require.NoError(t, err)
	tab := doc.Find("tab").Node
	require.NotEmpty(t, tab.Interactions)
	require.Equal(t, "navigate", tab.Interactions[0].Action)
	require.Equal(t, "map", tab.Interactions[0].To)
	require.Equal(t, "back", doc.Find("back").Node.Interactions[0].Action)
	require.True(t, doc.Find("home").Node.FlowStart)
}

func TestRecipeLandingInsertsSections(t *testing.T) {
	t.Parallel()
	doc, _, err := Apply(New("page"), "", []Op{{Op: "recipe", Name: "landing", Props: map[string]json.RawMessage{"title": json.RawMessage(`"Northwind"`)}}})
	require.NoError(t, err)
	require.Len(t, doc.Pages[0].Children, 1)
	page := doc.Pages[0].Children[0]
	require.GreaterOrEqual(t, len(page.Children), 5)
	report, err := Lint(doc, "", page.ID)
	require.NoError(t, err)
	require.GreaterOrEqual(t, report.Score, 90, report.Text(12))
	require.NotNil(t, doc.Theme)
	found := false
	Walk(doc.Pages[0].Children, func(n *Node, _ *Node, _ int) bool {
		if n.Type == TypeText && strings.Contains(n.TextValue(), "Northwind") {
			found = true
		}
		return true
	})
	require.True(t, found)
}

func TestStitchAppIsALinkedFlow(t *testing.T) {
	t.Parallel()
	doc, _, err := Apply(New("stitch"), "", []Op{{Op: "recipe", Name: "stitch", Props: map[string]json.RawMessage{
		"platform": json.RawMessage(`"ios"`),
		"product":  json.RawMessage(`"Northwind"`),
		"promise":  json.RawMessage(`"The week's jobs, on one screen"`),
		"detail":   json.RawMessage(`"Harbour Street bakery. Three ovens, two shifts."`),
	}}})
	require.NoError(t, err)
	require.Len(t, doc.Pages[0].Children, 3)
	linked := 0
	for _, screen := range doc.Pages[0].Children {
		require.InDelta(t, 390, screen.Width, 1, screen.Name)
		require.InDelta(t, 844, screen.Height, 1, screen.Name)
		report, err := Lint(doc, "", screen.ID)
		require.NoError(t, err)
		require.GreaterOrEqual(t, report.Score, 90, "%s: %s", screen.Name, report.Text(12))
		for _, is := range report.Issues {
			require.NotEqual(t, "error", is.Severity, "%s: %s", screen.Name, is.Message)
		}
		Walk([]*Node{screen}, func(n *Node, _ *Node, _ int) bool {
			for _, in := range n.Interactions {
				if in.Action == "navigate" {
					linked++
				}
			}
			return true
		})
	}
	require.Greater(t, linked, 0)
}

func TestBrandThemeKeepsContrast(t *testing.T) {
	t.Parallel()
	doc, _, err := Apply(New("brand"), "", []Op{{Op: "set_theme", Color: "#C2410C"}})
	require.NoError(t, err)
	require.Equal(t, "custom", doc.Theme.ID)
	require.NotNil(t, doc.Custom)
	require.GreaterOrEqual(t, Contrast(doc.Custom.Colors["text"][0], doc.Custom.Colors["bg"][0]), 4.5)
	require.GreaterOrEqual(t, Contrast(doc.Custom.Colors["text"][1], doc.Custom.Colors["bg"][1]), 4.5)
	doc, _, err = Apply(doc, "", []Op{{Op: "insert", Block: "web/hero", ID: "hero"}})
	require.NoError(t, err)
	report, err := Lint(doc, "", "hero")
	require.NoError(t, err)
	for _, is := range report.Issues {
		require.NotEqual(t, "error", is.Severity, is.Message)
	}
}

func TestBlockSchemaAndTokenCSS(t *testing.T) {
	t.Parallel()
	var hero BlockInfo
	for _, info := range Blocks("web") {
		if info.ID == "web/hero" {
			hero = info
		}
	}
	require.Equal(t, "object", hero.Schema["type"])
	doc, _, err := Apply(New("code"), "", []Op{{Op: "set_theme", Name: "zinc"}, {Op: "insert", Block: "web/button", ID: "btn"}})
	require.NoError(t, err)
	code, err := GenerateCode(doc, "btn", false)
	require.NoError(t, err)
	require.Contains(t, code.CSS, "--c-accent")
	require.Contains(t, code.CSS, "var(--")
}

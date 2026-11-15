package design

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// skillExample is the example script in the design-script skill, so the
// skill always shows a script that runs and passes the gate.
func skillExample(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../skills/builtin/design-script/SKILL.md")
	require.NoError(t, err)
	_, rest, ok := strings.Cut(string(data), "## Example")
	require.True(t, ok)
	_, code, ok := strings.Cut(rest, "```js\n")
	require.True(t, ok)
	code, _, ok = strings.Cut(code, "```")
	require.True(t, ok)
	return code
}

func errorsOf(issues []Issue) []Issue {
	var out []Issue
	for _, is := range issues {
		if is.Severity == "error" {
			out = append(out, is)
		}
	}
	return out
}

func TestScriptBuildsSystemAndComponents(t *testing.T) {
	t.Parallel()
	res, err := RunScript(context.Background(), New("COP31"), nil, skillExample(t), ScriptOptions{Name: "cop31"})
	require.NoError(t, err)
	d := res.Doc

	require.Len(t, d.Variables.Items, 9)
	require.Equal(t, []string{"Light", "Dark"}, d.Variables.Modes)
	require.Equal(t, "#4CC9A4", d.Variables.Items["c-brand"].Values["Dark"])
	require.Len(t, d.Styles.Text, 7)
	require.Equal(t, "upper", d.Styles.Text["t-overline"].Transform)
	require.Len(t, d.Styles.Effects, 1)
	require.Equal(t, []float64{4, 8, 12, 16, 20, 24, 32, 48, 64}, res.System.Space)

	sets := map[string]int{}
	for _, p := range d.Pages {
		Walk(p.Children, func(n *Node, _ *Node, _ int) bool {
			if n.Type == TypeComponent && n.Set != "" {
				sets[n.Set]++
			}
			return true
		})
	}
	require.Equal(t, map[string]int{"Button": 6, "List row": 2, "Tab": 2, "Chip": 2}, sets)

	require.Equal(t, "Library", d.Pages[0].Name, "the empty first page is dropped")
	lib := d.PageByRef("Library")
	require.NotNil(t, lib)
	screens := d.PageByRef("Screens")
	require.NotNil(t, screens)
	require.Len(t, screens.Children, 1)
	today := screens.Children[0]
	require.Equal(t, "01 Today", today.Name)
	require.True(t, IsScreen(today))
	require.Equal(t, 393.0, today.Width)

	// Instances take the size their content needs in the main's layout: the
	// longer label makes this button wider than its main.
	btn := d.Find("cop31:directions")
	require.NotNil(t, btn)
	require.Equal(t, TypeInstance, btn.Node.Type)
	main := d.Find(btn.Node.ComponentID).Node
	require.Equal(t, "primary", main.Variant["kind"])
	w, h, ok := InstanceSize(d, btn.Node)
	require.True(t, ok)
	require.InDelta(t, w, btn.Node.Width, 0.01)
	require.InDelta(t, h, btn.Node.Height, 0.01)
	require.Greater(t, btn.Node.Width, main.Width+30)
	require.Equal(t, 48.0, btn.Node.Height)
	var label *Node
	for _, c := range main.Children {
		if c.Name == "Label" {
			label = c
		}
	}
	require.NotNil(t, label)
	require.Equal(t, "Directions to Room B2", *btn.Node.Overrides[label.ID].Text)

	// Text carries its style and a bound colour.
	Walk(today.Children, func(n *Node, _ *Node, _ int) bool {
		if n.Type == TypeText {
			require.NotEmpty(t, n.TextStyle, n.Name)
			require.NotEmpty(t, n.Fills[0].Variable, n.Name)
		}
		return true
	})

	require.Empty(t, errorsOf(SystemLint(d, &res.System, "")))
	for _, p := range d.Pages {
		report, err := Lint(d, p.ID, "")
		require.NoError(t, err)
		require.Empty(t, errorsOf(report.Issues), p.Name)
	}
	for id := range res.System.Roots {
		require.True(t, strings.HasPrefix(id, "cop31:"), id)
	}
}

func TestScriptRerunReplacesAndKeepsHandEdits(t *testing.T) {
	t.Parallel()
	src := `colors({text: "#111111", bg: "#FFFFFF", brand: "#0B5D4B", line: "#DDDDDD"});
type({styles: {body: {size: 15}, label: {size: 15, weight: 600}, title: {size: 22, weight: 600}}});
const b = component("Badge", {dir: "row", pad: [4, 8], fill: "brand"}, [text("New", "label", {color: "bg", name: "Label"})]);
library([b]);
place(screen("Home", {device: "iphone", gap: 16, pad: 16}, [text("Hello", "title"), instance(b)]), {page: "Screens"});`
	first, err := RunScript(context.Background(), New("x"), nil, src, ScriptOptions{Name: "kit"})
	require.NoError(t, err)
	require.Empty(t, first.Kept)

	// Unchanged: a rerun replaces everything and keeps the same ids.
	second, err := RunScript(context.Background(), first.Doc, &first.System, src, ScriptOptions{Name: "kit"})
	require.NoError(t, err)
	require.Empty(t, second.Kept)
	require.Len(t, second.Replaced, len(first.System.Roots))
	require.Equal(t, first.Doc.Count(), second.Doc.Count())

	// The person retitles the screen by hand: the next run keeps it.
	edited, _, err := Apply(second.Doc, "", []Op{{Op: "update", ID: "kit:home", Props: rawProps(t, `{"name":"Home (final)"}`)}})
	require.NoError(t, err)
	third, err := RunScript(context.Background(), edited, &second.System, strings.Replace(src, `"New"`, `"Fresh"`, 1), ScriptOptions{Name: "kit"})
	require.NoError(t, err)
	require.Len(t, third.Kept, 1)
	require.Equal(t, "Home (final)", third.Doc.Find("kit:home").Node.Name)
	require.Contains(t, outlineText(third.Doc), "Fresh")

	forced, err := RunScript(context.Background(), edited, &second.System, src, ScriptOptions{Name: "kit", Force: true})
	require.NoError(t, err)
	require.Empty(t, forced.Kept)
	require.Equal(t, "Home", forced.Doc.Find("kit:home").Node.Name)

	// Layers the script stops making are removed.
	gone, err := RunScript(context.Background(), forced.Doc, &forced.System, strings.SplitN(src, "place(", 2)[0], ScriptOptions{Name: "kit"})
	require.NoError(t, err)
	require.Nil(t, gone.Doc.Find("kit:home"))
	require.Len(t, gone.Removed, 1)
}

func outlineText(d *Doc) string {
	var b strings.Builder
	for _, p := range d.Pages {
		Walk(p.Children, func(n *Node, _ *Node, _ int) bool {
			b.WriteString(n.TextValue() + "\n")
			return true
		})
	}
	return b.String()
}

func TestScriptErrorsPointAtTheScript(t *testing.T) {
	t.Parallel()
	_, err := RunScript(context.Background(), New("x"), nil, "colors({text: '#111'});\ntext('Hi', 'nope');", ScriptOptions{Name: "s"})
	var se *ScriptError
	require.ErrorAs(t, err, &se)
	require.Contains(t, se.Msg, "unknown text style")
	require.Contains(t, se.Msg, "s.js:2")

	_, err = RunScript(context.Background(), New("x"), nil, "while (true) {}", ScriptOptions{Name: "s", Timeout: 50 * time.Millisecond})
	require.ErrorAs(t, err, &se)
	require.Contains(t, se.Msg, "longer than")
}

func TestSystemLintFindsWhatMakesAFileSloppy(t *testing.T) {
	t.Parallel()
	src := `colors({text: "#111111", bg: "#FFFFFF", brand: "#0B5D4B", line: "#DDDDDD"});
type({styles: {body: {size: 15}, label: {size: 15, weight: 600}, title: {size: 22}}});
space([4, 8, 16, 24]);
radius({md: 12});
const loose = component("Card", {fill: "bg"}, [
  frame({name: "Frame 2", w: 10, h: 10, fill: "#FF0000"}),
  text("A", "body"),
]);
const set = variants("Toggle", {state: ["on", "off"]}, (v) => component("Toggle", {dir: "row", pad: 8, h: 44, fill: "brand"}, [text(v.state, "label", {color: "bg"})]));
library([loose, set]);
place(frame({name: "Board", w: 600, h: 400, fill: "bg"}, [
  col({name: "List", gap: 10, radius: 7}, [1, 2, 3].map((i) => row({name: "Item " + i, gap: 8}, [rect({w: 8, h: 8, fill: "brand"}), text("Row " + i, "body")]))),
]), {page: "Screens"});`
	res, err := RunScript(context.Background(), New("x"), nil, src, ScriptOptions{Name: "s"})
	require.NoError(t, err)
	// A set made by hand that misses a combination.
	d := res.Doc
	var rules []string
	broken, _, err := Apply(d, "", []Op{{Op: "update", ID: d.Find("s:toggle-2").Node.ID, Props: rawProps(t, `{"variant":{"state":"on","size":"lg"}}`)}})
	require.NoError(t, err)
	for _, is := range SystemLint(broken, &res.System, "") {
		rules = append(rules, is.Severity+" "+is.Rule)
	}
	require.Contains(t, rules, "error component-layout")
	require.Contains(t, rules, "warning component-names")
	require.Contains(t, rules, "warning component-colour")
	require.Contains(t, rules, "error variants")
	require.Contains(t, rules, "warning space-scale")
	require.Contains(t, rules, "warning radius-scale")
	require.Contains(t, rules, "warning detached-repeat")
}

func rawProps(t *testing.T, s string) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(s), &m))
	return m
}

func TestInstancesReflowInsteadOfStretching(t *testing.T) {
	t.Parallel()
	src := `colors({text: "#111111", bg: "#FFFFFF", brand: "#0B5D4B", "on-brand": "#FFFFFF"});
type({styles: {label: {size: 15, weight: 600}, body: {size: 15}, title: {size: 20}}});
const b = component("Button", {dir: "row", gap: 8, pad: [12, 20], h: 48, cross: "center", main: "center", fill: "brand"}, [text("Go", "label", {color: "on-brand", name: "Label"})]);
library([b]);
place(col({name: "Stack", gap: 8, w: 300}, [instance(b, {name: "Short"}), instance(b, {name: "Long", text: {Label: "Continue to payment"}}), instance(b, {name: "Wide", w: "fill"})]), {page: "Screens"});`
	res, err := RunScript(context.Background(), New("x"), nil, src, ScriptOptions{Name: "r"})
	require.NoError(t, err)
	d := res.Doc
	short, long, wide := d.Find("r:short").Node, d.Find("r:long").Node, d.Find("r:wide").Node
	require.Greater(t, long.Width, short.Width+60)
	require.Equal(t, 300.0, wide.Width)

	// Drawn, the label of the wide one sits in the middle at its own size:
	// laid out again, not scaled.
	r := Resolved(d)
	label := r.Find("r:wide").Node.Children[0]
	require.InDelta(t, (300-label.Width)/2, label.X, 0.5)
	require.InDelta(t, (48-label.Height)/2, label.Y, 0.5)
	svg, err := SVG(d, d.PageByRef("Screens"), nil)
	require.NoError(t, err)
	require.NotContains(t, svg, "scale(")
}

func TestDesignKitScriptPassesTheGate(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../skills/builtin/design-kit/SKILL.md")
	require.NoError(t, err)
	_, rest, ok := strings.Cut(string(data), "## Script")
	require.True(t, ok)
	_, code, ok := strings.Cut(rest, "```js\n")
	require.True(t, ok)
	code, _, ok = strings.Cut(code, "```")
	require.True(t, ok)
	res, err := RunScript(context.Background(), New("kit"), nil, code, ScriptOptions{Name: "kit"})
	require.NoError(t, err)
	var rules []string
	for _, is := range SystemLint(res.Doc, &res.System, "") {
		rules = append(rules, is.Severity+" "+is.Rule+": "+is.Message)
	}
	require.Empty(t, rules)
	sets := map[string]int{}
	for _, p := range res.Doc.Pages {
		Walk(p.Children, func(n *Node, _ *Node, _ int) bool {
			if n.Type == TypeComponent && n.Set != "" {
				sets[n.Set]++
			}
			return true
		})
		report, err := Lint(res.Doc, p.ID, "")
		require.NoError(t, err)
		require.Empty(t, errorsOf(report.Issues), p.Name)
	}
	require.Equal(t, 9, sets["Button"])
	require.Equal(t, 3, sets["Input"])
}

func TestDesignKitEveryLookPassesTheGate(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../skills/builtin/design-kit/SKILL.md")
	require.NoError(t, err)
	_, rest, _ := strings.Cut(string(data), "## Script")
	_, code, _ := strings.Cut(rest, "```js\n")
	code, _, _ = strings.Cut(code, "```")
	for _, name := range []string{"editorial", "swiss", "soft", "night", "civic", "vivid"} {
		src := strings.Replace(code, "const look = looks.editorial;", "const look = looks."+name+";", 1)
		res, err := RunScript(context.Background(), New("kit"), nil, src, ScriptOptions{Name: "kit"})
		require.NoError(t, err, name)
		var rules []string
		for _, is := range SystemLint(res.Doc, &res.System, "") {
			rules = append(rules, is.Severity+" "+is.Rule+": "+is.Message)
		}
		require.Empty(t, rules, name)
		for _, p := range res.Doc.Pages {
			report, err := Lint(res.Doc, p.ID, "")
			require.NoError(t, err, name)
			require.Empty(t, errorsOf(report.Issues), name+" "+p.Name)
		}
	}
}

func TestDesignKitNativeScreenHasAppChrome(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../skills/builtin/design-kit/SKILL.md")
	require.NoError(t, err)
	_, rest, _ := strings.Cut(string(data), "## Script")
	_, code, _ := strings.Cut(rest, "```js\n")
	code, rest, _ = strings.Cut(code, "```")
	_, screenCode, ok := strings.Cut(rest, "```js\n")
	require.True(t, ok)
	screenCode, _, _ = strings.Cut(screenCode, "```")
	res, err := RunScript(context.Background(), New("kit"), nil, code+"\n"+screenCode, ScriptOptions{Name: "kit"})
	require.NoError(t, err)
	for _, is := range SystemLint(res.Doc, &res.System, "") {
		require.NotEqual(t, "app-chrome", is.Rule, is.Message)
		require.NotEqual(t, "web-hero", is.Rule, is.Message)
		require.NotEqual(t, "no-material", is.Rule, is.Message)
		require.NotEqual(t, "raw-colour", is.Rule, is.Message)
		require.NotEqual(t, "space-scale", is.Rule, is.Message)
	}
	screens := res.Doc.PageByRef("Screens")
	require.NotNil(t, screens)
	report, err := Lint(res.Doc, screens.ID, "")
	require.NoError(t, err)
	for _, is := range report.Issues {
		require.NotEqual(t, "error", is.Severity, is.Rule+": "+is.Message)
	}

	// The same screen as a web page: a hero band and no chrome.
	web := code + `
place(screen("01 Web", { device: "iphone", fill: "bg", gap: 16, pad: 0 }, [
  col({ name: "Hero", w: "fill", h: 220, pad: 24, fill: "brand" }, [text("Welcome", "display", { color: "on-brand" })]),
  text("Body copy", "body"),
]), { page: "Screens" });`
	res, err = RunScript(context.Background(), New("kit"), nil, web, ScriptOptions{Name: "kit"})
	require.NoError(t, err)
	var rules []string
	for _, is := range SystemLint(res.Doc, &res.System, "") {
		rules = append(rules, is.Rule)
	}
	require.Contains(t, rules, "app-chrome")
	require.Contains(t, rules, "web-hero")
	require.Contains(t, rules, "no-material")
}

// kitBlocks returns the kit script and the code block under a heading.
func kitBlocks(t *testing.T, heading string) (kit, block string) {
	t.Helper()
	data, err := os.ReadFile("../skills/builtin/design-kit/SKILL.md")
	require.NoError(t, err)
	_, rest, _ := strings.Cut(string(data), "## Script")
	_, kit, _ = strings.Cut(rest, "```js\n")
	kit, _, _ = strings.Cut(kit, "```")
	_, rest, ok := strings.Cut(string(data), heading)
	require.True(t, ok, heading)
	_, block, ok = strings.Cut(rest, "```js\n")
	require.True(t, ok)
	block, _, _ = strings.Cut(block, "```")
	return kit, block
}

func TestDesignKitRichHomePassesAsAnApp(t *testing.T) {
	t.Parallel()
	kit, home := kitBlocks(t, "## Rich home")
	_, sessions := kitBlocks(t, "## Using it on a screen")
	kit = strings.Replace(kit, "const look = looks.editorial;", "const look = looks.vivid;", 1)
	res, err := RunScript(context.Background(), New("kit"), nil, kit+"\n"+sessions+"\n"+home, ScriptOptions{Name: "kit"})
	require.NoError(t, err)
	for _, is := range SystemLint(res.Doc, &res.System, "") {
		require.NotEqual(t, "error", is.Severity, is.Rule+": "+is.Message)
		for _, rule := range []string{"app-chrome", "web-hero", "no-material", "raw-colour", "space-scale", "radius-scale", "mixed-language", "component-target"} {
			require.NotEqual(t, rule, is.Rule, is.Message)
		}
	}
	screens := res.Doc.PageByRef("Screens")
	require.NotNil(t, screens)
	report, err := Lint(res.Doc, screens.ID, "")
	require.NoError(t, err)
	for _, is := range report.Issues {
		require.NotEqual(t, "error", is.Severity, is.Rule+": "+is.Message)
	}
	if dir := os.Getenv("KIT_PREVIEW"); dir != "" {
		svg, err := SVG(res.Doc, screens, nil)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "kit-home.svg"), []byte(svg), 0o644))
	}

	// A colour band lower down, without the status bar on it, is still a
	// web hero; a screen in two languages is flagged.
	bad := kit + `
place(screen("03 Bad", { device: "iphone", fill: "bg", gap: 16, pad: 0 }, [
  instance(statusBar, { name: "Status bar" }),
  col({ name: "Hero", w: "fill", h: 200, pad: 24, fill: "brand" }, [text("Hoş Geldiniz", "display", { color: "on-brand" })]),
  text("Latest updates", "title"),
  text("Canlı yayınlar ve haberler", "body"),
  text("Live now", "body"),
  map({ w: 361, h: 120, seed: "x" }),
  instance(homeIndicator, { name: "Home indicator" }),
]), { page: "Screens" });`
	res, err = RunScript(context.Background(), New("kit"), nil, bad, ScriptOptions{Name: "kit"})
	require.NoError(t, err)
	var rules []string
	for _, is := range SystemLint(res.Doc, &res.System, "") {
		rules = append(rules, is.Rule)
	}
	require.Contains(t, rules, "web-hero")
	require.Contains(t, rules, "mixed-language")
}

func TestScriptMaterialDrawsVectors(t *testing.T) {
	t.Parallel()
	code := `
colors({ bg: ["#F3F6F8", "#0D1418"], surface: ["#FFFFFF", "#151E24"], "surface-2": ["#E6ECF0", "#1E2A32"], text: ["#13222B", "#E9F0F4"], line: ["#D5DEE4", "#2A3A44"], brand: ["#0B5D8A", "#6CC0EE"], "map-water": ["#CDE1EE", "#14283A"], "map-park": ["#DCEAD9", "#16261C"] });
type({ family: "Geist", styles: { caption: { size: 13, weight: 500, line: 16 } } });
space([4, 8, 12, 16, 24]);
radius({ sm: 4, md: 8, lg: 16 });
place(col({ name: "Board", gap: 16, pad: 16 }, [
  map({ w: 320, h: 160, seed: "x", route: true, labelStyle: "caption", pins: [{ x: 0.2, y: 0.7, kind: "you" }, { x: 0.8, y: 0.3, label: "Room B2" }] }),
  chart("spark", [3, 5, 4, 8, 7, 9], { w: 160, h: 48 }),
  chart("bars", [3, 5, 4, 8], { w: 120, h: 48 }),
  chart("ring", 0.62, { w: 64 }),
  pattern("topo", { w: 200, h: 120, seed: "c" }),
  pattern("dots", { w: 80, h: 80 }),
  pattern("perforation", { w: 320, h: 12, color: "bg" }),
  pattern("lines", { w: 80, h: 80 }),
  path("M0 0 L40 40", { w: 40, h: 40, stroke: "brand", weight: 2 }),
]));`
	res, err := RunScript(context.Background(), New("material"), nil, code, ScriptOptions{Name: "m"})
	require.NoError(t, err)
	names := map[string]int{}
	var paths int
	Walk(res.Doc.Pages[0].Children, func(n *Node, _ *Node, _ int) bool {
		names[n.Name]++
		if n.Type == TypePath {
			paths++
			require.NotEmpty(t, n.D, n.Name)
			for _, s := range n.Strokes {
				require.NotEmpty(t, s.Variable, n.Name+" strokes with a token")
			}
		}
		return true
	})
	for _, want := range []string{"Map", "Water", "Streets", "Roads", "Route", "You", "Pin", "Pin label", "Chart/spark", "Chart/bars", "Bar/now", "Chart/ring", "Progress", "Pattern/topo", "Pattern/dots", "Pattern/perforation", "Pattern/lines"} {
		require.Positive(t, names[want], want)
	}
	require.GreaterOrEqual(t, paths, 10)
	svg, err := SVG(res.Doc, res.Doc.Pages[0], nil)
	require.NoError(t, err)
	require.Contains(t, svg, " A")
	require.Greater(t, strings.Count(svg, "<path"), 10)
}

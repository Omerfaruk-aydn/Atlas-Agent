package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/design"
	"github.com/stretchr/testify/require"
)

const testSystemScript = `colors({bg: ["#FFFFFF", "#101214"], surface: ["#F4F4F5", "#1A1D20"], text: ["#111315", "#EDEFF1"], brand: ["#0B5D4B", "#4CC9A4"], "on-brand": ["#FFFFFF", "#06231B"]});
type({styles: {title: {size: 22, weight: 600}, body: {size: 15}, label: {size: 15, weight: 600}, caption: {size: 13}}});
space([4, 8, 12, 16, 24, 32]);
radius({md: 12, full: 999});
const button = variants("Button", {state: ["default", "disabled"]}, (v) =>
  component("Button", {dir: "row", pad: [12, 20], h: 48, cross: "center", radius: "full", fill: v.state === "disabled" ? "surface" : "brand"}, [
    text("Continue", "label", {color: v.state === "disabled" ? "text" : "on-brand", name: "Label"})]));
const card = component("Card", {dir: "column", gap: 8, pad: 16, w: 361, radius: "md", fill: "surface"}, [text("Session", "title", {name: "Title"}), text("Hall A", "body", {name: "Meta"})]);
const chip = component("Chip", {dir: "row", pad: [12, 16], h: 44, radius: "full", fill: "surface"}, [text("All", "caption", {name: "Label"})]);
library([button, card, chip]);
`

func TestDesignScriptActionSavesAndGates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := design.NewStore(dir)
	out, err := designScript(context.Background(), dir, store, DesignCanvasParams{Action: "script", File: "demo", Code: testSystemScript})
	require.NoError(t, err)
	require.Contains(t, out, "Ran .atlas/design/scripts/demo.js on demo")
	require.Contains(t, out, "Button (2 variants)")
	_, err = os.Stat(filepath.Join(dir, ".atlas", "design", "scripts", "demo.js"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, ".atlas", "design", "systems", "demo.json"))
	require.NoError(t, err)

	// Rerun the saved script: same result, nothing kept.
	again, err := designScript(context.Background(), dir, store, DesignCanvasParams{Action: "script", File: "demo"})
	require.NoError(t, err)
	require.NotContains(t, again, "Kept because")

	// A component placed by hand stops the save and leaves the file alone.
	before, _, err := store.Load("demo")
	require.NoError(t, err)
	bad := testSystemScript + `library([component("Loose", {fill: "surface"}, [text("A", "body"), text("B", "body")])], {page: "More"});`
	rejected, err := designScript(context.Background(), dir, store, DesignCanvasParams{Action: "script", File: "demo", Code: bad})
	require.NoError(t, err)
	require.Contains(t, rejected, "NOT SAVED")
	require.Contains(t, rejected, "component-layout")
	after, _, err := store.Load("demo")
	require.NoError(t, err)
	require.Equal(t, before.Count(), after.Count())

	// A script error says where.
	_, err = designScript(context.Background(), dir, store, DesignCanvasParams{Action: "script", File: "demo", Code: "colors({});\nicon();\nnope();"})
	require.ErrorContains(t, err, "the script failed, nothing changed")
}

func TestDesignProgressWalksThePhases(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := design.NewStore(dir)
	_, err := store.Save("demo", design.New("demo"), "", true)
	require.NoError(t, err)

	out, err := designProgressText(dir, store, DesignCanvasParams{File: "demo"})
	require.NoError(t, err)
	require.Contains(t, out, "[ ] 1. Brief")
	require.Contains(t, out, "Now: Brief.")
	require.False(t, designPhaseMet(dir, "demo", "system"))

	brief := filepath.Join(dir, ".atlas", "design", "briefs", "demo.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(brief), 0o755))
	short := strings.Repeat("A conference companion for delegates. ", 12)
	require.NoError(t, os.WriteFile(brief, []byte(short), 0o644))
	out, err = designProgressText(dir, store, DesignCanvasParams{File: "demo"})
	require.NoError(t, err)
	require.Contains(t, out, "no \"Direction\" section")
	require.NoError(t, os.WriteFile(brief, []byte(short+"\n\n## Direction\nsoft, Outfit and Figtree, pill shapes\n\n## References\n- Citymapper: the next step first, in one glance\n- Apple Wallet: tickets as the main object\n"), 0o644))
	_, err = designScript(context.Background(), dir, store, DesignCanvasParams{Action: "script", File: "demo", Code: testSystemScript})
	require.NoError(t, err)

	out, err = designProgressText(dir, store, DesignCanvasParams{File: "demo"})
	require.NoError(t, err)
	require.Contains(t, out, "[x] 1. Brief")
	require.Contains(t, out, "[x] 2. System")
	require.Contains(t, out, "[x] 3. Components")
	require.Contains(t, out, "Now: Screens.")
	require.True(t, designPhaseMet(dir, "demo", "components"))
	require.False(t, designPhaseMet(dir, "demo", "screens"))

	// The browser task's design requirement can wait on a phase.
	r := browserRequirement{Subject: "demo", Expect: "@components; Button"}
	require.True(t, designRequirementMet(dir, r))
	r.Expect = "@screens"
	require.False(t, designRequirementMet(dir, r))
	require.Error(t, validDesignRequirement(BrowserRequirementInput{Subject: "demo", Expect: "@polish"}))
	require.NoError(t, validDesignRequirement(BrowserRequirementInput{Subject: "demo", Expect: "@review"}))
}

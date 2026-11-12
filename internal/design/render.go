package design

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed kit/gen/raster.mjs
var rasterScript []byte

// RenderStamp is the hash of a page at the moment it was rasterised.
// Lint in the agent tool treats a page as unfinished when this is missing
// or older than the page.
type RenderStamp struct {
	Page string `json:"page"`
	Hash string `json:"hash"`
}

// PageHash is a short hash of a page's JSON. It changes when the page
// changes and ignores the document's render stamps.
func PageHash(p *Page) string {
	if p == nil {
		return ""
	}
	data, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:12])
}

// NoteRender records that pageID was rasterised as it is now.
func (d *Doc) NoteRender(pageID string) {
	var page *Page
	for _, p := range d.Pages {
		if p.ID == pageID {
			page = p
			break
		}
	}
	if page == nil {
		return
	}
	hash := PageHash(page)
	for i := range d.Renders {
		if d.Renders[i].Page == pageID {
			d.Renders[i].Hash = hash
			return
		}
	}
	d.Renders = append(d.Renders, RenderStamp{Page: pageID, Hash: hash})
}

// Rendered reports whether pageID was rasterised and has not changed
// since.
func (d *Doc) Rendered(pageID string) bool {
	var page *Page
	for _, p := range d.Pages {
		if p.ID == pageID {
			page = p
			break
		}
	}
	if page == nil {
		return false
	}
	want := PageHash(page)
	for _, r := range d.Renders {
		if r.Page == pageID && r.Hash == want && r.Hash != "" {
			return true
		}
	}
	return false
}

func sanitizeRenders(d *Doc) {
	if len(d.Renders) == 0 {
		d.Renders = nil
		return
	}
	ids := map[string]bool{}
	for _, p := range d.Pages {
		ids[p.ID] = true
	}
	out := make([]RenderStamp, 0, len(d.Renders))
	seen := map[string]bool{}
	for _, r := range d.Renders {
		if !ids[r.Page] || seen[r.Page] || len(r.Hash) != 24 {
			continue
		}
		if _, err := hex.DecodeString(r.Hash); err != nil {
			continue
		}
		seen[r.Page] = true
		out = append(out, r)
	}
	if len(out) == 0 {
		d.Renders = nil
		return
	}
	d.Renders = out
}

// WithRenderCheck adds an error when the page has not been rasterised
// since it last changed, and lowers the score the same way an error does.
// Block tests call Lint directly and do not use this.
func (r LintReport) WithRenderCheck(d *Doc, pageID string) LintReport {
	if d != nil && d.Rendered(pageID) {
		return r
	}
	r.Issues = append([]Issue{{
		Severity: "error",
		Rule:     "render",
		Message:  "This page has not been rendered since it changed. Call action render, then critique, and look at the PNG before treating the design as done.",
	}}, r.Issues...)
	r.Score = max(0, r.Score-8)
	return r
}

// Critique is the fixed rubric for a rendered page. Geometric parts are
// filled from lint. The rest is for whoever looks at the PNG.
func Critique(d *Doc, pageRef, pngPath string) (string, error) {
	page := d.PageByRef(pageRef)
	if page == nil {
		return "", fmt.Errorf("no page %q", pageRef)
	}
	report, err := Lint(d, page.ID, "")
	if err != nil {
		return "", err
	}
	counts := map[string]int{}
	for _, is := range report.Issues {
		counts[is.Rule]++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "PNG: %s\n", pngPath)
	if !d.Rendered(page.ID) {
		b.WriteString("This PNG is not stamped on the file. Action render writes the stamp; without it, lint fails the page.\n")
	}
	fmt.Fprintf(&b, "Lint: score %d/100, %.0f%% of layers from kit blocks, %d issues.\n", report.Score, 100*report.Kit, len(report.Issues))
	b.WriteString("Rubric, answer each from the picture. A fail means the design is not done:\n")
	fmt.Fprintf(&b, "- hierarchy: %s\n", critiqueLine(counts["type-scale"], "type sizes stay near the theme scale", "too many text sizes"))
	fmt.Fprintf(&b, "- spacing: %s\n", critiqueLine(counts["spacing"], "spacing is on the 4px grid", "off-grid spacing"))
	fmt.Fprintf(&b, "- alignment: %s\n", critiqueLine(counts["alignment"], "no near-miss alignment", "edges that almost align"))
	fmt.Fprintf(&b, "- contrast: %s\n", critiqueLine(counts["contrast"]+counts["contrast-dark"], "no contrast failures in lint", "text below the contrast bar"))
	fmt.Fprintf(&b, "- copy: %s\n", critiqueLine(counts["placeholder"]+counts["sample-copy"]+counts["emoji"], "no placeholder, sample or emoji copy", "placeholder, sample copy, or emoji"))
	fmt.Fprintf(&b, "- brand: kit coverage %.0f%%. %s\n", 100*report.Kit, critiqueLine(counts["tokens"]+counts["kit-coverage"]+counts["gradient"], "colours follow the theme", "unbound colours or a purple-blue gradient"))
	empty := emptyFrames(page)
	fmt.Fprintf(&b, "- emptiness: %s\n", critiqueLine(empty, "no large empty frames", fmt.Sprintf("%d large frames have no text", empty)))
	if report.Colorblind > 0 {
		fmt.Fprintf(&b, "- colorblind: %d text and background pairs that pass in full colour fall under 3:1 for protanopia. Do not encode meaning by red versus green alone.\n", report.Colorblind)
	} else {
		b.WriteString("- colorblind: no sampled pair collapses under a protanopia simulation.\n")
	}
	b.WriteString("Look at the PNG for clipped text, cut-off labels, and grey or empty regions lint scored as fine.\n")
	return b.String(), nil
}

func critiqueLine(n int, ok, bad string) string {
	if n == 0 {
		return "pass (" + ok + "). Confirm in the PNG."
	}
	return fmt.Sprintf("fail (%d: %s).", n, bad)
}

func emptyFrames(page *Page) int {
	n := 0
	var walk func(*Node)
	walk = func(node *Node) {
		if node == nil || !node.IsVisible() {
			return
		}
		if node.Type == TypeFrame && node.Width*node.Height >= 8000 && !hasText(node) {
			n++
		}
		for _, c := range node.Children {
			walk(c)
		}
	}
	for _, c := range page.Children {
		walk(c)
	}
	return n
}

func hasText(n *Node) bool {
	if n.Type == TypeText && strings.TrimSpace(n.TextValue()) != "" {
		return true
	}
	for _, c := range n.Children {
		if c.IsVisible() && hasText(c) {
			return true
		}
	}
	return false
}

// Raster writes a PNG of an SVG using Node and playwright-core. fontsDir
// is the app's node_modules so the PNG uses the kit faces; empty still
// draws, with fallback fonts. playwrightDir is the playwright-core folder.
func Raster(svgPath, pngPath, fontsDir, playwrightDir string) error {
	if _, err := exec.LookPath("node"); err != nil {
		return fmt.Errorf("render needs node on PATH")
	}
	if _, err := os.Stat(filepath.Join(playwrightDir, "package.json")); err != nil {
		return fmt.Errorf("playwright-core was not found at %s", playwrightDir)
	}
	dir, err := os.MkdirTemp("", "atlas-raster")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	script := filepath.Join(dir, "raster.mjs")
	if err := os.WriteFile(script, rasterScript, 0o644); err != nil {
		return err
	}
	if fontsDir == "" {
		fontsDir = "-"
	}
	cmd := exec.Command("node", script, svgPath, pngPath, fontsDir, playwrightDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rasterise: %w\n%s", err, out)
	}
	info, err := os.Stat(pngPath)
	if err != nil || info.Size() < 32 {
		return fmt.Errorf("rasterise wrote no PNG")
	}
	return nil
}

// PlaywrightDir walks up from start looking for app/node_modules/playwright-core.
func PlaywrightDir(start string) string {
	dir, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	for range 8 {
		p := filepath.Join(dir, "app", "node_modules", "playwright-core", "package.json")
		if _, err := os.Stat(p); err == nil {
			return filepath.Dir(p)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// FontsDir is the node_modules that holds the kit faces, next to playwright.
func FontsDir(playwrightDir string) string {
	if playwrightDir == "" {
		return ""
	}
	dir := filepath.Dir(playwrightDir)
	if _, err := os.Stat(filepath.Join(dir, "@fontsource-variable", "inter")); err != nil {
		return ""
	}
	return dir
}

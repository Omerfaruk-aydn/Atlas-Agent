package design

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// Lint checks a design for what makes generated UI look careless:
// unreadable contrast, text that overflows or gets cut off, off-grid
// spacing, near-misses in alignment, too many fonts, sizes and colours,
// tiny touch targets on phones, overlong lines, emoji standing in for
// icons, and placeholder copy. It works on the design as drawn, with styles,
// variables and instances resolved.

// Issue is one finding.
type Issue struct {
	// Severity is error (fix it), warning (fix it unless intended) or info.
	Severity string `json:"severity"`
	Rule     string `json:"rule"`
	NodeID   string `json:"node_id,omitempty"`
	Message  string `json:"message"`
}

// LintReport is the result of a check.
type LintReport struct {
	Score int `json:"score"`
	Nodes int `json:"nodes"`
	// Kit is the share of layers that belong to an inserted block, 0 to 1.
	Kit float64 `json:"kit_coverage"`
	// Colorblind counts pairs that pass in full colour and fall under 3:1
	// for protanopia. It is not an issue and does not change the score.
	Colorblind int     `json:"colorblind,omitempty"`
	Issues     []Issue `json:"issues"`
}

// Contrast is the WCAG contrast ratio of fg drawn over bg; translucent
// colours are blended first (bg over white).
func Contrast(fg, bg string) float64 {
	b := blend(rgba(bg), [4]float64{1, 1, 1, 1})
	f := blend(rgba(fg), b)
	l1, l2 := luminance(f), luminance(b)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

func rgba(hex string) [4]float64 {
	if !IsColor(hex) {
		return [4]float64{0, 0, 0, 1}
	}
	h := hex[1:]
	if len(h) <= 4 {
		var s strings.Builder
		for _, c := range h {
			s.WriteRune(c)
			s.WriteRune(c)
		}
		h = s.String()
	}
	v := func(i int) float64 {
		n, _ := strconv.ParseUint(h[i:i+2], 16, 8)
		return float64(n) / 255
	}
	a := 1.0
	if len(h) == 8 {
		a = v(6)
	}
	return [4]float64{v(0), v(2), v(4), a}
}

// blend draws c over an opaque base.
func blend(c, base [4]float64) [4]float64 {
	a := c[3]
	return [4]float64{c[0]*a + base[0]*(1-a), c[1]*a + base[1]*(1-a), c[2]*a + base[2]*(1-a), 1}
}

func luminance(c [4]float64) float64 {
	ch := func(v float64) float64 {
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c[0]) + 0.7152*ch(c[1]) + 0.0722*ch(c[2])
}

var (
	placeholderRe = regexp.MustCompile(`(?i)\b(lorem ipsum|dolor sit amet|placeholder text|your text here|insert (text|title) here|sample text|text goes here)\b`)
	defaultNameRe = regexp.MustCompile(`^(Frame|Rectangle|Ellipse|Text|Group|Vector|Line|Polygon|Star|Image)( \d+)?$`)
)

func hasEmoji(s string) bool {
	for _, r := range s {
		if (r >= 0x1F300 && r <= 0x1FAFF) || (r >= 0x2600 && r <= 0x27BF && r != 0x2713 && r != 0x2714) || r == 0x2B50 {
			return true
		}
	}
	return false
}

type linter struct {
	report   *LintReport
	pageBg   string
	families map[string]int
	sizes    map[float64]int
	colours  map[string]bool
	unbound  int
	kitNodes int
	themed   bool
	unnamed  int
}

func (l *linter) addf(sev, rule string, n *Node, format string, args ...any) {
	id := ""
	if n != nil {
		id = n.ID
		format = fmt.Sprintf("%s %q: ", n.Type, n.Name) + format
	}
	l.report.Issues = append(l.report.Issues, Issue{Severity: sev, Rule: rule, NodeID: id, Message: fmt.Sprintf(format, args...)})
}

// background is the colour behind a node: every solid fill from the page
// up to the parent, translucent ones included. A gradient or image in
// that stack has no single colour, so the result is empty and contrast
// is skipped.
func (l *linter) background(path []*Node) string {
	acc := rgba(l.pageBg)
	acc[3] = 1
	for i := 0; i < len(path)-1; i++ {
		n := path[i]
		if !n.IsVisible() {
			continue
		}
		nodeA := 1.0
		if n.Opacity != nil {
			nodeA = *n.Opacity
		}
		for _, f := range n.Fills {
			if f.Visible != nil && !*f.Visible {
				continue
			}
			switch f.Type {
			case "solid":
				c := rgba(f.Color)
				if f.Opacity != nil {
					c[3] *= *f.Opacity
				}
				c[3] *= nodeA
				if c[3] <= 0 {
					continue
				}
				acc = blend(c, acc)
			case "image", "linear", "radial", "angular":
				return ""
			}
		}
	}
	return opaqueHex(acc)
}

func opaqueHex(c [4]float64) string {
	ch := func(v float64) int {
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		return int(v*255 + 0.5)
	}
	return fmt.Sprintf("#%02X%02X%02X", ch(c[0]), ch(c[1]), ch(c[2]))
}

// simulatedContrast is the same pair after a simple protanopia mix.
func simulatedContrast(fg, bg string) float64 {
	return Contrast(protan(fg), protan(bg))
}

func protan(hex string) string {
	c := rgba(hex)
	r := 0.567*c[0] + 0.433*c[1]
	g := 0.558*c[0] + 0.442*c[1]
	b := 0.242*c[1] + 0.758*c[2]
	return opaqueHex([4]float64{r, g, b, 1})
}

// Lint checks the nodes of a page, or only the subtree of node id when id
// is set. When the file has a second colour mode with different values,
// contrast is also checked there and reported as contrast-dark.
func Lint(d *Doc, pageRef, id string) (LintReport, error) {
	report, err := lintOnce(d, pageRef, id)
	if err != nil {
		return LintReport{}, err
	}
	if other, ok := otherMode(d); ok {
		extra, err := lintOnce(other, pageRef, id)
		if err != nil {
			return LintReport{}, err
		}
		for _, is := range extra.Issues {
			if is.Rule != "contrast" {
				continue
			}
			is.Rule = "contrast-dark"
			is.Message = "Other mode: " + is.Message
			report.Issues = append(report.Issues, is)
		}
	}
	sort.SliceStable(report.Issues, func(i, j int) bool {
		return severityRank(report.Issues[i].Severity) < severityRank(report.Issues[j].Severity)
	})
	score := 100
	for _, is := range report.Issues {
		switch is.Severity {
		case "error":
			score -= 8
		case "warning":
			score -= 3
		default:
			score--
		}
	}
	report.Score = max(0, score)
	return report, nil
}

// lintOnce checks one mode. It does not look at the other mode.
func lintOnce(d *Doc, pageRef, id string) (LintReport, error) {
	r := Resolved(d)
	page := r.PageByRef(pageRef)
	if page == nil {
		return LintReport{}, fmt.Errorf("no page %q", pageRef)
	}
	roots := page.Children
	if id != "" {
		at := r.Find(id)
		if at == nil {
			return LintReport{}, fmt.Errorf("no node %q", id)
		}
		roots = []*Node{at.Node}
	}
	report := &LintReport{}
	l := &linter{report: report, pageBg: page.Background, families: map[string]int{}, sizes: map[float64]int{}, colours: map[string]bool{}, themed: d.Theme != nil}
	for _, root := range roots {
		mobile := root.Width > 0 && root.Width < 500 && root.Height > 600
		l.walk(root, []*Node{root}, mobile, nil)
	}
	l.totals()
	return *report, nil
}

func otherMode(d *Doc) (*Doc, bool) {
	v := d.Variables
	if v == nil || len(v.Modes) < 2 {
		return nil, false
	}
	other := ""
	for _, m := range v.Modes {
		if !strings.EqualFold(m, v.Mode) {
			other = m
			break
		}
	}
	if other == "" {
		return nil, false
	}
	differ := false
	for _, item := range v.Items {
		if item != nil && item.Values[v.Mode] != item.Values[other] {
			differ = true
			break
		}
	}
	if !differ {
		return nil, false
	}
	c, err := clone(d)
	if err != nil {
		return nil, false
	}
	c.Variables.Mode = other
	// A translucent layer is judged over the page. Follow the theme's
	// page colour into the other mode, including the default grey a new
	// file starts with.
	from, to := "", ""
	if item := v.Items["c-bg"]; item != nil {
		from = item.Values[v.Mode]
		to = item.Values[other]
	}
	if to != "" {
		for _, p := range c.Pages {
			if strings.EqualFold(p.Background, from) || strings.EqualFold(p.Background, "#F5F5F5") {
				p.Background = to
			}
		}
	}
	return c, true
}

func severityRank(s string) int {
	switch s {
	case "error":
		return 0
	case "warning":
		return 1
	}
	return 2
}

func (l *linter) walk(n *Node, path []*Node, mobile bool, inKit *BlockRef) {
	if !n.IsVisible() {
		return
	}
	if n.Kit != nil && n.Kit.ID != "" {
		inKit = n.Kit
	}
	l.report.Nodes++
	if inKit != nil {
		l.kitNodes++
	}
	if defaultNameRe.MatchString(n.Name) {
		l.unnamed++
	}
	for _, f := range n.Fills {
		if f.Type == "solid" && n.Type != TypeImage {
			l.colours[strings.ToUpper(f.Color)] = true
			if l.themed && f.Variable == "" && rgba(f.Color)[3] > 0 {
				l.unbound++
			}
		}
		if f.Type == "linear" && len(f.Stops) >= 2 && slopGradient(f.Stops) {
			l.addf("info", "gradient", n, "a purple-to-blue gradient is the most common generated look; a solid accent or a gradient of the brand's own hues reads as more deliberate")
		}
	}
	if n.Layout != nil {
		for _, v := range append([]float64{n.Layout.Gap}, n.Layout.Pad[:]...) {
			if v != math.Trunc(v) || int(v)%2 != 0 {
				l.addf("warning", "spacing", n, "spacing %.4gpx is off the 4px grid (use 4, 8, 12, 16, 24, 32…)", v)
				break
			}
		}
	}
	if n.Type == TypeText {
		l.text(n, path, mobile, inKit)
	}
	if mobile && (len(n.Interactions) > 0 || strings.Contains(strings.ToLower(n.Name), "button")) && n.Type != TypeText {
		if n.Height < 40 || n.Width < 40 {
			l.addf("warning", "touch-target", n, "%.0f×%.0f is smaller than a finger; make tappable things at least 44×44 on a phone", n.Width, n.Height)
		}
	}
	clips := n.Type.IsContainer() && n.Type != TypeGroup && (n.Clip == nil || *n.Clip)
	var flow []*Node
	for _, c := range n.Children {
		if !c.IsVisible() {
			continue
		}
		// Decorations named "Decor..." bleed off the edge on purpose.
		if clips && c.Rotation == 0 && !strings.HasPrefix(c.Name, "Decor") && (c.X < -0.5 || c.Y < -0.5 || c.X+c.Width > n.Width+0.5 || c.Y+c.Height > n.Height+0.5) {
			sev := "warning"
			if inKit != nil {
				sev = "error"
			}
			l.addf(sev, "clipped", c, "sticks out of %q (%.0f×%.0f at %.0f,%.0f in a %.0f×%.0f frame) and is cut off", n.Name, c.Width, c.Height, c.X, c.Y, n.Width, n.Height)
		}
		if !c.Absolute {
			flow = append(flow, c)
		}
		l.walk(c, append(append([]*Node{}, path...), c), mobile, inKit)
	}
	// An instance's layers are placed by its main, not by hand.
	if n.Layout == nil && n.Type != TypeInstance && len(flow) > 1 {
		l.nearMisses(n, flow)
	}
}

func (l *linter) text(n *Node, path []*Node, mobile bool, inKit *BlockRef) {
	s := n.TextValue()
	if n.Font != nil {
		l.families[n.Font.Family]++
		l.sizes[n.Font.Size]++
		if n.Font.Size < 10 || (!mobile && n.Font.Size < 12) {
			l.addf("warning", "small-text", n, "%.0fpx text is hard to read; use 12px or more (10px only for phone tab labels)", n.Font.Size)
		}
	}
	if note := overflowNote(n); note != "" {
		l.addf("error", "overflow", n, "text overflows its box; use auto_resize \"height\" or make it taller")
	}
	if hasEmoji(s) {
		l.addf("warning", "emoji", n, "emoji used as an icon or decoration; place a real icon ({\"icon\":\"name\"}) instead")
	}
	if placeholderRe.MatchString(s) {
		l.addf("error", "placeholder", n, "placeholder copy; write the real words")
	}
	if inKit != nil && sampleLeftIn(inKit, s) {
		l.addf("warning", "sample-copy", n, "still the block's sample copy; give the slot the brief's own words")
	}
	if n.Font != nil && n.AutoResize != "width" && n.Width > 0 {
		chars := n.Width / (n.Font.Size * 0.5)
		if chars > 95 && len([]rune(s)) > 120 {
			l.addf("warning", "line-length", n, "lines run to about %.0f characters; keep body text between 45 and 80 (narrow the box)", chars)
		}
	}
	if len(n.Fills) == 0 || n.Fills[0].Type != "solid" {
		return
	}
	bg := l.background(path)
	if bg == "" {
		return
	}
	fg := n.Fills[0].Color
	a := rgba(fg)
	op := 1.0
	for _, p := range path {
		if p.Opacity != nil {
			op *= *p.Opacity
		}
	}
	if n.Fills[0].Opacity != nil {
		op *= *n.Fills[0].Opacity
	}
	if op < 1 {
		a[3] *= op
		fg = fmt.Sprintf("#%02X%02X%02X%02X", int(a[0]*255+0.5), int(a[1]*255+0.5), int(a[2]*255+0.5), int(a[3]*255+0.5))
	}
	ratio := Contrast(fg, bg)
	if ratio >= 4.5 && simulatedContrast(fg, bg) < 3 {
		l.report.Colorblind++
	}
	large := n.Font != nil && (n.Font.Size >= 24 || (n.Font.Size >= 18.5 && n.Font.Weight >= 700))
	need := 4.5
	if large {
		need = 3
	}
	switch {
	case ratio < need-1.5:
		l.addf("error", "contrast", n, "contrast %.2f:1 against %s is unreadable; needs %.1f:1", ratio, bg, need)
	case ratio < need:
		l.addf("warning", "contrast", n, "contrast %.2f:1 against %s is below %.1f:1", ratio, bg, need)
	}
}

// nearMisses finds siblings whose edges are almost, but not quite, lined up.
func (l *linter) nearMisses(parent *Node, kids []*Node) {
	edges := func(n *Node) [3]float64 { return [3]float64{n.X, n.X + n.Width/2, n.X + n.Width} }
	for i := range kids {
		for j := i + 1; j < len(kids); j++ {
			a, b := edges(kids[i]), edges(kids[j])
			for k := range 3 {
				if d := math.Abs(a[k] - b[k]); d > 0.5 && d < 4 {
					l.addf("warning", "alignment", parent, "%q and %q are %.1fpx off from lining up; align them or use auto layout", kids[i].Name, kids[j].Name, d)
					return
				}
			}
		}
	}
}

func slopGradient(stops []Stop) bool {
	hue := func(c string) float64 {
		v := rgba(c)
		mx, mn := math.Max(v[0], math.Max(v[1], v[2])), math.Min(v[0], math.Min(v[1], v[2]))
		if mx-mn < 0.15 {
			return -1
		}
		var h float64
		switch mx {
		case v[0]:
			h = math.Mod((v[1]-v[2])/(mx-mn), 6)
		case v[1]:
			h = (v[2]-v[0])/(mx-mn) + 2
		default:
			h = (v[0]-v[1])/(mx-mn) + 4
		}
		h *= 60
		if h < 0 {
			h += 360
		}
		return h
	}
	purple, blue := false, false
	for _, s := range stops {
		h := hue(s.Color)
		purple = purple || (h >= 255 && h <= 300)
		blue = blue || (h >= 190 && h < 255)
	}
	return purple && blue
}

func (l *linter) totals() {
	if len(l.families) > 3 {
		names := make([]string, 0, len(l.families))
		for f := range l.families {
			names = append(names, f)
		}
		sort.Strings(names)
		l.addf("warning", "fonts", nil, "%d font families (%s); keep to two plus a mono", len(names), strings.Join(names, ", "))
	}
	if len(l.sizes) > 9 {
		sizes := make([]float64, 0, len(l.sizes))
		for s := range l.sizes {
			sizes = append(sizes, s)
		}
		sort.Float64s(sizes)
		parts := make([]string, len(sizes))
		for i, s := range sizes {
			parts[i] = strconv.FormatFloat(s, 'f', -1, 64)
		}
		l.addf("warning", "type-scale", nil, "%d text sizes (%s); use the theme's type scale", len(sizes), strings.Join(parts, ", "))
	}
	if len(l.colours) > 16 {
		l.addf("warning", "palette", nil, "%d different fill colours; a theme needs about a dozen", len(l.colours))
	}
	if l.unbound > 0 {
		l.addf("info", "tokens", nil, "%d fills use their own colour instead of the theme's variables (c-*); they will not follow a theme or dark mode switch", l.unbound)
	}
	if l.report.Nodes > 0 {
		l.report.Kit = float64(l.kitNodes) / float64(l.report.Nodes)
	}
	if l.themed && l.report.Nodes >= 40 && l.kitNodes*2 < l.report.Nodes && l.unbound >= 8 {
		l.addf("error", "kit-coverage", nil, "only %.0f%% of layers come from kit blocks and %d fills ignore the theme; build with insert and change slots with set_block_props instead of drawing over the block", 100*l.report.Kit, l.unbound)
	}
	if l.unnamed > 3 {
		l.addf("info", "names", nil, "%d layers keep default names like \"Frame 3\"; name them for the person and for code export", l.unnamed)
	}
}

// Text describes the report for a person or a model, at most max issues.
func (r LintReport) Text(max int) string {
	var b strings.Builder
	counts := map[string]int{}
	for _, is := range r.Issues {
		counts[is.Severity]++
	}
	fmt.Fprintf(&b, "Quality score %d/100 over %d layers, %.0f%% from kit blocks: %d errors, %d warnings, %d notes.\n", r.Score, r.Nodes, 100*r.Kit, counts["error"], counts["warning"], counts["info"])
	for i, is := range r.Issues {
		if i == max {
			fmt.Fprintf(&b, "… and %d more\n", len(r.Issues)-i)
			break
		}
		where := ""
		if is.NodeID != "" {
			where = " (" + is.NodeID + ")"
		}
		fmt.Fprintf(&b, "- [%s %s]%s %s\n", is.Severity, is.Rule, where, firstUpper(is.Message))
	}
	if len(r.Issues) == 0 {
		b.WriteString("No issues found.\n")
	}
	return b.String()
}

func firstUpper(s string) string {
	for i, r := range s {
		return string(unicode.ToUpper(r)) + s[i+len(string(r)):]
	}
	return s
}

var (
	sampleOnce sync.Once
	sampleText map[string]map[string]bool
)

func defaultSamples(id string) map[string]bool {
	sampleOnce.Do(func() {
		sampleText = map[string]map[string]bool{}
		for _, info := range Blocks("") {
			set := map[string]bool{}
			collectSamples(info.Slots, set)
			sampleText[info.ID] = set
		}
	})
	if sampleText[id] == nil {
		return map[string]bool{}
	}
	return sampleText[id]
}

func collectSamples(v any, set map[string]bool) {
	switch x := v.(type) {
	case string:
		if len([]rune(x)) >= 8 {
			set[x] = true
		}
	case map[string]any:
		for _, item := range x {
			collectSamples(item, set)
		}
	case []map[string]string:
		for _, item := range x {
			for _, s := range item {
				collectSamples(s, set)
			}
		}
	case []any:
		for _, item := range x {
			collectSamples(item, set)
		}
	}
}

// sampleLeftIn reports text that is still a block's default and was not
// passed back in as a prop.
func sampleLeftIn(kit *BlockRef, text string) bool {
	text = strings.TrimSpace(text)
	if !defaultSamples(kit.ID)[text] {
		return false
	}
	for _, raw := range kit.Props {
		if strings.Contains(string(raw), text) {
			return false
		}
	}
	return true
}

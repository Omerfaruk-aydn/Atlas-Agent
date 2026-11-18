package design

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// SystemLint holds a file to its own design system: components built on
// auto layout with every variant present, text in text styles, colours from
// tokens, spacing and radii on the declared scales, repeated structures made
// into components, and mains kept off the screens. sys may be nil when no
// script declared the scales. pageRef limits the per-layer checks to a page;
// empty checks every page.
func SystemLint(d *Doc, sys *System, pageRef string) []Issue {
	s := &systemLinter{doc: d, sys: sys}
	if d.Styles != nil {
		s.textStyles = len(d.Styles.Text)
	}
	if d.Variables != nil {
		s.colourVars = len(d.Variables.Items)
	}
	if sys != nil {
		s.space = sys.Space
		s.radii = sys.RadiusValues()
	}
	s.variantSets()
	for _, p := range d.Pages {
		if pageRef != "" && p.ID != pageRef && !strings.EqualFold(p.Name, pageRef) {
			continue
		}
		for _, root := range p.Children {
			screen := IsScreen(root)
			s.walk(root, nil, screen, root)
			s.repeats(root)
			s.appChrome(root)
		}
	}
	s.totals()
	return s.issues
}

type systemLinter struct {
	doc        *Doc
	sys        *System
	issues     []Issue
	textStyles int
	colourVars int
	space      []float64
	radii      []float64

	looseText, looseColour int
	firstText, firstColour *Node
	offScale               int
	missingFonts           map[string]bool
}

func (s *systemLinter) addf(sev, rule string, n *Node, format string, args ...any) {
	id := ""
	if n != nil {
		id = n.ID
	}
	s.issues = append(s.issues, Issue{Severity: sev, Rule: rule, NodeID: id, Message: fmt.Sprintf(format, args...)})
}

// IsScreen reports whether a top-level layer is a screen: a frame at a device
// size, or one with a layout grid.
func IsScreen(n *Node) bool {
	if n.Type != TypeFrame {
		return false
	}
	if len(n.Grids) > 0 {
		return true
	}
	for _, w := range []float64{360, 375, 390, 393, 402, 412, 414, 428, 430, 440, 768, 820, 834, 1024, 1280, 1366, 1440, 1536, 1920} {
		if math.Abs(n.Width-w) < 0.5 && n.Height >= 560 {
			return true
		}
	}
	return false
}

func (s *systemLinter) walk(n *Node, comp *Node, screen bool, root *Node) {
	if !n.IsVisible() {
		return
	}
	if n.Type == TypeComponent {
		if screen && comp == nil {
			s.addf("warning", "main-on-screen", n, "main component %q sits inside screen %q; keep mains on the Library page and place an instance here", n.Name, root.Name)
		}
		comp = n
		s.component(n)
	}
	inComp := comp != nil
	if defaultNameRe.MatchString(n.Name) && inComp {
		s.addf("warning", "component-names", n, "layer %q inside component %q keeps a default name; name it for what it is (Label, Icon, Container)", n.Name, componentLabel(comp))
	}
	if n.Type == TypeText && n.Font != nil && n.Font.Family != "" && !KitFont(n.Font.Family) {
		if s.missingFonts == nil {
			s.missingFonts = map[string]bool{}
		}
		if !s.missingFonts[n.Font.Family] {
			s.missingFonts[n.Font.Family] = true
			s.addf("warning", "font-missing", n, "%q is not a face the renderer has, so every render shows a fallback and you are judging type you cannot see; use one of %s", n.Font.Family, strings.Join(KitFamilies, ", "))
		}
	}
	if n.Type == TypeText && n.TextStyle == "" && s.textStyles >= 3 {
		if inComp {
			s.addf("error", "component-text-style", n, "text %q in component %q has no text style; give it one of the file's %d text styles", short(n.TextValue()), comp.Name, s.textStyles)
		} else {
			s.looseText++
			if s.firstText == nil {
				s.firstText = n
			}
		}
	}
	if s.colourVars >= 4 && n.Type != TypeImage && n.FillStyle == "" {
		for _, f := range n.Fills {
			if f.Type != "solid" || f.Variable != "" || (f.Visible != nil && !*f.Visible) || rgba(f.Color)[3] < 1 {
				continue
			}
			if inComp {
				s.addf("warning", "component-colour", n, "%q in component %q is filled with %s instead of a colour token; bind it so modes and themes follow", n.Name, comp.Name, f.Color)
			} else {
				s.looseColour++
				if s.firstColour == nil {
					s.firstColour = n
				}
			}
			break
		}
	}
	if n.Layout != nil && len(s.space) > 0 {
		for _, v := range append([]float64{n.Layout.Gap}, n.Layout.Pad[:]...) {
			if v != 0 && !onScale(v, s.space) {
				s.offScale++
				if s.offScale <= 8 {
					s.addf("warning", "space-scale", n, "%q uses %gpx, which is not on the spacing scale %s", n.Name, v, scaleText(s.space))
				}
				break
			}
		}
	}
	if n.Radius != nil && len(s.radii) > 0 && n.Type != TypeText {
		for _, r := range n.Radius.Corners {
			if r != 0 && !onScale(r, s.radii) && r < math.Min(n.Width, n.Height)/2-0.5 {
				s.addf("warning", "radius-scale", n, "%q has radius %g, which is not on the radius scale %s", n.Name, r, scaleText(s.radii))
				break
			}
		}
	}
	for _, c := range n.Children {
		s.walk(c, comp, screen, root)
	}
}

func onScale(v float64, scale []float64) bool {
	for _, s := range scale {
		if math.Abs(v-s) < 0.01 {
			return true
		}
	}
	return false
}

func scaleText(scale []float64) string {
	parts := make([]string, len(scale))
	for i, v := range scale {
		parts[i] = fmt.Sprintf("%g", v)
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func short(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > 32 {
		return string(r[:31]) + "…"
	}
	return string(r)
}

// component checks one main: auto layout when it holds more than one layer
// in flow, a tappable size when it is a control.
func (s *systemLinter) component(n *Node) {
	flow := 0
	for _, c := range n.Children {
		if c.IsVisible() && !c.Absolute {
			flow++
		}
	}
	if flow >= 2 && n.Layout == nil {
		s.addf("error", "component-layout", n, "component %q places %d layers by hand; give it auto layout (direction, gap, padding) so it resizes with its content", n.Name, flow)
	}
	name := strings.ToLower(n.Name + " " + n.Set)
	for _, word := range []string{"button", "tab", "chip", "toggle", "switch", "checkbox", "radio", "icon button", "segment"} {
		if strings.Contains(name, word) && (n.Height < 44 && n.Height > 0) && !strings.Contains(name, "size=sm") && !strings.Contains(name, "size=xs") {
			s.addf("warning", "component-target", n, "%q is %.0fpx tall; a control people tap needs 44px (keep a small size only as an explicit size variant)", n.Name, n.Height)
			break
		}
	}
}

// variantSets checks that every set has each combination of its properties
// exactly once.
func (s *systemLinter) variantSets() {
	sets := map[string][]*Node{}
	var order []string
	for _, p := range s.doc.Pages {
		Walk(p.Children, func(n *Node, _ *Node, _ int) bool {
			if n.Type == TypeComponent && n.Set != "" {
				if _, ok := sets[n.Set]; !ok {
					order = append(order, n.Set)
				}
				sets[n.Set] = append(sets[n.Set], n)
			}
			return true
		})
	}
	for _, name := range order {
		members := sets[name]
		values := map[string]map[string]bool{}
		for _, m := range members {
			for k, v := range m.Variant {
				if values[k] == nil {
					values[k] = map[string]bool{}
				}
				values[k][v] = true
			}
		}
		keys := make([]string, 0, len(values))
		for k := range values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		seen := map[string]*Node{}
		for _, m := range members {
			if len(m.Variant) != len(keys) {
				s.addf("error", "variants", m, "variant %q of %q does not set every property (%s)", m.Name, name, strings.Join(keys, ", "))
				continue
			}
			key := variantKey(m.Variant, keys)
			if other := seen[key]; other != nil {
				s.addf("warning", "variants", m, "%q and %q are the same variant of %q (%s)", other.Name, m.Name, name, key)
			}
			seen[key] = m
		}
		combos := []map[string]string{{}}
		for _, k := range keys {
			vals := make([]string, 0, len(values[k]))
			for v := range values[k] {
				vals = append(vals, v)
			}
			sort.Strings(vals)
			var next []map[string]string
			for _, c := range combos {
				for _, v := range vals {
					o := map[string]string{k: v}
					for ck, cv := range c {
						o[ck] = cv
					}
					next = append(next, o)
				}
			}
			combos = next
		}
		var missing []string
		for _, c := range combos {
			if key := variantKey(c, keys); seen[key] == nil {
				missing = append(missing, key)
			}
		}
		if len(missing) > 0 {
			shown := missing
			if len(shown) > 6 {
				shown = shown[:6]
			}
			s.addf("error", "variants", members[0], "set %q misses %d of %d variants: %s", name, len(missing), len(combos), strings.Join(shown, "; "))
		}
	}
}

func variantKey(v map[string]string, keys []string) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + v[k]
	}
	return strings.Join(parts, ", ")
}

// repeats finds three or more sibling frames with the same structure that
// are not instances: a component waiting to be made.
func (s *systemLinter) repeats(root *Node) {
	reported := 0
	Walk([]*Node{root}, func(n *Node, _ *Node, _ int) bool {
		if n.Type == TypeComponent || n.Type == TypeInstance || reported >= 5 {
			return n.Type != TypeInstance
		}
		groups := map[string][]*Node{}
		for _, c := range n.Children {
			if (c.Type == TypeFrame || c.Type == TypeGroup) && c.IsVisible() && len(c.Children) >= 2 && !isSetRow(c) {
				sig := structureOf(c, 0)
				groups[sig] = append(groups[sig], c)
			}
		}
		for _, list := range groups {
			if len(list) >= 3 {
				s.addf("warning", "detached-repeat", list[0], "%d layers in %q (%q, %q…) repeat the same structure; make one a component and use instances", len(list), n.Name, list[0].Name, list[1].Name)
				reported++
				break
			}
		}
		return true
	})
}

// structureOf is a node's structure: types and layout, three levels deep.
func structureOf(n *Node, depth int) string {
	var b strings.Builder
	b.WriteString(string(n.Type))
	if n.Layout != nil {
		b.WriteString(":" + n.Layout.Dir)
	}
	if depth < 3 && len(n.Children) > 0 {
		b.WriteByte('(')
		for i, c := range n.Children {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(structureOf(c, depth+1))
		}
		b.WriteByte(')')
	}
	return b.String()
}

// isSetRow reports whether a frame only holds the variants of a component
// set: a row of a set's board, which is meant to repeat.
func isSetRow(n *Node) bool {
	for _, c := range n.Children {
		if c.Type != TypeComponent || c.Set == "" {
			return false
		}
	}
	return len(n.Children) > 0
}

func (s *systemLinter) totals() {
	if s.looseText > 0 {
		s.addf("warning", "text-style", s.firstText, "%d text layers on screens use their own font settings instead of the file's %d text styles; apply the styles", s.looseText, s.textStyles)
	}
	if s.looseColour > 0 {
		s.addf("warning", "raw-colour", s.firstColour, "%d layers on screens are filled with a raw #hex instead of a colour token", s.looseColour)
	}
	if s.offScale > 8 {
		s.addf("warning", "space-scale", nil, "%d layouts in all use spacing off the scale %s", s.offScale, scaleText(s.space))
	}
}

// Merge adds issues to a report and scores it again.
func (r LintReport) Merge(extra []Issue) LintReport {
	if len(extra) == 0 {
		return r
	}
	r.Issues = append(r.Issues, extra...)
	sort.SliceStable(r.Issues, func(i, j int) bool {
		return severityRank(r.Issues[i].Severity) < severityRank(r.Issues[j].Severity)
	})
	score := 100
	for _, is := range r.Issues {
		switch is.Severity {
		case "error":
			score -= 8
		case "warning":
			score -= 3
		default:
			score--
		}
	}
	r.Score = max(0, score)
	return r
}

// componentLabel names a main for a message: its set and variant, or its name.
func componentLabel(n *Node) string {
	if n.Set != "" {
		return n.Set + " / " + n.Name
	}
	return n.Name
}

// phoneWidth is the widest screen still read as a phone.
const phoneWidth = 500

// appChrome checks that a phone screen is built like an app and not like a
// web page: the system's status bar and home indicator are present, and the
// content is not a stack of full-width colour bands.
func (s *systemLinter) appChrome(root *Node) {
	if root.Width >= phoneWidth || !IsScreen(root) {
		return
	}
	var status, home, material bool
	var band *Node
	Walk(root.Children, func(n *Node, parent *Node, depth int) bool {
		if !n.IsVisible() {
			return false
		}
		name := strings.ToLower(s.layerName(n))
		if isMaterial(n, name) || s.instanceHasMaterial(n) {
			material = true
		}
		if strings.Contains(name, "status bar") || strings.Contains(name, "statusbar") {
			status = true
		}
		if strings.Contains(name, "home indicator") || strings.Contains(name, "gesture") {
			home = true
		}
		if band == nil && depth <= 1 && n.Type != TypeText && n.Height >= 140 && n.Width >= root.Width-1 &&
			!strings.Contains(name, "bar") && !strings.Contains(name, "sheet") && hasSolidFill(n) &&
			(n.Type == TypeFrame || n.Type == TypeRect) && !isMaterial(n, name) && !s.brandHeader(n) &&
			(parent == nil || !isMaterial(parent, strings.ToLower(s.layerName(parent)))) {
			band = n
		}
		return true
	})
	if !status {
		s.addf("warning", "app-chrome", root, "screen %q has no status bar; a phone screen starts with the system status bar (time, signal, battery), named \"Status bar\"", root.Name)
	}
	if !home && root.Height >= 800 {
		s.addf("warning", "app-chrome", root, "screen %q has no home indicator; end the screen with the 34px home indicator area, named \"Home indicator\"", root.Name)
	}
	if !material {
		s.addf("warning", "no-material", root, "screen %q is only boxes, text and icons, which reads as a wireframe; give it one thing specific to the subject: map(), chart(), pattern(), a path() illustration, or a real photo (design_canvas photo) placed with image()", root.Name)
	}
	if mixed := mixedLanguage(root); mixed != "" {
		s.addf("warning", "mixed-language", root, "screen %q mixes languages (%s); write every label in the person's language, keeping only names", root.Name, mixed)
	}
	if band != nil {
		s.addf("warning", "web-hero", band, "%q is a full-width colour band %.0fpx tall: that is a website hero. In an app, content sits in inset cards or grouped lists on the screen background, with a large title above", band.Name, band.Height)
	}
}

// brandHeader reports whether a full-width band is an app's brand header:
// it starts at the top edge and the status bar sits on it, as in event,
// city and bank apps. A band lower down is still a web hero.
func (s *systemLinter) brandHeader(n *Node) bool {
	if n.Y > 0.5 {
		return false
	}
	found := false
	Walk(n.Children, func(c *Node, _ *Node, _ int) bool {
		name := strings.ToLower(s.layerName(c))
		if strings.Contains(name, "status bar") || strings.Contains(name, "statusbar") {
			found = true
		}
		return !found
	})
	return found
}

// Words that only one of the two languages uses; a screen with two or more
// of each mixes them. Turkish letters count for Turkish.
var (
	englishWords = setOf("the", "and", "of", "for", "with", "your", "my", "now", "live", "latest", "news", "updates", "schedule", "home", "more", "view", "all", "today", "day", "access", "account", "speakers", "speaker", "map", "venue", "welcome", "see", "badges", "programme", "program", "broadcasts", "lineup")
	turkishWords = setOf("ve", "ile", "bir", "için", "bu", "şu", "an", "gün", "günü", "son", "haberler", "tümü", "daha", "fazla", "ana", "sayfa", "harita", "haritası", "hoş", "geldiniz", "canlı", "yayın", "yayınlar", "konuşmacılar", "program", "programı", "hesabım", "kasım", "sıfır", "atık", "bugün")
)

func setOf(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// mixedLanguage names the English and Turkish words of a screen when it
// uses both, or returns "".
func mixedLanguage(root *Node) string {
	en, tr := map[string]bool{}, map[string]bool{}
	Walk(root.Children, func(n *Node, _ *Node, _ int) bool {
		if !n.IsVisible() {
			return false
		}
		if n.Type != TypeText || n.Text == nil {
			return true
		}
		for _, w := range strings.FieldsFunc(*n.Text, func(r rune) bool {
			return !unicode.IsLetter(r) && r != '\'' && r != '’'
		}) {
			lw := strings.ToLower(w)
			if i := strings.IndexAny(lw, "'’"); i > 0 {
				lw = lw[:i] // Antalya'ya, speaker's: the stem decides.
			}
			switch {
			case turkishWords[lw] && !englishWords[lw]:
				tr[w] = true
			case englishWords[lw] && !turkishWords[lw]:
				en[w] = true
			case strings.ContainsAny(lw, "ğşıçöü") && !englishWords[lw]:
				tr[w] = true
			}
		}
		return true
	})
	if len(en) < 2 || len(tr) < 2 {
		return ""
	}
	return "English: " + strings.Join(firstWords(en, 3), ", ") + "; Turkish: " + strings.Join(firstWords(tr, 3), ", ")
}

func firstWords(m map[string]bool, n int) []string {
	out := make([]string, 0, len(m))
	for w := range m {
		out = append(out, w)
	}
	slices.Sort(out)
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// isMaterial reports whether a layer draws something of the subject rather
// than a box: a picture, a vector that is not an icon, or a layer named as a
// map, chart, illustration or pattern (instances of such components count).
func isMaterial(n *Node, name string) bool {
	if n.Type == TypeImage {
		return true
	}
	for _, f := range n.Fills {
		if f.Type == "image" {
			return true
		}
	}
	if n.Type == TypePath && !strings.HasPrefix(name, "icon") && math.Max(n.Width, n.Height) >= 40 {
		return true
	}
	for _, w := range []string{"map", "chart", "illustration", "pattern", "photo", "graph", "qr", "trail"} {
		if strings.HasPrefix(name, w) || strings.Contains(name, " "+w) || strings.Contains(name, "/"+w) {
			return true
		}
	}
	return false
}

// instanceHasMaterial reports whether an instance's main draws material, so
// a map inside a component counts on the screen that places it.
func (s *systemLinter) instanceHasMaterial(n *Node) bool {
	if n.Type != TypeInstance {
		return false
	}
	m := s.doc.Find(n.ComponentID)
	if m == nil || m.Node == nil {
		return false
	}
	found := false
	Walk(m.Node.Children, func(c *Node, _ *Node, _ int) bool {
		if !found && c.IsVisible() && (isMaterial(c, strings.ToLower(c.Name)) || s.instanceHasMaterial(c)) {
			found = true
		}
		return !found
	})
	return found
}

func (s *systemLinter) layerName(n *Node) string {
	if n.Type == TypeInstance {
		if m := s.doc.Find(n.ComponentID); m != nil && m.Node != nil {
			if m.Node.Set != "" {
				return n.Name + " " + m.Node.Set
			}
			return n.Name + " " + m.Node.Name
		}
	}
	return n.Name
}

func hasSolidFill(n *Node) bool {
	for _, f := range n.Fills {
		if f.Type == "solid" && (f.Visible == nil || *f.Visible) && rgba(f.Color)[3] > 0.5 {
			return true
		}
	}
	return false
}

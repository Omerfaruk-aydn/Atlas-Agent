package design

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// ImportOptions tune ImportHTML.
type ImportOptions struct {
	// Width is the width of the frame the page becomes; 0 means 1280.
	Width float64
	// Name names the root frame.
	Name string
}

// ImportHTML turns HTML with inline styles and <style> rules into a frame of
// design nodes: flexbox becomes auto layout, block flow becomes a vertical
// stack, text and images become text and image nodes. Selectors are simple
// ones (tag, .class, #id, tag.class, lists, *); combinators, media queries
// and pseudo classes are skipped. Margins are not modelled: use gap and
// padding. The returned tree has no ids; adding it with an "add" op gives it
// ids.
func ImportHTML(src string, opts ImportOptions) (*Node, error) {
	if len(src) > 2<<20 {
		return nil, fmt.Errorf("html is larger than 2 MB")
	}
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("html: %w", err)
	}
	im := &importer{vars: map[string]string{}, grow: map[*Node]bool{}}
	im.collect(doc)
	body := findTag(doc, "body")
	if body == nil {
		return nil, fmt.Errorf("html has no body")
	}
	width := opts.Width
	if width <= 0 {
		width = 1280
	}
	root := im.element(body, nil, cssStyle{}, true)
	if root == nil {
		return nil, fmt.Errorf("html has nothing visible to import")
	}
	if root.Type != TypeFrame {
		root = &Node{Type: TypeFrame, Name: "Page", Layout: &Layout{Dir: "column", Main: "start", Cross: "stretch"}, Children: []*Node{root}, Fills: []Paint{{Type: "solid", Color: "#FFFFFF"}}}
	}
	root.Width = width
	root.SizingX, root.SizingY = "", "hug"
	if root.Layout == nil {
		root.Layout = &Layout{Dir: "column", Main: "start", Cross: "stretch"}
	}
	name := opts.Name
	if name == "" {
		if t := findTag(doc, "title"); t != nil && t.FirstChild != nil {
			name = strings.TrimSpace(t.FirstChild.Data)
		}
	}
	if name == "" {
		name = "Imported page"
	}
	root.Name = cut(name, 120)
	if im.count > MaxNodes/2 {
		return nil, fmt.Errorf("html makes more than %d nodes", MaxNodes/2)
	}
	return root, nil
}

type sheetRule struct {
	tag, id string
	classes []string
	spec    int
	order   int
	decls   map[string]string
}

type cssStyle map[string]string

type importer struct {
	rules []sheetRule
	vars  map[string]string
	grow  map[*Node]bool
	count int
}

func findTag(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := findTag(c, tag); f != nil {
			return f
		}
	}
	return nil
}

func (im *importer) collect(n *html.Node) {
	if n.Type == html.ElementNode && n.Data == "style" && n.FirstChild != nil {
		im.parseSheet(n.FirstChild.Data)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		im.collect(c)
	}
}

func stripComments(s string) string {
	for {
		i := strings.Index(s, "/*")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i+2:], "*/")
		if j < 0 {
			return s[:i]
		}
		s = s[:i] + s[i+2+j+2:]
	}
}

func parseDecls(s string) map[string]string {
	out := map[string]string{}
	for _, d := range splitTop(s, ';') {
		k, v, ok := strings.Cut(d, ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "!important"))
		if k != "" && v != "" {
			out[k] = v
		}
	}
	return out
}

// splitTop splits on sep outside parentheses and quotes.
func splitTop(s string, sep byte) []string {
	var out []string
	depth := 0
	var quote byte
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		case c == sep && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func (im *importer) parseSheet(css string) {
	css = stripComments(css)
	for len(css) > 0 {
		open := strings.IndexByte(css, '{')
		if open < 0 {
			return
		}
		sel := strings.TrimSpace(css[:open])
		depth, end := 1, open+1
		for end < len(css) && depth > 0 {
			switch css[end] {
			case '{':
				depth++
			case '}':
				depth--
			}
			end++
		}
		body := css[open+1 : max(open+1, end-1)]
		css = css[min(end, len(css)):]
		if strings.HasPrefix(sel, "@") {
			continue
		}
		decls := parseDecls(body)
		for _, s := range strings.Split(sel, ",") {
			s = strings.TrimSpace(s)
			if s == ":root" || s == "html" {
				for k, v := range decls {
					if strings.HasPrefix(k, "--") {
						im.vars[k] = v
					}
				}
				if s == ":root" {
					continue
				}
			}
			r, ok := parseSelector(s)
			if !ok {
				continue
			}
			r.decls = decls
			r.order = len(im.rules)
			im.rules = append(im.rules, r)
		}
	}
}

// parseSelector reads one compound selector: [tag][#id][.class]...
func parseSelector(s string) (sheetRule, bool) {
	var r sheetRule
	if s == "" || strings.ContainsAny(s, " >+~[:()") {
		return r, false
	}
	if s == "*" {
		return r, true
	}
	i := 0
	for i < len(s) {
		j := i + 1
		for j < len(s) && s[j] != '.' && s[j] != '#' {
			j++
		}
		part := s[i:j]
		switch part[0] {
		case '.':
			r.classes = append(r.classes, part[1:])
			r.spec += 10
		case '#':
			r.id = part[1:]
			r.spec += 100
		default:
			r.tag = strings.ToLower(part)
			r.spec++
		}
		i = j
	}
	return r, true
}

func (r sheetRule) matches(n *html.Node) bool {
	if r.tag != "" && r.tag != n.Data {
		return false
	}
	if r.id != "" && attr(n, "id") != r.id {
		return false
	}
	if len(r.classes) > 0 {
		have := strings.Fields(attr(n, "class"))
	next:
		for _, want := range r.classes {
			for _, h := range have {
				if h == want {
					continue next
				}
			}
			return false
		}
	}
	return true
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

var inherited = []string{"color", "font-family", "font-size", "font-weight", "font-style", "line-height", "text-align", "letter-spacing", "text-decoration", "text-transform"}

var tagDefaults = map[string]map[string]string{
	"h1": {"font-size": "32px", "font-weight": "700"}, "h2": {"font-size": "24px", "font-weight": "700"},
	"h3": {"font-size": "19px", "font-weight": "700"}, "h4": {"font-size": "16px", "font-weight": "700"},
	"h5": {"font-size": "13px", "font-weight": "700"}, "h6": {"font-size": "11px", "font-weight": "700"},
	"b": {"font-weight": "700"}, "strong": {"font-weight": "700"}, "em": {"font-style": "italic"}, "i": {"font-style": "italic"},
	"u": {"text-decoration": "underline"}, "a": {"color": "#0000EE", "text-decoration": "underline"},
	"button": {"display": "flex", "justify-content": "center", "align-items": "center", "padding": "6px 12px", "font-size": "13px"},
	"ul":     {"padding": "0 0 0 24px"}, "ol": {"padding": "0 0 0 24px"},
}

// styleOf cascades defaults, rules (by specificity then order) and the style attribute over the inherited values.
func (im *importer) styleOf(n *html.Node, parent cssStyle) cssStyle {
	st := cssStyle{}
	for _, k := range inherited {
		if v, ok := parent[k]; ok {
			st[k] = v
		}
	}
	for k, v := range tagDefaults[n.Data] {
		st[k] = v
	}
	var hits []sheetRule
	for _, r := range im.rules {
		if r.matches(n) {
			hits = append(hits, r)
		}
	}
	for i := 1; i < len(hits); i++ { // insertion sort: rule counts are small
		for j := i; j > 0 && (hits[j].spec < hits[j-1].spec || hits[j].spec == hits[j-1].spec && hits[j].order < hits[j-1].order); j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
	apply := func(decls map[string]string) {
		resolved := map[string]string{}
		for k, v := range decls {
			if strings.HasPrefix(k, "--") {
				im.vars[k] = v
				continue
			}
			resolved[k] = im.resolveVars(v)
			st[k] = resolved[k]
		}
		expandShorthands(st, resolved)
	}
	for _, r := range hits {
		apply(r.decls)
	}
	apply(parseDecls(attr(n, "style")))
	return st
}

func (im *importer) resolveVars(v string) string {
	for i := 0; i < 4 && strings.Contains(v, "var("); i++ {
		start := strings.Index(v, "var(")
		depth, end := 0, -1
		for j := start + 3; j < len(v); j++ {
			if v[j] == '(' {
				depth++
			} else if v[j] == ')' {
				depth--
				if depth == 0 {
					end = j
					break
				}
			}
		}
		if end < 0 {
			return v
		}
		name, fallback, _ := strings.Cut(v[start+4:end], ",")
		val, ok := im.vars[strings.TrimSpace(name)]
		if !ok {
			val = strings.TrimSpace(fallback)
		}
		v = v[:start] + val + v[end+1:]
	}
	return v
}

// expandShorthands lets the longhands the importer reads follow the shorthands.
func expandShorthands(st cssStyle, decls map[string]string) {
	if v, ok := decls["font"]; ok {
		for _, f := range splitTop(v, ' ') {
			f = strings.TrimSpace(f)
			switch {
			case f == "bold" || f == "bolder" || len(f) == 3 && f[0] >= '1' && f[0] <= '9' && strings.HasSuffix(f, "00"):
				st["font-weight"] = f
			case f == "italic":
				st["font-style"] = "italic"
			case strings.Contains(f, "px") || strings.Contains(f, "rem") || strings.Contains(f, "em"):
				size, _, _ := strings.Cut(f, "/")
				st["font-size"] = size
			}
		}
		if i := strings.LastIndex(v, " "); i >= 0 {
			st["font-family"] = strings.TrimSpace(v[i+1:])
		}
	}
	if v, ok := decls["background"]; ok {
		if strings.Contains(v, "gradient(") || strings.HasPrefix(v, "url(") {
			st["background-image"] = v
		} else {
			st["background-color"] = v
		}
	}
	if v, ok := decls["border-radius"]; ok {
		st["border-radius"] = v
	}
	if v, ok := decls["flex-flow"]; ok {
		if f := strings.Fields(v); len(f) > 0 {
			st["flex-direction"] = f[0]
		}
	}
	if v, ok := decls["flex"]; ok {
		if f := strings.Fields(v); len(f) > 0 && f[0] != "0" && f[0] != "none" {
			st["flex-grow"] = "1"
		}
	}
	if v, ok := decls["gap"]; ok {
		f := strings.Fields(v)
		if len(f) > 0 {
			st["gap"] = f[0]
		}
	}
	for _, key := range []string{"padding", "margin"} {
		if v, ok := decls[key]; ok {
			sides := sideValues(v)
			for i, side := range []string{"top", "right", "bottom", "left"} {
				st[key+"-"+side] = sides[i]
			}
		}
	}
}

func sideValues(v string) [4]string {
	f := strings.Fields(v)
	switch len(f) {
	case 1:
		return [4]string{f[0], f[0], f[0], f[0]}
	case 2:
		return [4]string{f[0], f[1], f[0], f[1]}
	case 3:
		return [4]string{f[0], f[1], f[2], f[1]}
	case 4:
		return [4]string{f[0], f[1], f[2], f[3]}
	}
	return [4]string{}
}

// length reads a CSS length in px; ok is false for auto, percentages and units the importer does not know.
func length(v string, em float64) (float64, bool) {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "0" {
		return 0, true
	}
	for _, u := range []struct {
		suffix string
		mul    float64
	}{{"px", 1}, {"rem", 16}, {"em", em}, {"pt", 4.0 / 3}} {
		if strings.HasSuffix(v, u.suffix) {
			f, err := strconv.ParseFloat(strings.TrimSuffix(v, u.suffix), 64)
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
				return 0, false
			}
			return f * u.mul, true
		}
	}
	return 0, false
}

func percent(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	if !strings.HasSuffix(v, "%") {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64)
	return f, err == nil
}

var namedColors = map[string]string{
	"black": "#000000", "white": "#FFFFFF", "red": "#FF0000", "green": "#008000", "blue": "#0000FF", "yellow": "#FFFF00",
	"gray": "#808080", "grey": "#808080", "orange": "#FFA500", "purple": "#800080", "pink": "#FFC0CB", "silver": "#C0C0C0",
	"navy": "#000080", "teal": "#008080", "lime": "#00FF00", "maroon": "#800000", "cyan": "#00FFFF", "aqua": "#00FFFF", "magenta": "#FF00FF",
}

// parseColor reads a color and its alpha; ok is false for transparent and unknown values.
func parseColor(v string) (hex string, alpha float64, ok bool) {
	v = strings.TrimSpace(strings.ToLower(v))
	if c, found := namedColors[v]; found {
		return c, 1, true
	}
	if strings.HasPrefix(v, "#") {
		h := v[1:]
		if len(h) == 3 || len(h) == 4 {
			var b strings.Builder
			for _, c := range h {
				b.WriteRune(c)
				b.WriteRune(c)
			}
			h = b.String()
		}
		switch len(h) {
		case 6:
			if IsColor("#" + h) {
				return "#" + strings.ToUpper(h), 1, true
			}
		case 8:
			if a, err := strconv.ParseUint(h[6:], 16, 8); err == nil && IsColor("#"+h[:6]) {
				return "#" + strings.ToUpper(h[:6]), float64(a) / 255, true
			}
		}
		return "", 0, false
	}
	if strings.HasPrefix(v, "rgb") && strings.Contains(v, "(") {
		inner := v[strings.Index(v, "(")+1 : strings.LastIndex(v, ")")]
		inner = strings.NewReplacer("/", " ", ",", " ").Replace(inner)
		f := strings.Fields(inner)
		if len(f) < 3 {
			return "", 0, false
		}
		var ch [3]int
		for i := range ch {
			x, err := strconv.ParseFloat(strings.TrimSuffix(f[i], "%"), 64)
			if err != nil {
				return "", 0, false
			}
			if strings.HasSuffix(f[i], "%") {
				x = x * 255 / 100
			}
			ch[i] = int(math.Round(clamp(x, 0, 255)))
		}
		a := 1.0
		if len(f) > 3 {
			x, err := strconv.ParseFloat(strings.TrimSuffix(f[3], "%"), 64)
			if err == nil {
				if strings.HasSuffix(f[3], "%") {
					x /= 100
				}
				a = clamp(x, 0, 1)
			}
		}
		return fmt.Sprintf("#%02X%02X%02X", ch[0], ch[1], ch[2]), a, true
	}
	return "", 0, false
}

func ptr[T any](v T) *T { return &v }

func (im *importer) paints(st cssStyle) []Paint {
	if bg := st["background-image"]; strings.Contains(bg, "linear-gradient(") {
		if p, ok := gradient(bg); ok {
			return []Paint{p}
		}
	}
	if v, ok := st["background-color"]; ok {
		if c, a, ok := parseColor(v); ok && a > 0 {
			p := Paint{Type: "solid", Color: c}
			if a < 1 {
				p.Opacity = ptr(a)
			}
			return []Paint{p}
		}
	}
	return nil
}

// gradient reads linear-gradient([angle|to side,] color [stop%], ...).
func gradient(v string) (Paint, bool) {
	start := strings.Index(v, "linear-gradient(")
	if start < 0 {
		return Paint{}, false
	}
	body := v[start+len("linear-gradient("):]
	if end := strings.LastIndex(body, ")"); end >= 0 {
		body = body[:end]
	}
	parts := splitTop(body, ',')
	angle := 180.0
	first := strings.ToLower(strings.TrimSpace(parts[0]))
	switch {
	case strings.HasSuffix(first, "deg"):
		if f, err := strconv.ParseFloat(strings.TrimSuffix(first, "deg"), 64); err == nil {
			angle = f
		}
		parts = parts[1:]
	case strings.HasPrefix(first, "to "):
		angle = map[string]float64{"to top": 0, "to right": 90, "to bottom": 180, "to left": 270, "to bottom right": 135, "to bottom left": 225, "to top right": 45, "to top left": 315}[first]
		parts = parts[1:]
	}
	var stops []Stop
	for _, part := range parts {
		f := strings.Fields(strings.TrimSpace(part))
		if len(f) == 0 {
			continue
		}
		c := f[0]
		if strings.HasPrefix(c, "rgb") {
			c = strings.TrimSpace(part)
			if i := strings.LastIndex(c, ")"); i >= 0 {
				c = c[:i+1]
			}
		}
		hex, _, ok := parseColor(c)
		if !ok {
			return Paint{}, false
		}
		stops = append(stops, Stop{Color: hex, Offset: -1})
		if len(f) > 1 {
			if p, ok := percent(f[len(f)-1]); ok {
				stops[len(stops)-1].Offset = p / 100
			}
		}
	}
	if len(stops) < 2 {
		return Paint{}, false
	}
	for i := range stops {
		if stops[i].Offset < 0 {
			stops[i].Offset = float64(i) / float64(len(stops)-1)
		}
	}
	return Paint{Type: "linear", Stops: stops, Angle: ptr(angle)}, true
}

func (im *importer) fontOf(st cssStyle) (*Font, float64) {
	size := 16.0
	if v, ok := length(st["font-size"], 16); ok && v > 0 {
		size = v
	}
	weight := 400
	switch w := strings.ToLower(st["font-weight"]); w {
	case "", "normal":
	case "bold", "bolder":
		weight = 700
	default:
		if n, err := strconv.Atoi(w); err == nil {
			weight = max(100, min(900, n/100*100))
		}
	}
	family := "Inter"
	if f := st["font-family"]; f != "" {
		first := strings.Trim(strings.TrimSpace(splitTop(f, ',')[0]), `"'`)
		if first != "" && !strings.HasPrefix(first, "-") && first != "sans-serif" && first != "serif" && first != "system-ui" {
			family = first
		}
	}
	font := &Font{Family: family, Size: size, Weight: weight}
	if st["font-style"] == "italic" {
		font.Style = "italic"
	}
	return font, size
}

// inlineTags are text-level elements flowed into their parent's text.
var inlineTags = map[string]bool{"span": true, "b": true, "strong": true, "em": true, "i": true, "u": true, "a": true, "small": true, "code": true, "label": true, "br": true, "sup": true, "sub": true, "mark": true}

var skipTags = map[string]bool{"script": true, "style": true, "head": true, "meta": true, "link": true, "title": true, "noscript": true, "template": true}

// textOf collects an element's flowed text; ok is false when a block-level child is inside.
func textOf(n *html.Node, sb *strings.Builder) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch {
		case c.Type == html.TextNode:
			sb.WriteString(c.Data)
		case c.Type == html.ElementNode && c.Data == "br":
			sb.WriteString("\n")
		case c.Type == html.ElementNode && inlineTags[c.Data]:
			if !textOf(c, sb) {
				return false
			}
		case c.Type == html.ElementNode && skipTags[c.Data]:
		case c.Type == html.CommentNode:
		default:
			return false
		}
	}
	return true
}

func collapse(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.Join(strings.Fields(l), " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func (im *importer) element(n *html.Node, parent *html.Node, parentStyle cssStyle, root bool) *Node {
	if n.Type != html.ElementNode || skipTags[n.Data] {
		return nil
	}
	im.count++
	if im.count > MaxNodes/2 {
		return nil
	}
	st := im.styleOf(n, parentStyle)
	if st["display"] == "none" || st["visibility"] == "hidden" {
		return nil
	}
	name := n.Data
	if c := strings.Fields(attr(n, "class")); len(c) > 0 {
		name = n.Data + "." + c[0]
	}
	if id := attr(n, "id"); id != "" {
		name = "#" + id
	}
	font, em := im.fontOf(st)
	box := im.box(st, em)

	switch n.Data {
	case "img":
		node := &Node{Type: TypeImage, Name: cut(name, 120), Src: attr(n, "src"), Fit: "", Width: 200, Height: 120, Radius: box.Radius, Effects: box.Effects, Opacity: box.Opacity}
		if node.Src == "" || strings.HasPrefix(node.Src, "http") {
			// A remote image cannot be embedded: keep a labelled placeholder.
			node = &Node{Type: TypeRect, Name: cut(name+" (image)", 120), Width: 200, Height: 120, Radius: box.Radius, Fills: []Paint{{Type: "solid", Color: "#E5E7EB"}}}
		}
		if w, ok := length(firstNonEmpty(st["width"], attr(n, "width")+"px"), em); ok && w > 0 {
			node.Width = w
			node.SizingX = ""
		}
		if h, ok := length(firstNonEmpty(st["height"], attr(n, "height")+"px"), em); ok && h > 0 {
			node.Height = h
		}
		if st["object-fit"] == "contain" {
			node.Fit = "contain"
		}
		return im.place(node, st, parent, em)
	case "svg", "canvas", "video", "iframe":
		node := &Node{Type: TypeRect, Name: cut(name, 120), Width: 100, Height: 100, Fills: []Paint{{Type: "solid", Color: "#E5E7EB"}}}
		if w, ok := length(firstNonEmpty(st["width"], attr(n, "width")+"px"), em); ok && w > 0 {
			node.Width = w
		}
		if h, ok := length(firstNonEmpty(st["height"], attr(n, "height")+"px"), em); ok && h > 0 {
			node.Height = h
		}
		return im.place(node, st, parent, em)
	case "hr":
		node := &Node{Type: TypeRect, Name: "hr", Width: 100, Height: 1, SizingX: "fill", Fills: []Paint{{Type: "solid", Color: "#D1D5DB"}}}
		return im.place(node, st, parent, em)
	}

	var sb strings.Builder
	flows := textOf(n, &sb)
	text := collapse(sb.String())
	styled := len(box.Fills) > 0 || len(box.Strokes) > 0 || box.hasPad || st["display"] == "flex" || st["display"] == "inline-flex" || st["display"] == "grid" || root
	if flows && !styled && n.Data != "button" {
		if text == "" {
			return nil
		}
		return im.place(im.textNode(name, text, st, font), st, parent, em)
	}

	frame := &Node{Type: TypeFrame, Name: cut(name, 120), Fills: box.Fills, Strokes: box.Strokes, Effects: box.Effects, Radius: box.Radius, Opacity: box.Opacity, Layout: im.layout(st, em, box)}
	if st["overflow"] == "hidden" {
		frame.Clip = ptr(true)
	}
	if flows {
		if text != "" {
			frame.Children = append(frame.Children, im.textNode("text", text, st, font))
		}
	} else {
		// Loose text between block children becomes its own text node.
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			switch c.Type {
			case html.TextNode:
				if t := collapse(c.Data); t != "" {
					frame.Children = append(frame.Children, im.textNode("text", t, st, font))
				}
			case html.ElementNode:
				if child := im.element(c, n, st, false); child != nil {
					frame.Children = append(frame.Children, child)
				}
			}
		}
	}
	if len(frame.Children) == 0 && len(frame.Fills) == 0 && len(frame.Strokes) == 0 && !root {
		if h, ok := length(st["height"], em); !ok || h <= 0 {
			return nil
		}
	}
	frame.Children = orEmpty(frame.Children)
	if frame.Layout != nil && frame.Layout.Dir == "row" {
		// In a row, children take their own width unless they grow.
		for _, c := range frame.Children {
			if c.SizingX == "fill" && !im.grow[c] {
				c.SizingX = "hug"
				if c.Type == TypeText {
					c.AutoResize = "width"
				}
			}
		}
	}
	return im.place(frame, st, parent, em)
}

func orEmpty(c []*Node) []*Node {
	if c == nil {
		return []*Node{}
	}
	return c
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" && v != "px" {
			return v
		}
	}
	return ""
}

type boxStyle struct {
	Fills   []Paint
	Strokes []Stroke
	Effects []Effect
	Radius  *Radius
	Opacity *float64
	Pad     Pad
	hasPad  bool
}

func (im *importer) box(st cssStyle, em float64) boxStyle {
	var b boxStyle
	b.Fills = im.paints(st)
	if v := st["border"]; v != "" && v != "none" {
		f := strings.Fields(v)
		w := 1.0
		var col string
		for _, x := range f {
			if l, ok := length(x, em); ok {
				w = l
			} else if x != "solid" && x != "dashed" && x != "dotted" {
				col = x
			}
		}
		if col == "" {
			col = "#000000"
		}
		if c, a, ok := parseColor(col); ok && w > 0 {
			s := Stroke{Color: c, Width: w, Align: "inside"}
			if a < 1 {
				s.Opacity = ptr(a)
			}
			b.Strokes = []Stroke{s}
		}
	}
	if v := st["border-radius"]; v != "" {
		f := strings.Fields(strings.ReplaceAll(v, "/", " "))
		var px [4]float64
		okAll := len(f) > 0
		for i := range px {
			if len(f) == 0 {
				break
			}
			src := f[min(i, len(f)-1)]
			if len(f) == 2 {
				src = f[i%2]
			}
			l, ok := length(src, em)
			if !ok {
				if p, isPct := percent(src); isPct && p >= 50 {
					l, ok = 9999, true
				}
			}
			px[i], okAll = l, okAll && ok
		}
		if okAll {
			if px[0] == px[1] && px[1] == px[2] && px[2] == px[3] {
				b.Radius = &Radius{Corners: px}
			} else {
				b.Radius = &Radius{Corners: px, Each: true}
			}
		}
	}
	if v := st["box-shadow"]; v != "" && v != "none" {
		first := splitTop(v, ',')[0]
		var nums []float64
		var col string
		inset := false
		for _, x := range splitTop(strings.TrimSpace(first), ' ') {
			x = strings.TrimSpace(x)
			if x == "inset" {
				inset = true
			} else if l, ok := length(x, em); ok {
				nums = append(nums, l)
			} else if x != "" {
				col += x
			}
		}
		if len(nums) >= 2 {
			e := Effect{Type: "drop-shadow", X: ptr(nums[0]), Y: ptr(nums[1])}
			if inset {
				e.Type = "inner-shadow"
			}
			if len(nums) > 2 {
				e.Blur = nums[2]
			}
			if len(nums) > 3 {
				e.Spread = ptr(nums[3])
			}
			if c, a, ok := parseColor(col); ok {
				e.Color = c
				if a < 1 {
					e.Color = fmt.Sprintf("%s%02X", c, int(math.Round(a*255)))
				}
			} else {
				e.Color = "#00000040"
			}
			b.Effects = []Effect{e}
		}
	}
	if v := st["opacity"]; v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f < 1 {
			b.Opacity = ptr(clamp(f, 0, 1))
		}
	}
	for i, side := range []string{"top", "right", "bottom", "left"} {
		if l, ok := length(st["padding-"+side], em); ok {
			b.Pad[i] = l
			b.hasPad = b.hasPad || l > 0
		}
	}
	return b
}

func (im *importer) layout(st cssStyle, em float64, box boxStyle) *Layout {
	l := &Layout{Dir: "column", Main: "start", Cross: "stretch", Pad: box.Pad}
	flex := st["display"] == "flex" || st["display"] == "inline-flex"
	if flex {
		if strings.HasPrefix(st["flex-direction"], "row") || st["flex-direction"] == "" {
			l.Dir = "row"
		}
		l.Cross = "start"
		switch st["align-items"] {
		case "center":
			l.Cross = "center"
		case "flex-end", "end":
			l.Cross = "end"
		case "stretch", "":
			if l.Dir == "column" {
				l.Cross = "stretch"
			}
		}
		switch st["justify-content"] {
		case "center":
			l.Main = "center"
		case "flex-end", "end":
			l.Main = "end"
		case "space-between":
			l.Main = "between"
		}
	}
	if g, ok := length(st["gap"], em); ok {
		l.Gap = g
	} else if st["display"] == "grid" {
		l.Dir = "row"
		l.Cross = "start"
	}
	return l
}

func (im *importer) textNode(name, text string, st cssStyle, font *Font) *Node {
	color := "#000000"
	var alpha *float64
	if c, a, ok := parseColor(st["color"]); ok {
		color = c
		if a < 1 {
			alpha = ptr(a)
		}
	}
	node := &Node{Type: TypeText, Name: cut(name, 120), Text: ptr(text), Font: font, Fills: []Paint{{Type: "solid", Color: color, Opacity: alpha}}, AutoResize: "height", SizingX: "fill"}
	if lh := st["line-height"]; lh != "" {
		if l, ok := length(lh, font.Size); ok {
			node.LineHeight = ptr(l)
		} else if f, err := strconv.ParseFloat(lh, 64); err == nil && f > 0 {
			node.LineHeight = ptr(math.Round(f*font.Size*100) / 100)
		}
	}
	if ls, ok := length(st["letter-spacing"], font.Size); ok {
		node.LetterSpacing = ptr(ls)
	}
	switch st["text-align"] {
	case "center":
		node.Align = "center"
	case "right", "end":
		node.Align = "right"
	}
	if strings.Contains(st["text-decoration"], "underline") {
		node.Decoration = "underline"
	} else if strings.Contains(st["text-decoration"], "line-through") {
		node.Decoration = "strike"
	}
	switch st["text-transform"] {
	case "uppercase":
		node.Transform = "upper"
	case "lowercase":
		node.Transform = "lower"
	}
	return node
}

// place applies width, height, flex and absolute positioning to a built node.
func (im *importer) place(n *Node, st cssStyle, parent *html.Node, em float64) *Node {
	if st["position"] == "absolute" || st["position"] == "fixed" {
		n.Absolute = true
		if x, ok := length(st["left"], em); ok {
			n.X = x
		}
		if y, ok := length(st["top"], em); ok {
			n.Y = y
		}
	}
	if w, ok := length(st["width"], em); ok && w > 0 {
		n.Width, n.SizingX = w, ""
		if n.Type == TypeText {
			n.AutoResize = "height"
		}
	} else if p, ok := percent(st["width"]); ok && p >= 99 {
		n.SizingX = "fill"
	} else if parent != nil && (n.Type == TypeFrame || n.Type == TypeText) {
		// Block children stretch across a column; in a row they hug.
		n.SizingX = "fill"
	}
	if st["flex-grow"] == "1" {
		im.grow[n] = true
	}
	if h, ok := length(st["height"], em); ok && h > 0 {
		n.Height, n.SizingY = h, ""
	} else if n.Type == TypeFrame || n.Type == TypeText {
		n.SizingY = "hug"
	}
	if mw, ok := length(st["max-width"], em); ok && mw > 0 && n.SizingX == "fill" && n.Type == TypeFrame {
		n.Width, n.SizingX = mw, ""
		if n.Layout != nil && n.Layout.Cross == "stretch" {
			n.Layout.Cross = "start"
		}
	}
	return n
}

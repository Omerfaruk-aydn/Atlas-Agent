package design

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"sync"
)

// The design kit: themes (colour, type, radius and shadow tokens) and icons
// taken from open source design systems, and blocks built on them. The data
// files are made by kit/gen/generate.mjs; see there for the sources and
// their licenses.

//go:embed kit/themes.json
var themesJSON []byte

//go:embed kit/icons.json.gz
var iconsGz []byte

// TypeToken is one step of a theme's type scale.
type TypeToken struct {
	Family        string  `json:"family"`
	Size          float64 `json:"size"`
	Weight        int     `json:"weight"`
	LineHeight    float64 `json:"line_height"`
	LetterSpacing float64 `json:"letter_spacing,omitempty"`
}

// ThemeRadius is a theme's corner radii, small to extra large.
type ThemeRadius struct {
	SM float64 `json:"sm"`
	MD float64 `json:"md"`
	LG float64 `json:"lg"`
	XL float64 `json:"xl"`
}

// ThemeFonts are a theme's typefaces.
type ThemeFonts struct {
	Heading string `json:"heading"`
	Body    string `json:"body"`
	Mono    string `json:"mono"`
}

// Theme is a complete visual system a design can switch to.
type Theme struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Source   string     `json:"source"`
	Platform string     `json:"platform"`
	Note     string     `json:"note"`
	DarkOnly bool       `json:"dark_only,omitempty"`
	Fonts    ThemeFonts `json:"fonts"`
	// Colors holds each colour token's light and dark value.
	Colors      map[string][2]string `json:"colors"`
	Radius      ThemeRadius          `json:"radius"`
	Type        map[string]TypeToken `json:"type"`
	BorderWidth float64              `json:"border_width,omitempty"`
}

// ThemeRef is the theme a document uses: its id and an optional accent hue.
type ThemeRef struct {
	ID     string `json:"id"`
	Accent string `json:"accent,omitempty"`
}

// ColorTokens are the colour roles every theme defines, in display order.
var ColorTokens = []string{
	"bg", "surface", "card", "muted", "border", "border-strong",
	"text", "text-muted", "text-subtle",
	"accent", "on-accent", "accent-soft", "on-accent-soft",
	"success", "warning", "error", "info", "overlay", "shadow",
}

// TypeSlots are the steps of every theme's type scale, largest first. The
// last four are derived from the others by deriveType.
var TypeSlots = []string{"display", "h1", "h2", "h3", "h4", "title", "lead", "body", "body-strong", "small", "small-strong", "label", "caption", "overline"}

// deriveType adds the emphasis steps a theme's data leaves out: a title for
// cards and list rows, strong body and small text, and a small caps overline.
func deriveType(t *Theme) {
	ty := map[string]TypeToken{}
	for k, v := range t.Type {
		ty[k] = v
	}
	strong := 600
	if t.Platform == "android" {
		strong = 500
	}
	with := func(base TypeToken, weight int) TypeToken {
		base.Weight = weight
		return base
	}
	switch t.Platform {
	case "ios":
		ty["title"] = TypeToken{Family: t.Fonts.Heading, Size: 17, Weight: 600, LineHeight: 22, LetterSpacing: -0.4}
		ty["overline"] = TypeToken{Family: t.Fonts.Body, Size: 13, Weight: 400, LineHeight: 18, LetterSpacing: -0.1}
	case "android":
		ty["title"] = TypeToken{Family: t.Fonts.Heading, Size: 16, Weight: 500, LineHeight: 24, LetterSpacing: 0.15}
		ty["overline"] = TypeToken{Family: t.Fonts.Body, Size: 12, Weight: 500, LineHeight: 16, LetterSpacing: 0.5}
	default:
		lead := ty["lead"]
		ty["title"] = TypeToken{Family: t.Fonts.Heading, Size: lead.Size, Weight: strong, LineHeight: lead.LineHeight - 2}
		ty["overline"] = TypeToken{Family: t.Fonts.Body, Size: 12, Weight: 600, LineHeight: 16, LetterSpacing: 1}
	}
	ty["body-strong"] = with(ty["body"], strong)
	ty["small-strong"] = with(ty["small"], strong)
	t.Type = ty
}

// Accents whose step 9 is light enough to need dark text on it.
var brightAccents = map[string]bool{"amber": true, "yellow": true, "lime": true, "mint": true, "sky": true}

type kitData struct {
	Themes []Theme                        `json:"themes"`
	Radix  map[string]map[string][]string `json:"radix"`
}

var (
	kitOnce sync.Once
	kit     kitData
	kitErr  error
)

func loadKit() (*kitData, error) {
	kitOnce.Do(func() {
		kitErr = json.Unmarshal(themesJSON, &kit)
	})
	return &kit, kitErr
}

// Themes lists the built-in themes.
func Themes() []Theme {
	k, err := loadKit()
	if err != nil {
		return nil
	}
	return k.Themes
}

// Accents lists the hues a theme's accent can be switched to.
func Accents() []string {
	k, err := loadKit()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(k.Radix))
	for name := range k.Radix {
		if !isGrey(name) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func isGrey(name string) bool {
	return slices.Contains([]string{"gray", "mauve", "slate", "sage", "olive", "sand"}, name)
}

// LookupTheme returns the theme with this id, with its accent switched to a
// Radix hue when accent is set.
func LookupTheme(id, accent string) (Theme, error) {
	k, err := loadKit()
	if err != nil {
		return Theme{}, err
	}
	var t Theme
	found := false
	for _, th := range k.Themes {
		if th.ID == id {
			t, found = th, true
			break
		}
	}
	if !found {
		ids := make([]string, len(k.Themes))
		for i, th := range k.Themes {
			ids[i] = th.ID
		}
		return Theme{}, fmt.Errorf("no theme %q; themes: %s", id, strings.Join(ids, ", "))
	}
	colors := make(map[string][2]string, len(t.Colors))
	for key, v := range t.Colors {
		colors[key] = v
	}
	t.Colors = colors
	deriveType(&t)
	if accent == "" {
		ensureContrast(&t)
		return t, nil
	}
	scale := k.Radix[accent]
	if scale == nil || isGrey(accent) {
		return Theme{}, fmt.Errorf("no accent %q; accents: %s", accent, strings.Join(Accents(), ", "))
	}
	l, d := scale["light"], scale["dark"]
	t.Colors["accent"] = [2]string{l[8], d[8]}
	t.Colors["accent-soft"] = [2]string{l[2], d[2]}
	t.Colors["on-accent-soft"] = [2]string{l[10], d[10]}
	if brightAccents[accent] {
		t.Colors["on-accent"] = [2]string{l[11], l[11]}
	} else {
		t.Colors["on-accent"] = [2]string{"#FFFFFF", "#FFFFFF"}
	}
	ensureContrast(&t)
	return t, nil
}

// contrastPairs are the text colours the kit draws on each background, and
// the WCAG ratio they need there.
var contrastPairs = []struct {
	fg   string
	bgs  []string
	need float64
}{
	{"text", []string{"bg", "surface", "card", "muted"}, 4.5},
	{"text-muted", []string{"bg", "surface", "card", "muted"}, 4.5},
	{"on-accent", []string{"accent"}, 4.5},
	{"on-accent-soft", []string{"accent-soft", "bg", "card"}, 4.5},
	{"success", []string{"bg", "card"}, 4.5},
	{"warning", []string{"bg", "card"}, 4.5},
	{"error", []string{"bg", "card"}, 4.5},
	{"info", []string{"bg", "card"}, 4.5},
}

// mixHex moves a colour a share k of the way toward a target colour.
func mixHex(hex string, target [3]float64, k float64) string {
	c := rgba(hex)
	f := func(v, t float64) int { return int((v+(t-v)*k)*255 + 0.5) }
	return fmt.Sprintf("#%02X%02X%02X", f(c[0], target[0]), f(c[1], target[1]), f(c[2], target[2]))
}

// ensureAccent keeps the accent usable as text and as a fill. In light mode a
// mid-tone accent is darkened until it reads on the page and carries white
// text at 4.5:1; in dark mode it is lightened until it reads on the page.
// Bright accents (amber, lime) keep their colour and take dark text instead,
// which ensureContrast then picks.
func ensureAccent(t *Theme) {
	acc := t.Colors["accent"]
	for mode := range 2 {
		orig := acc[mode]
		cur := orig
		target := [3]float64{0, 0, 0}
		if mode == 1 {
			target = [3]float64{1, 1, 1}
		}
		ok := func(c string) bool {
			for _, bg := range []string{"bg", "surface", "card"} {
				if Contrast(c, t.Colors[bg][mode]) < 4.5 {
					return false
				}
			}
			if mode == 0 {
				return Contrast("#FFFFFF", c) >= 4.5
			}
			return true
		}
		if mode == 0 && luminance(rgba(orig)) >= 0.4 {
			continue
		}
		for step := 0; step <= 40 && !ok(cur); step++ {
			cur = mixHex(orig, target, float64(step)*0.025)
		}
		if ok(cur) {
			acc[mode] = cur
		}
	}
	t.Colors["accent"] = acc
}

// ensureContrast darkens (or, on dark backgrounds, lightens) text colours
// that some source themes leave below 4.5:1, so every block stays readable.
func ensureContrast(t *Theme) {
	ensureAccent(t)
	for _, p := range contrastPairs {
		pair := t.Colors[p.fg]
		for mode := range 2 {
			fg := pair[mode]
			for _, bg := range p.bgs {
				bgColor := t.Colors[bg][mode]
				// A translucent wash is drawn over the page, not over white.
				if rgba(bgColor)[3] < 0.99 {
					bgColor = overPage(bgColor, t.Colors["bg"][mode])
				}
				fg = readableOn(fg, bgColor, p.need)
			}
			pair[mode] = fg
		}
		t.Colors[p.fg] = pair
	}
}

// readableOn moves fg toward black or white, whichever lies away from bg,
// until it reaches the ratio; colours that already read are kept as they are.
func readableOn(fg, bg string, need float64) string {
	if Contrast(fg, bg) >= need {
		return fg
	}
	back := blend(rgba(bg), [4]float64{1, 1, 1, 1})
	c := blend(rgba(fg), back)
	target := [4]float64{0, 0, 0, 1}
	if luminance(back) < 0.18 {
		target = [4]float64{1, 1, 1, 1}
	}
	out := fg
	for step := 1; step <= 20; step++ {
		k := float64(step) / 20
		mixed := [4]float64{c[0] + (target[0]-c[0])*k, c[1] + (target[1]-c[1])*k, c[2] + (target[2]-c[2])*k, 1}
		out = fmt.Sprintf("#%02X%02X%02X", int(mixed[0]*255+0.5), int(mixed[1]*255+0.5), int(mixed[2]*255+0.5))
		if Contrast(out, bg) >= need {
			return out
		}
	}
	return out
}

// overPage blends a possibly translucent colour over an opaque page
// colour and returns the opaque result.
func overPage(color, page string) string {
	base := rgba(page)
	base[3] = 1
	return opaqueHex(blend(rgba(color), base))
}

// DefaultTheme is the theme a platform's blocks use when the design has none.
func DefaultTheme(platform string) string {
	switch platform {
	case "ios":
		return "ios"
	case "android":
		return "material"
	default:
		return "zinc"
	}
}

// ThemeOf returns the document's theme, or false when it has none.
func ThemeOf(d *Doc) (Theme, bool) {
	if d.Theme == nil {
		return Theme{}, false
	}
	if d.Theme.ID == "custom" && d.Custom != nil {
		return *d.Custom, true
	}
	t, err := LookupTheme(d.Theme.ID, d.Theme.Accent)
	return t, err == nil
}

// modeIndex is 0 for a light mode and 1 for a dark one.
func modeIndex(d *Doc) int {
	if d.Variables != nil && strings.EqualFold(d.Variables.Mode, "dark") {
		return 1
	}
	return 0
}

// Variable and style ids the theme owns.
func colorVar(token string) string { return "c-" + token }
func textStyle(slot string) string { return "t-" + slot }
func effectStyle(level string) string {
	return "e-" + level
}

// shadows are a theme's three elevation levels.
func (t Theme) shadows() map[string][]Effect {
	base := t.Colors["shadow"][0]
	rgb := strings.ToUpper(base)
	if len(rgb) == 9 {
		rgb = rgb[:7]
	}
	f := func(v float64) *float64 { return &v }
	return map[string][]Effect{
		"sm": {
			{Type: "drop-shadow", X: f(0), Y: f(1), Blur: 2, Spread: f(0), Color: rgb + "14"},
			{Type: "drop-shadow", X: f(0), Y: f(1), Blur: 3, Spread: f(0), Color: rgb + "0F"},
		},
		"md": {
			{Type: "drop-shadow", X: f(0), Y: f(2), Blur: 4, Spread: f(-1), Color: rgb + "0F"},
			{Type: "drop-shadow", X: f(0), Y: f(8), Blur: 16, Spread: f(-4), Color: rgb + "1F"},
		},
		"lg": {
			{Type: "drop-shadow", X: f(0), Y: f(4), Blur: 8, Spread: f(-2), Color: rgb + "14"},
			{Type: "drop-shadow", X: f(0), Y: f(24), Blur: 48, Spread: f(-12), Color: rgb + "33"},
		},
	}
}

// setTheme switches the document to a theme: colour variables for light and
// dark, a text style per type step and three shadow styles. Layers made from
// the kit point at these, so they all follow.
func (d *Doc) setTheme(op Op) error {
	var t Theme
	var err error
	if op.Color != "" {
		t, err = ThemeFromBrand(op.Color)
	} else {
		t, err = LookupTheme(op.Name, op.Accent)
	}
	if err != nil {
		return err
	}
	if d.Variables == nil {
		d.Variables = &Variables{Items: map[string]*Variable{}}
	}
	v := d.Variables
	if v.Items == nil {
		v.Items = map[string]*Variable{}
	}
	for _, m := range []string{"Light", "Dark"} {
		if !slices.Contains(v.Modes, m) {
			v.Modes = append(v.Modes, m)
		}
	}
	switch {
	case op.Mode != "":
		if !slices.Contains(v.Modes, op.Mode) {
			return fmt.Errorf("mode %q is not one of the modes %s", op.Mode, strings.Join(v.Modes, ", "))
		}
		v.Mode = op.Mode
	case t.DarkOnly:
		v.Mode = "Dark"
	case v.Mode != "Light" && v.Mode != "Dark":
		v.Mode = "Light"
	}
	for _, token := range ColorTokens {
		pair := t.Colors[token]
		values := map[string]string{}
		for _, m := range v.Modes {
			values[m] = pair[0]
			if m == "Dark" {
				values[m] = pair[1]
			}
		}
		v.Items[colorVar(token)] = &Variable{Name: "Color/" + token, Values: values}
	}
	if d.Styles == nil {
		d.Styles = &Styles{}
	}
	if d.Styles.Text == nil {
		d.Styles.Text = map[string]*TextStyle{}
	}
	for _, slot := range TypeSlots {
		tok := t.Type[slot]
		lh, ls := tok.LineHeight, tok.LetterSpacing
		style := &TextStyle{Name: "Text/" + slot, Font: Font{Family: tok.Family, Size: tok.Size, Weight: tok.Weight}, LineHeight: &lh}
		if ls != 0 {
			style.LetterSpacing = &ls
		}
		d.Styles.Text[textStyle(slot)] = style
	}
	if d.Styles.Effects == nil {
		d.Styles.Effects = map[string]*EffectStyle{}
	}
	for level, effects := range t.shadows() {
		d.Styles.Effects[effectStyle(level)] = &EffectStyle{Name: "Shadow/" + level, Effects: effects}
	}
	d.Theme = &ThemeRef{ID: t.ID, Accent: op.Accent}
	if t.ID == "custom" {
		d.Custom = &t
	} else {
		d.Custom = nil
	}
	// Text measures by its own font, so copy the styles onto the text that
	// uses them, and the active mode's colours onto bound paints.
	mode := modeIndex(d)
	for _, p := range d.Pages {
		Walk(p.Children, func(n *Node, _ *Node, _ int) bool {
			if s := d.Styles.Text[n.TextStyle]; s != nil && n.Type == TypeText && strings.HasPrefix(n.TextStyle, "t-") {
				f := s.Font
				n.Font = &f
				n.LineHeight, n.LetterSpacing = s.LineHeight, s.LetterSpacing
			}
			for i := range n.Fills {
				if c, ok := themeColor(t, n.Fills[i].Variable, mode); ok {
					n.Fills[i].Color = c
				}
			}
			for i := range n.Strokes {
				if c, ok := themeColor(t, n.Strokes[i].Variable, mode); ok {
					n.Strokes[i].Color = c
				}
			}
			return true
		})
	}
	return nil
}

func themeColor(t Theme, variable string, mode int) (string, bool) {
	token, ok := strings.CutPrefix(variable, "c-")
	if !ok {
		return "", false
	}
	pair, ok := t.Colors[token]
	if !ok {
		return "", false
	}
	return pair[mode], true
}

// Icon is one icon of the kit's set (Lucide, ISC license): stroked outlines
// in a 24×24 box.
type Icon struct {
	Name string   `json:"name"`
	D    string   `json:"d"`
	Tags []string `json:"tags"`
}

var (
	iconOnce sync.Once
	icons    map[string]Icon
	iconList []string
	iconErr  error
)

func loadIcons() (map[string]Icon, error) {
	iconOnce.Do(func() {
		zr, err := gzip.NewReader(bytes.NewReader(iconsGz))
		if err != nil {
			iconErr = err
			return
		}
		data, err := io.ReadAll(zr)
		if err != nil {
			iconErr = err
			return
		}
		raw := map[string]Icon{}
		if err := json.Unmarshal(data, &raw); err != nil {
			iconErr = err
			return
		}
		icons = map[string]Icon{}
		for name, ic := range raw {
			ic.Name = name
			icons[name] = ic
			iconList = append(iconList, name)
		}
		sort.Strings(iconList)
	})
	return icons, iconErr
}

// LookupIcon returns the icon with this name.
func LookupIcon(name string) (Icon, error) {
	all, err := loadIcons()
	if err != nil {
		return Icon{}, err
	}
	ic, ok := all[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		hint := ""
		if found := SearchIcons(name, 6); len(found) > 0 {
			names := make([]string, len(found))
			for i, f := range found {
				names[i] = f.Name
			}
			hint = "; close matches: " + strings.Join(names, ", ")
		}
		return Icon{}, fmt.Errorf("no icon %q%s", name, hint)
	}
	return ic, nil
}

// SearchIcons finds icons by name and tag, best matches first.
func SearchIcons(query string, limit int) []Icon {
	all, err := loadIcons()
	if err != nil {
		return nil
	}
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil
	}
	type hit struct {
		name  string
		score int
	}
	var hits []hit
	for _, name := range iconList {
		ic := all[name]
		score := 0
		for _, w := range words {
			switch {
			case name == w:
				score += 100
			case strings.HasPrefix(name, w):
				score += 40
			case strings.Contains(name, w):
				score += 20
			case slices.Contains(ic.Tags, w):
				score += 15
			case slices.ContainsFunc(ic.Tags, func(t string) bool { return strings.Contains(t, w) }):
				score += 5
			default:
				score = -1000
			}
		}
		if score > 0 {
			hits = append(hits, hit{name, score - len(name)/4})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Icon, len(hits))
	for i, h := range hits {
		out[i] = all[h.name]
	}
	return out
}

// IconCount is the number of icons in the set.
func IconCount() int {
	all, _ := loadIcons()
	return len(all)
}

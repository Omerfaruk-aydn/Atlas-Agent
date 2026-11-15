package design

import "strings"

// kitFonts maps a family a design names to the name the bundled
// @font-face declares and the generic family to fall back to. It mirrors
// app/renderer/src/features/design/kit-fonts.ts: a script that says
// "Fraunces" must render in Fraunces, not in the browser's sans.
var kitFonts = map[string][2]string{
	"inter":             {"Inter Variable", "sans-serif"},
	"albert sans":       {"Albert Sans Variable", "sans-serif"},
	"dm sans":           {"DM Sans Variable", "sans-serif"},
	"figtree":           {"Figtree Variable", "sans-serif"},
	"fraunces":          {"Fraunces Variable", "serif"},
	"fustat":            {"Fustat Variable", "sans-serif"},
	"geist":             {"Geist Variable", "sans-serif"},
	"montserrat":        {"Montserrat Variable", "sans-serif"},
	"outfit":            {"Outfit Variable", "sans-serif"},
	"playwrite us trad": {"Playwrite US Trad Variable", "cursive"},
	"roboto":            {"Roboto Variable", "sans-serif"},
	"jetbrains mono":    {"JetBrains Mono Variable", "monospace"},
	"poppins":           {"Poppins", "sans-serif"},
}

// KitFamilies are the typefaces the renderer has, by the name a design
// uses.
var KitFamilies = []string{"Albert Sans", "DM Sans", "Figtree", "Fraunces", "Fustat", "Geist", "Inter", "JetBrains Mono", "Montserrat", "Outfit", "Playwrite US Trad", "Poppins", "Roboto"}

// FontStack is the CSS font-family list for a family: the bundled face
// first, then the name as written, then a generic family of the same kind.
func FontStack(family string) string {
	name := strings.TrimSpace(family)
	if name == "" {
		name = "Inter"
	}
	key := strings.ToLower(strings.TrimSuffix(name, " Variable"))
	kit, ok := kitFonts[key]
	if !ok {
		return name + ", Inter Variable, sans-serif"
	}
	if kit[0] == name {
		return name + ", " + kit[1]
	}
	return kit[0] + ", " + name + ", " + kit[1]
}

// KitFont reports whether the renderer draws family in its own face.
func KitFont(family string) bool {
	_, ok := kitFonts[strings.ToLower(strings.TrimSuffix(strings.TrimSpace(family), " Variable"))]
	return ok
}

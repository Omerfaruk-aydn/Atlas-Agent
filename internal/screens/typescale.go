package screens

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Named ratios of a modular scale, as typographers use them.
var scaleRatios = []struct {
	Name  string
	Ratio float64
}{
	{"minor second", 1.067},
	{"major second", 1.125},
	{"minor third", 1.2},
	{"major third", 1.25},
	{"perfect fourth", 1.333},
	{"augmented fourth", 1.414},
	{"perfect fifth", 1.5},
	{"golden", 1.618},
}

func ratioNamed(s string) (float64, bool) {
	s = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "-", " "))
	s = strings.ReplaceAll(s, "_", " ")
	for _, r := range scaleRatios {
		if r.Name == s {
			return r.Ratio, true
		}
	}
	return 0, false
}

func nextRatio(r float64) float64 {
	for _, x := range scaleRatios {
		if x.Ratio > r+0.001 {
			return x.Ratio
		}
	}
	return r
}

// TypeScale builds a modular type scale. spec is a list of key=value words:
// base (px, default 16), ratio (number or name, default major third), neg and
// pos (steps below and above the base, default 2 and 6), and for a fluid
// scale in the manner of Utopia: fluid=320-1240, max_base, max_ratio. Sizes
// are rounded to half pixels; the result also gives line-heights.
func TypeScale(spec string) (string, error) {
	base, ratio := 16.0, 1.25
	neg, pos := 2, 6
	maxBase, maxRatio := 0.0, 0.0
	minVW, maxVW := 0.0, 0.0
	for _, kv := range strings.Fields(strings.ReplaceAll(spec, ",", " ")) {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			// A bare number is the base, a bare name the ratio.
			if f, err := strconv.ParseFloat(kv, 64); err == nil {
				base = f
			}
			continue
		}
		switch strings.ToLower(k) {
		case "base":
			base, _ = strconv.ParseFloat(v, 64)
		case "ratio":
			if r, ok := ratioNamed(strings.ReplaceAll(v, "_", " ")); ok {
				ratio = r
			} else if f, err := strconv.ParseFloat(v, 64); err == nil {
				ratio = f
			} else {
				return "", fmt.Errorf("ratio %q: use a number or one of %s", v, ratioNames())
			}
		case "neg":
			neg, _ = strconv.Atoi(v)
		case "pos":
			pos, _ = strconv.Atoi(v)
		case "max_base":
			maxBase, _ = strconv.ParseFloat(v, 64)
		case "max_ratio":
			maxRatio, _ = strconv.ParseFloat(v, 64)
		case "fluid":
			lo, hi, ok := strings.Cut(v, "-")
			if !ok {
				return "", fmt.Errorf("fluid=%s: write the viewport range as fluid=320-1240", v)
			}
			minVW, _ = strconv.ParseFloat(lo, 64)
			maxVW, _ = strconv.ParseFloat(hi, 64)
		}
	}
	if base < 8 || base > 40 || ratio < 1.01 || ratio > 2 {
		return "", fmt.Errorf("base must be 8 to 40 px and ratio 1.01 to 2 (e.g. base=17 ratio=1.2 or ratio=major-third)")
	}
	neg, pos = clamp(neg, 0, 4), clamp(pos, 1, 9)
	half := func(v float64) float64 { return math.Round(v*2) / 2 }

	var b strings.Builder
	fluid := minVW > 0 && maxVW > minVW
	if fluid {
		if maxBase == 0 {
			maxBase = base + 2
		}
		if maxRatio == 0 {
			maxRatio = nextRatio(ratio)
		}
		fmt.Fprintf(&b, "Fluid scale: %gpx at %gpx wide, ratio %g, to %gpx at %gpx wide, ratio %g.\n", base, minVW, ratio, maxBase, maxVW, maxRatio)
	} else {
		fmt.Fprintf(&b, "Modular scale: base %gpx, ratio %g.\n", base, ratio)
	}
	b.WriteString("step   size    line-height   use\n")
	for i := pos; i >= -neg; i-- {
		size := half(base * math.Pow(ratio, float64(i)))
		line := lineHeightFor(size)
		use := stepUse(i)
		if fluid {
			hi := half(maxBase * math.Pow(maxRatio, float64(i)))
			slope := (hi - size) / (maxVW - minVW)
			inter := size - slope*minVW
			fmt.Fprintf(&b, "%+d   %g to %gpx   %-8g   clamp(%.4grem, %.4grem + %.4gvw, %.4grem)   %s\n", i, size, hi, line, size/16, inter/16, slope*100, hi/16, use)
			continue
		}
		fmt.Fprintf(&b, "%+d   %gpx   %-8g   %s\n", i, size, line, use)
	}
	b.WriteString("\nPick five to seven steps and name them by role in DESIGN.md typography (display, title, heading, body, meta...). A system that uses every step has no hierarchy; skip steps so neighbouring roles differ by at least one full step. Native platforms keep their own ramps (see design_system platform): use this scale for web and for brand display type.\n")
	return b.String(), nil
}

func ratioNames() string {
	var n []string
	for _, r := range scaleRatios {
		n = append(n, strings.ReplaceAll(r.Name, " ", "-"))
	}
	sort.Strings(n)
	return strings.Join(n, ", ")
}

func clamp(v, lo, hi int) int { return max(lo, min(hi, v)) }

func lineHeightFor(size float64) float64 {
	switch {
	case size <= 14:
		return 1.5
	case size <= 20:
		return 1.4
	case size <= 32:
		return 1.25
	case size <= 56:
		return 1.1
	}
	return 1.02
}

func stepUse(i int) string {
	switch {
	case i <= -2:
		return "caption, legal"
	case i == -1:
		return "meta, labels"
	case i == 0:
		return "body"
	case i == 1:
		return "lead, control, row title"
	case i == 2:
		return "heading"
	case i == 3:
		return "title"
	case i <= 5:
		return "display"
	}
	return "hero display"
}

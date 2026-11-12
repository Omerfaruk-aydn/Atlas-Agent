package design

import (
	"fmt"
	"math"
	"strings"
)

// ThemeFromBrand builds a full theme from one colour. Text colours are
// then repaired so they clear 4.5:1 on the surfaces they sit on.
func ThemeFromBrand(hex string) (Theme, error) {
	if !IsColor(hex) || len(strings.TrimPrefix(hex, "#")) < 6 {
		return Theme{}, fmt.Errorf("color %q is not a #RRGGBB colour", hex)
	}
	base, err := LookupTheme("zinc", "")
	if err != nil {
		return Theme{}, err
	}
	base.ID = "custom"
	base.Name = "Custom"
	base.Source = "Brand colour"
	base.Note = "Derived from " + strings.ToUpper(hex) + "."
	base.Platform = "web"
	base.DarkOnly = false
	base.Colors = brandRamp(strings.ToUpper(hex))
	ensureContrast(&base)
	return base, nil
}

func brandRamp(hex string) map[string][2]string {
	c := rgba(hex)
	h, s, _ := rgbToHSL(c[0], c[1], c[2])
	if s < 0.15 {
		s = 0.15
	}
	light := func(sat, l float64) string { return hslHex(h, sat, l) }
	soft := func(solid string) string {
		if len(solid) >= 7 {
			return solid[:7] + "33"
		}
		return solid
	}
	accentL := hslHex(h, math.Min(0.85, s+0.1), 0.42)
	accentD := hslHex(h, math.Min(0.8, s+0.05), 0.68)
	return map[string][2]string{
		"bg":             {light(0.06, 0.98), light(0.08, 0.09)},
		"surface":        {light(0.05, 0.96), light(0.07, 0.12)},
		"card":           {"#FFFFFF", light(0.06, 0.15)},
		"muted":          {light(0.06, 0.94), light(0.05, 0.18)},
		"border":         {light(0.05, 0.88), light(0.04, 0.24)},
		"border-strong":  {light(0.06, 0.72), light(0.04, 0.36)},
		"text":           {light(0.08, 0.12), light(0.06, 0.96)},
		"text-muted":     {light(0.05, 0.38), light(0.04, 0.72)},
		"text-subtle":    {light(0.04, 0.55), light(0.03, 0.58)},
		"accent":         {accentL, accentD},
		"on-accent":      {"#FFFFFF", "#101014"},
		"accent-soft":    {soft(accentL), soft(accentD)},
		"on-accent-soft": {light(0.08, 0.12), light(0.06, 0.96)},
		"success":        {"#1F7A45", "#7DCEA0"},
		"warning":        {"#8A5A00", "#E6C36A"},
		"error":          {"#B42318", "#F0A8A2"},
		"info":           {accentL, accentD},
		"overlay":        {"#10101466", "#00000099"},
		"shadow":         {"#101014", "#000000"},
	}
}

func rgbToHSL(r, g, b float64) (h, s, l float64) {
	max := math.Max(r, math.Max(g, b))
	min := math.Min(r, math.Min(g, b))
	l = (max + min) / 2
	d := max - min
	if d < 1e-9 {
		return 0, 0, l
	}
	if l > 0.5 {
		s = d / (2 - max - min)
	} else {
		s = d / (max + min)
	}
	switch max {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	return h / 6, s, l
}

func hslHex(h, s, l float64) string {
	var r, g, b float64
	if s == 0 {
		r, g, b = l, l, l
	} else {
		var q float64
		if l < 0.5 {
			q = l * (1 + s)
		} else {
			q = l + s - l*s
		}
		p := 2*l - q
		r = hue(p, q, h+1.0/3)
		g = hue(p, q, h)
		b = hue(p, q, h-1.0/3)
	}
	ch := func(v float64) int {
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		return int(v*255 + 0.5)
	}
	return fmt.Sprintf("#%02X%02X%02X", ch(r), ch(g), ch(b))
}

func hue(p, q, t float64) float64 {
	if t < 0 {
		t++
	}
	if t > 1 {
		t--
	}
	switch {
	case t < 1.0/6:
		return p + (q-p)*6*t
	case t < 1.0/2:
		return q
	case t < 2.0/3:
		return p + (q-p)*(2.0/3-t)*6
	default:
		return p
	}
}

package browser

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Field value semantics decide which differences between the requested and
// the accepted value are formatting and which are a different value. Text
// is compared exactly: case, inner spacing and line breaks are content.
const (
	semanticText    = "text"
	semanticNumber  = "number"
	semanticColor   = "color"
	semanticBoolean = "boolean"
)

// colorFieldName recognizes text inputs that edit a color, such as a design
// tool's "Fill" or "Stroke" hex field. Only these get hex normalization; a
// hex-looking value in any other text field is ordinary text.
var colorFieldName = regexp.MustCompile(`(?i)\b(colou?r|fill|stroke|hex|background)\b`)

// fieldSemantic maps a field kind (from fields.js) and its accessible name
// to the comparison it gets.
func fieldSemantic(kind, name string) string {
	switch kind {
	case "checkbox", "radio", "custom_switch", "custom_checkbox", "custom_radio":
		return semanticBoolean
	case "native_color":
		return semanticColor
	case "number", "native_range", "custom_spinbutton", "custom_slider":
		return semanticNumber
	case "text":
		if colorFieldName.MatchString(name) {
			return semanticColor
		}
	}
	return semanticText
}

// CompareFieldValue classifies an accepted value against the request and
// the value that was there before, comparing text exactly: the same text is
// applied, the old value surviving is not applied, anything else is a value
// the application chose (adjusted).
func CompareFieldValue(requested, accepted, before string) string {
	return compareFieldAs(semanticText, requested, accepted, before)
}

// CompareTypedFieldValue compares with the semantics of the field kind and
// name: numbers keep their units and convert only within one dimension
// (1s = 1000ms, 12pt = 16px; never px and %), colors normalize hex notation
// only in color fields, and booleans compare their truth value.
func CompareTypedFieldValue(kind, name, requested, accepted, before string) string {
	return compareFieldAs(fieldSemantic(kind, name), requested, accepted, before)
}

// CompareSelectOption compares a native select's chosen option, given by its
// visible label and its value, with a request naming either one.
func CompareSelectOption(requested, label, value, before string) string {
	switch {
	case label == requested:
		return FieldApplied
	case value != "" && value == requested:
		return FieldNormalized
	case strings.EqualFold(strings.TrimSpace(label), strings.TrimSpace(requested)) || (value != "" && strings.EqualFold(value, strings.TrimSpace(requested))):
		return FieldNormalized
	case label == before:
		return FieldNotApplied
	}
	return FieldAdjusted
}

func compareFieldAs(semantic, requested, accepted, before string) string {
	if accepted == requested {
		return FieldApplied
	}
	if equivalentFieldAs(semantic, requested, accepted) {
		return FieldNormalized
	}
	if accepted == before || equivalentFieldAs(semantic, accepted, before) {
		return FieldNotApplied
	}
	return FieldAdjusted
}

func equivalentFieldAs(semantic, a, b string) bool {
	switch semantic {
	case semanticColor:
		ha, oka := hexColor(a)
		hb, okb := hexColor(b)
		return oka && okb && ha == hb
	case semanticNumber:
		qa, oka := parseQuantity(a)
		qb, okb := parseQuantity(b)
		if !oka || !okb || qa.dimension != qb.dimension {
			return false
		}
		return math.Abs(qa.value-qb.value) <= 1e-9*math.Max(1, math.Max(math.Abs(qa.value), math.Abs(qb.value)))
	case semanticBoolean:
		ba, oka := parseFieldBool(a)
		bb, okb := parseFieldBool(b)
		return oka && okb && ba == bb
	}
	return false
}

func hexColor(s string) (string, bool) {
	s = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "#"))
	if len(s) != 3 && len(s) != 6 && len(s) != 8 {
		return "", false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return "", false
		}
	}
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) == 8 && strings.HasSuffix(s, "ff") {
		s = s[:6]
	}
	return s, true
}

type fieldQuantity struct {
	value     float64
	dimension string
}

var quantityPattern = regexp.MustCompile(`^([+-]?(?:\d+(?:[.,]\d+)?|[.,]\d+))\s*([a-zA-Z%°]*)$`)

// quantityUnits converts each unit to a base unit of its dimension. Only
// fixed CSS ratios are listed; em and rem depend on the font and stay
// separate dimensions, and a bare number is its own dimension.
var quantityUnits = map[string]struct {
	dimension string
	factor    float64
}{
	"":     {"unitless", 1},
	"px":   {"length", 1},
	"pt":   {"length", 4.0 / 3},
	"pc":   {"length", 16},
	"in":   {"length", 96},
	"cm":   {"length", 96 / 2.54},
	"mm":   {"length", 96 / 25.4},
	"%":    {"percent", 1},
	"deg":  {"angle", 1},
	"°":    {"angle", 1},
	"rad":  {"angle", 180 / math.Pi},
	"turn": {"angle", 360},
	"ms":   {"time", 1},
	"s":    {"time", 1000},
	"em":   {"em", 1},
	"rem":  {"rem", 1},
}

func parseQuantity(s string) (fieldQuantity, bool) {
	m := quantityPattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return fieldQuantity{}, false
	}
	unit, ok := quantityUnits[strings.ToLower(m[2])]
	if !ok {
		return fieldQuantity{}, false
	}
	f, err := strconv.ParseFloat(strings.Replace(m[1], ",", ".", 1), 64)
	if err != nil {
		return fieldQuantity{}, false
	}
	return fieldQuantity{value: f * unit.factor, dimension: unit.dimension}, true
}

func parseFieldBool(s string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "on", "1", "yes", "checked":
		return true, true
	case "false", "off", "0", "no", "unchecked":
		return false, true
	}
	return false, false
}

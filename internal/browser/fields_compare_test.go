package browser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCompareFieldValueDoesNotInventEquivalence is the regression for
// values treated as equal after units, case or spacing were thrown away.
func TestCompareFieldValueDoesNotInventEquivalence(t *testing.T) {
	t.Parallel()
	// Generic text compares exactly.
	for _, c := range []struct{ requested, accepted string }{
		{"100px", "100%"},
		{"1s", "1ms"},
		{"SecretA", "secreta"},
		{"a  b", "a b"},
		{"line1\nline2", "line1 line2"},
		{"abc", "ABC"},
		{"F7F3EA", "f7f3ea"},
		{"#abc", "#aabbcc"},
		{"100", "100.0"},
		{"007", "7"},
	} {
		require.Equal(t, FieldAdjusted, CompareFieldValue(c.requested, c.accepted, "old"), "%q vs %q", c.requested, c.accepted)
	}
	require.Equal(t, FieldApplied, CompareFieldValue("Exact Text", "Exact Text", "old"))
	require.Equal(t, FieldNotApplied, CompareFieldValue("new", "old", "old"))
	require.Equal(t, FieldAdjusted, CompareFieldValue("new", "OLD", "old"), "a case change of the old value is still a different value")
}

func TestCompareTypedFieldValueBySemantics(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind, name, requested, accepted, want string
	}{
		// Numbers keep their units; only same-dimension conversions count.
		{"number", "Width", "100", "100.0", FieldNormalized},
		{"number", "Width", "100", "100", FieldApplied},
		{"text", "Width", "100px", "100%", FieldAdjusted},
		{"number", "Delay", "1s", "1ms", FieldAdjusted},
		{"number", "Delay", "1s", "1000ms", FieldNormalized},
		{"number", "Width", "100px", "100", FieldAdjusted},
		{"number", "Angle", "90deg", "90°", FieldNormalized},
		{"number", "Width", "12pt", "16px", FieldNormalized},
		{"number", "Width", "1em", "16px", FieldAdjusted},
		{"custom_spinbutton", "Gap", "8", "8", FieldApplied},
		// Colors normalize only in color fields.
		{"text", "Fill color", "f7f3ea", "F7F3EA", FieldNormalized},
		{"text", "Fill", "#abc", "AABBCC", FieldNormalized},
		{"native_color", "Accent", "#AABBCC", "#aabbcc", FieldNormalized},
		{"text", "Stroke", "#aabbccff", "aabbcc", FieldNormalized},
		{"text", "Fill color", "f7f3ea", "f7f3eb", FieldAdjusted},
		{"text", "Build id", "f7f3ea", "F7F3EA", FieldAdjusted},
		{"text", "Commit", "abc", "ABC", FieldAdjusted},
		// Booleans.
		{"checkbox", "Accept", "true", "true", FieldApplied},
		{"checkbox", "Accept", "on", "true", FieldNormalized},
		{"checkbox", "Accept", "yes", "false", FieldAdjusted},
		{"custom_switch", "Dark", "false", "false", FieldApplied},
		// Text keeps case, inner spacing and line breaks.
		{"text", "Name", "SecretA", "secreta", FieldAdjusted},
		{"textarea", "Notes", "a\nb", "a b", FieldAdjusted},
		{"contenteditable", "Body", "two  spaces", "two spaces", FieldAdjusted},
	}
	for _, c := range cases {
		require.Equal(t, c.want, CompareTypedFieldValue(c.kind, c.name, c.requested, c.accepted, "\x00"), "%s %q: %q vs %q", c.kind, c.name, c.requested, c.accepted)
	}
	// The old value surviving is not applied, under the same semantics.
	require.Equal(t, FieldNotApplied, CompareTypedFieldValue("number", "Width", "200", "100.0", "100"))
	require.Equal(t, FieldNotApplied, CompareTypedFieldValue("text", "Fill color", "000000", "ffffff", "FFFFFF"))
}

func TestCompareSelectOptionValueAndLabel(t *testing.T) {
	t.Parallel()
	require.Equal(t, FieldApplied, CompareSelectOption("Large", "Large", "l", "Small"))
	require.Equal(t, FieldNormalized, CompareSelectOption("l", "Large", "l", "Small"), "the option value names the same choice")
	require.Equal(t, FieldNormalized, CompareSelectOption("large", "Large", "l", "Small"))
	require.Equal(t, FieldNotApplied, CompareSelectOption("Large", "Small", "s", "Small"))
	require.Equal(t, FieldAdjusted, CompareSelectOption("Large", "Medium", "m", "Small"))
}

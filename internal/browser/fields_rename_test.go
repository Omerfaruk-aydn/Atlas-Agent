package browser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRenamedFieldNameReplacesOnlyTheValue: a field named by its value
// ("Font size: 12 px") is looked up under the committed value. A name that
// does not hold the old value exactly once is not guessed at.
func TestRenamedFieldNameReplacesOnlyTheValue(t *testing.T) {
	t.Parallel()
	require.Equal(t, "Font size: 16 px", RenamedFieldName("Font size: 12 px", "12", "16"))
	require.Empty(t, RenamedFieldName("Width", "12", "16"))
	require.Empty(t, RenamedFieldName("12 by 12", "12", "16"))
	require.Empty(t, RenamedFieldName("Font size: 12 px", "12", "12"))
}

// TestSameSelectionIgnoresNodeCounterOnlyWithAppIDs: a re-rendered layer
// row is the same object when the application's id matches; without
// application ids a different node is a different selection.
func TestSameSelectionIgnoresNodeCounterOnlyWithAppIDs(t *testing.T) {
	t.Parallel()
	require.True(t, SameSelection([]string{`["n1","1:2"]row`}, []string{`["n7","1:2"]row`}))
	require.False(t, SameSelection([]string{`["n1","1:2"]row`}, []string{`["n7","1:3"]row`}))
	require.False(t, SameSelection([]string{`["n1",""]row`}, []string{`["n7",""]row`}))
	require.False(t, SameSelection([]string{`["n1","1:2"]row`}, nil))
}

// TestShownModeConfirms: Figma's W field keeps the number after "Fill
// container" and shows "Fill" beside it. Only a newly shown first word of
// the option counts; unchanged or unrelated text does not.
func TestShownModeConfirms(t *testing.T) {
	t.Parallel()
	require.True(t, ShownModeConfirms("", "Fill", "Fill container"))
	require.True(t, ShownModeConfirms("163", "254 Fill", "Fill container"))
	// Adjacent spans join without a space ("Fill⌄").
	require.True(t, ShownModeConfirms("⌄", "Fill⌄", "Fill container"))
	require.False(t, ShownModeConfirms("Fill", "Fill", "Fill container"))
	require.False(t, ShownModeConfirms("Hug", "Fixed", "Fill container"))
	require.False(t, ShownModeConfirms("", "Filled", "Fill container"))
	require.False(t, ShownModeConfirms("", "Fill", ""))
}

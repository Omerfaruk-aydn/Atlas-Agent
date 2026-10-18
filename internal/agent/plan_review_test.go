package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSummaryNoteOnlyWithAWorkPlan(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.Empty(t, workPlanSummaryNote(root))
	writeWorkPlan(t, root, `{"goal":"g","features":[{"id":"F1","title":"a"}]}`)
	note := workPlanSummaryNote(root)
	require.Contains(t, note, "## Work plan")
	require.Contains(t, note, "do not copy its features")
	require.Contains(t, note, "exact last failing command")
}

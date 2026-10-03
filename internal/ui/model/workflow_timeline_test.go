package model

import (
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func TestTimelineSortsEventsAndBoundsDetails(t *testing.T) {
	t.Parallel()
	p := workflowPanel{timeline: true, snapshot: engineering.WorkflowSnapshot{Operations: []engineering.Operation{{ID: "old", Tool: "bash", Status: "failed", StartedAt: 1}}, Checks: []engineering.Check{{Name: "recent", RunID: "new", CheckedAt: 2, Evidence: strings.Repeat("界", 200)}}}}
	entries := workflowTimeline(p.snapshot)
	require.Equal(t, "new/recent", entries[0].ID)
	p.snapshot.Checkpoints = []engineering.Checkpoint{{ID: "checkpoint", Stage: 2}}
	entries = workflowTimeline(p.snapshot)
	require.Len(t, entries, 3)
	require.Equal(t, "checkpoint", entries[2].ID)
	for _, details := range []bool{false, true} {
		p.details = details
		text := p.render(30, 8)
		require.LessOrEqual(t, len(strings.Split(text, "\n")), 8)
		for _, line := range strings.Split(text, "\n") {
			require.LessOrEqual(t, ansi.StringWidth(line), 30)
		}
	}
}

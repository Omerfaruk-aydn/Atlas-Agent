package model

import (
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agentstate"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func TestPlatformViewsRenderBoundedUnicodeAndEvidence(t *testing.T) {
	t.Parallel()
	s := engineering.WorkflowSnapshot{PlatformJobs: []agentstate.Job{{ID: "watch", Kind: "heartbeat", MaxRuns: 10, Prompt: strings.Repeat("Türkçe界", 100)}}, PlatformTasks: []agentstate.Task{{ID: "task", Title: "Review", Status: "review", Worker: "atlas", Evidence: []string{"proof.txt"}, Acceptance: []string{"passes"}}}, SourceMemories: []agentstate.Memory{{ID: "fact", Text: "Go", Status: "stale", SessionID: "origin"}}}
	views := projectWorkflow(s)
	require.Contains(t, views[workflowDurableBoard][0].Details, "Evidence: proof.txt")
	require.Contains(t, views[workflowSourceMemory][0].Label, "stale")
	p := workflowPanel{snapshot: s, views: views, open: true}
	for _, tab := range []int{workflowAutomation, workflowDurableBoard, workflowSourceMemory} {
		p.tab = tab
		for _, width := range []int{1, 8, 40, 120} {
			text := p.render(width, 12)
			require.LessOrEqual(t, len(strings.Split(text, "\n")), 12)
			for _, line := range strings.Split(text, "\n") {
				require.LessOrEqual(t, ansi.StringWidth(line), width)
			}
		}
	}
}

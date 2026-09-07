package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSetSummarizeOptionsUpdatesLiveState verifies that a chat-driven
// config change (atlas_config) to auto_summarize_at, disable_auto_summarize,
// or the "compact" model role takes effect on an already-running session's
// next turn, instead of only after the agent is rebuilt from scratch. See
// coordinator.UpdateModels, which calls this after every config reload.
func TestSetSummarizeOptionsUpdatesLiveState(t *testing.T) {
	agent := NewSessionAgent(SessionAgentOptions{
		AutoSummarizeAt: 0.25,
	})
	sa, ok := agent.(*sessionAgent)
	require.True(t, ok)

	require.InDelta(t, 0.25, sa.autoSummarizeAt.Get(), 0)
	require.False(t, sa.disableAutoSummarize.Get())
	require.Nil(t, sa.compactModel.Get().model)

	newCompact := &Model{}
	agent.SetSummarizeOptions(0.5, true, newCompact)

	require.InDelta(t, 0.5, sa.autoSummarizeAt.Get(), 0)
	require.True(t, sa.disableAutoSummarize.Get())
	require.Same(t, newCompact, sa.compactModel.Get().model)

	// Switching the compact role back off (nil) must also take effect live.
	agent.SetSummarizeOptions(0.5, true, nil)
	require.Nil(t, sa.compactModel.Get().model)
}

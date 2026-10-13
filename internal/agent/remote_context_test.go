package agent

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// The block is regenerated each step: an old one (from before a summary or
// a reconnect) is replaced, never kept next to the new one.
func TestWithRemoteContextReplacesTheOldBlock(t *testing.T) {
	t.Parallel()
	msgs := []fantasy.Message{fantasy.NewSystemMessage("system"), fantasy.NewUserMessage("chrome u aç")}
	step1 := withRemoteContext(msgs, "session_id=ss-1 generation=1")
	require.Len(t, step1, 3)
	step2 := withRemoteContext(step1, "session_id=ss-2 generation=2")
	require.Len(t, step2, 3)
	last := step2[2].Content[0].(fantasy.TextPart).Text
	require.Contains(t, last, "ss-2")
	require.NotContains(t, last, "ss-1")
	require.Len(t, withRemoteContext(step2, ""), 2, "disconnected: no block")
}

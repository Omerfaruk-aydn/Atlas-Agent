package hooks

import (
	"context"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAgentActionIsBoundedAndCannotGrantPermissions(t *testing.T) {
	t.Parallel()
	calls := 0
	r := NewRunner([]config.HookConfig{{Agent: "review", Prompt: "inspect", MaxFires: 1}}, t.TempDir(), t.TempDir()).WithAgentAction(func(ctx context.Context, h config.HookConfig, id string, payload []byte) HookResult {
		calls++
		return HookResult{Decision: DecisionAllow, Context: "observed", UpdatedInput: `{"changed":true}`}
	})
	first, err := r.Run(t.Context(), EventPreToolUse, "session", "edit", "{}")
	require.NoError(t, err)
	require.Equal(t, "observed", first.Context)
	require.Equal(t, DecisionNone, first.Decision)
	require.Empty(t, first.UpdatedInput)
	second, err := r.Run(t.Context(), EventPreToolUse, "session", "edit", "{}")
	require.NoError(t, err)
	require.Empty(t, second.Context)
	require.Equal(t, 1, calls)
}

package agent

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/hooks"
	"github.com/stretchr/testify/require"
)

func TestRequiredPreHookFailureDoesNotDispatchTool(t *testing.T) {
	t.Parallel()
	inner := &fakeTool{name: "bash", resp: fantasy.NewTextResponse("must not run")}
	pre := hooks.NewRunner([]config.HookConfig{{Command: "exit 1", Required: true}}, t.TempDir(), t.TempDir())
	resp, err := newHookedTool(inner, pre, nil).Run(t.Context(), fantasy.ToolCall{ID: "required-pre", Name: "bash", Input: `{}`})
	require.NoError(t, err)
	require.False(t, inner.called)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "Required hook failed")
	require.Contains(t, resp.Metadata, `"required_failure":true`)
}

func TestRequiredPostHookFailureStopsTurnWithoutClaimingRollback(t *testing.T) {
	t.Parallel()
	inner := &fakeTool{name: "bash", resp: fantasy.NewTextResponse("effect completed")}
	post := hooks.NewRunner([]config.HookConfig{{Command: "exit 1", Required: true}}, t.TempDir(), t.TempDir())
	resp, err := newHookedTool(inner, nil, post).Run(t.Context(), fantasy.ToolCall{ID: "required-post", Name: "bash", Input: `{}`})
	require.NoError(t, err)
	require.True(t, inner.called)
	require.True(t, resp.IsError)
	require.True(t, resp.StopTurn)
	require.Contains(t, resp.Content, "effect completed")
	require.Contains(t, resp.Content, "cannot be rolled back")
}

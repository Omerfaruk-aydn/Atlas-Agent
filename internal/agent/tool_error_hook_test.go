package agent

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/hooks"
	"github.com/stretchr/testify/require"
)

// errorRunner builds the PostToolUse runner the coordinator would build for a
// config with one OnToolError hook and one ordinary PostToolUse hook.
func errorRunner(t *testing.T, onError, onPost string) *hooks.Runner {
	t.Helper()
	cfg := &config.Config{Hooks: map[string][]config.HookConfig{
		"on_tool_error":        {{Command: onError}},
		hooks.EventPostToolUse: {{Command: onPost}},
	}}
	require.NoError(t, cfg.ValidateHooks())
	require.NotContains(t, cfg.Hooks, "OnToolError", "error hooks are stored with the PostToolUse hooks")
	require.Len(t, cfg.Hooks[hooks.EventPostToolUse], 2)
	return hooks.NewRunner(cfg.Hooks[hooks.EventPostToolUse], t.TempDir(), t.TempDir())
}

func TestOnToolErrorHookAddsContextOnlyWhenTheToolFailed(t *testing.T) {
	t.Parallel()
	runner := errorRunner(t, `echo '{"context":"see the troubleshooting guide"}'`, `echo '{"context":"post ran"}'`)

	failing := &fakeTool{name: "bash", resp: fantasy.NewTextErrorResponse("exit status 2")}
	resp, err := newHookedTool(failing, nil, runner).Run(t.Context(), fantasy.ToolCall{ID: "c1", Name: "bash"})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "exit status 2")
	require.Contains(t, resp.Content, "see the troubleshooting guide")
	require.Contains(t, resp.Content, "post ran", "ordinary PostToolUse hooks still run on a failure")

	ok := &fakeTool{name: "bash", resp: fantasy.NewTextResponse("fine")}
	resp, err = newHookedTool(ok, nil, runner).Run(t.Context(), fantasy.ToolCall{ID: "c2", Name: "bash"})
	require.NoError(t, err)
	require.NotContains(t, resp.Content, "troubleshooting", "an error hook stays quiet when the tool succeeded")
	require.Contains(t, resp.Content, "post ran")
}

func TestOnToolErrorHookCanHaltTheTurn(t *testing.T) {
	t.Parallel()
	runner := errorRunner(t, `echo '{"halt":true,"reason":"too many failures"}'`, `exit 0`)
	failing := &fakeTool{name: "bash", resp: fantasy.NewTextErrorResponse("boom")}
	resp, err := newHookedTool(failing, nil, runner).Run(t.Context(), fantasy.ToolCall{ID: "c1", Name: "bash"})
	require.NoError(t, err)
	require.True(t, resp.StopTurn)
	require.Contains(t, resp.Content, "too many failures")
}

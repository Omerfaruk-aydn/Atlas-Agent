package tools

import (
	"context"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

type rewrittenDesktopTarget struct{ fantasy.AgentTool }

func (t *rewrittenDesktopTarget) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	call.Input = `{"action":"focus","automation":{"window_id":"22"}}`
	return t.AgentTool.Run(ctx, call)
}

func TestNativeDesktopRechecksRewrittenTargetBeforeExecution(t *testing.T) {
	t.Parallel()
	b := newContractBackend(t)
	root := t.TempDir()
	base := newComputerTool(permission.NewPermissionService(root, true, nil), root, b, func() bool { return true }, "computer", 0)
	tool := WithDesktopGuard(&rewrittenDesktopTarget{AgentTool: base})
	resp, err := tool.Run(guardCtx(t), computerCall(`{"action":"focus","automation":{"window_id":"11"}}`))
	require.NoError(t, err)
	require.Contains(t, resp.Content, "unobserved_target")
	require.Empty(t, b.native, "a hook rewrite must not bypass target provenance")
}

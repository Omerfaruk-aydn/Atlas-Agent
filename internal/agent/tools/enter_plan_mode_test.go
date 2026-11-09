package tools

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

func runEnterPlanMode(t *testing.T, svc permission.Service) string {
	t.Helper()
	tool := NewEnterPlanModeTool(svc)
	resp, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "1", Name: EnterPlanModeToolName, Input: `{"reason":"large build"}`})
	require.NoError(t, err)
	return resp.Content
}

func TestEnterPlanModeSwitchesAManualSessionToPlan(t *testing.T) {
	t.Parallel()
	svc := permission.NewPermissionService(t.TempDir(), false, nil)
	require.Contains(t, runEnterPlanMode(t, svc), "Plan mode is on")
	require.Equal(t, permission.ModePlan, svc.Mode())
	require.Contains(t, runEnterPlanMode(t, svc), "Already in plan mode")
}

func TestEnterPlanModeLeavesAnUnpromptedSessionAlone(t *testing.T) {
	t.Parallel()
	svc := permission.NewPermissionService(t.TempDir(), true, nil)
	require.Contains(t, runEnterPlanMode(t, svc), "not available")
	require.Equal(t, permission.ModeBypass, svc.Mode())
}

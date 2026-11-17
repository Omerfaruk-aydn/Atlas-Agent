package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

func TestExitPlanModeRefusesAPlanNamingMissingFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	svc := permission.NewPermissionService(dir, false, nil)
	svc.SetMode(permission.ModePlan)
	tool := NewExitPlanModeTool(svc, dir)
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "s1")
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "1", Name: ExitPlanModeToolName, Input: `{"plan":"[MODIFY] ghost.go"}`})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "ghost.go")
	require.Equal(t, permission.ModePlan, svc.Mode())
}

func TestExitPlanModeApprovalModes(t *testing.T) {
	t.Parallel()
	run := func(mode, plan string) (permission.PermissionMode, string) {
		dir := t.TempDir()
		svc := permission.NewPermissionService(dir, false, nil)
		svc.SetMode(permission.ModePlan)
		tool := NewExitPlanModeTool(svc, dir, func() string { return mode })
		ctx := context.WithValue(t.Context(), SessionIDContextKey, "s1")
		in, err := json.Marshal(ExitPlanModeParams{Plan: plan})
		require.NoError(t, err)
		resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "1", Name: ExitPlanModeToolName, Input: string(in)})
		require.NoError(t, err)
		return svc.Mode(), resp.Content
	}

	// A small plan proceeds in auto mode without any prompt.
	mode, out := run("auto", "[NEW] a.go\n[NEW] b.go\nRe-plan if:\n- the API changes\n")
	require.Equal(t, permission.ModeManual, mode)
	require.Contains(t, out, "plan_approval: auto")

	// turbo skips the prompt for any lint-clean plan, even a free-form one.
	mode, out = run("turbo", "just do it")
	require.Equal(t, permission.ModeManual, mode)
	require.Contains(t, out, "turbo")

	// A bad plan is still refused in turbo mode.
	mode, out = run("turbo", "[MODIFY] ghost.go")
	require.Equal(t, permission.ModePlan, mode)
	require.Contains(t, out, "ghost.go")
}

func TestExitPlanModeReturnsTheUsersChangeRequest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	svc := permission.NewPermissionService(dir, false, nil)
	svc.SetMode(permission.ModePlan)
	tool := NewExitPlanModeTool(svc, dir)
	events := svc.Subscribe(t.Context())
	go func() {
		for ev := range events {
			permission.SetDenyFeedback(ev.Payload.ToolCallID, "also cover the Windows path")
			svc.Deny(ev.Payload)
			return
		}
	}()
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "s1")
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "call-9", Name: ExitPlanModeToolName, Input: `{"plan":"do it"}`})
	require.NoError(t, err)
	require.Contains(t, resp.Content, "requested changes")
	require.Contains(t, resp.Content, "also cover the Windows path")
	require.Equal(t, permission.ModePlan, svc.Mode())
	require.Empty(t, permission.TakeDenyFeedback("call-9"), "the comment is consumed")
}

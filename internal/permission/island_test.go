package permission

import (
	"context"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	"github.com/stretchr/testify/require"
)

type islandTestRenderer struct{}

func (islandTestRenderer) Render(activity.Event) {}
func (islandTestRenderer) Close()                {}

func requestFromIsland(t *testing.T, decision activity.PermissionDecision) (bool, *activity.Manager, Service) {
	t.Helper()
	m := activity.New(islandTestRenderer{})
	t.Cleanup(m.Close)
	ctx, end := activity.StartFlow(context.Background(), "island-session")
	t.Cleanup(end)
	m.Begin(ctx, activity.Event{Session: "island-session", Resource: "browser"})()
	svc := NewPermissionService(t.TempDir(), false, nil)
	type result struct {
		granted bool
		err     error
	}
	done := make(chan result, 1)
	go func() {
		granted, err := svc.Request(ctx, CreatePermissionRequest{SessionID: "island-session", ToolCallID: "call", ToolName: "browser", Action: "click", Description: "Click browser element: Buy", Path: t.TempDir()})
		done <- result{granted, err}
	}()
	require.Eventually(t, func() bool { return m.Snapshot().Prompt != nil }, 2*time.Second, time.Millisecond)
	e := m.Snapshot()
	require.Equal(t, activity.StateAwaitPermission, e.State)
	require.True(t, e.Visible)
	require.Equal(t, "browser", e.Prompt.Permission.Tool)
	require.Equal(t, "click", e.Prompt.Permission.Action)
	select {
	case <-done:
		t.Fatal("The pending action ran before a decision")
	case <-time.After(30 * time.Millisecond):
	}
	require.NoError(t, m.Respond(e.Prompt.Revision, activity.PromptResponse{Decision: decision}))
	r := <-done
	require.NoError(t, r.err)
	require.ErrorIs(t, m.Respond(e.Prompt.Revision, activity.PromptResponse{Decision: activity.DecisionAllowOnce}), activity.ErrPromptStale)
	return r.granted, m, svc
}

func TestIslandGrantResolvesThroughPermissionService(t *testing.T) {
	granted, m, _ := requestFromIsland(t, activity.DecisionAllowOnce)
	require.True(t, granted)
	require.Equal(t, activity.StateResuming, m.Snapshot().State)
}

func TestIslandDenialIsFinal(t *testing.T) {
	granted, m, _ := requestFromIsland(t, activity.DecisionDeny)
	require.False(t, granted)
	require.Equal(t, activity.StateDenied, m.Snapshot().State)
}

func TestIslandSessionGrantIsRememberedLikeTheTerminal(t *testing.T) {
	granted, _, svc := requestFromIsland(t, activity.DecisionAllowSession)
	require.True(t, granted)
	s := svc.(*permissionService)
	found := false
	s.sessionPermissions.Seq2()(func(key PermissionKey, _ bool) bool {
		found = key.SessionID == "island-session" && key.ToolName == "browser" && key.Action == "click"
		return !found
	})
	require.True(t, found)
}

func TestIslandPromptNeverShowsTypedContentOrSecrets(t *testing.T) {
	typed := islandPrompt(PermissionRequest{ToolName: "browser", Action: "type", Description: `Type into browser element: hunter2`})
	require.Empty(t, typed.Permission.Detail)
	for raw, hidden := range map[string]string{
		"Navigate browser to: https://user:pw@example.com/login?token=abc#frag":     "abc",
		"curl -H 'Authorization: Bearer sk-live-0123456789abcdef' https://api.test": "sk-live",
		"deploy --password hunter2 now":                                             "hunter2",
		"export API_KEY=AKIAABCDEFGHIJKLMNOP":                                       "AKIA",
		"use ghp_0123456789abcdefghijklmnopqrstuvwxyzABCD":                          "ghp_",
	} {
		shown := RedactForDisplay(raw)
		require.NotContains(t, shown, hidden, raw)
	}
	require.Equal(t, "Navigate browser to: https://example.com/login?•••", RedactForDisplay("Navigate browser to: https://user:pw@example.com/login?token=abc"))
	require.Equal(t, "Edit internal/activity/renderer_windows.go", RedactForDisplay("Edit internal/activity/renderer_windows.go"))
}

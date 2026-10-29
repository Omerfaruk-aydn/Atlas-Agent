package permission

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScopedGrantOnce(t *testing.T) {
	root := t.TempDir()
	service := NewPermissionService(root, false, nil).(*permissionService)
	require.NoError(t, service.GrantScoped("s", "bash", "execute", root, GrantOnce))
	req := CreatePermissionRequest{SessionID: "s", ToolName: "bash", Action: "execute", Path: root}
	granted, err := service.Request(WithoutPrompts(t.Context()), req)
	require.NoError(t, err)
	require.True(t, granted)
	granted, err = service.Request(WithoutPrompts(t.Context()), req)
	require.ErrorIs(t, err, ErrPromptUnavailable)
	require.False(t, granted)
}

func TestScopedGrantDoesNotLeakAcrossScope(t *testing.T) {
	root := t.TempDir()
	service := NewPermissionService(root, false, nil).(*permissionService)
	require.NoError(t, service.GrantScoped("s", "bash", "execute", root, GrantTurn))
	for _, req := range []CreatePermissionRequest{
		{SessionID: "other", ToolName: "bash", Action: "execute", Path: root},
		{SessionID: "s", ToolName: "write", Action: "execute", Path: root},
		{SessionID: "s", ToolName: "bash", Action: "other", Path: root},
		{SessionID: "s", ToolName: "bash", Action: "execute", Path: t.TempDir()},
	} {
		granted, err := service.Request(WithoutPrompts(t.Context()), req)
		require.ErrorIs(t, err, ErrPromptUnavailable)
		require.False(t, granted)
	}
	granted, err := service.Request(WithoutPrompts(t.Context()), CreatePermissionRequest{SessionID: "s", ToolName: "bash", Action: "execute", Path: root})
	require.NoError(t, err)
	require.True(t, granted)
}

func TestScopedGrantValidation(t *testing.T) {
	service := NewPermissionService(t.TempDir(), false, nil).(*permissionService)
	require.Error(t, service.GrantScoped("", "bash", "execute", "", GrantTurn))
	require.Error(t, service.GrantScoped("s", "bash", "execute", "", "forever"))
	require.NoError(t, service.GrantScoped("s", "bash", "execute", "", GrantSession))
}

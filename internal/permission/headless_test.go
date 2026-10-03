package permission

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHeadlessPermissionsKeepGrantsAndRejectPrompts(t *testing.T) {
	t.Parallel()
	denied := NewPermissionService(t.TempDir(), false, nil)
	granted, err := denied.Request(WithoutPrompts(t.Context()), CreatePermissionRequest{SessionID: "session", ToolCallID: "tool", ToolName: "bash", Action: "execute", Path: "."})
	require.ErrorIs(t, err, ErrPromptUnavailable)
	require.False(t, granted)
	allowed := NewPermissionService(t.TempDir(), false, []string{"bash:execute"})
	granted, err = allowed.Request(WithoutPrompts(t.Context()), CreatePermissionRequest{SessionID: "session", ToolCallID: "tool", ToolName: "bash", Action: "execute", Path: "."})
	require.NoError(t, err)
	require.True(t, granted)
}

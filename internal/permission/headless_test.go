package permission

import (
	"os"
	"path/filepath"
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

func TestLoadHeadlessPolicyIsStrict(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "policy.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"allowed_tools":["view","bash:execute"]}`), 0o600))
	policy, err := LoadHeadlessPolicy(path)
	require.NoError(t, err)
	require.Equal(t, []string{"view", "bash:execute"}, policy.AllowedTools)
	for _, invalid := range []string{`{"allowed_tools":["*"]}`, `{"allowed_tools":[""]}`, `{"allowed_tools":[],"unknown":true}`, `{"allowed_tools":[]} {}`} {
		require.NoError(t, os.WriteFile(path, []byte(invalid), 0o600))
		_, err = LoadHeadlessPolicy(path)
		require.Error(t, err, invalid)
	}
}

package tools

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

func TestRehearseToolPreviewsThenAppliesWithoutTouchingTreeFirst(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("old"), 0o644))
	perms := &recordingPermissions{Service: permission.NewPermissionService(root, true, nil)}
	tool := NewRehearseTool(root, t.TempDir(), perms)
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "rehearse")

	response, err := tool.Run(ctx, fantasy.ToolCall{ID: "1", Input: `{"task":"edit","files":[{"path":"a.txt","content":"new"},{"path":"sub/b.txt","content":"b"}],"checks":["test -f sub/b.txt"]}`})
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	require.Contains(t, response.Content, "verdict: ok")
	require.Contains(t, response.Content, "sub/b.txt")
	require.Equal(t, "execute", perms.requests[0].Action)

	data, err := os.ReadFile(filepath.Join(root, "a.txt"))
	require.NoError(t, err)
	require.Equal(t, "old", string(data))

	id := regexp.MustCompile(`rh-[0-9]{8}-[0-9]{6}-[0-9a-f]{6}`).FindString(response.Content)
	require.NotEmpty(t, id)
	response, err = tool.Run(ctx, fantasy.ToolCall{ID: "2", Input: `{"action":"apply","id":"` + id + `"}`})
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	require.Equal(t, "write", perms.requests[1].Action)
	data, err = os.ReadFile(filepath.Join(root, "a.txt"))
	require.NoError(t, err)
	require.Equal(t, "new", string(data))
}

func TestRehearseToolHonoursDeniedPermission(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	perms := &recordingPermissions{Service: permission.NewPermissionService(root, true, nil)}
	perms.deny = func(permission.CreatePermissionRequest) bool { return true }
	tool := NewRehearseTool(root, t.TempDir(), perms)
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "rehearse")

	response, err := tool.Run(ctx, fantasy.ToolCall{ID: "1", Input: `{"files":[{"path":"a.txt","content":"x"}]}`})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.NoFileExists(t, filepath.Join(root, "a.txt"))
}

func TestRehearseToolRejectsEmptyPlanAndPathEscapes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tool := NewRehearseTool(root, t.TempDir(), permission.NewPermissionService(root, true, nil))
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "rehearse")

	response, err := tool.Run(ctx, fantasy.ToolCall{ID: "1", Input: `{}`})
	require.NoError(t, err)
	require.True(t, response.IsError)

	response, err = tool.Run(ctx, fantasy.ToolCall{ID: "2", Input: `{"files":[{"path":"../escape.txt","content":"x"}]}`})
	require.NoError(t, err)
	require.Contains(t, response.Content, "verdict: failed")
	require.NoFileExists(t, filepath.Join(filepath.Dir(root), "escape.txt"))
}

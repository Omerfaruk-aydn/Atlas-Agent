package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/speech"
	"github.com/stretchr/testify/require"
)

type videoPermissionRecorder struct {
	mockPermissionService
	request permission.CreatePermissionRequest
	granted bool
}

func (p *videoPermissionRecorder) Request(_ context.Context, r permission.CreatePermissionRequest) (bool, error) {
	p.request = r
	return p.granted, nil
}

func TestVideoToolRejectsUnsupportedActionsAndModels(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "video.mp4")
	require.NoError(t, os.WriteFile(path, []byte("placeholder"), 0o600))
	tool := NewVideoTool(root, nil, speech.DictationOptions{})
	response, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "test", Input: `{"file_path":"video.mp4","action":"delete"}`})
	require.NoError(t, err)
	require.True(t, response.IsError)
	response, err = tool.Run(t.Context(), fantasy.ToolCall{ID: "test", Input: `{"file_path":"video.mp4"}`})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, "supports images")
}

func TestVideoOutsideWorkspaceRequiresReadPermission(t *testing.T) {
	t.Parallel()
	root, outside := t.TempDir(), t.TempDir()
	path := filepath.Join(outside, "external.mp4")
	require.NoError(t, os.WriteFile(path, []byte("placeholder"), 0o600))
	perms := &videoPermissionRecorder{}
	tool := NewVideoTool(root, perms, speech.DictationOptions{})
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "test-session")
	input, err := json.Marshal(VideoParams{FilePath: path, Action: "inspect"})
	require.NoError(t, err)
	response, err := tool.Run(ctx, fantasy.ToolCall{ID: "video-call", Input: string(input)})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Equal(t, "video", perms.request.ToolName)
	require.Equal(t, "read", perms.request.Action)
	require.Equal(t, "test-session", perms.request.SessionID)
	require.Equal(t, "video-call", perms.request.ToolCallID)
}

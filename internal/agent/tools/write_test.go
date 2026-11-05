package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

type denyWritePermission struct{ mockPermissionService }

func (*denyWritePermission) Request(context.Context, permission.CreatePermissionRequest) (bool, error) {
	return false, nil
}

type changeWritePermission struct {
	mockPermissionService
	change func() error
}

func (p *changeWritePermission) Request(context.Context, permission.CreatePermissionRequest) (bool, error) {
	return true, p.change()
}

type mockFileTrackerService struct{}

func (m mockFileTrackerService) RecordRead(ctx context.Context, sessionID, path string) {}

func (m mockFileTrackerService) LastReadTime(ctx context.Context, sessionID, path string) time.Time {
	return time.Now()
}

func (m mockFileTrackerService) ListReadFiles(ctx context.Context, sessionID string) ([]string, error) {
	return nil, nil
}

func TestWriteToolWritesEmptyNewFile(t *testing.T) {
	t.Parallel()

	workingDir := t.TempDir()
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "test-session")

	tool := NewWriteTool(nil, &mockPermissionService{}, &mockHistoryService{}, mockFileTrackerService{}, workingDir, PathPolicy{})

	input, err := json.Marshal(WriteParams{FilePath: "empty.txt", Content: ""})
	require.NoError(t, err)

	resp, err := tool.Run(ctx, fantasy.ToolCall{
		ID:    "test-call",
		Name:  WriteToolName,
		Input: string(input),
	})
	require.NoError(t, err)
	require.False(t, resp.IsError)

	b, err := os.ReadFile(filepath.Join(workingDir, "empty.txt"))
	require.NoError(t, err)
	require.Equal(t, "", string(b))
}

func TestWriteToolDenialDoesNotCreateParentDirectories(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ctx := context.WithValue(t.Context(), SessionIDContextKey, t.Name())
	tool := NewWriteTool(nil, &denyWritePermission{}, &mockHistoryService{}, mockFileTrackerService{}, root, PathPolicy{})
	input, err := json.Marshal(WriteParams{FilePath: "new/nested/file.txt", Content: "content"})
	require.NoError(t, err)
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "denied", Name: WriteToolName, Input: string(input)})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	_, err = os.Stat(filepath.Join(root, "new"))
	require.True(t, os.IsNotExist(err))
}

func TestWriteToolConfinesPathAfterPermission(t *testing.T) {
	t.Parallel()
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "probe")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	require.NoError(t, os.Remove(filepath.Join(root, "probe")))
	require.NoError(t, os.Mkdir(filepath.Join(root, "sub"), 0o755))
	p := &changeWritePermission{change: func() error {
		if err := os.Rename(filepath.Join(root, "sub"), filepath.Join(root, "saved")); err != nil {
			return err
		}
		return os.Symlink(outside, filepath.Join(root, "sub"))
	}}
	ctx := context.WithValue(t.Context(), SessionIDContextKey, t.Name())
	tool := NewWriteTool(nil, p, &mockHistoryService{}, mockFileTrackerService{}, root, PathPolicy{Root: root, Restrict: true})
	input, err := json.Marshal(WriteParams{FilePath: "sub/file.txt", Content: "content"})
	require.NoError(t, err)
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "race", Name: WriteToolName, Input: string(input)})
	require.True(t, err != nil || resp.IsError)
	require.NoFileExists(t, filepath.Join(outside, "file.txt"))
}

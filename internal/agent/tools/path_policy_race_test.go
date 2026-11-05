package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestRestrictedToolsConfineWritesAfterPermission(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("download"))
	}))
	t.Cleanup(server.Close)
	for _, name := range []string{"edit", "multiedit", "download"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root, outside := t.TempDir(), t.TempDir()
			if err := os.Symlink(outside, filepath.Join(root, "probe")); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			require.NoError(t, os.Remove(filepath.Join(root, "probe")))
			require.NoError(t, os.Mkdir(filepath.Join(root, "sub"), 0o755))
			called := false
			p := &changeWritePermission{change: func() error {
				called = true
				if err := os.Rename(filepath.Join(root, "sub"), filepath.Join(root, "saved")); err != nil {
					return err
				}
				return os.Symlink(outside, filepath.Join(root, "sub"))
			}}
			policy := PathPolicy{Root: root, Restrict: true}
			var tool fantasy.AgentTool
			var params any
			switch name {
			case "edit":
				tool = NewEditTool(nil, p, &mockHistoryService{}, mockFileTrackerService{}, root, policy)
				params = EditParams{FilePath: "sub/file.txt", NewString: "content"}
			case "multiedit":
				tool = NewMultiEditTool(nil, p, &mockHistoryService{}, mockFileTrackerService{}, root, policy)
				params = MultiEditParams{FilePath: "sub/file.txt", Edits: []MultiEditOperation{{NewString: "content"}}}
			case "download":
				tool = NewDownloadTool(p, root, server.Client(), URLPolicy{}, policy, 0)
				params = DownloadParams{FilePath: "sub/file.txt", URL: server.URL}
			}
			input, err := json.Marshal(params)
			require.NoError(t, err)
			ctx := context.WithValue(t.Context(), SessionIDContextKey, t.Name())
			resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "race", Name: name, Input: string(input)})
			require.True(t, called, "permission callback was not reached")
			require.True(t, err != nil || resp.IsError)
			require.NoFileExists(t, filepath.Join(outside, "file.txt"))
		})
	}
}

func TestCreationDoesNotOverwriteFileCreatedDuringPermission(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"write", "edit", "multiedit"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path := filepath.Join(root, "file.txt")
			p := &changeWritePermission{change: func() error {
				return os.WriteFile(path, []byte("newer"), 0o600)
			}}
			policy := PathPolicy{Root: root, Restrict: true}
			var tool fantasy.AgentTool
			var params any
			switch name {
			case "write":
				tool = NewWriteTool(nil, p, &mockHistoryService{}, mockFileTrackerService{}, root, policy)
				params = WriteParams{FilePath: "file.txt", Content: "ours"}
			case "edit":
				tool = NewEditTool(nil, p, &mockHistoryService{}, mockFileTrackerService{}, root, policy)
				params = EditParams{FilePath: "file.txt", NewString: "ours"}
			case "multiedit":
				tool = NewMultiEditTool(nil, p, &mockHistoryService{}, mockFileTrackerService{}, root, policy)
				params = MultiEditParams{FilePath: "file.txt", Edits: []MultiEditOperation{{NewString: "ours"}}}
			}
			input, err := json.Marshal(params)
			require.NoError(t, err)
			ctx := context.WithValue(t.Context(), SessionIDContextKey, t.Name())
			resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "created", Name: name, Input: string(input)})
			require.True(t, err != nil || resp.IsError)
			actual, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, "newer", string(actual))
		})
	}
}

func TestWriteRejectsFileChangedDuringPermission(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o600))
	p := &changeWritePermission{change: func() error { return os.WriteFile(path, []byte("newer"), 0o600) }}
	tool := NewWriteTool(nil, p, &mockHistoryService{}, mockFileTrackerService{}, root, PathPolicy{Root: root, Restrict: true})
	input, err := json.Marshal(WriteParams{FilePath: "file.txt", Content: "ours"})
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), SessionIDContextKey, t.Name())
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "changed", Name: WriteToolName, Input: string(input)})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	actual, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "newer", string(actual))
}

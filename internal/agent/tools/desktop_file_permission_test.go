package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

func TestComputerFileCheckPermissionUsesResolvedPath(t *testing.T) {
	t.Parallel()
	root, outside := t.TempDir(), t.TempDir()
	for _, test := range []struct {
		name, path string
		deny       bool
		safe       bool
	}{
		{"workspace", filepath.Join(root, "saved.txt"), false, true},
		{"outside allowed", filepath.Join(outside, "saved.txt"), false, false},
		{"outside denied", filepath.Join(outside, "private.txt"), true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, os.WriteFile(test.path, []byte("secret-file-value"), 0o600))
			permissions := &recordingPermissions{Service: permission.NewPermissionService(root, true, nil)}
			permissions.deny = func(r permission.CreatePermissionRequest) bool { return test.deny }
			state := &computerToolState{workingDir: root, permissions: permissions, actionTimeout: time.Second}
			input, err := json.Marshal(ComputerParams{Action: "file_check", File: &DesktopFileCheck{Path: test.path, Condition: "content", Expected: "secret-file-value"}})
			require.NoError(t, err)
			ctx := context.WithValue(t.Context(), SessionIDContextKey, "file-permission")
			r, err := newComputerToolWithState(state, "test").Run(ctx, fantasy.ToolCall{ID: "read-file", Input: string(input)})
			require.NoError(t, err)
			require.Equal(t, test.deny, r.IsError, r.Content)
			require.Len(t, permissions.requests, 1)
			req := permissions.requests[0]
			require.Equal(t, "file_check", req.Action)
			require.Equal(t, test.safe, req.Safe)
			resolved, err := filepath.EvalSymlinks(test.path)
			require.NoError(t, err)
			require.Equal(t, resolved, req.Path)
			result, ok := ResponseOutcome(r)
			require.True(t, ok)
			require.Equal(t, !test.deny, result.Verified)
			require.False(t, result.Mutation)
			if test.deny {
				require.NotContains(t, r.Content, "secret-file-value")
				require.Equal(t, "none", result.Dispatch)
			}
		})
	}
}

func TestFileCheckSymlinkCannotInheritWorkspaceSafePermission(t *testing.T) {
	t.Parallel()
	root, outside := t.TempDir(), t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("Symlink creation unavailable: %v", err)
	}
	permissions := &recordingPermissions{Service: permission.NewPermissionService(root, true, nil), deny: func(r permission.CreatePermissionRequest) bool { return !r.Safe }}
	state := &computerToolState{workingDir: root, permissions: permissions}
	r, err := state.checkFile(t.Context(), ComputerParams{File: &DesktopFileCheck{Path: filepath.Join(link, "missing.txt"), Condition: "absent"}}, fantasy.ToolCall{})
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Len(t, permissions.requests, 1)
	require.False(t, permissions.requests[0].Safe)
	resolved, err := filepath.EvalSymlinks(outside)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(resolved, "missing.txt"), permissions.requests[0].Path)
}

func TestDesktopFlowFileCheckPreservesUserPause(t *testing.T) {
	t.Parallel()
	p := PipelineParams{Desktop: &DesktopWorkflowParams{Mode: "flow", Flow: &DesktopFlowParams{Nodes: []DesktopFlowNode{
		{ID: "first", Kind: "file_check", File: &DesktopFileCheck{Path: filepath.Join(t.TempDir(), "a.txt"), Condition: "exists"}, Next: "second"},
		{ID: "second", Kind: "file_check", File: &DesktopFileCheck{Path: filepath.Join(t.TempDir(), "b.txt"), Condition: "exists"}},
	}}}}
	input, err := json.Marshal(p)
	require.NoError(t, err)
	calls := 0
	pause := fantasy.NewTextResponse("await permission")
	pause.StopTurn = true
	pause.Metadata = `{"question":{"id":"q-file","options":["allow","deny"]},"waiting_for_user":true}`
	r, err := NewToolPipeline(func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		require.Equal(t, ComputerToolName, c.Name)
		require.Contains(t, c.Input, `"action":"file_check"`)
		return pause, nil
	}).Run(t.Context(), fantasy.ToolCall{Input: string(input)})
	require.NoError(t, err)
	require.True(t, r.StopTurn)
	require.Equal(t, 1, calls)
	require.Contains(t, r.Metadata, `"id":"q-file"`)
	require.Contains(t, r.Metadata, `"waiting_for_user":true`)
}

func TestDesktopFileContentLimitAndMatchingDigest(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "saved.txt")
	data := bytes.Repeat([]byte("x"), maxFileCheckBytes+1)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	r := runDesktopFileCheck(t.Context(), DesktopFileCheck{Path: path, Condition: "content"})
	require.True(t, r.IsError)
	require.Contains(t, r.Content, "file_too_large:")
	require.NotContains(t, r.Content, `"sha256"`, "oversized content must be rejected before hashing")
	data = []byte("same snapshot\r\n")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	r = runDesktopFileCheck(t.Context(), DesktopFileCheck{Path: path, Condition: "content", Expected: string(data), LineEndings: "exact"})
	require.False(t, r.IsError, r.Content)
	sum := sha256.Sum256(data)
	require.Contains(t, r.Content, hex.EncodeToString(sum[:]))
}

type cancelFileReader struct {
	cancel context.CancelFunc
	reads  int
}

func (r *cancelFileReader) Read(p []byte) (int, error) {
	r.reads++
	r.cancel()
	return copy(p, "bytes"), nil
}

func TestFileReadCancellationDuringRead(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reader := &cancelFileReader{cancel: cancel}
	n, err := io.Copy(io.Discard, &flowContextReader{ctx: ctx, reader: reader})
	require.ErrorIs(t, err, context.Canceled)
	require.EqualValues(t, 5, n)
	require.Equal(t, 1, reader.reads)
	path := filepath.Join(t.TempDir(), "saved.txt")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
	_, _, _, err = fileSnapshot(ctx, path, false)
	require.ErrorIs(t, err, context.Canceled)
}

func TestCapturePatternWholeReadbackAlternation(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"result 9000 invalid", "prefix result 9000", "result 9000\ninvalid"} {
		content, err := json.Marshal(map[string]any{"window_id": "21", "actual": value})
		require.NoError(t, err)
		_, err = captureFlowValue(string(content), computer.AutomationRequest{WindowID: "21"}, &DesktopFlowCapture{Name: "result", Pattern: `result (\d+)|other`})
		require.ErrorContains(t, err, "capture_mismatch")
	}
	value, err := captureFlowValue(`{"actual":"result 9000","window_id":"21"}`, computer.AutomationRequest{WindowID: "21"}, &DesktopFlowCapture{Name: "result", Pattern: `result (\d+)`})
	require.NoError(t, err)
	require.Equal(t, "9000", value)
	_, err = captureFlowValue(`{"actual":"result 9000","window_id":"21"}`, computer.AutomationRequest{WindowID: "21"}, &DesktopFlowCapture{Name: "result", Pattern: strings.Repeat("(", 2)})
	require.Error(t, err)
}

func TestPipelineInvalidFileCheckStopsBeforeEarlierInput(t *testing.T) {
	t.Parallel()
	for _, arguments := range []map[string]any{
		{"action": "file_check"},
		{"action": "file_check", "file": map[string]any{"path": "relative.txt", "condition": "exists"}},
		{"action": "key", "key": "enter", "file": map[string]any{"path": filepath.Join(t.TempDir(), "saved.txt"), "condition": "exists"}},
	} {
		p := PipelineParams{Steps: []PipelineStep{
			{ID: "input", Tool: ComputerToolName, Arguments: map[string]any{"action": "key", "key": "enter"}},
			{ID: "invalid", Tool: ComputerToolName, Arguments: arguments},
		}}
		input, err := json.Marshal(p)
		require.NoError(t, err)
		r, err := NewToolPipeline(func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			t.Fatal("Invalid file plan must not dispatch any earlier input")
			return fantasy.ToolResponse{}, nil
		}).Run(t.Context(), fantasy.ToolCall{Input: string(input)})
		require.NoError(t, err)
		require.True(t, r.IsError, r.Content)
		require.Contains(t, r.Content, "step invalid:")
	}
}

func TestDesktopFileAbsentRejectsDanglingLink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	link := filepath.Join(dir, "occupied.txt")
	if err := os.Symlink(filepath.Join(dir, "missing.txt"), link); err != nil {
		t.Skipf("Symlink creation unavailable: %v", err)
	}
	r := runDesktopFileCheck(t.Context(), DesktopFileCheck{Path: link, Condition: "absent"})
	require.True(t, r.IsError)
	require.Contains(t, r.Content, "file_present:")
}

func TestComputerFileAbsentChecksEntryWithoutFollowingTarget(t *testing.T) {
	t.Parallel()
	root, outside := t.TempDir(), t.TempDir()
	target := filepath.Join(outside, "private.txt")
	require.NoError(t, os.WriteFile(target, []byte("private-target-content"), 0o600))
	link := filepath.Join(root, "occupied.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("Symlink creation unavailable: %v", err)
	}
	permissions := &recordingPermissions{Service: permission.NewPermissionService(root, true, nil)}
	state := &computerToolState{workingDir: root, permissions: permissions}
	r, err := state.checkFile(t.Context(), ComputerParams{File: &DesktopFileCheck{Path: link, Condition: "absent"}}, fantasy.ToolCall{})
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Contains(t, r.Content, "file_present:")
	require.NotContains(t, r.Content, "private-target-content")
	require.Len(t, permissions.requests, 1)
	resolved, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(resolved, "occupied.txt"), permissions.requests[0].Path)
	require.True(t, permissions.requests[0].Safe)
}

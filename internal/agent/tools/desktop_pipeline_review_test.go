package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func TestReportReviewCaptureAlreadyWritten(t *testing.T) {
	t.Parallel()
	d := newCalcNotepad(t)
	d.field, d.foreground = "ready", "11"
	p := DesktopWorkflowParams{Mode: "flow", Flow: &DesktopFlowParams{Nodes: []DesktopFlowNode{
		{ID: "pad", Kind: "resolve", Application: "Notepad", Next: "write"},
		{
			ID: "write", Kind: "operation", WindowRef: "pad", Next: "check",
			Input:      ComputerParams{Action: "set_value", Automation: computer.AutomationRequest{Name: "Field", Text: "ready"}},
			Checkpoint: computer.AutomationRequest{Name: "Field", Condition: "value", Expected: "ready"},
			Capture:    &DesktopFlowCapture{Name: "written"},
		},
		{ID: "check", Kind: "verify", WindowRef: "pad", Checkpoint: computer.AutomationRequest{Name: "Field", Condition: "value", Expected: "{{written}}"}},
	}}}
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, d.invoke)
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Zero(t, d.mutations)

	// The completed capture must survive reopening the durable journal.
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "already-written")
	store := engineering.NewStore(t.TempDir())
	p.Flow.RunID = "already-written"
	run := func() fantasy.ToolResponse {
		input, err := json.Marshal(PipelineParams{Desktop: &p})
		require.NoError(t, err)
		r, err := NewToolPipeline(d.invoke, store).Run(ctx, fantasy.ToolCall{Input: string(input)})
		require.NoError(t, err)
		return r
	}
	r = run()
	require.False(t, r.IsError, r.Content)
	p.Flow.Resume = true
	r = run()
	require.False(t, r.IsError, r.Content)
	require.Zero(t, d.mutations)
	files, err := filepath.Glob(filepath.Join(store.Dir(), "desktop-runs", "*.json"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	saved, err := os.ReadFile(files[0])
	require.NoError(t, err)
	require.Contains(t, string(saved), `"captures":{"written":"ready"}`)
}

func TestReportReviewFileCheckUsesGuardedDispatch(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "private.txt")
	require.NoError(t, os.WriteFile(path, []byte("private-value"), 0o600))
	p := PipelineParams{Desktop: &DesktopWorkflowParams{Mode: "flow", Flow: &DesktopFlowParams{Nodes: []DesktopFlowNode{
		{ID: "read", Kind: "file_check", File: &DesktopFileCheck{Path: path, Condition: "content", Expected: "private-value"}},
	}}}}
	input, err := json.Marshal(p)
	require.NoError(t, err)
	calls := 0
	r, err := NewToolPipeline(func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		return fantasy.NewTextErrorResponse("permission_denied: child read blocked"), nil
	}).Run(t.Context(), fantasy.ToolCall{Input: string(input)})
	require.NoError(t, err)
	require.Positive(t, calls, r.Content)
	require.True(t, r.IsError, r.Content)
}

func TestReportReviewCaptureRequiresWholePattern(t *testing.T) {
	t.Parallel()
	value, err := captureFlowValue(`{"actual":"result 9000 invalid","window_id":"21"}`, computer.AutomationRequest{WindowID: "21"}, &DesktopFlowCapture{Name: "result", Pattern: `result (\d+)`})
	require.Error(t, err, "partial-match was accepted as %q despite the full-readback pattern contract", value)
}

func TestReportReviewRenameConflictPreservesPause(t *testing.T) {
	t.Parallel()
	calls := 0
	question := fantasy.NewTextResponse("waiting for user")
	question.StopTurn = true
	question.Metadata = `{"question":{"id":"q1","options":["allow","deny"]},"waiting_for_user":true}`
	_, done, r := resolveRenameConflict(t.Context(), desktopWindowInfo{ID: "22"}, DesktopRenameParams{OldName: "hesap", NewName: "sonuc"}, func(context.Context, ComputerParams) (fantasy.ToolResponse, error) {
		calls++
		return question, nil
	})
	require.True(t, done)
	require.True(t, r.StopTurn, r.Content)
	require.Equal(t, question.Metadata, r.Metadata)
	require.Equal(t, 1, calls, "a user pause must prevent the second read")
}

func TestRenameConflictPauseAtEveryFileRead(t *testing.T) {
	t.Parallel()
	dir := conflictFolder(t, "9000", "8638")
	for stage := 1; stage <= 6; stage++ {
		t.Run(fmt.Sprint(stage), func(t *testing.T) {
			t.Parallel()
			calls := 0
			pause := fantasy.NewTextResponse("await user")
			pause.StopTurn = true
			pause.Metadata = `{"permission":{"id":"p1"},"waiting_for_user":true}`
			_, done, r := resolveRenameConflict(t.Context(), desktopWindowInfo{ID: "22"}, DesktopRenameParams{OldName: "hesap", NewName: "sonuc", OnConflict: "keep_both"}, func(ctx context.Context, p ComputerParams) (fantasy.ToolResponse, error) {
				calls++
				if calls == stage {
					return pause, nil
				}
				switch p.Action {
				case "assert":
					data, err := json.Marshal(map[string]any{"window_id": "22", "actual": p.Automation.Name + ".txt", "folder_path": dir})
					require.NoError(t, err)
					return fantasy.NewTextResponse(string(data)), nil
				case "file_check":
					return runDesktopFileCheck(ctx, *p.File), nil
				case "find":
					return fantasy.NewTextResponse(`{"result":{"matches":[],"truncated":false}}`), nil
				default:
					t.Fatalf("Unexpected action %s", p.Action)
					return fantasy.ToolResponse{}, nil
				}
			})
			require.True(t, done)
			require.True(t, r.StopTurn, r.Content)
			require.Equal(t, pause.Metadata, r.Metadata)
			require.Equal(t, stage, calls, "no additional read after pause")
		})
	}
}

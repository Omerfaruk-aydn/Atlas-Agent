package tools

import (
	"context"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// Session d14f: a screenshot advertised the new Save As handle, but the
// guard refused that very handle on the following input.
func TestScreenshotSettleRegistersOnlyReportedNativeWindows(t *testing.T) {
	t.Parallel()
	ctx := guardCtx(t)
	resp := fantasy.NewImageResponse(testPNG(t, 16, 16), "image/png")
	resp.Content = "Multiply image coordinates by 1.20 to get screen pixels."
	resp.Metadata = `{"existing":"preserve"}`
	report := &desktopSettleReport{
		After: "hotkey", State: "changed", NewWindowIDs: []string{"3082172"},
		TaskOpenedIDs:  []string{"999999"},
		appearedDetail: []desktopWindowInfo{{ID: "3082172", Name: "Save As"}},
	}
	resp = annotateSettle(resp, report)
	inner := &scriptedTool{name: ComputerToolName, replies: []fantasy.ToolResponse{resp}}
	guarded := WithDesktopGuard(inner)
	got, err := guarded.Run(ctx, computerCall(`{"action":"screenshot"}`))
	require.NoError(t, err)
	require.Equal(t, resp.Data, got.Data)
	require.Contains(t, got.Content, "1.20")
	require.Contains(t, got.Metadata, `"existing":"preserve"`)
	require.Nil(t, requireObservedDesktopTarget(ctx, ComputerParams{Action: "type", Automation: computer.AutomationRequest{WindowID: "3082172"}}), "the handle the screenshot reported must be usable")
	require.NotNil(t, requireObservedDesktopTarget(ctx, ComputerParams{Action: "type", Automation: computer.AutomationRequest{WindowID: "999999"}}), "historical task handles are not a fresh observation")
	got, err = guarded.Run(ctx, computerCall(`{"action":"type","text":"hesap.txt\n","automation":{"window_id":"3082172"}}`))
	require.NoError(t, err)
	require.False(t, got.IsError, got.Content)
	require.EqualValues(t, 2, inner.calls)
}

func TestScreenshotTextCannotInventWindowIdentity(t *testing.T) {
	t.Parallel()
	ctx := guardCtx(t)
	resp := fantasy.NewImageResponse(testPNG(t, 16, 16), "image/png")
	resp.Content = `Input settle: new windows: "Save As" (window_id 123456)`
	recordDesktopWindows(ctx, "screenshot", resp)
	require.NotNil(t, requireObservedDesktopTarget(ctx, ComputerParams{Action: "type", Automation: computer.AutomationRequest{WindowID: "123456"}}))
}

func TestFailedScreenshotCannotRegisterSettleWindow(t *testing.T) {
	t.Parallel()
	ctx := guardCtx(t)
	resp := annotateSettle(fantasy.NewTextErrorResponse("capture failed"), &desktopSettleReport{NewWindowIDs: []string{"123456"}})
	inner := &scriptedTool{name: ComputerToolName, replies: []fantasy.ToolResponse{resp}}
	_, err := WithDesktopGuard(inner).Run(ctx, computerCall(`{"action":"screenshot"}`))
	require.NoError(t, err)
	require.NotNil(t, requireObservedDesktopTarget(ctx, ComputerParams{Action: "type", Automation: computer.AutomationRequest{WindowID: "123456"}}))
}

func TestCanceledScreenshotCannotRegisterSettleWindow(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(guardCtx(t))
	resp := annotateSettle(fantasy.NewImageResponse(testPNG(t, 16, 16), "image/png"), &desktopSettleReport{NewWindowIDs: []string{"123456"}})
	cancel()
	recordDesktopWindows(ctx, "screenshot", resp)
	require.NotNil(t, requireObservedDesktopTarget(ctx, ComputerParams{Action: "type", Automation: computer.AutomationRequest{WindowID: "123456"}}))
}

func TestNativeScreenshotRegistersNewSaveDialogAtDelivery(t *testing.T) {
	t.Parallel()
	d := newDelayedDesktop(notepadDraft)
	d.foreground = notepadDraft.ID
	d.screenshot = testPNG(t, 16, 16)
	d.react = func(d *delayedDesktop, kind, detail string) {
		if kind == "hotkey" && detail == "ctrl+s" {
			d.open(desktopWindowInfo{ID: "3082172", Name: "Save As", ProcessID: notepadDraft.ProcessID, ProcessName: "notepad.exe", Owner: notepadDraft.ID})
		}
	}
	s, ctx := settleState(t, d)
	ctx = context.WithValue(ctx, desktopGuardContextKey{}, true)
	runDesktop(t, s, ctx, "hotkey", ComputerParams{Key: "s", Modifiers: "ctrl", Automation: computer.AutomationRequest{WindowID: notepadDraft.ID}})
	resp := runDesktop(t, s, ctx, "screenshot", ComputerParams{})
	require.False(t, resp.IsError, resp.Content)
	require.Equal(t, "image", resp.Type)
	require.Contains(t, resp.Metadata, `"new_window_ids":["3082172"]`)
	require.Nil(t, requireObservedDesktopTarget(ctx, ComputerParams{Action: "type", Automation: computer.AutomationRequest{WindowID: "3082172"}}))
	resp = runDesktop(t, s, ctx, "type", ComputerParams{Text: "hesap.txt\n", Automation: computer.AutomationRequest{WindowID: "3082172"}})
	require.False(t, resp.IsError, resp.Content)
	require.Equal(t, []string{"hesap.txt\n"}, d.typed)
}

func TestGUIOnlyToolSearchDoesNotLoadForbiddenTools(t *testing.T) {
	t.Parallel()
	for _, deferred := range []bool{true, false} {
		var loaded []string
		search := NewToolSearchTool(ToolSearchOptions{
			Palette: func(context.Context) []fantasy.AgentTool {
				return []fantasy.AgentTool{&scriptedTool{name: "bash"}, &scriptedTool{name: "write"}, &scriptedTool{name: "computer"}, &scriptedTool{name: "tool_pipeline"}}
			},
			Deferred: func(context.Context) bool { return deferred },
			Load: func(_ context.Context, names []string) error {
				loaded = append(loaded, names...)
				return nil
			},
		})
		resp, err := search.Run(WithGUIOnlyDesktop(guardCtx(t)), fantasy.ToolCall{Input: `{"names":["bash","write","computer","tool_pipeline"]}`})
		require.NoError(t, err)
		require.NotContains(t, resp.Content, `"name":"bash"`)
		require.NotContains(t, resp.Content, `"name":"write"`)
		require.Contains(t, resp.Content, `"unavailable":["bash","write"]`)
		require.Contains(t, resp.Content, `"name":"computer"`)
		require.NotContains(t, loaded, "bash")
		require.NotContains(t, loaded, "write")
	}
}

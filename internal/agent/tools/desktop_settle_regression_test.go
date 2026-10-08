package tools

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestFailedReadDoesNotAcknowledgeUnreportedWindow(t *testing.T) {
	d := newDelayedDesktop(explorerWindow)
	s, ctx := settleState(t, d)
	runDesktop(t, s, ctx, "key", ComputerParams{Key: "enter", Automation: computer.AutomationRequest{WindowID: explorerWindow.ID}})
	d.mu.Lock()
	d.open(notepadOpened)
	d.mu.Unlock()
	r := runDesktop(t, s, ctx, "capture_window", ComputerParams{Automation: computer.AutomationRequest{WindowID: "missing"}})
	require.True(t, r.IsError)
	require.NotContains(t, r.Content, notepadOpened.ID)
	require.NotEmpty(t, DesktopOpenTransitions(ctx, t.Name()), "failed capture never reported the appeared window")
}

func TestTooltipMustNotSettlePendingOpen(t *testing.T) {
	d := newDelayedDesktop(explorerWindow)
	d.react = func(d *delayedDesktop, kind, detail string) {
		if kind == "key" && detail == "enter" {
			d.after(60*time.Millisecond, func(d *delayedDesktop) {
				d.windows = append(d.windows, desktopWindowInfo{ID: "tooltip", WindowClass: "tooltips_class32"})
			})
			d.after(250*time.Millisecond, func(d *delayedDesktop) { d.open(notepadOpened) })
		}
	}
	s, ctx := settleState(t, d)
	runDesktop(t, s, ctx, "key", ComputerParams{Key: "enter", Automation: computer.AutomationRequest{WindowID: explorerWindow.ID}})
	r := runDesktop(t, s, ctx, "windows", ComputerParams{})
	require.Contains(t, r.Content, notepadOpened.ID, "transient shell changes do not complete the expected open")
}

type blockedSettleScreenshot struct {
	*delayedDesktop
	started, release, finished chan struct{}
}

func (b *blockedSettleScreenshot) Screenshot() ([]byte, error) {
	close(b.started)
	<-b.release
	defer close(b.finished)
	return b.delayedDesktop.Screenshot()
}

func TestCanceledReadNeverAcknowledgesLateSuccessfulCapture(t *testing.T) {
	d := newDelayedDesktop(explorerWindow)
	d.screenshot = testPNG(t, 4, 4)
	s, ctx := settleState(t, d)
	b := &blockedSettleScreenshot{delayedDesktop: d, started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
	s.backend = b
	runDesktop(t, s, ctx, "key", ComputerParams{Key: "enter", Automation: computer.AutomationRequest{WindowID: explorerWindow.ID}})
	d.mu.Lock()
	d.open(notepadOpened)
	d.mu.Unlock()
	canceled, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := s.runWithTimeout(canceled, "screenshot", ComputerParams{Action: "screenshot"})
		done <- err
	}()
	<-b.started
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	close(b.release)
	<-b.finished
	// The next call waits for the abandoned worker to release its desktop lease.
	resp, err := s.runWithTimeout(ctx, "screen_size", ComputerParams{Action: "screen_size"})
	require.NoError(t, err)
	require.False(t, resp.IsError)
	require.NotEmpty(t, DesktopOpenTransitions(ctx, t.Name()), "the canceled capture was never delivered")
}

func TestBackgroundWindowCannotAcknowledgePendingOpen(t *testing.T) {
	d := newDelayedDesktop(explorerWindow)
	s, ctx := settleState(t, d)
	s.settleTiming.OpenWait = 80 * time.Millisecond
	runDesktop(t, s, ctx, "key", ComputerParams{Key: "enter", Automation: computer.AutomationRequest{WindowID: explorerWindow.ID}})
	d.mu.Lock()
	d.windows = append(d.windows, desktopWindowInfo{ID: "other", Name: "Unrelated background", ProcessID: 999, ProcessName: "other.exe"})
	d.mu.Unlock()
	resp := runDesktop(t, s, ctx, "windows", ComputerParams{})
	require.Contains(t, resp.Content, `"state":"pending"`)
	d.mu.Lock()
	d.open(notepadOpened)
	d.mu.Unlock()
	require.NotEmpty(t, DesktopOpenTransitions(ctx, t.Name()), "an unrelated background window cannot resolve the open")
}

func TestDesktopActWithoutPostconditionWaitsBeforeReadback(t *testing.T) {
	d := newDelayedDesktop(explorerWindow)
	d.values["result"] = "old"
	d.react = func(d *delayedDesktop, kind, detail string) {
		if kind == "type" {
			d.after(100*time.Millisecond, func(d *delayedDesktop) { d.values["result"] = "accepted" })
		}
	}
	s, ctx := settleState(t, d)
	s.settleTiming.MinSettle = 150 * time.Millisecond
	resp, err := runDesktopWorkflow(ctx, DesktopWorkflowParams{Mode: "act", Input: ComputerParams{Action: "type", Text: "new", Automation: computer.AutomationRequest{WindowID: explorerWindow.ID}}}, fantasy.ToolCall{ID: "act"}, func(ctx context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var input ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &input))
		if input.Action == "observe" {
			input.Action = "find"
			input.Automation.ElementID = "result"
		}
		return s.runWithTimeout(ctx, input.Action, input)
	})
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "accepted")
}

type blockedSettleLister struct {
	*delayedDesktop
	block                      atomic.Bool
	started, release, finished chan struct{}
}

func (b *blockedSettleLister) ListWindows(ctx context.Context) (json.RawMessage, error) {
	if b.block.CompareAndSwap(true, false) {
		close(b.started)
		<-b.release
		defer close(b.finished)
	}
	return b.delayedDesktop.ListWindows(ctx)
}

func TestCanceledTransitionRefusalDoesNotAcknowledgeWindow(t *testing.T) {
	d := newDelayedDesktop(explorerWindow, notepadDraft)
	s, ctx := settleState(t, d)
	runDesktop(t, s, ctx, "key", ComputerParams{Key: "enter", Automation: computer.AutomationRequest{WindowID: explorerWindow.ID}})
	d.mu.Lock()
	d.open(notepadOpened)
	d.mu.Unlock()
	b := &blockedSettleLister{delayedDesktop: d, started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
	b.block.Store(true)
	s.backend = b
	canceled, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := s.runWithTimeout(canceled, "focus", ComputerParams{Action: "focus", Automation: computer.AutomationRequest{WindowID: notepadDraft.ID}})
		done <- err
	}()
	<-b.started
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	close(b.release)
	<-b.finished
	resp, err := s.runWithTimeout(ctx, "screen_size", ComputerParams{Action: "screen_size"})
	require.NoError(t, err)
	require.False(t, resp.IsError)
	require.NotEmpty(t, DesktopOpenTransitions(ctx, t.Name()), "a canceled refusal did not report the new window")
}

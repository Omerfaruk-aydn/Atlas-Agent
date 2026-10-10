package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// Click routes pointer input through the scripted desktop so tests can open
// and close menus the way the shell does.
func (d *delayedDesktop) Click(x, y int, button computer.MouseButton) error {
	_ = d.fakeComputerBackend.Click(x, y, button)
	kind := "click"
	if button == computer.ButtonRight {
		kind = "right_click"
	}
	d.input(kind, "")
	return nil
}

func (d *delayedDesktop) add(w desktopWindowInfo) {
	d.windows = append(d.windows, w)
}

func (d *delayedDesktop) remove(ids ...string) {
	kept := d.windows[:0]
	for _, w := range d.windows {
		if !strings.Contains(" "+strings.Join(ids, " ")+" ", " "+w.ID+" ") {
			kept = append(kept, w)
		}
	}
	d.windows = kept
}

var (
	shellDesktop = desktopWindowInfo{ID: "65552", Name: "Program Manager", ProcessID: 8204, ProcessName: "explorer.exe", WindowClass: "Progman"}
	terminalApp  = desktopWindowInfo{ID: "330", Name: "Atlas", ProcessID: 7000, ProcessName: "WindowsTerminal.exe", WindowClass: "CASCADIA_HOSTING_WINDOW_CLASS", Foreground: true}
	contextMenu  = desktopWindowInfo{ID: "11602064", ProcessID: 8204, ProcessName: "explorer.exe", WindowClass: "#32768"}
	newSubmenu   = desktopWindowInfo{ID: "15205102", ProcessID: 8204, ProcessName: "explorer.exe", WindowClass: "#32768"}
)

func settleReportOf(t *testing.T, resp fantasy.ToolResponse) desktopSettleReport {
	t.Helper()
	var result struct {
		Settle *desktopSettleReport `json:"input_settle"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &result), resp.Content)
	require.NotNil(t, result.Settle, resp.Content)
	return *result.Settle
}

// Session 69361c0a: a right-click on the desktop opened a context menu
// (#32768, explorer.exe, never foreground). The result listed the menu as a
// new window and in the same line said "No window change was observed" and
// pending, after waiting the full 1500 ms. The menu belongs to the process
// the click made foreground, so it is the input's result.
func TestDesktopContextMenuIsReportedAsChange(t *testing.T) {
	// Not parallel: this test measures time.
	d := newDelayedDesktop(terminalApp, shellDesktop)
	d.screenshot = testPNG(t, 4, 4)
	d.react = func(d *delayedDesktop, kind, _ string) {
		switch kind {
		case "right_click":
			d.after(40*time.Millisecond, func(d *delayedDesktop) {
				d.foreground = shellDesktop.ID
				d.add(contextMenu)
			})
		case "click":
			if len(d.clicked) == 2 {
				d.after(40*time.Millisecond, func(d *delayedDesktop) { d.add(newSubmenu) })
			} else {
				// Choosing "Klasör" dismisses both menus and starts an
				// inline rename on the desktop; no window opens.
				d.after(40*time.Millisecond, func(d *delayedDesktop) { d.remove(contextMenu.ID, newSubmenu.ID) })
			}
		}
	}
	s, ctx := settleState(t, d)
	s.settleTiming.OpenWait = 1500 * time.Millisecond

	runDesktop(t, s, ctx, "right_click", ComputerParams{X: 1600, Y: 300})
	started := time.Now()
	resp := runDesktop(t, s, ctx, "screenshot", ComputerParams{})
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "Input settle: changed after right_click")
	require.Contains(t, resp.Content, contextMenu.ID)
	require.NotContains(t, resp.Content, "No window change")
	require.NotContains(t, resp.Content, "pending")
	require.Less(t, time.Since(started), 700*time.Millisecond, "an observed menu ends the wait")

	// The submenu opened by "Yeni" is the click's result as well.
	runDesktop(t, s, ctx, "click", ComputerParams{X: 1650, Y: 636})
	resp = runDesktop(t, s, ctx, "windows", ComputerParams{})
	report := settleReportOf(t, resp)
	require.Equal(t, "changed", report.State, resp.Content)
	require.Equal(t, []string{newSubmenu.ID}, report.RelatedIDs)
	require.Empty(t, report.UnrelatedIDs)
	require.Equal(t, "click", report.After)
	require.NotEmpty(t, report.ObservedAt)

	// Choosing the menu item closes both menus: a change, not "pending".
	runDesktop(t, s, ctx, "click", ComputerParams{X: 1375, Y: 638})
	started = time.Now()
	resp = runDesktop(t, s, ctx, "windows", ComputerParams{})
	report = settleReportOf(t, resp)
	require.Equal(t, "changed", report.State, resp.Content)
	require.ElementsMatch(t, []string{contextMenu.ID, newSubmenu.ID}, report.ClosedIDs)
	require.Less(t, time.Since(started), 700*time.Millisecond)
}

// Session 69361c0a: after the menu click, `type` was sent and the next
// screenshot said "pending after click ... No window change was observed
// within 1500 ms after click". The latest input was type, which opens no
// window; it must neither be labeled as the click nor wait for a window.
func TestTypeAfterTransitionReportsLatestInputWithoutPending(t *testing.T) {
	// Not parallel: this test measures time.
	d := newDelayedDesktop(explorerWindow)
	d.screenshot = testPNG(t, 4, 4)
	s, ctx := settleState(t, d)
	s.settleTiming.OpenWait = 1500 * time.Millisecond
	runDesktop(t, s, ctx, "right_click", ComputerParams{X: 400, Y: 300})
	runDesktop(t, s, ctx, "type", ComputerParams{Text: "Atlas-Test\n"})
	started := time.Now()
	resp := runDesktop(t, s, ctx, "screenshot", ComputerParams{})
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "after type")
	require.Contains(t, resp.Content, "tracking the earlier right_click")
	require.NotContains(t, resp.Content, "pending")
	require.Less(t, time.Since(started), time.Second, "no wait for a window the type input cannot open")
}

// A plain click may change windows but usually does not: an observation
// after it waits only for a stable list and reports no pending transition.
func TestPlainClickWithoutWindowChangeIsNotPending(t *testing.T) {
	// Not parallel: this test measures time.
	d := newDelayedDesktop(explorerWindow)
	s, ctx := settleState(t, d)
	s.settleTiming.OpenWait = 1500 * time.Millisecond
	runDesktop(t, s, ctx, "click", ComputerParams{X: 400, Y: 300})
	started := time.Now()
	resp := runDesktop(t, s, ctx, "windows", ComputerParams{})
	report := settleReportOf(t, resp)
	require.Equal(t, "stable", report.State, resp.Content)
	require.Equal(t, "input_settle", report.WaitReason)
	require.Less(t, time.Since(started), time.Second)
}

// A window of another application that appears while Enter's result is
// still pending neither completes nor acknowledges the transition; the
// report names it as unrelated, and the real result still blocks the next
// input until it is observed.
func TestUnrelatedWindowDoesNotCompleteTransition(t *testing.T) {
	t.Parallel()
	toast := desktopWindowInfo{ID: "990", Name: "Yeni bildirim", ProcessID: 9999, ProcessName: "Teams.exe", WindowClass: "Chrome_WidgetWin_1"}
	d := newDelayedDesktop(explorerWindow, notepadDraft)
	d.react = func(d *delayedDesktop, kind, detail string) {
		if kind == "key" && detail == "enter" {
			d.after(20*time.Millisecond, func(d *delayedDesktop) { d.add(toast) })
		}
	}
	s, ctx := settleState(t, d)
	runDesktop(t, s, ctx, "key", ComputerParams{Key: "enter", Automation: computer.AutomationRequest{WindowID: explorerWindow.ID}})
	resp := runDesktop(t, s, ctx, "windows", ComputerParams{})
	report := settleReportOf(t, resp)
	require.Equal(t, "pending", report.State, resp.Content)
	require.Equal(t, []string{toast.ID}, report.UnrelatedIDs)
	require.Empty(t, report.RelatedIDs)
	require.Contains(t, report.Note, "not linked")
	require.Equal(t, "expected_window_change", report.WaitReason)

	// The real result arrives late; the next input is refused until it is seen.
	d.mu.Lock()
	d.open(notepadOpened)
	d.mu.Unlock()
	resp = runDesktop(t, s, ctx, "hotkey", ComputerParams{Key: "o", Modifiers: "ctrl", Automation: computer.AutomationRequest{WindowID: explorerWindow.ID}})
	require.True(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "delayed_transition")
	require.Contains(t, resp.Content, notepadOpened.ID)
	require.Empty(t, d.hotkeys)
}

// Enter on an owned Save dialog closes it. The closed target is the
// transition's result: changed, with the dialog in closed_window_ids.
func TestClosedTargetIsReportedAsChange(t *testing.T) {
	t.Parallel()
	notepad := notepadDraft
	notepad.Foreground = false
	dialog := desktopWindowInfo{ID: "1312792", Name: "Farklı Kaydet", ProcessID: notepad.ProcessID, ProcessName: "notepad.exe", WindowClass: "#32770", Owner: notepad.ID, Foreground: true}
	d := newDelayedDesktop(notepad, dialog)
	d.react = func(d *delayedDesktop, kind, detail string) {
		if kind == "key" && detail == "enter" {
			d.after(20*time.Millisecond, func(d *delayedDesktop) {
				d.remove(dialog.ID)
				d.foreground = notepad.ID
				d.retitle(notepad.ID, "hesap - Not Defteri")
			})
		}
	}
	s, ctx := settleState(t, d)
	runDesktop(t, s, ctx, "key", ComputerParams{Key: "enter", Automation: computer.AutomationRequest{WindowID: dialog.ID}})
	resp := runDesktop(t, s, ctx, "windows", ComputerParams{})
	report := settleReportOf(t, resp)
	require.Equal(t, "changed", report.State, resp.Content)
	require.Equal(t, []string{dialog.ID}, report.ClosedIDs)
	require.Contains(t, report.Note, "verify the intended result")
}

// slowListDesktop delays the delivered window list beyond the action
// timeout, so the observation is abandoned after settle saw the change.
type slowListDesktop struct {
	*delayedDesktop
	delayMu sync.Mutex
	delay   time.Duration
}

func (d *slowListDesktop) Automation(ctx context.Context, p computer.AutomationRequest) (json.RawMessage, error) {
	if p.Action == "windows" {
		d.delayMu.Lock()
		delay := d.delay
		d.delayMu.Unlock()
		time.Sleep(delay)
	}
	return d.delayedDesktop.Automation(ctx, p)
}

// An observation that times out before delivery does not acknowledge the
// window it saw and does not make it task-opened; the next input is still
// refused until the window is actually reported.
func TestUndeliveredObservationDoesNotAcknowledgeWindow(t *testing.T) {
	t.Parallel()
	d := newDelayedDesktop(explorerWindow, notepadDraft)
	d.react = func(d *delayedDesktop, kind, _ string) {
		if kind == "double_click" {
			d.after(20*time.Millisecond, func(d *delayedDesktop) { d.open(notepadOpened) })
		}
	}
	slow := &slowListDesktop{delayedDesktop: d, delay: 400 * time.Millisecond}
	_, ctx := settleState(t, d)
	s := &computerToolState{backend: slow, workingDir: t.TempDir(), actionTimeout: 5 * time.Second, settleTiming: testSettleTiming()}
	runDesktop(t, s, ctx, "double_click", ComputerParams{X: 10, Y: 10})

	s.actionTimeout = 150 * time.Millisecond
	resp, err := s.runWithTimeout(ctx, "windows", ComputerParams{Action: "windows"})
	require.NoError(t, err)
	require.True(t, resp.IsError, "the observation timed out: %s", resp.Content)

	state := desktopTransitionsFor(t.Name())
	state.mu.Lock()
	_, owned := state.taskOpened[notepadOpened.ID]
	acknowledged := state.pending != nil && state.pending.Acknowledged
	state.mu.Unlock()
	require.False(t, owned, "an undelivered observation made the window task-opened")
	require.False(t, acknowledged, "an undelivered observation acknowledged the transition")

	s.actionTimeout = 5 * time.Second
	slow.delayMu.Lock()
	slow.delay = 0
	slow.delayMu.Unlock()
	resp = runDesktop(t, s, ctx, "focus", ComputerParams{Automation: computer.AutomationRequest{WindowID: notepadDraft.ID}})
	require.True(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "delayed_transition")
}

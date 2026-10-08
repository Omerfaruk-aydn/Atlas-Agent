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

// delayedDesktop is a scripted desktop whose windows change some time after
// an input, the way real applications open windows, navigate folders and
// retitle documents asynchronously.
type delayedDesktop struct {
	fakeComputerBackend
	mu         sync.Mutex
	windows    []desktopWindowInfo
	foreground string
	due        []delayedChange
	// react schedules changes for an input: kind is key/hotkey/double_click/
	// launch_app/click, detail is the key, chord or application.
	react    func(d *delayedDesktop, kind, detail string)
	launches int
	values   map[string]string
	reads    []time.Time
}

type delayedChange struct {
	at    time.Time
	apply func(d *delayedDesktop)
}

func newDelayedDesktop(windows ...desktopWindowInfo) *delayedDesktop {
	d := &delayedDesktop{values: map[string]string{}}
	d.size = computer.Size{Width: 1920, Height: 1080}
	for _, w := range windows {
		d.windows = append(d.windows, w)
		if w.Foreground {
			d.foreground = w.ID
		}
	}
	return d
}

func (d *delayedDesktop) after(delay time.Duration, apply func(d *delayedDesktop)) {
	d.due = append(d.due, delayedChange{at: time.Now().Add(delay), apply: apply})
}

// tick applies changes that are due. The caller holds d.mu.
func (d *delayedDesktop) tick() {
	now := time.Now()
	kept := d.due[:0]
	for _, change := range d.due {
		if now.Before(change.at) {
			kept = append(kept, change)
			continue
		}
		change.apply(d)
	}
	d.due = kept
}

func (d *delayedDesktop) open(w desktopWindowInfo) {
	for i := range d.windows {
		d.windows[i].Foreground = false
	}
	w.Foreground = true
	d.windows = append(d.windows, w)
	d.foreground = w.ID
}

func (d *delayedDesktop) retitle(id, name string) {
	for i := range d.windows {
		if d.windows[i].ID == id {
			d.windows[i].Name = name
		}
	}
}

func (d *delayedDesktop) input(kind, detail string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tick()
	if d.react != nil {
		d.react(d, kind, detail)
	}
}

func (d *delayedDesktop) ListWindows(context.Context) (json.RawMessage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tick()
	windows := make([]desktopWindowInfo, len(d.windows))
	copy(windows, d.windows)
	for i := range windows {
		windows[i].Foreground = windows[i].ID == d.foreground
	}
	return json.Marshal(windows)
}

func (d *delayedDesktop) ForegroundWindow() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tick()
	return d.foreground
}

func (d *delayedDesktop) Origin() computer.Point { return computer.Point{} }

func (d *delayedDesktop) Automation(ctx context.Context, p computer.AutomationRequest) (json.RawMessage, error) {
	switch p.Action {
	case "windows":
		d.mu.Lock()
		d.reads = append(d.reads, time.Now())
		d.mu.Unlock()
		return d.ListWindows(ctx)
	case "launch_app":
		d.mu.Lock()
		d.launches++
		d.mu.Unlock()
		d.input("launch_app", p.Name)
		return json.RawMessage(`{"launch_requested":true,"application":"` + p.Name + `","verify_required":true}`), nil
	case "focus":
		d.mu.Lock()
		d.tick()
		d.foreground = p.WindowID
		d.mu.Unlock()
		return json.RawMessage(`{"focused":true,"window_id":"` + p.WindowID + `"}`), nil
	case "find":
		d.mu.Lock()
		defer d.mu.Unlock()
		d.tick()
		d.reads = append(d.reads, time.Now())
		data, _ := json.Marshal(map[string]any{"count": 1, "matches": []map[string]any{{"element_id": p.ElementID, "name": d.values[p.ElementID]}}})
		return data, nil
	}
	return json.RawMessage(`{}`), nil
}

func (d *delayedDesktop) KeyPress(key string) error {
	_ = d.fakeComputerBackend.KeyPress(key)
	d.input("key", key)
	return nil
}

func (d *delayedDesktop) Hotkey(modifiers []string, key string) error {
	_ = d.fakeComputerBackend.Hotkey(modifiers, key)
	d.input("hotkey", strings.Join(modifiers, "+")+"+"+key)
	return nil
}

func (d *delayedDesktop) DoubleClick(x, y int) error {
	_ = d.fakeComputerBackend.DoubleClick(x, y)
	d.input("double_click", "")
	return nil
}

func (d *delayedDesktop) TypeText(text string) error {
	_ = d.fakeComputerBackend.TypeText(text)
	d.input("type", text)
	return nil
}

func (d *delayedDesktop) Screenshot() ([]byte, error) {
	d.mu.Lock()
	d.reads = append(d.reads, time.Now())
	d.mu.Unlock()
	return d.fakeComputerBackend.Screenshot()
}

func testSettleTiming() desktopSettleTiming {
	return desktopSettleTiming{MinSettle: 40 * time.Millisecond, Poll: 10 * time.Millisecond, ReadSettle: 300 * time.Millisecond, OpenWait: 600 * time.Millisecond, LaunchWait: 900 * time.Millisecond, Track: 5 * time.Second}
}

func settleState(t *testing.T, d *delayedDesktop) (*computerToolState, context.Context) {
	t.Helper()
	desktopTransitions.Lock()
	delete(desktopTransitions.sessions, t.Name())
	desktopTransitions.Unlock()
	ctx := context.WithValue(t.Context(), SessionIDContextKey, t.Name())
	return &computerToolState{backend: d, workingDir: t.TempDir(), actionTimeout: 5 * time.Second, settleTiming: testSettleTiming()}, ctx
}

func runDesktop(t *testing.T, s *computerToolState, ctx context.Context, action string, p ComputerParams) fantasy.ToolResponse {
	t.Helper()
	p.Action = action
	resp, err := s.runWithTimeout(ctx, action, p)
	require.NoError(t, err)
	return resp
}

var (
	explorerWindow = desktopWindowInfo{ID: "853342", Name: "Atlas-Test", ProcessID: 4100, ProcessName: "explorer.exe", WindowClass: "CabinetWClass", Foreground: true}
	notepadDraft   = desktopWindowInfo{ID: "26017886", Name: "hesap - Not Defteri", ProcessID: 5200, ProcessName: "notepad.exe", WindowClass: "Notepad"}
	notepadOpened  = desktopWindowInfo{ID: "526168", Name: "sonuc - Not Defteri", ProcessID: 5300, ProcessName: "notepad.exe", WindowClass: "Notepad"}
)

// Session 9be3b932: Enter on a file opened Notepad about a second later; a
// window list taken 52 ms after Enter showed nothing, and the model started a
// second open with Ctrl+O in another window.
func TestDelayedFileOpenIsReportedAndBlocksSecondOpen(t *testing.T) {
	t.Parallel()
	d := newDelayedDesktop(explorerWindow, notepadDraft)
	d.react = func(d *delayedDesktop, kind, detail string) {
		if kind == "key" && detail == "enter" && d.foreground == explorerWindow.ID {
			d.after(150*time.Millisecond, func(d *delayedDesktop) { d.open(notepadOpened) })
		}
	}
	s, ctx := settleState(t, d)
	resp := runDesktop(t, s, ctx, "key", ComputerParams{Key: "enter", Automation: computer.AutomationRequest{WindowID: explorerWindow.ID}})
	require.False(t, resp.IsError, resp.Content)

	// As in the session, the model focuses the other window and sends
	// Ctrl+O without an observation in between. Neither input is sent: the
	// delayed window is reported instead.
	resp = runDesktop(t, s, ctx, "focus", ComputerParams{Automation: computer.AutomationRequest{WindowID: notepadDraft.ID}})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "delayed_transition")
	require.Contains(t, resp.Content, notepadOpened.ID)
	got, ok := ResponseOutcome(resp)
	require.True(t, ok)
	require.Equal(t, "none", got.Dispatch)
	require.Equal(t, notepadOpened.ID, d.ForegroundWindow(), "focus was not moved away from the new window")
	resp = runDesktop(t, s, ctx, "hotkey", ComputerParams{Key: "o", Modifiers: "ctrl", Automation: computer.AutomationRequest{WindowID: notepadDraft.ID}})
	require.True(t, resp.IsError)
	require.Empty(t, d.hotkeys, "the second open was not dispatched")

	// The window is now known; a deliberate retry is not refused again.
	d.mu.Lock()
	d.foreground = notepadDraft.ID
	d.mu.Unlock()
	resp = runDesktop(t, s, ctx, "hotkey", ComputerParams{Key: "o", Modifiers: "ctrl", Automation: computer.AutomationRequest{WindowID: notepadDraft.ID}})
	require.False(t, resp.IsError, resp.Content)
	require.Len(t, d.hotkeys, 1)
}

// A window list requested right after an opening input waits for the
// expected change instead of reporting the stale pre-input state.
func TestWindowListAfterOpenWaitsForDelayedWindow(t *testing.T) {
	// Not parallel: the desktop lease is shared, and this test measures time.
	d := newDelayedDesktop(explorerWindow)
	d.react = func(d *delayedDesktop, kind, _ string) {
		if kind == "hotkey" {
			d.after(200*time.Millisecond, func(d *delayedDesktop) {
				d.open(desktopWindowInfo{ID: "777", Name: "Dosya Gezgini", ProcessID: 4100, ProcessName: "explorer.exe", WindowClass: "CabinetWClass"})
			})
		}
	}
	s, ctx := settleState(t, d)
	runDesktop(t, s, ctx, "hotkey", ComputerParams{Key: "e", Modifiers: "win"})
	started := time.Now()
	resp := runDesktop(t, s, ctx, "windows", ComputerParams{})
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, `"window_id":"777"`, "the delayed window is in the list")
	var result struct {
		Settle desktopSettleReport `json:"input_settle"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &result), resp.Content)
	require.Equal(t, "changed", result.Settle.State)
	require.Equal(t, []string{"777"}, result.Settle.NewWindowIDs)
	require.Equal(t, []string{"777"}, result.Settle.TaskOpenedIDs)
	require.Less(t, time.Since(started), 600*time.Millisecond, "the wait ends at the change, not at the bound")

	// The reported window is acknowledged: a later launch is not refused.
	resp = runDesktop(t, s, ctx, "launch_app", ComputerParams{Automation: computer.AutomationRequest{Name: "Not Defteri"}})
	require.False(t, resp.IsError, resp.Content)
}

// launch_app waits for the launched window and reports it; a launch whose
// window has not appeared yet is reported as pending, not as success or
// failure, and a second launch waits for the first one to resolve.
func TestLaunchReportsLateWindowAndDoesNotStartSecondInstance(t *testing.T) {
	t.Parallel()
	existing := desktopWindowInfo{ID: "1901574", Name: "Hesap Makinesi", ProcessID: 900, ProcessName: "ApplicationFrameHost.exe", WindowClass: "ApplicationFrameWindow", Foreground: true}
	other := desktopWindowInfo{ID: "1050808", Name: "Hesap Makinesi", ProcessID: 900, ProcessName: "ApplicationFrameHost.exe", WindowClass: "ApplicationFrameWindow"}
	d := newDelayedDesktop(existing, other)
	d.react = func(d *delayedDesktop, kind, _ string) {
		if kind == "launch_app" && d.launches == 1 {
			// A splash phase longer than the launch wait.
			d.after(1200*time.Millisecond, func(d *delayedDesktop) {
				d.open(desktopWindowInfo{ID: "591766", Name: "Hesap Makinesi", ProcessID: 900, ProcessName: "ApplicationFrameHost.exe", WindowClass: "ApplicationFrameWindow"})
			})
		}
	}
	s, ctx := settleState(t, d)
	resp := runDesktop(t, s, ctx, "launch_app", ComputerParams{Automation: computer.AutomationRequest{Name: "Hesap Makinesi"}})
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, `"state":"pending"`, "not yet open is reported as pending")

	resp = runDesktop(t, s, ctx, "launch_app", ComputerParams{Automation: computer.AutomationRequest{Name: "Hesap Makinesi"}})
	require.True(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "delayed_transition")
	require.Contains(t, resp.Content, "591766")
	require.Equal(t, 1, d.launches, "only one instance was launched")
	require.NotContains(t, resp.Content, "1901574", "pre-existing windows are not reported as new")
}

// Pre-existing windows of the same application stay separate from the
// window this task opened.
func TestNewWindowOfAlreadyOpenApplicationIsTrackedSeparately(t *testing.T) {
	t.Parallel()
	existing := desktopWindowInfo{ID: "1901574", Name: "Hesap Makinesi", ProcessID: 900, ProcessName: "ApplicationFrameHost.exe", WindowClass: "ApplicationFrameWindow", Foreground: true}
	d := newDelayedDesktop(existing)
	d.react = func(d *delayedDesktop, kind, _ string) {
		if kind == "launch_app" {
			d.after(80*time.Millisecond, func(d *delayedDesktop) {
				d.open(desktopWindowInfo{ID: "591766", Name: "Hesap Makinesi", ProcessID: 900, ProcessName: "ApplicationFrameHost.exe", WindowClass: "ApplicationFrameWindow"})
			})
		}
	}
	s, ctx := settleState(t, d)
	resp := runDesktop(t, s, ctx, "launch_app", ComputerParams{Automation: computer.AutomationRequest{Name: "Hesap Makinesi"}})
	require.False(t, resp.IsError, resp.Content)
	var result struct {
		Settle desktopSettleReport `json:"input_settle"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &result), resp.Content)
	require.Equal(t, []string{"591766"}, result.Settle.NewWindowIDs)
	require.Equal(t, []string{"591766"}, result.Settle.TaskOpenedIDs)
}

// A document retitle or folder navigation is a change of the same window:
// it settles the read and is never mistaken for a new application window.
func TestRetitleSettlesReadWithoutNewWindow(t *testing.T) {
	t.Parallel()
	d := newDelayedDesktop(desktopWindowInfo{ID: "853342", Name: "Masaüstü", ProcessID: 4100, ProcessName: "explorer.exe", WindowClass: "CabinetWClass", Foreground: true})
	retitled := make(chan time.Time, 1)
	d.react = func(d *delayedDesktop, kind, _ string) {
		if kind == "double_click" {
			d.after(120*time.Millisecond, func(d *delayedDesktop) {
				d.retitle("853342", "Atlas-Test")
				retitled <- time.Now()
			})
		}
	}
	s, ctx := settleState(t, d)
	d.screenshot = testPNG(t, 4, 4)
	runDesktop(t, s, ctx, "double_click", ComputerParams{X: 10, Y: 10})
	resp := runDesktop(t, s, ctx, "screenshot", ComputerParams{})
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "Input settle: changed after double_click")
	at := <-retitled
	d.mu.Lock()
	captured := d.reads[len(d.reads)-1]
	d.mu.Unlock()
	require.True(t, captured.After(at), "the capture follows the navigation instead of racing it")
	require.Empty(t, DesktopOpenTransitions(ctx, t.Name()))
}

// Session 9be3b932: type, Enter and the result read in one response. The
// read must observe the value the application shows after Enter.
func TestTypeEnterReadbackReadsSettledValue(t *testing.T) {
	t.Parallel()
	calc := desktopWindowInfo{ID: "591766", Name: "Hesap Makinesi", ProcessID: 900, ProcessName: "ApplicationFrameHost.exe", WindowClass: "ApplicationFrameWindow", Foreground: true}
	d := newDelayedDesktop(calc)
	d.values["result"] = "Ekran değeri 0"
	d.react = func(d *delayedDesktop, kind, detail string) {
		if kind == "key" && detail == "enter" {
			d.after(30*time.Millisecond, func(d *delayedDesktop) { d.values["result"] = "Ekran değeri 69.104" })
		}
	}
	s, ctx := settleState(t, d)
	scope := computer.AutomationRequest{WindowID: calc.ID}
	runDesktop(t, s, ctx, "type", ComputerParams{Text: "1234*56", Automation: scope})
	runDesktop(t, s, ctx, "key", ComputerParams{Key: "enter", Automation: scope})
	resp := runDesktop(t, s, ctx, "find", ComputerParams{Automation: computer.AutomationRequest{WindowID: calc.ID, ElementID: "result"}})
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "69.104")
	require.Equal(t, []string{"1234*56"}, d.typed)
	require.Equal(t, []string{"enter"}, d.keys)
}

// Cancellation while an input waits for an earlier transition sends nothing,
// and a result that arrives after cancellation starts no input.
func TestCanceledWaitSendsNoInput(t *testing.T) {
	t.Parallel()
	d := newDelayedDesktop(explorerWindow)
	s, ctx := settleState(t, d)
	s.settleTiming.OpenWait = 3 * time.Second
	runDesktop(t, s, ctx, "key", ComputerParams{Key: "enter", Automation: computer.AutomationRequest{WindowID: explorerWindow.ID}})
	canceled, cancel := context.WithCancel(ctx)
	time.AfterFunc(80*time.Millisecond, cancel)
	resp, err := s.runWithTimeout(canceled, "hotkey", ComputerParams{Action: "hotkey", Key: "o", Modifiers: "ctrl", Automation: computer.AutomationRequest{WindowID: explorerWindow.ID}})
	require.Error(t, err)
	require.True(t, resp.IsError)
	// A window that appears after the cancel must not trigger the input.
	d.mu.Lock()
	d.open(notepadOpened)
	d.mu.Unlock()
	time.Sleep(50 * time.Millisecond)
	require.Empty(t, d.hotkeys)
}

// An unresolved recipe input has no proof that permits skipping readiness.
func TestUnverifiedRecipeReadsWaitForTransition(t *testing.T) {
	// Not parallel: the desktop lease is shared, and this test measures time.
	d := newDelayedDesktop(explorerWindow)
	s, ctx := settleState(t, d)
	s.settleTiming.OpenWait = 2 * time.Second
	runDesktop(t, s, ctx, "key", ComputerParams{Key: "enter", Automation: computer.AutomationRequest{WindowID: explorerWindow.ID}})
	started := time.Now()
	resp, err := s.runWithTimeout(ctx, "windows", ComputerParams{Action: "windows"})
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, `"state":"pending"`)
	require.GreaterOrEqual(t, time.Since(started), 1500*time.Millisecond)
}

// The completion check sees a window that appeared after an input and was
// never reported to the model; reporting it closes that open work.
func TestUnobservedTransitionIsOpenWork(t *testing.T) {
	t.Parallel()
	d := newDelayedDesktop(explorerWindow, notepadDraft)
	s, ctx := settleState(t, d)
	runDesktop(t, s, ctx, "key", ComputerParams{Key: "enter", Automation: computer.AutomationRequest{WindowID: explorerWindow.ID}})
	require.Empty(t, DesktopOpenTransitions(ctx, t.Name()), "nothing appeared yet")
	d.mu.Lock()
	d.open(notepadOpened)
	d.foreground = notepadDraft.ID
	d.mu.Unlock()
	open := DesktopOpenTransitions(ctx, t.Name())
	require.Len(t, open, 1)
	require.Contains(t, open[0], notepadOpened.ID)
	resp := runDesktop(t, s, ctx, "hotkey", ComputerParams{Key: "o", Modifiers: "ctrl", Automation: computer.AutomationRequest{WindowID: notepadDraft.ID}})
	require.Contains(t, resp.Content, "delayed_transition")
	require.Empty(t, DesktopOpenTransitions(ctx, t.Name()), "the refusal reported the window")
}

// The shared guard lets a deliberate retry through after a delayed
// transition refusal instead of treating it as an unchanged failure.
func TestGuardAllowsRetryAfterDelayedTransition(t *testing.T) {
	t.Parallel()
	resp := delayedTransitionResponse(desktopTransition{Action: "key", Target: "1"}, []desktopWindowInfo{notepadOpened}, "hotkey")
	code, class := classifyFailure(t.Context(), resp, nil, false)
	require.Equal(t, "delayed_transition", code)
	require.Equal(t, failureTransition, class)
	require.Nil(t, repeatBlock(&desktopRecord{code: code, class: class, failures: 3}, &desktopGuardState{}, false))
}

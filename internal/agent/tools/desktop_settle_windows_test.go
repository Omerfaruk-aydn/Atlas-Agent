//go:build windows

package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/stretchr/testify/require"
)

// settleFixtureScript is an owned WPF window. Its button opens a child window
// 700 ms later, like an application that opens a document or dialog late,
// and its multi-line text box records exactly what keyboard input produced.
const settleFixtureScript = `Add-Type -AssemblyName PresentationFramework
$form=New-Object System.Windows.Window; $form.Title='Atlas settle fixture'; $form.Width=360; $form.Height=260; $form.Left=40; $form.Top=40; $form.Topmost=$true
$panel=New-Object System.Windows.Controls.StackPanel
$doc=New-Object System.Windows.Controls.TextBox; $doc.AcceptsReturn=$true; $doc.Height=120; [System.Windows.Automation.AutomationProperties]::SetName($doc,'Fixture document')
$open=New-Object System.Windows.Controls.Button; $open.Content='Open later'
$script:count=0
$open.Add_Click({ $t=New-Object System.Windows.Threading.DispatcherTimer; $t.Interval=[TimeSpan]::FromMilliseconds(700); $t.Add_Tick({ $this.Stop(); $script:count++; $w=New-Object System.Windows.Window; $w.Title=('Atlas delayed child ' + $script:count); $w.Width=220; $w.Height=120; $w.Left=420; $w.Top=40; $w.ShowActivated=$false; $w.Show() }.GetNewClosure()); $t.Start() })
$null=$panel.Children.Add($doc); $null=$panel.Children.Add($open); $form.Content=$panel
$form.Add_ContentRendered({ $null=$doc.Focus(); $helper=New-Object System.Windows.Interop.WindowInteropHelper($form); [System.IO.File]::WriteAllText($env:ATLAS_FIXTURE_READY,[string]$helper.Handle.ToInt64()) })
$app=New-Object System.Windows.Application; $null=$app.Run($form)`

func startSettleFixture(t *testing.T, ctx context.Context) string {
	t.Helper()
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := exec.CommandContext(ctx, "powershell.exe", "-STA", "-NoProfile", "-NonInteractive", "-Command", settleFixtureScript)
	cmd.Env = append(os.Environ(), "ATLAS_FIXTURE_READY="+ready)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	for {
		if data, err := os.ReadFile(ready); err == nil && len(data) > 0 {
			return string(data)
		}
		select {
		case <-ctx.Done():
			t.Fatal("Fixture not ready")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func fixtureChildren(t *testing.T, ctx context.Context, backend computer.Backend) []desktopWindowInfo {
	t.Helper()
	data, err := backend.(desktopWindowLister).ListWindows(ctx)
	require.NoError(t, err)
	var windows []desktopWindowInfo
	require.NoError(t, json.Unmarshal(data, &windows))
	var children []desktopWindowInfo
	for _, w := range windows {
		if strings.HasPrefix(w.Name, "Atlas delayed child") {
			children = append(children, w)
		}
	}
	return children
}

// The native path: a real late window is waited for by the read that
// follows the input, a second open while it is unreported is refused before
// dispatch, and multi-line text arrives as real line breaks.
func TestNativeDelayedWindowSettleFixture(t *testing.T) {
	if os.Getenv("ATLAS_DESKTOP_FIXTURE") != "1" {
		t.Skip("Set ATLAS_DESKTOP_FIXTURE=1 for owned desktop fixture")
	}
	ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), SessionIDContextKey, t.Name()), 60*time.Second)
	defer cancel()
	id := startSettleFixture(t, ctx)
	backend, err := computer.Open()
	require.NoError(t, err)
	s := &computerToolState{backend: backend, workingDir: t.TempDir(), actionTimeout: 20 * time.Second}
	run := func(action string, p ComputerParams) (string, bool) {
		t.Helper()
		p.Action = action
		resp, err := s.runWithTimeout(ctx, action, p)
		require.NoError(t, err)
		return resp.Content, resp.IsError
	}
	openLater := computer.AutomationRequest{WindowID: id, Name: "Open later", Role: "ControlType.Button"}

	// Invoke opens the child 700 ms later; the very next window list waits.
	content, failed := run("invoke", ComputerParams{Automation: openLater})
	require.False(t, failed, content)
	started := time.Now()
	content, failed = run("windows", ComputerParams{})
	require.False(t, failed, content)
	waited := time.Since(started)
	var listed struct {
		Settle desktopSettleReport `json:"input_settle"`
		Result []desktopWindowInfo `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(content), &listed), content)
	require.Equal(t, "changed", listed.Settle.State, content)
	require.Len(t, listed.Settle.NewWindowIDs, 1)
	require.True(t, strings.HasPrefix(nameOf(listed.Result, listed.Settle.NewWindowIDs[0]), "Atlas delayed child"))
	t.Logf("Delayed child reported by the first window list after %s (input_settle waited %d ms)", waited.Round(time.Millisecond), listed.Settle.WaitedMS)

	// A second child opens late while the model does not look; the next
	// open is refused before dispatch instead of starting a third window.
	content, failed = run("invoke", ComputerParams{Automation: openLater})
	require.False(t, failed, content)
	time.Sleep(1200 * time.Millisecond)
	before := len(fixtureChildren(t, ctx, backend))
	content, failed = run("invoke", ComputerParams{Automation: openLater})
	require.True(t, failed)
	require.Contains(t, content, "delayed_transition")
	time.Sleep(1200 * time.Millisecond)
	require.Equal(t, before, len(fixtureChildren(t, ctx, backend)), "the refused open started no window")
	require.Equal(t, 2, before)

	// Multi-line text in one call, read back from the control.
	content, failed = run("focus", ComputerParams{Automation: computer.AutomationRequest{WindowID: id}})
	if failed || backend.(interface{ ForegroundWindow() string }).ForegroundWindow() != id {
		t.Logf("Fixture not foreground (%s); keyboard part omitted", content)
		return
	}
	content, failed = run("type", ComputerParams{Text: "1234 x 56 = 69104\n69104 / 8 = 8638\n8638 + 362 = 9000", Automation: computer.AutomationRequest{WindowID: id}})
	require.False(t, failed, content)
	content, failed = run("assert", ComputerParams{Automation: computer.AutomationRequest{WindowID: id, Name: "Fixture document", Condition: "value", Expected: "1234 x 56 = 69104\r\n69104 / 8 = 8638\r\n8638 + 362 = 9000", WaitMS: 3000}})
	require.False(t, failed, content)
	t.Log("Three lines typed with one call and read back from the control value")
}

func nameOf(windows []desktopWindowInfo, id string) string {
	for _, w := range windows {
		if w.ID == id {
			return w.Name
		}
	}
	return ""
}

//go:build windows

package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestComputerObservationOwnWindowsFixture(t *testing.T) {
	if os.Getenv("ATLAS_DESKTOP_FIXTURE") != "1" {
		t.Skip("Set ATLAS_DESKTOP_FIXTURE=1 for owned desktop fixture")
	}
	ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), SessionIDContextKey, "workflow-fixture"), 30*time.Second)
	defer cancel()
	ready := filepath.Join(t.TempDir(), "ready")
	script := `Add-Type -AssemblyName PresentationFramework
 $form=New-Object System.Windows.Window; $form.Title='Atlas workflow fixture'; $form.ShowInTaskbar=$false; $form.Width=300; $form.Height=200; $form.Left=40; $form.Top=40
 $panel=New-Object System.Windows.Controls.StackPanel
 $input=New-Object System.Windows.Controls.TextBox; [System.Windows.Automation.AutomationProperties]::SetName($input,'Fixture search'); $input.Add_KeyDown({if($_.Key -eq [System.Windows.Input.Key]::Return){$label.Text='Submitted'}})
 $label=New-Object System.Windows.Controls.TextBlock; $label.Text='Waiting'
 $first=New-Object System.Windows.Controls.Button; $first.Content='Duplicate'
 $second=New-Object System.Windows.Controls.Button; $second.Content='Duplicate'
 $timer=New-Object System.Windows.Threading.DispatcherTimer; $timer.Interval=[TimeSpan]::FromMilliseconds(500); $timer.Add_Tick({$label.Text='Ready';$timer.Stop()})
 $first.Add_Click({$timer.Start()}); $second.Add_Click({$label.Text='Wrong'})
 $change=New-Object System.Windows.Controls.Button; $change.Content='Replace'; $change.Add_Click({$change.Content='Changed'})
 $null=$panel.Children.Add($input); $null=$panel.Children.Add($first); $null=$panel.Children.Add($second); $null=$panel.Children.Add($change); $null=$panel.Children.Add($label); $form.Content=$panel
 $form.Add_ContentRendered({$helper=New-Object System.Windows.Interop.WindowInteropHelper($form);[System.IO.File]::WriteAllText($env:ATLAS_FIXTURE_READY,[string]$helper.Handle.ToInt64())})
 $app=New-Object System.Windows.Application; $null=$app.Run($form)`
	cmd := exec.CommandContext(ctx, "powershell.exe", "-STA", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(os.Environ(), "ATLAS_FIXTURE_READY="+ready)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	require.NoError(t, cmd.Start())
	defer func() { cancel(); _ = cmd.Wait() }()
	var id string
	for id == "" {
		data, err := os.ReadFile(ready)
		if err == nil {
			id = string(data)
		}
		if id == "" {
			select {
			case <-ctx.Done():
				t.Fatal("Fixture not ready")
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	backend, err := computer.Open()
	require.NoError(t, err)
	s := &computerToolState{backend: backend}
	observation, err := s.runComputerAction(ctx, "observe", ComputerParams{Automation: computer.AutomationRequest{WindowID: id}})
	require.NoError(t, err)
	require.False(t, observation.IsError, observation.Content)
	require.Equal(t, "image", observation.Type)
	var o desktopObservation
	require.NoError(t, json.Unmarshal([]byte(observation.Content), &o))
	var duplicates []desktopElement
	var changed desktopElement
	for _, e := range o.Elements {
		if e.Name == "Duplicate" && e.Role == "ControlType.Button" {
			duplicates = append(duplicates, e)
		}
		if e.Name == "Replace" && e.Role == "ControlType.Button" {
			changed = e
		}
	}
	require.Len(t, duplicates, 2)
	require.NotEmpty(t, changed.ID)
	r, err := s.runComputerAction(ctx, "invoke", ComputerParams{SnapshotID: o.ID, Element: duplicates[0].Number})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	r, err = s.runComputerAction(ctx, "assert", ComputerParams{Automation: computer.AutomationRequest{WindowID: id, Name: "Ready", Condition: "visible", WaitMS: 5000}})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	observation, err = s.runComputerAction(ctx, "observe", ComputerParams{Automation: computer.AutomationRequest{WindowID: id}})
	require.NoError(t, err)
	require.False(t, observation.IsError, observation.Content)
	require.NoError(t, json.Unmarshal([]byte(observation.Content), &o))
	for _, e := range o.Elements {
		if e.Name == "Replace" && e.Role == "ControlType.Button" {
			changed = e
		}
	}
	// Simulate an application-side target replacement between observation and action.
	driver := backend.(computer.AutomationBackend)
	_, err = driver.Automation(ctx, computer.AutomationRequest{Action: "invoke", WindowID: id, ElementID: changed.ID})
	require.NoError(t, err)
	r, err = s.runComputerAction(ctx, "invoke", ComputerParams{SnapshotID: o.ID, Element: changed.Number})
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Contains(t, r.Content, "stale_observation")
	if backend.(interface{ ForegroundWindow() string }).ForegroundWindow() == id {
		invoke := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			var p ComputerParams
			if err := json.Unmarshal([]byte(call.Input), &p); err != nil {
				return fantasy.ToolResponse{}, err
			}
			return s.runComputerAction(ctx, p.Action, p)
		}
		r, err = runDesktopWorkflow(ctx, DesktopWorkflowParams{Mode: "fill_submit", Input: ComputerParams{Action: "set_value", Automation: computer.AutomationRequest{WindowID: id, Name: "Fixture search", Role: "ControlType.Edit", Text: "Verified fixture query"}}, WaitFor: computer.AutomationRequest{Name: "Submitted", Condition: "visible", WaitMS: 5000}}, fantasy.ToolCall{ID: "owned-search"}, invoke)
		require.NoError(t, err)
		require.False(t, r.IsError, r.Content)
		require.Equal(t, "image", r.Type)
		require.Contains(t, r.Content, `"condition_verified":true`)
		t.Log("Owned field focused, value verified, Enter submitted and expected state observed")
	} else {
		t.Log("Owned fixture not foreground; physical submission omitted")
	}
}

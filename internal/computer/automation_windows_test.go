//go:build windows

package computer

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

func TestWindowsAutomationReadAndFixtureOCR(t *testing.T) {
	if testing.Short() {
		t.Skip("Windows runtime fixture excluded by short mode")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	backend := &windowsBackend{}
	data, err := backend.Automation(ctx, AutomationRequest{Action: "monitors"})
	require.NoError(t, err)
	var monitors []map[string]any
	require.NoError(t, json.Unmarshal(data, &monitors))
	require.NotEmpty(t, monitors)
	data, err = backend.Automation(ctx, AutomationRequest{Action: "windows"})
	require.NoError(t, err)
	var windows []map[string]any
	require.NoError(t, json.Unmarshal(data, &windows))
	large := image.NewRGBA(image.Rect(0, 0, 720, 160))
	for y := range 160 {
		for x := range 720 {
			large.Set(x, y, color.White)
		}
	}
	parsed, err := opentype.Parse(goregular.TTF)
	require.NoError(t, err)
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: 48, DPI: 96, Hinting: font.HintingFull})
	require.NoError(t, err)
	defer face.Close()
	drawer := font.Drawer{Dst: large, Src: image.NewUniform(color.Black), Face: face, Dot: fixed.P(20, 90)}
	drawer.DrawString("ATLAS 12345")
	path := filepath.Join(t.TempDir(), "ocr-fixture.png")
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, png.Encode(f, large))
	require.NoError(t, f.Close())
	data, err = backend.Automation(ctx, AutomationRequest{Action: "ocr", ImagePath: path, Name: "12345"})
	require.NoError(t, err)
	var result struct {
		Text    string                                                    `json:"text"`
		Matches []struct{ X, Y, Width, Height, CenterX, CenterY float64 } `json:"matches"`
	}
	require.NoError(t, json.Unmarshal(data, &result))
	require.Contains(t, strings.ToUpper(result.Text), "ATLAS")
	require.Contains(t, result.Text, "12345")
	require.Len(t, result.Matches, 1)
	require.Greater(t, result.Matches[0].Width, float64(0))
	// Repeated labels must remain ambiguous rather than pick the first row.
	drawer.Dot = fixed.P(500, 90)
	drawer.DrawString("12345")
	f, err = os.Create(path)
	require.NoError(t, err)
	require.NoError(t, png.Encode(f, large))
	require.NoError(t, f.Close())
	data, err = backend.Automation(ctx, AutomationRequest{Action: "ocr", ImagePath: path, Name: "12345"})
	require.NoError(t, err)
	var duplicate struct {
		MatchCount int  `json:"match_count"`
		Unique     bool `json:"unique_match"`
	}
	require.NoError(t, json.Unmarshal(data, &duplicate))
	require.Equal(t, 2, duplicate.MatchCount, string(data))
	require.False(t, duplicate.Unique)
}

func TestWindowsAutomationOwnForm(t *testing.T) {
	if os.Getenv("ATLAS_DESKTOP_FIXTURE") != "1" {
		t.Skip("Set ATLAS_DESKTOP_FIXTURE=1 to test an ephemeral GUI fixture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	ready := filepath.Join(t.TempDir(), "ready.txt")
	cmd := exec.CommandContext(ctx, "powershell.exe", "-STA", "-NoProfile", "-NonInteractive", "-Command", `Add-Type -AssemblyName PresentationFramework; $form=New-Object System.Windows.Window; $form.Title='Atlas automation fixture'; $form.ShowInTaskbar=$false; $form.Width=300; $form.Height=160; $form.Left=20; $form.Top=20; $panel=New-Object System.Windows.Controls.StackPanel; $input=New-Object System.Windows.Controls.TextBox; [System.Windows.Automation.AutomationProperties]::SetName($input,'Fixture input'); $button=New-Object System.Windows.Controls.Button; $button.Content='Fixture save'; $label=New-Object System.Windows.Controls.TextBlock; $label.Text='Waiting'; $button.Add_Click({$label.Text='Saved'}); $null=$panel.Children.Add($input); $null=$panel.Children.Add($button); $null=$panel.Children.Add($label); $ghost=New-Object System.Windows.Controls.Button; $ghost.Content='Fixture save'; $ghost.Visibility=[System.Windows.Visibility]::Collapsed; $null=$panel.Children.Add($ghost); $rejectInput=New-Object System.Windows.Controls.TextBox; [System.Windows.Automation.AutomationProperties]::SetName($rejectInput,'Rejecting input'); $rejectInput.Add_TextChanged({if($rejectInput.Text.Length -gt 0){$rejectInput.Text=''}}); $null=$panel.Children.Add($rejectInput); $form.Content=$panel; $form.Add_ContentRendered({$null=$input.Focus();$helper=New-Object System.Windows.Interop.WindowInteropHelper($form); [System.IO.File]::WriteAllText($env:ATLAS_FIXTURE_READY,[string]$helper.Handle.ToInt64())}); $app=New-Object System.Windows.Application; $null=$app.Run($form)`)
	cmd.Env = append(os.Environ(), "ATLAS_FIXTURE_READY="+ready)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	require.NoError(t, cmd.Start())
	defer func() { cancel(); _ = cmd.Wait() }()
	var hwnd string
	for {
		data, err := os.ReadFile(ready)
		if err == nil && len(data) > 0 {
			hwnd = string(data)
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("Fixture form did not open")
		case <-time.After(50 * time.Millisecond):
		}
	}
	driver := &windowsBackend{}
	windowsData, err := driver.Automation(ctx, AutomationRequest{Action: "windows"})
	require.NoError(t, err)
	require.Contains(t, string(windowsData), "Atlas automation fixture")
	require.Contains(t, string(windowsData), `"foreground"`)
	focused, err := driver.Automation(ctx, AutomationRequest{Action: "focus", WindowID: hwnd})
	if err != nil {
		// Windows may deny foreground activation while the user is active.
		require.ErrorContains(t, err, "focus_denied")
		require.Empty(t, focused)
		t.Log("Foreground activation denied by Windows; no success reported")
	} else {
		require.Contains(t, string(focused), `"focused":true`)
		require.Equal(t, hwnd, driver.ForegroundWindow())
	}
	run := func(p AutomationRequest) string {
		t.Helper()
		p.WindowID = hwnd
		data, err := driver.Automation(ctx, p)
		require.NoError(t, err)
		return string(data)
	}
	inspection := run(AutomationRequest{Action: "inspect"})
	require.Contains(t, inspection, "Fixture input")
	require.Contains(t, inspection, "supported_patterns")
	var observed struct {
		Elements []struct {
			ID   string `json:"element_id"`
			Name string `json:"name"`
		} `json:"elements"`
	}
	require.NoError(t, json.Unmarshal([]byte(inspection), &observed))
	// An incomplete name scan must not trigger an action on an unseen target.
	_, incompleteErr := driver.Automation(ctx, AutomationRequest{Action: "invoke", WindowID: hwnd, Name: "Fixture save", MaxElements: 1})
	require.ErrorContains(t, incompleteErr, "observation_incomplete")
	_, rootPatternErr := driver.Automation(ctx, AutomationRequest{Action: "invoke", WindowID: hwnd, ElementID: observed.Elements[0].ID, MaxElements: 1})
	require.ErrorContains(t, rootPatternErr, "unsupported_pattern")
	require.Contains(t, run(AutomationRequest{Action: "inspect", MaxElements: 1}), `"truncated":true`)
	_, patternErr := driver.Automation(ctx, AutomationRequest{Action: "invoke", WindowID: hwnd, Name: "Fixture input"})
	require.ErrorContains(t, patternErr, "unsupported_pattern")
	require.Contains(t, run(AutomationRequest{Action: "find", Name: "Fixture input"}), `"count":1`)
	valueResult := run(AutomationRequest{Action: "set_value", Name: "Fixture input", Text: "Örnek 123"})
	require.Contains(t, valueResult, `"value_verified":true`)
	require.Contains(t, valueResult, `"keyboard_focused":`)
	require.Contains(t, run(AutomationRequest{Action: "assert", Name: "Fixture input", Condition: "value", Expected: "Örnek 123"}), `"passed":true`)
	require.Contains(t, run(AutomationRequest{Action: "assert", Name: "Fixture save", Role: "ControlType.Button", Condition: "enabled"}), `"passed":true`)
	if driver.ForegroundWindow() == hwnd {
		require.NoError(t, driver.Hotkey([]string{"ctrl"}, "a"))
		if driver.ForegroundWindow() == hwnd {
			require.NoError(t, driver.TypeText("Shortcut verified"))
			require.Contains(t, run(AutomationRequest{Action: "assert", Name: "Fixture input", Condition: "value", Expected: "Shortcut verified"}), `"passed":true`)
			t.Log("Physical Ctrl+A replaced the entire owned fixture input")
		}
	} else {
		t.Log("Owned fixture not foreground; physical keyboard test omitted")
	}
	run(AutomationRequest{Action: "invoke", Name: "Fixture save", Role: "ControlType.Button"})
	require.Contains(t, run(AutomationRequest{Action: "assert", Name: "Saved", Condition: "visible"}), `"passed":true`)
	_, rejectedValueErr := driver.Automation(ctx, AutomationRequest{Action: "set_value", WindowID: hwnd, Name: "Rejecting input", Text: "Rejected value"})
	require.ErrorContains(t, rejectedValueErr, "value_not_applied")
}

func BenchmarkWindowsNativeWindowObservation(b *testing.B) {
	driver := &windowsBackend{}
	for b.Loop() {
		_, err := driver.Automation(b.Context(), AutomationRequest{Action: "windows"})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestWindowsLaunchUnknownAppDoesNotLaunch(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	driver := &windowsBackend{}
	_, err := driver.Automation(ctx, AutomationRequest{Action: "launch_app", Name: "Atlas nonexistent fixture 58c89b7c-7ca2-49e7"})
	require.ErrorContains(t, err, "target_missing")
}

func TestWindowsAppIdentifierValidationInTurkishLocale(t *testing.T) {
	var guard string
	for _, line := range strings.Split(automationScript, "\n") {
		if strings.Contains(line, "if ($appId -") {
			guard = strings.TrimSpace(line)
			break
		}
	}
	require.NotEmpty(t, guard)
	for _, tc := range []struct {
		id    string
		valid bool
	}{{"AppleInc.AppleMusicWin_nzyj5cx40ttqa!App", true}, {"Microsoft.WindowsCalculator_8wekyb3d8bbwe!App", true}, {"a;calc.exe", false}, {`a"b`, false}} {
		t.Run(tc.id, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			script := fmt.Sprintf(`[Threading.Thread]::CurrentThread.CurrentCulture=[Globalization.CultureInfo]::GetCultureInfo('tr-TR'); $appId=$env:ATLAS_TEST_APP_ID; %s; Write-Output 'accepted'`, guard)
			cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
			cmd.Env = append(os.Environ(), "ATLAS_TEST_APP_ID="+tc.id)
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			output, err := cmd.CombinedOutput()
			if tc.valid {
				require.NoError(t, err, string(output))
				require.Contains(t, string(output), "accepted")
			} else {
				require.Error(t, err)
				require.Contains(t, string(output), "Unsupported application identifier")
			}
		})
	}
}

func TestWindowsLaunchInstalledApp(t *testing.T) {
	name := os.Getenv("ATLAS_TEST_LAUNCH_APP")
	if name == "" {
		t.Skip("Set ATLAS_TEST_LAUNCH_APP to verify an explicitly selected installed application")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	driver := &windowsBackend{}
	data, err := driver.Automation(ctx, AutomationRequest{Action: "launch_app", Name: name})
	require.NoError(t, err)
	require.Contains(t, string(data), `"launch_requested":true`)
	for {
		data, err = driver.Automation(ctx, AutomationRequest{Action: "windows"})
		require.NoError(t, err)
		var windows []struct {
			Name string `json:"name"`
			ID   string `json:"window_id"`
		}
		require.NoError(t, json.Unmarshal(data, &windows))
		for _, w := range windows {
			if strings.EqualFold(w.Name, name) {
				require.NotEmpty(t, w.ID)
				t.Log("Installed application launch accepted and visible window observed")
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("Requested application window did not appear")
		case <-time.After(200 * time.Millisecond):
		}
	}
}

//go:build windows

package computer

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAutomationWorkerCanceledRequestDoesNotStart(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	w := &automationWorker{}
	_, used, err := w.run(ctx, AutomationRequest{Action: "invoke"}, []byte(`{"action":"invoke"}`))
	require.True(t, used, "Do not fall back and dispatch a canceled request")
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, w.Started)
}

func TestAutomationWorkerQueuedRequestHonorsCancellation(t *testing.T) {
	w := &automationWorker{}
	w.mu.Lock()
	defer w.mu.Unlock()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, err := w.run(ctx, AutomationRequest{Action: "invoke"}, []byte(`{"action":"invoke"}`))
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("Canceled request waited for the occupied worker")
	}
	require.Zero(t, w.Started)
}

type partialWorkerWriter struct{ bytes int }

func (w partialWorkerWriter) Write(p []byte) (int, error) {
	return min(w.bytes, len(p)), io.ErrClosedPipe
}

func (partialWorkerWriter) Close() error { return nil }

func TestAutomationWorkerPartialWriteHasUnknownDelivery(t *testing.T) {
	w := &automationWorker{stdin: partialWorkerWriter{bytes: 1}}
	_, sent, err := w.exchange(t.Context(), []byte(`{"action":"invoke"}`))
	require.ErrorIs(t, err, io.ErrClosedPipe)
	require.True(t, sent, "A partial write must not permit mutation replay")
}

func TestAutomationWorkerDiagnosticsAreBounded(t *testing.T) {
	b := &boundedBuffer{}
	large := strings.Repeat("x", workerStderrLimit*2)
	n, err := b.Write([]byte(large))
	require.NoError(t, err)
	require.Equal(t, len(large), n)
	require.Len(t, b.String(), workerStderrLimit)
	_, err = b.Write([]byte("last error"))
	require.NoError(t, err)
	require.Len(t, b.String(), workerStderrLimit)
	require.True(t, strings.HasSuffix(b.String(), "last error"))
}

// TestAutomationWorkerReusesOneProcess checks that read-only requests are
// served by one worker and give the same result as the one-shot path.
func TestAutomationWorkerReusesOneProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	backend := &windowsBackend{}
	defer backend.automation.Close()
	request := AutomationRequest{Action: "monitors"}
	data, err := json.Marshal(request)
	require.NoError(t, err)

	first, err := backend.Automation(ctx, request)
	require.NoError(t, err)
	second, err := backend.Automation(ctx, request)
	require.NoError(t, err)
	once, err := backend.automationOnce(ctx, data)
	require.NoError(t, err)

	require.Equal(t, 1, backend.automation.Started)
	require.Equal(t, 1, backend.automation.Reused)
	require.JSONEq(t, string(once), string(first))
	require.JSONEq(t, string(first), string(second))
}

// TestAutomationWorkerReportsScriptErrors checks that a failure reported by
// the script reaches the caller with its code, the same as one-shot.
func TestAutomationWorkerReportsScriptErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	backend := &windowsBackend{}
	defer backend.automation.Close()
	// The script itself refuses a tree query without a window.
	_, err := backend.Automation(ctx, AutomationRequest{Action: "find", Name: "Save"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid_target")
	// The worker survives a reported failure.
	_, err = backend.Automation(ctx, AutomationRequest{Action: "monitors"})
	require.NoError(t, err)
	require.Equal(t, 1, backend.automation.Started)
}

// TestAutomationWorkerNeverResendsMutatingInput kills the worker while a
// mutating request is running. The request must not be sent again, and the
// caller must learn that its effect is unknown.
func TestAutomationWorkerNeverResendsMutatingInput(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "calls.txt")
	original := automationScript
	automationScript = `$p = $AtlasRequest
Add-Content -LiteralPath '` + marker + `' -Value $p.action
if ($p.action -eq 'invoke') { Start-Sleep -Seconds 30 }
'{"ok":true}'`
	defer func() { automationScript = original }()

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	backend := &windowsBackend{}
	defer backend.automation.Close()
	_, err := backend.Automation(ctx, AutomationRequest{Action: "monitors"})
	require.NoError(t, err)

	// The worker is running; run holds the worker's mutex for the whole
	// exchange, so the process is taken now.
	cmd := backend.automation.cmd
	require.NotNil(t, cmd)
	go func() {
		// Kill the worker, as a crash would, once the invoke request has
		// reached the script.
		for ctx.Err() == nil {
			data, _ := os.ReadFile(marker)
			if strings.Contains(string(data), "invoke") {
				_ = cmd.Process.Kill()
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	_, err = backend.Automation(ctx, AutomationRequest{Action: "invoke", Name: "Save"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "outcome_unknown")

	data, err := os.ReadFile(marker)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(data), "invoke"), string(data))
}

// TestAutomationWorkerTiming measures cold and warm requests. It reports
// numbers rather than asserting them, because they depend on the machine.
// "monitors" measures process and script cost alone; "inspect" adds a UI
// Automation tree read of a fixture window this test owns.
func TestAutomationWorkerTiming(t *testing.T) {
	if os.Getenv("ATLAS_AUTOMATION_TIMING") != "1" {
		t.Skip("Set ATLAS_AUTOMATION_TIMING=1 to measure automation latency")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	hwnd := startTimingFixture(ctx, t)
	const rounds = 10
	for _, request := range []AutomationRequest{
		{Action: "monitors"},
		{Action: "inspect", WindowID: hwnd},
	} {
		data, err := json.Marshal(request)
		require.NoError(t, err)
		oneShot := &windowsBackend{}
		var onceTotal time.Duration
		for range rounds {
			start := time.Now()
			_, err := oneShot.automationOnce(ctx, data)
			require.NoError(t, err)
			onceTotal += time.Since(start)
		}
		worker := &windowsBackend{}
		start := time.Now()
		_, err = worker.Automation(ctx, request)
		require.NoError(t, err)
		cold := time.Since(start)
		var warmTotal time.Duration
		for range rounds {
			start := time.Now()
			_, err := worker.Automation(ctx, request)
			require.NoError(t, err)
			warmTotal += time.Since(start)
		}
		worker.automation.Close()
		require.Equal(t, 1, worker.automation.Started)
		t.Logf("%s: one-shot mean %v (n=%d); worker cold %v, warm mean %v (n=%d)",
			request.Action, onceTotal/rounds, rounds, cold, warmTotal/rounds, rounds)
	}
}

// startTimingFixture shows a small WPF window owned by the test and returns
// its handle.
func startTimingFixture(ctx context.Context, t *testing.T) string {
	t.Helper()
	ready := filepath.Join(t.TempDir(), "ready.txt")
	fixtureCtx, stop := context.WithCancel(ctx)
	cmd := exec.CommandContext(fixtureCtx, "powershell.exe", "-STA", "-NoProfile", "-NonInteractive", "-Command", `Add-Type -AssemblyName PresentationFramework; $form=New-Object System.Windows.Window; $form.Title='Atlas timing fixture'; $form.ShowInTaskbar=$false; $form.ShowActivated=$false; $form.Width=300; $form.Height=200; $form.Left=20; $form.Top=20; $panel=New-Object System.Windows.Controls.StackPanel; 1..20 | ForEach-Object { $b=New-Object System.Windows.Controls.Button; $b.Content="Item $_"; $null=$panel.Children.Add($b) }; $form.Content=$panel; $form.Add_ContentRendered({$helper=New-Object System.Windows.Interop.WindowInteropHelper($form); [System.IO.File]::WriteAllText($env:ATLAS_FIXTURE_READY,[string]$helper.Handle.ToInt64())}); $app=New-Object System.Windows.Application; $null=$app.Run($form)`)
	cmd.Env = append(os.Environ(), "ATLAS_FIXTURE_READY="+ready)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { stop(); _ = cmd.Wait() })
	for {
		data, err := os.ReadFile(ready)
		if err == nil && len(data) > 0 {
			return string(data)
		}
		select {
		case <-ctx.Done():
			t.Fatal("Timing fixture did not start")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

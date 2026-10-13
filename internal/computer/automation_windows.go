//go:build windows

package computer

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
	"unicode/utf16"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
)

//go:embed automation.ps1
var automationScript string

func (b *windowsBackend) Origin() Point {
	return Point{X: getSystemMetrics(smXVirtualScreen), Y: getSystemMetrics(smYVirtualScreen)}
}

// ForegroundWindow identifies the actual input destination before pixel actions.
func (b *windowsBackend) ForegroundWindow() string {
	hwnd, _, _ := modUser32.NewProc("GetForegroundWindow").Call()
	return strconv.FormatUint(uint64(hwnd), 10)
}

func (b *windowsBackend) Automation(ctx context.Context, p AutomationRequest) (json.RawMessage, error) {
	if hwnd, err := strconv.ParseUint(p.WindowID, 10, 64); err == nil && activity.IsOverlayWindow(uintptr(hwnd)) {
		return nil, fmt.Errorf("invalid_target: activity overlay is not an automation target")
	}
	if err := ValidateAutomationRequest(p); err != nil {
		return nil, err
	}
	if p.Action == "focus" {
		return b.focusWindow(ctx, p.WindowID)
	}
	if p.Action == "windows" {
		return b.listNativeWindows(ctx)
	}
	data, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	// OCR runs in its own process: WinRT keeps the image file mapped in a
	// long-lived process, so it could not be rewritten or removed.
	if p.Action != "ocr" && os.Getenv("ATLAS_COMPUTER_AUTOMATION_WORKER") != "0" {
		output, used, err := b.automation.run(ctx, p, data)
		if used {
			if err != nil {
				return nil, err
			}
			return automationResult(output)
		}
	}
	return b.automationOnce(ctx, data)
}

// automationOnce runs one request in a new PowerShell process. It is the
// fallback when the persistent worker cannot be used.
func (b *windowsBackend) automationOnce(ctx context.Context, data []byte) (json.RawMessage, error) {
	// Load the embedded script from a private temporary file so growing UIA
	// helpers cannot exceed Windows' 32767-character command-line limit.
	file, err := os.CreateTemp("", "atlas-automation-*.ps1")
	if err != nil {
		return nil, fmt.Errorf("create automation script: %w", err)
	}
	path := file.Name()
	defer os.Remove(path)
	if err = file.Chmod(0o600); err == nil {
		_, err = file.WriteString(automationScript)
	}
	closeErr := file.Close()
	if err != nil {
		return nil, fmt.Errorf("write automation script: %w", err)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close automation script: %w", closeErr)
	}
	loader := `& ([scriptblock]::Create([System.IO.File]::ReadAllText($env:ATLAS_AUTOMATION_SCRIPT)))`
	units := utf16.Encode([]rune(loader))
	encoded := make([]byte, len(units)*2)
	for i, v := range units {
		binary.LittleEndian.PutUint16(encoded[i*2:], v)
	}
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(encoded))
	cmd.Env = append(os.Environ(), "ATLAS_AUTOMATION_SCRIPT="+path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Stdin = bytes.NewReader(data)
	var output, stderr bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("accessibility_unavailable: Windows automation failed (%w); inspect a screenshot or use manual handoff", err)
	}
	if output.Len() > 4*1024*1024 {
		return nil, fmt.Errorf("accessibility output exceeded limit")
	}
	return automationResult(output.Bytes())
}

// automationResult validates one script result and turns a reported
// failure into an error carrying its code.
func automationResult(output []byte) (json.RawMessage, error) {
	result := bytes.TrimSpace(bytes.TrimPrefix(output, []byte{0xef, 0xbb, 0xbf}))
	if !json.Valid(result) {
		return nil, fmt.Errorf("invalid Windows automation response")
	}
	var failure struct {
		Code     string `json:"error_code"`
		Recovery string `json:"recovery"`
		Detail   string `json:"detail"`
	}
	if err := json.Unmarshal(result, &failure); err == nil && failure.Code != "" {
		return nil, fmt.Errorf("%s: %s; %s", failure.Code, failure.Detail, failure.Recovery)
	}
	return append(json.RawMessage(nil), result...), nil
}

// focusWindow uses HWND activation independently of accessibility providers.
func (b *windowsBackend) focusWindow(ctx context.Context, id string) (json.RawMessage, error) {
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil || n == 0 || uint64(uintptr(n)) != n || strconv.FormatUint(n, 10) != id {
		return nil, fmt.Errorf("invalid_target: focus requires an explicit numeric window_id from windows")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hwnd := uintptr(n)
	valid, _, _ := modUser32.NewProc("IsWindow").Call(hwnd)
	if valid == 0 {
		return nil, fmt.Errorf("target_missing: window %s disappeared; list windows again", id)
	}
	visible, _, _ := modUser32.NewProc("IsWindowVisible").Call(hwnd)
	if visible == 0 {
		return nil, fmt.Errorf("target_not_actionable: window %s is hidden", id)
	}
	minimized, _, _ := modUser32.NewProc("IsIconic").Call(hwnd)
	if minimized != 0 {
		_, _, _ = modUser32.NewProc("ShowWindowAsync").Call(hwnd, 9) // SW_RESTORE.
	}
	_, _, _ = modUser32.NewProc("SetForegroundWindow").Call(hwnd)
	// Activation can complete asynchronously on another input queue.
	deadline := time.NewTimer(500 * time.Millisecond)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if b.ForegroundWindow() == id {
			return json.Marshal(map[string]any{"focused": true, "window_id": id, "foreground_window_id": id})
		}
					return setForeground()
				}
				if user("AttachThreadInput", self, fgThread, 1) == 0 {
					return fmt.Errorf("AttachThreadInput refused")
				}
				defer user("AttachThreadInput", self, fgThread, 0)
				user("BringWindowToTop", hwnd)
				return setForeground()
			}},
			{Method: "alt_unlock", Run: func() error {
				const vkMenu, keyUp = 0x12, 0x0002
				user("keybd_event", vkMenu, 0, 0, 0)
				user("keybd_event", vkMenu, 0, keyUp, 0)
				return setForeground()
			}},
		},
		Settle: func(ctx context.Context, d time.Duration) bool {
			deadline := time.NewTimer(d)
			defer deadline.Stop()
			tick := time.NewTicker(20 * time.Millisecond)
			defer tick.Stop()
			for {
				if b.ForegroundWindow() == id {
					return true
				}
				select {
				case <-ctx.Done():
					return false
				case <-deadline.C:
					return false
				case <-tick.C:
				}
			}
		},
	}
}

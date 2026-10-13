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
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	"golang.org/x/sys/windows"
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
	if p.Action == "monitors" {
		// Native and DPI aware; see listNativeMonitors.
		return b.listNativeMonitors(ctx)
	}
	if UsesCatalog(p) {
		return CatalogAutomation(ctx, b, p)
	}
	if p.Action == "launch_app" {
		out, err := b.automationScriptRun(ctx, p)
		// Programs without a Start menu entry (or with a localized name
		// the Start menu does not use) are found through the catalog.
		if err != nil && strings.Contains(err.Error(), "Target missing") {
			if res, cerr := CatalogAutomation(ctx, b, p); cerr == nil {
				return res, nil
			}
		}
		return out, err
	}
	return b.automationScriptRun(ctx, p)
}

// automationScriptRun serves a request through the PowerShell automation
// script, preferring the persistent worker.
func (b *windowsBackend) automationScriptRun(ctx context.Context, p AutomationRequest) (json.RawMessage, error) {
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
	if valid, _, _ := modUser32.NewProc("IsWindow").Call(hwnd); valid == 0 {
		return nil, fmt.Errorf("target_missing: window %s disappeared; list windows again", id)
	}
	want := nativeWindowIdentity(hwnd)
	res, err := focusWithRecovery(ctx, id, want, b.nativeFocusOps(hwnd, id), 500*time.Millisecond)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"focused": true, "window_id": id, "foreground_window_id": b.ForegroundWindow(), "method": res.Method, "tried": res.Tried})
}

func nativeWindowIdentity(hwnd uintptr) WindowIdentity {
	var pid uint32
	_, _, _ = modUser32.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	var className [256]uint16
	_, _, _ = modUser32.NewProc("GetClassNameW").Call(hwnd, uintptr(unsafe.Pointer(&className[0])), uintptr(len(className)))
	return WindowIdentity{ProcessID: pid, ClassName: syscall.UTF16ToString(className[:])}
}

// nativeFocusOps escalates from a plain SetForegroundWindow, which Windows
// refuses for a background process (the foreground lock), to attaching to
// the foreground thread's input state, and finally to the documented
// unlock of a lone Alt key tap, which Windows treats as the last input
// event and which does not reach the target window.
func (b *windowsBackend) nativeFocusOps(hwnd uintptr, id string) focusOps {
	user := func(name string, args ...uintptr) uintptr {
		r, _, _ := modUser32.NewProc(name).Call(args...)
		return r
	}
	setForeground := func() error {
		if user("SetForegroundWindow", hwnd) == 0 {
			return fmt.Errorf("SetForegroundWindow refused")
		}
		return nil
	}
	return focusOps{
		Exists:   func() bool { return user("IsWindow", hwnd) != 0 },
		Visible:  func() bool { return user("IsWindowVisible", hwnd) != 0 },
		Identity: func() WindowIdentity { return nativeWindowIdentity(hwnd) },
		Restore: func() {
			if user("IsIconic", hwnd) != 0 {
				user("ShowWindow", hwnd, 9) // SW_RESTORE.
			}
		},
		Foreground: func() bool { return b.ForegroundWindow() == id },
		Attempts: []focusAttempt{
			{Method: "set_foreground", Run: setForeground},
			{Method: "attach_thread_input", Run: func() error {
				fg := user("GetForegroundWindow")
				if fg == 0 {
					return setForeground()
				}
				fgThread := user("GetWindowThreadProcessId", fg, 0)
				self := uintptr(windows.GetCurrentThreadId())
				if fgThread == 0 || fgThread == self {
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

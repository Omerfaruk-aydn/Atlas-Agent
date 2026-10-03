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
	"os/exec"
	"strconv"
	"syscall"
	"unicode/utf16"
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
	data, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	units := utf16.Encode([]rune(automationScript))
	encoded := make([]byte, len(units)*2)
	for i, v := range units {
		binary.LittleEndian.PutUint16(encoded[i*2:], v)
	}
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(encoded))
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
	result := bytes.TrimSpace(bytes.TrimPrefix(output.Bytes(), []byte{0xef, 0xbb, 0xbf}))
	if !json.Valid(result) {
		return nil, fmt.Errorf("invalid Windows automation response")
	}
	var failure struct {
		Code     string `json:"error_code"`
		Recovery string `json:"recovery"`
	}
	if err := json.Unmarshal(result, &failure); err == nil && failure.Code != "" {
		return nil, fmt.Errorf("%s: %s", failure.Code, failure.Recovery)
	}
	return append(json.RawMessage(nil), result...), nil
}

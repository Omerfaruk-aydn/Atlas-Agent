package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

// contractBackend records every native call so tests can prove that a rejected
// call reached nothing.
type contractBackend struct {
	efficientDesktopBackend
	native     []string
	inputs     int32
	lastReq    computer.AutomationRequest
	foreground string
}

func newContractBackend(t *testing.T) *contractBackend {
	t.Helper()
	b := &contractBackend{efficientDesktopBackend: efficientDesktopBackend{fakeComputerBackend: fakeComputerBackend{size: computer.Size{Width: 400, Height: 300}, screenshot: testPNG(t, 400, 300)}}, foreground: "11"}
	b.call = func(_ context.Context, p computer.AutomationRequest) (json.RawMessage, error) {
		b.native = append(b.native, p.Action)
		b.lastReq = p
		switch p.Action {
		case "windows":
			return json.RawMessage(`[{"window_id":"11","process_id":1,"class_name":"Notepad","foreground":` + boolJSON(b.foreground == "11") + `},{"window_id":"22","process_id":2,"class_name":"CabinetWClass","foreground":` + boolJSON(b.foreground == "22") + `}]`), nil
		case "inspect":
			return json.RawMessage(`{"truncated":false,"elements":[{"element_id":"1","name":"Save","role":"ControlType.Button","enabled":true,"x":10,"y":10,"width":20,"height":20}]}`), nil
		case "find":
			return json.RawMessage(`{"count":0,"matches":[]}`), nil
		case "ocr":
			return json.RawMessage(`{"text":"","lines":[]}`), nil
		}
		return json.RawMessage(`{}`), nil
	}
	return b
}

func boolJSON(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func (b *contractBackend) Click(x, y int, button computer.MouseButton) error {
	atomic.AddInt32(&b.inputs, 1)
	return b.efficientDesktopBackend.Click(x, y, button)
}

func (b *contractBackend) TypeText(text string) error {
	atomic.AddInt32(&b.inputs, 1)
	return b.efficientDesktopBackend.TypeText(text)
}

func (b *contractBackend) KeyPress(key string) error {
	atomic.AddInt32(&b.inputs, 1)
	return b.efficientDesktopBackend.KeyPress(key)
}

func (b *contractBackend) Hotkey(modifiers []string, key string) error {
	atomic.AddInt32(&b.inputs, 1)
	return b.efficientDesktopBackend.Hotkey(modifiers, key)
}

func (b *contractBackend) ForegroundWindow() string { return b.foreground }

// runRaw sends provider-shaped JSON through the real tool entry point.
func runRaw(t *testing.T, b *contractBackend, input string) fantasy.ToolResponse {
	t.Helper()
	ctx := context.WithValue(t.Context(), SessionIDContextKey, t.Name())
	tool := newComputerTool(permission.NewPermissionService(t.TempDir(), true, nil), t.TempDir(), b, func() bool { return true }, "computer", 0)
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "call", Name: ComputerToolName, Input: input})
	require.NoError(t, err)
	return resp
}

func contractOf(t *testing.T, resp fantasy.ToolResponse) computer.ContractError {
	t.Helper()
	require.True(t, resp.IsError, resp.Content)
	_, document, found := strings.Cut(resp.Content, "Tool contract: ")
	require.True(t, found, "response has no structured contract: %s", resp.Content)
	var contract computer.ContractError
	require.NoError(t, json.Unmarshal([]byte(strings.Split(document, "\n")[0]), &contract))
	require.False(t, contract.InputSent)
	require.Equal(t, "none", contract.Effect)
	require.NotEmpty(t, contract.NextStep)
	return contract
}

// Inputs below are the shapes captured from session 7d92f539 (MiMo) and the
// same mistakes as other providers make them. None may send input.
func TestComputerRejectsMalformedProviderShapesBeforeInput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, input, code string
		contains          string
	}{
		{"fabricated window id", `{"action":"key","key":"ctrl+shift+n","automation":{"window_id":"shell"}}`, "invalid_target", "native window handle"},
		{"prepare sent to computer", `{"action":"prepare","automation":{"application":"Hesap Makinesi"}}`, "wrong_tool", "tool_pipeline desktop mode"},
		{"unknown action", `{"action":"smash"}`, "unknown_action", "must be one of"},
		{"window id at top level", `{"action":"observe","window_id":"11"}`, "unknown_field", "put it inside automation"},
		{"application inside automation", `{"action":"windows","automation":{"application":"Notepad"}}`, "unknown_field", "automation.application"},
		{"echoed output field", `{"action":"inspect","automation":{"window_id":"11","image_origin":{"x":0,"y":0}}}`, "unknown_field", "output field"},
		{"extra wrapper", `{"action":"inspect","automation":{"outer":{"window_id":"11"}}}`, "unknown_field", "no outer wrapper"},
		{"ocr x/y without size", `{"action":"ocr","x":50,"y":60}`, "invalid_region", "whole screen"},
		{"ocr one dimension", `{"action":"ocr","x":1,"y":2,"width":30}`, "invalid_region", "both width and height"},
		{"ocr region conflict", `{"action":"ocr","width":40,"height":30,"automation":{"width":41,"height":30}}`, "conflicting_fields", "no coordinate was guessed"},
		{"ocr negative", `{"action":"ocr","x":-1,"y":0,"width":4,"height":4}`, "invalid_region", "negative"},
		{"region keys for observe", `{"action":"observe","automation":{"window_id":"11","width":40}}`, "misplaced_field", "not used by action"},
		{"unknown role", `{"action":"find","automation":{"window_id":"11","role":"Textbox"}}`, "invalid_role", "ControlType.Edit"},
		{"role garbage", `{"action":"find","automation":{"window_id":"11","role":"ControlType.Banana"}}`, "invalid_role", "Valid roles"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := newContractBackend(t)
			resp := runRaw(t, b, tc.input)
			contract := contractOf(t, resp)
			require.Equal(t, tc.code, contract.Code)
			require.Contains(t, resp.Content, tc.contains)
			require.Empty(t, b.native, "native layer must not be reached")
			require.Zero(t, atomic.LoadInt32(&b.inputs), "no desktop input may be sent")
			require.NotContains(t, resp.Metadata, `"window_id":"1`, "examples must not contain a runnable target")
		})
	}
}

func TestComputerExamplesAreNeverRunnableTargets(t *testing.T) {
	t.Parallel()
	for _, input := range []string{`{"action":"prepare"}`, `{"action":"ocr","x":1,"y":1}`, `{"action":"find","automation":{"role":"x"}}`, `{"action":"key","automation":{"window_id":"shell"}}`} {
		b := newContractBackend(t)
		contract := contractOf(t, runRaw(t, b, input))
		if contract.Example == "" {
			continue
		}
		var example struct {
			Action     string `json:"action"`
			Automation struct {
				WindowID string `json:"window_id"`
			} `json:"automation"`
		}
		_ = json.Unmarshal([]byte(contract.Example), &example)
		require.False(t, numericWindowIDPattern(example.Automation.WindowID), "example %s carries a numeric window id", contract.Example)
		// A copied example must itself be rejected, never executed.
		if strings.Contains(contract.Example, "<") && example.Automation.WindowID != "" {
			replay := runRaw(t, b, contract.Example)
			require.True(t, replay.IsError)
			require.Empty(t, b.native)
		}
	}
}

func numericWindowIDPattern(id string) bool {
	return id != "" && strings.Trim(id, "0123456789") == ""
}

func newContractBackendWith(t *testing.T, _ *contractBackend) *contractBackend {
	return newContractBackend(t)
}

type computerError string

func (e computerError) Error() string { return string(e) }

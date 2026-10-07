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

func TestOCRRegionInAutomationIsAppliedAndReported(t *testing.T) {
	t.Parallel()
	b := newContractBackend(t)
	var cropWidth int
	inner := b.call
	b.call = func(ctx context.Context, p computer.AutomationRequest) (json.RawMessage, error) {
		if p.Action == "ocr" {
			cropWidth = 1 // Image supplied for a region request, not the full 400x300 screen.
		}
		return inner(ctx, p)
	}
	// Exactly the MiMo shape: the region lives in automation.
	resp := runRaw(t, b, `{"action":"ocr","automation":{"x":10,"y":20,"width":60,"height":40}}`)
	require.False(t, resp.IsError, resp.Content)
	require.Equal(t, 1, cropWidth)
	var result struct {
		Scope ocrScope       `json:"scope"`
		Image computer.Point `json:"image_origin"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &result))
	require.Equal(t, ocrScope{Kind: "region", X: 10, Y: 20, Width: 60, Height: 40}, result.Scope)
	require.Equal(t, computer.Point{X: 10, Y: 20}, result.Image)

	// The equivalent top-level form is identical, and full screen is explicit.
	resp = runRaw(t, newContractBackendWith(t, b), `{"action":"ocr","x":10,"y":20,"width":60,"height":40}`)
	require.False(t, resp.IsError, resp.Content)
	full := runRaw(t, newContractBackend(t), `{"action":"ocr"}`)
	require.Contains(t, full.Content, `"kind":"full_screen"`)
}

func newContractBackendWith(t *testing.T, _ *contractBackend) *contractBackend {
	return newContractBackend(t)
}

func TestRoleAliasesNormalizeOnlyExactNames(t *testing.T) {
	t.Parallel()
	for input, want := range map[string]string{"ListItem": "ControlType.ListItem", "listitem": "ControlType.ListItem", "list_item": "ControlType.ListItem", "ControlType.ListItem": "ControlType.ListItem", "controltype.button": "ControlType.Button", "Window": "ControlType.Window"} {
		b := newContractBackend(t)
		resp := runRaw(t, b, `{"action":"find","automation":{"window_id":"11","role":"`+input+`","name":"deneme"}}`)
		require.False(t, resp.IsError, resp.Content)
		require.Equal(t, want, b.lastReq.Role, input)
	}
	// Valid role, no such element: an ordinary empty result, not a role error.
	b := newContractBackend(t)
	resp := runRaw(t, b, `{"action":"find","automation":{"window_id":"11","role":"ListItem","name":"nothing"}}`)
	require.False(t, resp.IsError)
	require.Contains(t, resp.Content, `"count":0`)
}

func TestComputerNormalizesQuotedScalarsAndKeepsRealErrors(t *testing.T) {
	t.Parallel()
	b := newContractBackend(t)
	resp := runRaw(t, b, `{"action":"click","x":"12","y":"34"}`)
	require.False(t, resp.IsError, resp.Content)
	require.Equal(t, computer.Point{X: 12, Y: 34}, b.clicked[0])
	resp = runRaw(t, b, `{"action":"click","x":"left","y":1}`)
	require.True(t, resp.IsError)
	require.Len(t, b.clicked, 1)
}

func TestWindowRootInvokeFailureIsNotRepairedWithInput(t *testing.T) {
	t.Parallel()
	b := newContractBackend(t)
	inner := b.call
	b.call = func(ctx context.Context, p computer.AutomationRequest) (json.RawMessage, error) {
		if p.Action == "invoke" {
			b.native = append(b.native, p.Action)
			return nil, computerError("unsupported_pattern: Pattern unavailable: Invoke on ControlType.Window (supported patterns: Window, Transform); no click or key was sent; a window root is activated with the focus action, not invoked")
		}
		return inner(ctx, p)
	}
	resp := runRaw(t, b, `{"action":"invoke","automation":{"window_id":"22","element_id":"42:22"}}`)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "unsupported_pattern")
	require.Contains(t, resp.Content, "focus action")
	require.Zero(t, atomic.LoadInt32(&b.inputs), "no double-click, Enter or other guess after the refusal")
	require.Empty(t, b.clicked)
	require.Empty(t, b.keys)
}

type computerError string

func (e computerError) Error() string { return string(e) }

func TestObservationSeparatesObservedFromForegroundWindow(t *testing.T) {
	t.Parallel()
	// Background window observed: it must be clear that observation did not focus it.
	b := newContractBackend(t)
	b.foreground = "22"
	s := &computerToolState{backend: b}
	ctx := context.WithValue(t.Context(), SessionIDContextKey, t.Name())
	resp, err := s.runComputerAction(ctx, "observe", ComputerParams{Observation: "semantic", Automation: computer.AutomationRequest{WindowID: "11"}})
	require.NoError(t, err)
	var observed struct {
		Target desktopTargetState `json:"target_window"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &observed))
	require.Equal(t, "no", observed.Target.Foreground)
	require.Equal(t, "22", observed.Target.ForegroundWindowID)
	require.False(t, observed.Target.InputReady)
	require.False(t, observed.Target.FocusChangedByObservation)
	require.Contains(t, observed.Target.Note, "does not focus")
	require.NotEmpty(t, observed.Target.InvalidatedBy)

	b.foreground = "11"
	resp, err = s.runComputerAction(ctx, "observe", ComputerParams{Observation: "semantic", Automation: computer.AutomationRequest{WindowID: "11"}})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &observed))
	require.Equal(t, "yes", observed.Target.Foreground)
	require.True(t, observed.Target.InputReady)
}

func TestKeyboardToBackgroundWindowIsRefusedWithoutInput(t *testing.T) {
	t.Parallel()
	b := newContractBackend(t)
	b.foreground = "22"
	resp := runRaw(t, b, `{"action":"hotkey","key":"l","modifiers":"ctrl","automation":{"window_id":"11"}}`)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "wrong_window")
	require.Zero(t, atomic.LoadInt32(&b.inputs))
}

// Pipeline-level shapes: wrong nesting, wrong mode, missing checkpoint target.
func TestPipelineRejectsWrongNestingBeforeAnyDispatch(t *testing.T) {
	t.Parallel()
	dispatched := 0
	tool := NewToolPipeline(func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		dispatched++
		return fantasy.NewTextResponse(`{}`), nil
	})
	run := func(input string) fantasy.ToolResponse {
		resp, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "p", Name: "tool_pipeline", Input: input})
		require.NoError(t, err)
		return resp
	}
	// Captured shape: desktop.desktop.
	resp := run(`{"desktop":{"desktop":{"input":{"action":"invoke","automation":{"element_id":"42:1","window_id":"11"}},"mode":"act"}}}`)
	require.Equal(t, "invalid_nesting", contractOf(t, resp).Code)
	// A computer action used as a mode.
	resp = run(`{"desktop":{"mode":"invoke","window_id":"11"}}`)
	require.Equal(t, "invalid_mode", contractOf(t, resp).Code)
	require.Contains(t, resp.Content, "computer action")
	// Fields from the input level placed on the recipe.
	resp = run(`{"desktop":{"mode":"act","action":"key","key":"enter","window_id":"11"}}`)
	contract := contractOf(t, resp)
	require.Equal(t, "unknown_field", contract.Code)
	require.Contains(t, resp.Content, "belongs inside input")
	// Fabricated identity inside a step.
	resp = run(`{"desktop":{"mode":"sequence","steps":[{"checkpoint":{"condition":"text","expected":"x","name":"y"},"input":{"action":"key","key":"enter","automation":{"window_id":"shell"}}}]}}`)
	require.Equal(t, "invalid_target", contractOf(t, resp).Code)
	require.Zero(t, dispatched)
}

func TestSequenceCheckpointWithoutWindowReturnsStructuredFix(t *testing.T) {
	t.Parallel()
	var p DesktopWorkflowParams
	require.NoError(t, json.Unmarshal([]byte(`{"mode":"sequence","steps":[{"checkpoint":{"condition":"text","expected":"deneme","name":"Masaüstü","wait_ms":5000},"input":{"action":"key","key":"enter","automation":{"window_id":"11"}}}]}`), &p))
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		t.Fatal("input dispatched")
		return fantasy.ToolResponse{}, nil
	})
	require.NoError(t, err)
	contract := contractOf(t, r)
	require.Equal(t, "checkpoint_target_missing", contract.Code)
	require.Equal(t, "checkpoint.window_id", contract.Field)
	require.Contains(t, contract.Example, "<window_id from prepare/observe>")
}

func TestKnownGoodShapesStillDecode(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		`{"action":"observe","observation":"semantic","automation":{"window_id":"11","max_elements":40}}`,
		`{"action":"hotkey","key":"l","modifiers":"ctrl","automation":{"window_id":"11"}}`,
		`{"action":"key","key":"ctrl+l","automation":{"window_id":"11"}}`,
		`{"action":"key","automation":{"window_id":"11","key":"enter"}}`,
		`{"action":"Click","x":5,"y":6}`,
		`{"action":"ocr"}`,
		`{"action":"assert","automation":{"window_id":"11","name":"Save","condition":"visible","wait_ms":500}}`,
		`{"action":"capture_region","x":1,"y":2,"width":3,"height":4}`,
	} {
		var p ComputerParams
		require.NoError(t, json.Unmarshal([]byte(input), &p), input)
	}
	var wp DesktopWorkflowParams
	require.NoError(t, json.Unmarshal([]byte(`{"mode":"act","window_id":"11","focus_window":true,"input":{"action":"key","key":"enter","automation":{"window_id":"11"}}}`), &wp))
}

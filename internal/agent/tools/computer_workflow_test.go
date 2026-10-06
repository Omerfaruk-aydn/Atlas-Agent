package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestComputerObservationChangedTarget(t *testing.T) {
	b := &efficientDesktopBackend{}
	b.call = func(_ context.Context, p computer.AutomationRequest) (json.RawMessage, error) {
		return json.RawMessage(`{"matches":[{"element_id":"1","name":"changed","role":"Button","enabled":true}]}`), nil
	}
	s := &computerToolState{backend: b}
	s.observations.put("", desktopObservation{ID: "snap", Created: time.Now(), WindowID: "11", Elements: []desktopElement{{ID: "1", Name: "original", Role: "Button", Enabled: true}}})
	r, err := s.runComputerAction(t.Context(), "invoke", ComputerParams{SnapshotID: "snap", Element: 1})
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Contains(t, r.Content, "stale_observation")
}

func TestComputerObservationReferenceIsolationAndExpiry(t *testing.T) {
	var c desktopObservations
	c.put("a", desktopObservation{ID: "snap", Created: time.Now()})
	_, err := c.get("b", "snap", 1)
	require.Error(t, err)
	c.put("a", desktopObservation{ID: "snap", Created: time.Now().Add(-time.Minute), Elements: []desktopElement{{ID: "1"}}})
	_, err = c.get("a", "snap", 1)
	require.Error(t, err)
}

func TestPipelineGateStopsMutation(t *testing.T) {
	for _, content := range []string{`{"passed":false}`, `{}`, `{"passed":"true"}`} {
		calls := 0
		tool := NewToolPipeline(func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			calls++
			return fantasy.NewTextResponse(content), nil
		})
		data, _ := json.Marshal(PipelineParams{Steps: []PipelineStep{{ID: "check", Tool: "computer", RequirePassed: true, Arguments: map[string]any{"action": "assert"}}, {ID: "input", Tool: "computer"}}})
		r, err := tool.Run(t.Context(), fantasy.ToolCall{Input: string(data)})
		require.NoError(t, err)
		require.True(t, r.IsError)
		require.Equal(t, 1, calls)
	}
}

func TestComputerHealthDoesNotInput(t *testing.T) {
	b := &fakeComputerBackend{size: computer.Size{Width: 20, Height: 20}, screenshot: testPNG(t, 20, 20)}
	s := &computerToolState{backend: b}
	r, err := s.runComputerAction(t.Context(), "health", ComputerParams{})
	require.NoError(t, err)
	require.False(t, r.IsError)
	require.Contains(t, r.Content, "skipped")
	require.Empty(t, b.clicked)
	require.Empty(t, b.typed)
}

func TestComputerObservationReportsForegroundDialog(t *testing.T) {
	t.Parallel()
	b := &efficientDesktopBackend{fakeComputerBackend: fakeComputerBackend{size: computer.Size{Width: 40, Height: 40}, screenshot: testPNG(t, 40, 40)}}
	windowsCalls := 0
	b.call = func(_ context.Context, p computer.AutomationRequest) (json.RawMessage, error) {
		switch p.Action {
		case "inspect":
			return json.RawMessage(`{"elements":[]}`), nil
		case "windows":
			windowsCalls++
			return json.RawMessage(`[{"window_id":"11","process_id":5,"x":-100,"y":0,"width":40,"height":40},{"window_id":"22","name":"Save As","process_id":5,"process_name":"Notepad.exe","owner_window_id":"11","foreground":true,"x":-100,"y":0,"width":40,"height":40}]`), nil
		default:
			t.Fatalf("Unexpected action %s", p.Action)
			return nil, nil
		}
	}
	s := &computerToolState{backend: b}
	r, err := s.observe(t.Context(), ComputerParams{Automation: computer.AutomationRequest{WindowID: "11"}})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	var observed struct {
		WindowID   string `json:"window_id"`
		Foreground struct {
			WindowID string `json:"window_id"`
			Owner    string `json:"owner_window_id"`
		} `json:"foreground_window"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.Content), &observed))
	require.Equal(t, "11", observed.WindowID, "Foreground metadata must not retarget the requested observation")
	require.Equal(t, "22", observed.Foreground.WindowID)
	require.Equal(t, "11", observed.Foreground.Owner)
	require.Equal(t, 1, windowsCalls, "Reuse the capture's window list")
}

func TestComputerObservationReturnsImageAndInvalidates(t *testing.T) {
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "owner")
	b := &efficientDesktopBackend{fakeComputerBackend: fakeComputerBackend{size: computer.Size{Width: 40, Height: 40}, screenshot: testPNG(t, 40, 40)}}
	mutations := 0
	b.call = func(_ context.Context, p computer.AutomationRequest) (json.RawMessage, error) {
		switch p.Action {
		case "inspect":
			return json.RawMessage(`{"truncated":true,"elements":[{"element_id":"1","name":"Save","role":"Button","enabled":true,"x":-100,"y":0,"width":20,"height":20}]}`), nil
		case "windows":
			return json.RawMessage(`[{"window_id":"11","x":-100,"y":0,"width":40,"height":40}]`), nil
		case "find":
			return json.RawMessage(`{"matches":[{"element_id":"1","name":"Save","role":"Button","enabled":true,"x":-100,"y":0,"width":20,"height":20}]}`), nil
		case "invoke":
			mutations++
			return json.RawMessage(`{"action_sent":true,"verify_required":true}`), nil
		}
		t.Fatalf("Unexpected action %s", p.Action)
		return nil, nil
	}
	s := &computerToolState{backend: b}
	r, err := s.runComputerAction(ctx, "observe", ComputerParams{Automation: computer.AutomationRequest{WindowID: "11"}})
	require.NoError(t, err)
	require.False(t, r.IsError)
	require.Equal(t, "image", r.Type)
	require.NotEmpty(t, r.Data)
	var o desktopObservation
	require.NoError(t, json.Unmarshal([]byte(r.Content), &o))
	require.True(t, o.Truncated)
	require.Equal(t, 1, o.Elements[0].Number)
	p := ComputerParams{SnapshotID: o.ID, Element: 1}
	r, err = s.runComputerAction(ctx, "invoke", p)
	require.NoError(t, err)
	require.False(t, r.IsError)
	require.Equal(t, 1, mutations)
	r, err = s.runComputerAction(ctx, "invoke", p)
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Equal(t, 1, mutations)
}

func TestComputerObservationCacheBound(t *testing.T) {
	var c desktopObservations
	start := time.Now()
	for i := 0; i < 100; i++ {
		c.put(fmt.Sprint(i), desktopObservation{ID: "s", Created: start.Add(time.Duration(i) * time.Nanosecond), Elements: []desktopElement{{ID: "1"}}})
	}
	require.Len(t, c.entries, 64)
	_, err := c.get("0", "s", 1)
	require.Error(t, err)
}

func TestComputerHealthPartialFailureAndCancellation(t *testing.T) {
	b := &efficientDesktopBackend{fakeComputerBackend: fakeComputerBackend{size: computer.Size{Width: 20, Height: 20}, screenshot: testPNG(t, 20, 20)}}
	b.call = func(context.Context, computer.AutomationRequest) (json.RawMessage, error) {
		return nil, fmt.Errorf("accessibility_unavailable: fixture provider unavailable")
	}
	s := &computerToolState{backend: b}
	r, err := s.health(t.Context(), ComputerParams{Automation: computer.AutomationRequest{WindowID: "11"}})
	require.NoError(t, err)
	var report struct {
		Status string                        `json:"status"`
		Checks map[string]desktopHealthCheck `json:"checks"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.Content), &report))
	require.Equal(t, "degraded", report.Status)
	require.Equal(t, "ok", report.Checks["capture"].Status)
	require.Equal(t, "failed", report.Checks["accessibility"].Status)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r, err = s.runComputerAction(ctx, "health", ComputerParams{})
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, r.Content)
}

func TestPipelineObservationReferenceAndGate(t *testing.T) {
	calls := 0
	tool := NewToolPipeline(func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		if calls == 1 {
			r := fantasy.NewImageResponse([]byte("fixture"), "image/png")
			r.Content = `{"snapshot_id":"fresh","elements":[]}`
			return r, nil
		}
		require.Contains(t, c.Input, `"snapshot_id":"fresh"`)
		return fantasy.NewTextResponse(`{"passed":true}`), nil
	})
	p := PipelineParams{Steps: []PipelineStep{{ID: "observe", Tool: "computer", Arguments: map[string]any{"action": "observe"}}, {ID: "verify", Tool: "computer", RequirePassed: true, ObservationFrom: "observe", Arguments: map[string]any{"action": "assert", "element": 1}}}}
	data, _ := json.Marshal(p)
	r, err := tool.Run(t.Context(), fantasy.ToolCall{Input: string(data)})
	require.NoError(t, err)
	require.False(t, r.IsError)
	require.Equal(t, 2, calls)
	require.Equal(t, "image", r.Type)
	p.Steps[0].Arguments["action"] = "windows"
	data, _ = json.Marshal(p)
	r, err = tool.Run(t.Context(), fantasy.ToolCall{Input: string(data)})
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Equal(t, 2, calls)
}

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/stretchr/testify/require"
)

type observationModeBackend struct {
	efficientDesktopBackend
	captures int
}

func TestComputerObservationRetainsFocusedControlBeyondTreeLimit(t *testing.T) {
	t.Parallel()
	for _, password := range []bool{false, true} {
		b := &observationModeBackend{}
		b.call = func(_ context.Context, p computer.AutomationRequest) (json.RawMessage, error) {
			if p.Action == "windows" {
				return json.RawMessage(`[{"window_id":"11","foreground":true}]`), nil
			}
			return json.Marshal(map[string]any{"truncated": true, "elements": []any{}, "focused_element": map[string]any{"element_id": "deep-address", "keyboard_focused": true, "password": password, "name": "Address", "value_available": true, "value": `C:\Users\Ömer&Ceylin\deneme`}})
		}
		s := &computerToolState{backend: b}
		r, err := s.observe(t.Context(), ComputerParams{Observation: "semantic", Automation: computer.AutomationRequest{WindowID: "11", MaxElements: 10}})
		require.NoError(t, err)
		require.False(t, r.IsError, r.Content)
		var observed struct {
			Focused *desktopElement `json:"focused_element"`
		}
		require.NoError(t, json.Unmarshal([]byte(r.Content), &observed))
		require.NotNil(t, observed.Focused)
		if password {
			require.Equal(t, "[password]", observed.Focused.Value)
			require.NotContains(t, r.Content, "Ceylin")
		} else {
			require.Equal(t, `C:\Users\Ömer&Ceylin\deneme`, observed.Focused.Value)
		}
		require.Zero(t, b.captures)
		s.observations.mu.Lock()
		cached, marshalErr := json.Marshal(s.observations.entries)
		s.observations.mu.Unlock()
		require.NoError(t, marshalErr)
		require.NotContains(t, string(cached), "Ceylin")
	}
}

func TestComputerObservationDistinguishesDisplayPathFromExactValue(t *testing.T) {
	t.Parallel()
	b := &observationModeBackend{}
	b.call = func(_ context.Context, p computer.AutomationRequest) (json.RawMessage, error) {
		if p.Action == "windows" {
			return json.RawMessage(`[{"window_id":"11","foreground":true}]`), nil
		}
		return json.RawMessage(`{"elements":[{"element_id":"address-label","name":"Address: C:\\Users\\ÖmerCeylin\\deneme","role":"Pane"},{"element_id":"address-edit","name":"Address","role":"Edit","enabled":true,"value_available":true,"value":"C:\\Users\\Ömer&Ceylin\\deneme"}]}`), nil
	}
	s := &computerToolState{backend: b}
	r, err := s.observe(t.Context(), ComputerParams{Observation: "semantic", Automation: computer.AutomationRequest{WindowID: "11"}})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	var result struct {
		Warning  string           `json:"display_path_warning"`
		Elements []desktopElement `json:"elements"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.Content), &result))
	require.Contains(t, result.Warning, "ampersands")
	require.Equal(t, `Address: C:\Users\ÖmerCeylin\deneme`, result.Elements[0].Name)
	require.Equal(t, `C:\Users\Ömer&Ceylin\deneme`, result.Elements[1].Value)
	require.Zero(t, b.captures)
}

func TestComputerAutoObservationFallsBackWhenProviderUnavailable(t *testing.T) {
	b := &observationModeBackend{efficientDesktopBackend: efficientDesktopBackend{fakeComputerBackend: fakeComputerBackend{size: computer.Size{Width: 40, Height: 40}, screenshot: testPNG(t, 40, 40)}}}
	b.call = func(_ context.Context, p computer.AutomationRequest) (json.RawMessage, error) {
		if p.Action == "inspect" {
			return nil, errors.New("accessibility_unavailable: fixture provider")
		}
		return json.RawMessage(`[{"window_id":"11","foreground":true,"x":-100,"width":40,"height":40}]`), nil
	}
	s := &computerToolState{backend: b}
	r, err := s.observe(t.Context(), ComputerParams{Observation: "auto", Automation: computer.AutomationRequest{WindowID: "11"}})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Equal(t, "image", r.Type)
	require.Contains(t, r.Content, `"accessibility_status":"unavailable"`)
	require.NotContains(t, r.Content, "snapshot_id")
	require.Equal(t, 1, b.captures)
}

func TestComputerSemanticObservationRedactionAndBounds(t *testing.T) {
	t.Parallel()
	b := &observationModeBackend{}
	b.call = func(_ context.Context, p computer.AutomationRequest) (json.RawMessage, error) {
		if p.Action == "windows" {
			return json.RawMessage(`[{"window_id":"11","foreground":true},{"window_id":"22","owner_window_id":"11"}]`), nil
		}
		return json.Marshal(map[string]any{"elements": []any{
			map[string]any{"element_id": "1", "role": "Edit", "password": true, "value": "secret", "text": "secret", "value_available": true, "text_available": true},
			map[string]any{"element_id": "2", "role": "Document", "text": strings.Repeat("é", 5000), "text_available": true},
		}})
	}
	s := &computerToolState{backend: b}
	r, err := s.observe(t.Context(), ComputerParams{Observation: "semantic", Automation: computer.AutomationRequest{WindowID: "11"}})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.NotContains(t, r.Content, "secret")
	var result struct {
		Elements []struct {
			Value          string `json:"value"`
			Text           string `json:"text"`
			ValueAvailable bool   `json:"value_available"`
			TextAvailable  bool   `json:"text_available"`
			TextTruncated  bool   `json:"text_truncated"`
		} `json:"elements"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.Content), &result))
	require.Equal(t, "[password]", result.Elements[0].Value)
	require.Equal(t, "[password]", result.Elements[0].Text)
	require.False(t, result.Elements[0].ValueAvailable)
	require.False(t, result.Elements[0].TextAvailable)
	require.Len(t, []rune(result.Elements[1].Text), 4096)
	require.True(t, result.Elements[1].TextTruncated)
	require.Zero(t, b.captures)
}

func TestComputerSemanticObservationReportsForegroundDialog(t *testing.T) {
	t.Parallel()
	b := &observationModeBackend{}
	b.call = func(_ context.Context, p computer.AutomationRequest) (json.RawMessage, error) {
		if p.Action == "inspect" {
			return json.RawMessage(`{"elements":[{"element_id":"1","role":"Text","name":"Ready"}]}`), nil
		}
		return json.RawMessage(`[{"window_id":"11"},{"window_id":"22","name":"Save As","owner_window_id":"11","foreground":true}]`), nil
	}
	s := &computerToolState{backend: b}
	r, err := s.observe(t.Context(), ComputerParams{Observation: "semantic", Automation: computer.AutomationRequest{WindowID: "11"}})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	var result struct {
		WindowID   string            `json:"window_id"`
		Foreground desktopWindowInfo `json:"foreground_window"`
		Sufficient bool              `json:"semantic_sufficient"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.Content), &result))
	require.Equal(t, "11", result.WindowID)
	require.Equal(t, "22", result.Foreground.ID)
	require.Equal(t, "11", result.Foreground.Owner)
	require.False(t, result.Sufficient)
	require.Zero(t, b.captures)
}

func (b *observationModeBackend) Screenshot() ([]byte, error) {
	b.captures++
	return b.fakeComputerBackend.Screenshot()
}

func TestComputerObservationModes(t *testing.T) {
	t.Parallel()
	readable := `{"elements":[{"element_id":"1","name":"Editor","role":"Edit","enabled":true,"value":"hello","value_available":true,"text":"hello","text_available":true,"keyboard_focused":true}]}`
	for _, tc := range []struct {
		name, mode, tree string
		image            bool
	}{
		{"default", "", readable, true},
		{"visual", "visual", readable, true},
		{"semantic", "semantic", readable, false},
		{"auto_readable", "auto", readable, false},
		{"auto_focused_legacy_content", "auto", `{"elements":[{"element_id":"1","role":"ControlType.Pane","name":"1234 x 56 = 69104","enabled":true,"keyboard_focused":true}]}`, false},
		{"auto_frame", "auto", `{"elements":[{"element_id":"1","role":"Window","name":"Editor"}]}`, true},
		{"auto_truncated", "auto", `{"truncated":true,"elements":[{"role":"Text","name":"Ready"}]}`, true},
		{"auto_unreadable", "auto", `{"elements":[{"role":"Button","name":"Save"},{"role":"Document","name":"Editor","text_available":false}]}`, true},
		{"auto_password", "auto", `{"elements":[{"role":"Edit","password":true,"value":"secret","text":"secret"}]}`, true},
		{"auto_clipped", "auto", `{"elements":[{"role":"Edit","value":"hello","value_available":true,"value_truncated":true}]}`, true},
		{"semantic_partial", "semantic", `{"truncated":true,"elements":[{"role":"Window"}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := &observationModeBackend{efficientDesktopBackend: efficientDesktopBackend{fakeComputerBackend: fakeComputerBackend{size: computer.Size{Width: 40, Height: 40}, screenshot: testPNG(t, 40, 40)}}}
			b.call = func(_ context.Context, p computer.AutomationRequest) (json.RawMessage, error) {
				switch p.Action {
				case "inspect":
					return json.RawMessage(tc.tree), nil
				case "windows":
					return json.RawMessage(`[{"window_id":"11","foreground":true,"x":-100,"y":0,"width":40,"height":40}]`), nil
				default:
					t.Fatalf("Unexpected action %s", p.Action)
					return nil, nil
				}
			}
			s := &computerToolState{backend: b}
			r, err := s.observe(t.Context(), ComputerParams{Observation: tc.mode, Automation: computer.AutomationRequest{WindowID: "11"}})
			require.NoError(t, err)
			require.False(t, r.IsError, r.Content)
			if tc.image {
				require.Equal(t, "image", r.Type)
				require.NotEmpty(t, r.Data)
				require.Equal(t, 1, b.captures)
			} else {
				require.Equal(t, "text", r.Type)
				require.Empty(t, r.Data)
				require.Zero(t, b.captures)
			}
			var result map[string]any
			require.NoError(t, json.Unmarshal([]byte(r.Content), &result))
			require.NotEmpty(t, result["snapshot_id"])
			require.NotContains(t, result, "passed")
			require.NotContains(t, result, "task_complete")
			if tc.tree == readable {
				e := result["elements"].([]any)[0].(map[string]any)
				require.Equal(t, "hello", e["value"])
				require.Equal(t, "hello", e["text"])
				require.Equal(t, true, e["keyboard_focused"])
				cached, err := s.observations.get("", result["snapshot_id"].(string), 1)
				require.NoError(t, err)
				serialized, err := json.Marshal(cached)
				require.NoError(t, err)
				require.NotContains(t, string(serialized), "hello")
			}
		})
	}
}

package tools

import (
	"context"
	"encoding/json"

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

func (b *observationModeBackend) Screenshot() ([]byte, error) {
	b.captures++
	return b.fakeComputerBackend.Screenshot()
}

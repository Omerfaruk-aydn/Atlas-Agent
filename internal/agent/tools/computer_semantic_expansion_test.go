package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/stretchr/testify/require"
)

func TestComputerAutoExpandsSmallTruncatedScanBeforeImage(t *testing.T) {
	b := &observationModeBackend{}
	var limits []int
	b.call = func(_ context.Context, p computer.AutomationRequest) (json.RawMessage, error) {
		if p.Action == "windows" {
			return json.RawMessage(`[{"window_id":"11","foreground":true}]`), nil
		}
		limits = append(limits, p.MaxElements)
		if len(limits) == 1 {
			return json.RawMessage(`{"truncated":true,"elements":[]}`), nil
		}
		return json.RawMessage(`{"truncated":false,"elements":[{"element_id":"file","role":"ControlType.ListItem","name":"sonuc","enabled":true,"width":100,"height":25},{"element_id":"search","role":"ControlType.Edit","name":"Search","enabled":true}]}`), nil
	}
	s := &computerToolState{backend: b}
	r, err := s.observe(t.Context(), ComputerParams{Observation: "auto", Automation: computer.AutomationRequest{WindowID: "11", MaxElements: 30}})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Equal(t, []int{30, 150}, limits)
	require.Equal(t, "text", r.Type)
	require.Zero(t, b.captures)
	require.Contains(t, r.Content, `"inspection_expanded":true`)
	require.Contains(t, r.Content, "content_readback_warning")
}

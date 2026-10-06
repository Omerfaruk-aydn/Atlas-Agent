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

func TestComputerSemanticExpansionKeepsFallbackAndExplicitModes(t *testing.T) {
	for _, kind := range []string{"still_truncated", "wrong_identity", "semantic", "visual"} {
		t.Run(kind, func(t *testing.T) {
			b := &observationModeBackend{efficientDesktopBackend: efficientDesktopBackend{fakeComputerBackend: fakeComputerBackend{size: computer.Size{Width: 40, Height: 40}, screenshot: testPNG(t, 40, 40)}}}
			reads := 0
			b.call = func(_ context.Context, p computer.AutomationRequest) (json.RawMessage, error) {
				if p.Action == "windows" {
					return json.RawMessage(`[{"window_id":"11","foreground":true,"x":-100,"width":40,"height":40}]`), nil
				}
				reads++
				if kind == "wrong_identity" && reads == 2 {
					return json.RawMessage(`{"window_id":"22","elements":[]}`), nil
				}
				return json.RawMessage(`{"truncated":true,"elements":[]}`), nil
			}
			mode := "auto"
			if kind == "semantic" || kind == "visual" {
				mode = kind
			}
			s := &computerToolState{backend: b}
			r, err := s.observe(t.Context(), ComputerParams{Observation: mode, Automation: computer.AutomationRequest{WindowID: "11", MaxElements: 30}})
			require.NoError(t, err)
			switch kind {
			case "still_truncated":
				require.False(t, r.IsError)
				require.Equal(t, 2, reads)
				require.Equal(t, 1, b.captures)
				require.Contains(t, r.Content, `"truncated":true`)
			case "wrong_identity":
				require.True(t, r.IsError)
				require.Zero(t, b.captures)
			case "semantic":
				require.False(t, r.IsError)
				require.Equal(t, 1, reads)
				require.Zero(t, b.captures)
			case "visual":
				require.False(t, r.IsError)
				require.Equal(t, 1, reads)
				require.Equal(t, 1, b.captures)
			}
		})
	}
}

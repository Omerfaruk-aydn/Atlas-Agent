package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/stretchr/testify/require"
)

func TestDesktopReadinessStopsAtActualCondition(t *testing.T) {
	for _, initiallyReady := range []bool{true, false} {
		calls := 0
		backend := &efficientDesktopBackend{}
		backend.call = func(_ context.Context, p computer.AutomationRequest) (json.RawMessage, error) {
			require.Equal(t, "assert", p.Action)
			calls++
			if initiallyReady || calls == 2 {
				return json.RawMessage(`{"passed":true,"actual":"Ready"}`), nil
			}
			return json.RawMessage(`{"passed":false,"actual":"Loading"}`), nil
		}
		state := &computerToolState{backend: backend}
		r, err := state.runAutomation(t.Context(), "assert", ComputerParams{Automation: computer.AutomationRequest{WindowID: "11", ElementID: "status", Condition: "text", Expected: "Ready", WaitMS: 1000}})
		require.NoError(t, err)
		require.False(t, r.IsError, r.Content)
		require.Contains(t, r.Content, `"actual":"Ready"`)
		if initiallyReady {
			require.Equal(t, 1, calls)
		} else {
			require.Equal(t, 2, calls)
		}
	}
}

func TestDesktopReadinessRetriesTransientReadsOnly(t *testing.T) {
	for _, code := range []string{"observation_incomplete", "unsupported_pattern"} {
		calls := 0
		backend := &efficientDesktopBackend{}
		backend.call = func(context.Context, computer.AutomationRequest) (json.RawMessage, error) {
			calls++
			if calls == 1 {
				return nil, errors.New(code + ": fixture")
			}
			return json.RawMessage(`{"passed":true,"actual":"Ready"}`), nil
		}
		state := &computerToolState{backend: backend}
		r, err := state.runAutomation(t.Context(), "assert", ComputerParams{Automation: computer.AutomationRequest{WindowID: "11", Name: "Ready", Condition: "visible", WaitMS: 1000}})
		require.NoError(t, err)
		if code == "unsupported_pattern" {
			require.True(t, r.IsError)
			require.Equal(t, 1, calls)
		} else {
			require.False(t, r.IsError, r.Content)
			require.Equal(t, 2, calls)
		}
	}
}

func TestDesktopReadinessHasBoundedTimeout(t *testing.T) {
	backend := &efficientDesktopBackend{}
	backend.call = func(context.Context, computer.AutomationRequest) (json.RawMessage, error) {
		return json.RawMessage(`{"passed":false}`), nil
	}
	state := &computerToolState{backend: backend}
	r, err := state.runAutomation(t.Context(), "assert", ComputerParams{Automation: computer.AutomationRequest{WindowID: "11", Name: "status", Condition: "visible", WaitMS: 20}})
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Contains(t, r.Content, "condition_timeout")
}

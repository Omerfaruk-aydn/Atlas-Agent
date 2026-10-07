package tools

import (
	"context"
	"encoding/json"

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

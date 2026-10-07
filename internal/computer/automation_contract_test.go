package computer

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAutomationCheckpointEquivalentWrapper(t *testing.T) {
	t.Parallel()
	var flat, wrapped AutomationRequest
	require.NoError(t, json.Unmarshal([]byte(`{"window_id":"11","element_id":"result","condition":"text","expected":"69.104","wait_ms":3000}`), &flat))
	require.NoError(t, json.Unmarshal([]byte(`{"automation":{"window_id":"11","element_id":"result","wait_ms":3000},"condition":"text","expected":"69.104"}`), &wrapped))
	require.Equal(t, flat, wrapped)
}

func TestAutomationCheckpointConflictsFailWithoutPartialAssignment(t *testing.T) {
	t.Parallel()
	for _, data := range []string{
		`{"window_id":"11","automation":{"window_id":"22"}}`,
		`{"automation":null}`,
		`{"automation":"invalid"}`,
		`{"automation":{"automation":{"window_id":"11"}}}`,
		`{"automation":{"wait_ms":"3000"}}`,
	} {
		p := AutomationRequest{WindowID: "unchanged"}
		require.Error(t, json.Unmarshal([]byte(data), &p))
		require.Equal(t, "unchanged", p.WindowID)
	}
}

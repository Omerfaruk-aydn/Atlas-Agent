package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDesktopResultPutsDecisionIdentityBeforeLargeControlTree(t *testing.T) {
	t.Parallel()
	input := map[string]any{"elements": []any{map[string]any{"name": strings.Repeat("control", 1000)}}, "window_id": "11", "snapshot_id": "current", "foreground_window": map[string]any{"window_id": "22"}, "desktop_state": map[string]any{"foreground_window_id": "22"}, "workflow": map[string]any{"input_window_id": "11"}, "focused_element": map[string]any{"element_id": "address"}}
	data, err := marshalDesktopResult(input)
	require.NoError(t, err)
	tree := strings.Index(string(data), `"elements"`)
	for _, key := range []string{"window_id", "snapshot_id", "foreground_window", "desktop_state", "workflow", "focused_element"} {
		require.Less(t, strings.Index(string(data), `"`+key+`"`), tree)
	}
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))
	want, err := json.Marshal(input)
	require.NoError(t, err)
	require.JSONEq(t, string(want), string(data))
	require.Contains(t, input, "window_id", "Rendering must not mutate evidence")
}

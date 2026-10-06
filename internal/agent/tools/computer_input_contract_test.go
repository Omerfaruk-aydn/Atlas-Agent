package tools

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestComputerInputContractEquivalentKeyboardEncodings(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		`{"action":"hotkey","key":"n","modifiers":"ctrl+shift","automation":{"window_id":"11"}}`,
		`{"action":"hotkey","automation":{"key":"n","modifiers":"ctrl+shift","window_id":"11"}}`,
		`{"action":"key","automation":{"key":"ctrl+shift+n","window_id":"11"}}`,
	} {
		var p ComputerParams
		require.NoError(t, json.Unmarshal([]byte(input), &p))
		require.Equal(t, "hotkey", p.Action)
		require.Equal(t, "n", p.Key)
		require.Equal(t, "ctrl+shift", p.Modifiers)
		require.Equal(t, "11", p.Automation.WindowID)
		require.NoError(t, validateDesktopInput(p))
		var batch ComputerBatchInput
		require.NoError(t, json.Unmarshal([]byte(input), &batch))
		require.Equal(t, p.Key, batch.Key)
		require.Equal(t, p.Modifiers, batch.Modifiers)
	}
	var escape ComputerParams
	require.NoError(t, json.Unmarshal([]byte(`{"action":"key","automation":{"key":"escape","window_id":"11"}}`), &escape))
	require.Equal(t, "esc", escape.Key)
	var literal ComputerParams
	require.NoError(t, json.Unmarshal([]byte(`{"action":"key","key":"+"}`), &literal))
	require.Equal(t, "key", literal.Action)
	require.Equal(t, "+", literal.Key)
}

func TestComputerInputContractRejectsConflictsWithoutPartialAssignment(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		`{"action":"key","key":"enter","automation":{"key":"escape"}}`,
		`{"action":"hotkey","key":"ctrl+n","modifiers":"alt"}`,
		`{"action":"key","key":"bogus+n"}`,
		`{"action":"key","automation":{"key":12}}`,
		`{"action":"key","automation":{"key":null}}`,
		`{"action":"invoke","automation":{"key":"enter"}}`,
	} {
		p := ComputerParams{Action: "unchanged"}
		require.Error(t, json.Unmarshal([]byte(input), &p))
		require.Equal(t, "unchanged", p.Action)
	}
}

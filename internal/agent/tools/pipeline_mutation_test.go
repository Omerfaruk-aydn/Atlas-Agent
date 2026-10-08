package tools

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPipelineMutationClassification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, input string
		mutation    bool
	}{
		{"help", `{"help":"desktop_flow"}`, false},
		{"observe", `{"desktop":{"mode":"observe","window_id":"11"}}`, false},
		{"resolve", `{"desktop":{"mode":"flow","flow":{"nodes":[{"id":"a","kind":"resolve","application":"Notepad"}]}}}`, false},
		{"press", `{"desktop":{"mode":"flow","flow":{"nodes":[{"id":"a","kind":"operation"}]}}}`, true},
		{"prepare", `{"desktop":{"mode":"prepare","application":"Notepad"}}`, true},
		{"browser_read", `{"browser":{"steps":[{"id":"read","action":"read_field"}]}}`, false},
		{"browser_write", `{"browser":{"steps":[{"id":"write","action":"set_field"}]}}`, true},
		{"saved_flow", `{"flow":{"name":"saved"}}`, true},
		{"generic_read", `{"steps":[{"id":"read","tool":"computer","arguments":{"action":"windows"}}]}`, false},
		{"generic_write", `{"steps":[{"id":"write","tool":"computer","arguments":{"action":"type","text":"example"}}]}`, true},
		{"item_action", `{"steps":[{"id":"items","tool":"computer","items":["key"],"arguments":{"action":"{{item}}","key":"enter"}}]}`, true},
		{"generic_file", `{"steps":[{"id":"write","tool":"write","arguments":{"file_path":"example.txt"}}]}`, true},
		{"cli_run", `{"steps":[{"id":"run","tool":"cli_bridge","arguments":{"action":"run"}}]}`, true},
		{"cli_draft", `{"steps":[{"id":"draft","tool":"cli_bridge","arguments":{"action":"draft"}}]}`, true},
		{"cli_preview", `{"steps":[{"id":"preview","tool":"cli_bridge","arguments":{"action":"preview"}}]}`, false},
		{"cli_status", `{"steps":[{"id":"status","tool":"cli_bridge","arguments":{"action":"status"}}]}`, false},
		{"nested_invalid", `{"steps":[{"id":"nested","tool":"tool_pipeline","arguments":{}}]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.mutation, CallMutates("tool_pipeline", tc.input))
		})
	}
}

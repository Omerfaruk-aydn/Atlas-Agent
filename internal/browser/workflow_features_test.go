package browser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkflowRequestPreflight(t *testing.T) {
	t.Parallel()
	for _, p := range []Request{
		{Action: "semantic_click", Role: "button", Name: "Save", Scope: "#profile"},
		{Action: "wait_for", Condition: "ready"},
		{Action: "wait_for", Condition: "network_idle"},
		{Action: "assert", Selector: "#check", Condition: "checked", Expected: "true"},
		{Action: "assert", Selector: ".row", Condition: "count", Expected: "3"},
	} {
		require.NoError(t, ValidateWorkflowRequest(p))
	}
	for _, p := range []Request{
		{Action: "semantic_click"},
		{Action: "assert", Condition: "sleep"},
		{Action: "upload", Paths: []string{"file.txt"}},
		{Action: "tab_select"},
		{Action: "wait_for", Condition: "ready", TimeoutMS: 30001},
		{Action: "find", Role: "button", ExpectedOrigin: "file:///private"},
	} {
		require.Error(t, ValidateWorkflowRequest(p))
	}
}

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

func TestBrowserOriginCanonicalization(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"https://Example.test:443/path", "https://example.test"} {
		origin, err := browserOrigin(raw)
		require.NoError(t, err)
		require.Equal(t, "https://example.test", origin)
	}
	_, err := browserOrigin("https://user:secret@example.test")
	require.Error(t, err)
	_, err = browserOrigin("javascript:alert(1)")
	require.Error(t, err)
}

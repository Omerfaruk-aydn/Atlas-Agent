package evaluation

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBenchMetricsPassAtKAndRegression(t *testing.T) {
	t.Parallel()
	records := []BenchRecord{{Task: "a", Variant: "base", Attempt: 0, Passed: false}, {Task: "a", Variant: "base", Attempt: 1, Passed: true, FailOpen: 2}, {Task: "b", Variant: "base", Attempt: 0, Passed: true}}
	metrics := BenchMetrics(records)
	require.Len(t, metrics, 1)
	require.Equal(t, 0.5, metrics[0].PassAt1)
	require.Equal(t, 1.0, metrics[0].PassAtK)
	require.Equal(t, 2, metrics[0].FailOpen)
	require.NotEmpty(t, BenchRegressions(records, []BenchRecord{{Task: "a", Variant: "base", Passed: true}, {Task: "b", Variant: "base", Passed: true}}))
}

func TestBenchAblationEnablesImplementedStagesAndPreservesOtherConfig(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"options":{"evaluator":{"enabled":true},"harness":{"trace":false}}}`)
	data, err := BenchGateConfig(raw, map[string]bool{"evaluator": false, "contract": true, "mutation": false})
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, json.Unmarshal(data, &result))
	options := result["options"].(map[string]any)
	require.Equal(t, true, options["evaluator"].(map[string]any)["enabled"])
	h := options["harness"].(map[string]any)
	require.Equal(t, false, h["trace"])
	require.Equal(t, []any{"evaluator", "mutation"}, h["disabled_gates"])
	data, err = BenchGateConfig(raw, map[string]bool{"mutation": true, "lint_fix": true})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &result))
	h = result["options"].(map[string]any)["harness"].(map[string]any)
	require.Equal(t, true, h["mutation"])
	require.Equal(t, true, h["lint_fix"])
	_, err = BenchGateConfig(raw, map[string]bool{"unknown": false})
	require.Error(t, err)
}

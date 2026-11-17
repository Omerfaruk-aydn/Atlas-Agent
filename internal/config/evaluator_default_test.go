package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// The completion gate is on unless it is switched off.
func TestEvaluatorIsOnByDefaultAndCanBeSwitchedOff(t *testing.T) {
	t.Parallel()
	var none *Options
	require.True(t, none.EvaluatorSettings().Enabled)
	require.True(t, (&Options{}).EvaluatorSettings().Enabled)

	decode := func(raw string) Evaluator {
		var opts Options
		require.NoError(t, json.Unmarshal([]byte(raw), &opts))
		return opts.EvaluatorSettings()
	}
	require.True(t, decode(`{"evaluator":{"max_rounds":3}}`).Enabled, "tuning the gate must not turn it off")
	require.Equal(t, 3, decode(`{"evaluator":{"max_rounds":3}}`).Rounds())
	require.False(t, decode(`{"evaluator":{"enabled":false}}`).Enabled)
	require.True(t, decode(`{"evaluator":{"enabled":true}}`).Enabled)
	require.True(t, decode(`{}`).Enabled)
}

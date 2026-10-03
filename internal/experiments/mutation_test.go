package experiments

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMutationReportSeparatesSurvivorsAndErrors(t *testing.T) {
	t.Parallel()
	result, err := MutationReport([]byte(`{"schemaVersion":"1","files":{"app.ts":{"mutants":[{"status":"Killed"},{"status":"Survived"},{"status":"NoCoverage"},{"status":"CompileError"}]}}}`))
	require.NoError(t, err)
	require.Equal(t, 1, result.Killed)
	require.Equal(t, 1, result.Survived)
	require.Equal(t, 1, result.NoCoverage)
	require.Equal(t, 1, result.Invalid)
	require.InDelta(t, 100.0/3, *result.Score, 0.001)
	_, err = MutationReport([]byte(`{"schemaVersion":"1","files":{"app.ts":{"mutants":[]}}}`))
	require.ErrorContains(t, err, "no mutants")
}

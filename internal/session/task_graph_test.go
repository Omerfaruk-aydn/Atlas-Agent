package session

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGraphRejectsCycleAndEarlyCompletion(t *testing.T) {
	a := Todo{ID: "a", Status: TodoStatusPending, DependsOn: []string{"b"}}
	b := Todo{ID: "b", Status: TodoStatusPending, DependsOn: []string{"a"}}
	require.ErrorContains(t, ValidateTaskGraph([]Todo{a, b}), "cycle")
	b.DependsOn = nil
	a.Status = TodoStatusCompleted
	require.ErrorContains(t, ValidateTaskGraph([]Todo{a, b}), "before dependency")
	b.Status = TodoStatusCompleted
	require.NoError(t, ValidateTaskGraph([]Todo{a, b}))
}

func TestWaveHonorsDependenciesAndOwnership(t *testing.T) {
	todos := []Todo{
		{ID: "a", Status: TodoStatusPending, OwnedPaths: []string{"src"}},
		{ID: "b", Status: TodoStatusPending, OwnedPaths: []string{"src/file.go"}},
		{ID: "c", Status: TodoStatusPending, OwnedPaths: []string{"docs"}},
		{ID: "d", Status: TodoStatusPending, DependsOn: []string{"a"}, OwnedPaths: []string{"tests"}},
	}
	wave, err := ReadyTaskWave(todos, 4)
	require.NoError(t, err)
	require.Len(t, wave, 2)
	require.Equal(t, "a", wave[0].ID)
	require.Equal(t, "c", wave[1].ID)
	todos[0].OwnedPaths = []string{"../outside"}
	require.Error(t, ValidateTaskGraph(todos))
}

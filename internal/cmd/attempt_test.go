package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAttemptPick(t *testing.T) {
	t.Parallel()
	for input, want := range map[string]int{"A": 1, "b": 2, " C ": 3, "2": 2, "10": 10} {
		got, err := parseAttemptPick(input)
		require.NoError(t, err, input)
		require.Equal(t, want, got, input)
	}
	for _, input := range []string{"", "0", "-1", "AB", "?"} {
		_, err := parseAttemptPick(input)
		require.Error(t, err, input)
	}
}

func TestAttemptCommandsAreRegisteredWithExpectedFlags(t *testing.T) {
	t.Parallel()
	command := newAttemptCommand()
	for _, name := range []string{"n", "models", "judge", "no-judge", "verify", "lint", "setup", "parallel", "timeout", "yolo", "keep", "apply", "json"} {
		require.NotNil(t, command.Flags().Lookup(name), name)
	}
	require.Error(t, command.Args(command, nil), "a task is required")

	attempts := newAttemptsCommand()
	var names []string
	for _, sub := range attempts.Commands() {
		names = append(names, sub.Name())
	}
	require.ElementsMatch(t, []string{"list", "show", "apply", "clean"}, names)

	found := map[string]bool{}
	for _, c := range rootCmd.Commands() {
		found[c.Name()] = true
	}
	require.True(t, found["attempt"])
	require.True(t, found["attempts"])
}

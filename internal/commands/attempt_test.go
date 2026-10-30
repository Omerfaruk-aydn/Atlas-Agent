package commands

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAttemptCommandCarriesTaskAsLiteralArgument(t *testing.T) {
	t.Parallel()
	commands := attemptCommands()
	require.Len(t, commands, 1)
	command := commands[0]
	require.Equal(t, "attempt", command.Name)
	require.Contains(t, command.Content, "$TASK")
	require.Contains(t, command.Content, "Do not apply anything until I agree")
	require.Len(t, command.Arguments, 1)
	require.True(t, command.Arguments[0].Required)
}

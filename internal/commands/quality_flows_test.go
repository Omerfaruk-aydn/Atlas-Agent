package commands

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQualitySlashCommandsExposeRequiredArguments(t *testing.T) {
	t.Parallel()
	commands := qualityCommands()
	require.Len(t, commands, 7)
	for _, command := range commands {
		require.NotEmpty(t, command.ID)
		require.Len(t, command.Arguments, 1)
		require.True(t, command.Arguments[0].Required)
		require.Contains(t, command.Content, "$"+command.Arguments[0].ID)
	}
}

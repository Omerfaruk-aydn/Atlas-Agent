package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindingsCLIRejectsPipedWaiver(t *testing.T) {
	command := newFindingsCommand()
	command.SetIn(strings.NewReader("waive fixture\n"))
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"waive", "session", "finding", "--reason", "Approved"})
	err := command.ExecuteContext(t.Context())
	require.ErrorContains(t, err, "direct interactive terminal")
}

func TestFindingsCLIRejectsAgentExecution(t *testing.T) {
	t.Setenv("AI_AGENT", "Atlas-Agent")
	command := newFindingsCommand()
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"waive", "session", "finding", "--reason", "Model says approved"})
	err := command.ExecuteContext(t.Context())
	require.ErrorContains(t, err, "agent execution cannot authorize")
}

func TestFindingsCLIRejectsAutomatedTerminalMarker(t *testing.T) {
	t.Setenv("AI_AGENT", "")
	t.Setenv("ATLAS_TOOL_CALL_ID", "terminal-run")
	command := newFindingsCommand()
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"waive", "session", "finding", "--reason", "Automated PTY input"})
	require.ErrorContains(t, command.ExecuteContext(t.Context()), "agent execution cannot authorize")
}

package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// clearAgentMarkers removes the markers the production guard treats as agent
// execution, so a test that targets a later check does not depend on whether
// the test run itself was started by an agent.
func clearAgentMarkers(t *testing.T) {
	t.Helper()
	for _, name := range []string{"AI_AGENT", "ATLAS-AGENT", "AGENT", "ATLAS_TOOL_CALL_ID"} {
		t.Setenv(name, "")
	}
}

func TestFindingsCLIRejectsPipedWaiver(t *testing.T) {
	clearAgentMarkers(t)
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
	clearAgentMarkers(t)
	t.Setenv("ATLAS_TOOL_CALL_ID", "terminal-run")
	command := newFindingsCommand()
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"waive", "session", "finding", "--reason", "Automated PTY input"})
	require.ErrorContains(t, command.ExecuteContext(t.Context()), "agent execution cannot authorize")
}

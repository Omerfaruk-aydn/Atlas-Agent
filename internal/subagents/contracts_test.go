package subagents

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContractsRouteByRequirementsAndPreserveIndependentCopies(t *testing.T) {
	t.Parallel()
	all := Builtin()
	for _, s := range all {
		require.NotNil(t, s.Contract)
		require.NoError(t, s.Validate())
		require.Contains(t, s.RolePrompt(), "role_contract")
		require.NotEmpty(t, s.Contract.DecisionRights)
		require.NotEmpty(t, s.Contract.OutOfScope)
		require.NotEmpty(t, s.Contract.StopConditions)
		require.NotEmpty(t, s.Contract.EvidenceRequired)
	}
	for _, tc := range []struct {
		req  RouteRequest
		name string
	}{
		{RouteRequest{Prompt: "Build an accessible responsive settings panel", TaskType: "frontend", RequiredTools: []string{"write"}, Output: "implementation"}, "frontend"},
		{RouteRequest{Prompt: "Review backend database API changes", TaskType: "review", RequiredTools: []string{"bash"}, Output: "findings"}, "review"},
		{RouteRequest{Prompt: "arayüz geliştir"}, "frontend"},
		{RouteRequest{TaskType: "planning", Output: "plan"}, "planner"},
	} {
		got, ok := Route(all, tc.req, nil)
		require.True(t, ok)
		require.Equal(t, tc.name, got.Name)
	}
	_, ok := Route(all, RouteRequest{TaskType: "review", RequiredTools: []string{"write"}}, nil)
	require.False(t, ok)
	_, ok = Route(all, RouteRequest{TaskType: "frontend"}, []string{"view"})
	require.False(t, ok)
	all[0].Contract.TaskTypes[0] = "corrupted"
	require.NotEqual(t, "corrupted", Builtin()[0].Contract.TaskTypes[0])
	all[0].Contract.DecisionRights[0] = "corrupted"
	require.NotEqual(t, "corrupted", Builtin()[0].Contract.DecisionRights[0])
	content, err := Render(all[1])
	require.NoError(t, err)
	parsed, err := ParseContent(content)
	require.NoError(t, err)
	require.Equal(t, all[1].Contract, parsed.Contract)
}

func TestHandoffRejectsContradictoryOrIncompleteReports(t *testing.T) {
	t.Parallel()
	valid := `{"task_id":"api","summary":"Inspected API","changed_files":[],"checks":[{"command":"go test","exit_code":0,"evidence":"exit 0"}],"risks":[],"dependencies":[],"decision":"passed"}`
	_, err := ParseHandoff(valid, "api")
	require.NoError(t, err)
	_, err = ParseHandoff(valid, "other")
	require.Error(t, err)
	for _, s := range []string{
		`{"task_id":"api","summary":"done","decision":"passed"}`,
		`{"task_id":"api","summary":"done","changed_files":[],"checks":[{"command":"go test","exit_code":null,"evidence":"not available"}],"risks":[],"dependencies":[],"decision":"passed"}`,
		`{"task_id":"api","summary":"done","changed_files":[],"checks":[],"risks":["broken API"],"dependencies":[],"decision":"passed"}`,
	} {
		_, err := ParseHandoff(s, "api")
		require.Error(t, err)
	}
}

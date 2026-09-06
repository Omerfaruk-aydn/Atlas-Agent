package agent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// The reported failure: a model sent agent_names as a JSON-encoded
// string rather than an array, and the tool call died with
// "cannot unmarshal string into Go struct field ... of type []string".
// The caller had the right idea and no way to act on the error.
func TestStringListAcceptsAQuotedArray(t *testing.T) {
	var params DebateParams
	raw := `{"question":"which","agent_names":"[\"review\",\"security\"]"}`

	require.NoError(t, json.Unmarshal([]byte(raw), &params))
	require.Equal(t, []string{"review", "security"}, []string(params.AgentNames))
}

func TestStringListAcceptsAnOrdinaryArray(t *testing.T) {
	var params DebateParams
	raw := `{"question":"which","agent_names":["review","security"]}`

	require.NoError(t, json.Unmarshal([]byte(raw), &params))
	require.Equal(t, []string{"review", "security"}, []string(params.AgentNames))
}

func TestStringListAcceptsACommaSeparatedString(t *testing.T) {
	var params DebateParams
	raw := `{"question":"which","agent_names":"review, security"}`

	require.NoError(t, json.Unmarshal([]byte(raw), &params))
	require.Equal(t, []string{"review", "security"}, []string(params.AgentNames))
}

func TestStringListAcceptsASingleName(t *testing.T) {
	var list stringList
	require.NoError(t, json.Unmarshal([]byte(`"review"`), &list))
	require.Equal(t, []string{"review"}, []string(list))
}

func TestStringListTreatsEmptyAsNothing(t *testing.T) {
	var list stringList
	require.NoError(t, json.Unmarshal([]byte(`"  "`), &list))
	require.Empty(t, list)
}

// Tolerance stops at shapes that are lists. A number is a mistake worth
// reporting, not one worth guessing at.
func TestStringListRejectsWhatIsNotAList(t *testing.T) {
	var list stringList
	require.Error(t, json.Unmarshal([]byte(`42`), &list))
}

func TestOrchestrateTakesAQuotedArrayToo(t *testing.T) {
	var params OrchestrateParams
	raw := `{"prompt":"p","agent_names":"[\"review\",\"security\"]"}`

	require.NoError(t, json.Unmarshal([]byte(raw), &params))
	require.Equal(t, []string{"review", "security"}, []string(params.AgentNames))
}

func TestDelegateTakesAQuotedTaskArray(t *testing.T) {
	var params DelegateParams
	raw := `{"tasks":"[{\"agent_name\":\"review\",\"prompt\":\"a\"},{\"agent_name\":\"security\",\"prompt\":\"b\"}]"}`

	require.NoError(t, json.Unmarshal([]byte(raw), &params))
	require.Len(t, params.Tasks, 2)
	require.Equal(t, "review", params.Tasks[0].AgentName)
	require.Equal(t, "b", params.Tasks[1].Prompt)
}

// A task list whose own contents are wrong still fails: only the outer
// quoting is forgiven.
func TestDelegateStillRejectsBrokenTasks(t *testing.T) {
	var params DelegateParams
	require.Error(t, json.Unmarshal([]byte(`{"tasks":"not a list at all"}`), &params))
}

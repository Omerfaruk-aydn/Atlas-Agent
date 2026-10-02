package workspace

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestTodoQualitySurvivesClientRoundTrip(t *testing.T) {
	t.Parallel()
	todos := []session.Todo{{ID: "feature", DependsOn: []string{"base"}, Agent: "backend", OwnedPaths: []string{"src"}, Content: "Feature", Status: session.TodoStatusCompleted, AcceptanceCriteria: []string{"Integration passes"}, Verification: "passed", Evidence: []session.TodoEvidence{{Kind: "command", Detail: "integration: exit 0"}}}}
	require.Equal(t, todos, protoToTodos(todosToProto(todos)))
}

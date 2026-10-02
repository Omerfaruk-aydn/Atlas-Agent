package server

import (
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestTodoQualitySurvivesServerJSON(t *testing.T) {
	t.Parallel()
	original := session.Session{Todos: []session.Todo{{ID: "feature", DependsOn: []string{"base"}, Agent: "backend", OwnedPaths: []string{"src"}, Content: "Feature", Status: session.TodoStatusCompleted, AcceptanceCriteria: []string{"Integration passes"}, Verification: "passed", Evidence: []session.TodoEvidence{{Kind: "command", Detail: "integration: exit 0"}}}}}
	data, err := json.Marshal(sessionToProto(original))
	require.NoError(t, err)
	var restored session.Session
	require.NoError(t, json.Unmarshal(data, &restored))
	require.Equal(t, original.Todos, restored.Todos)
}

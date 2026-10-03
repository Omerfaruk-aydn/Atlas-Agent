package session

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTodoQualityValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		todo  Todo
		valid bool
	}{
		{"legacy", Todo{Content: "Build", Status: TodoStatusCompleted}, true},
		{"unverified", Todo{Content: "Build", Status: TodoStatusCompleted, AcceptanceCriteria: []string{"Build passes"}}, false},
		{"failed", Todo{Content: "Build", Status: TodoStatusCompleted, AcceptanceCriteria: []string{"Build passes"}, Verification: "failed", Evidence: []TodoEvidence{{Kind: "command", Detail: "go build: exit 1"}}}, false},
		{"passed", Todo{Content: "Build", Status: TodoStatusCompleted, AcceptanceCriteria: []string{"Build passes"}, Verification: "passed", Evidence: []TodoEvidence{{Kind: "command", Detail: "go build: exit 0"}}}, true},
		{"user", Todo{Content: "Review UI", Status: TodoStatusCompleted, AcceptanceCriteria: []string{"User approves"}, Verification: "user_confirmed", Evidence: []TodoEvidence{{Kind: "user", Detail: "User explicitly confirmed the UI"}}}, true},
		{"false user provenance", Todo{Content: "Review UI", Status: TodoStatusCompleted, AcceptanceCriteria: []string{"User approves"}, Verification: "user_confirmed", Evidence: []TodoEvidence{{Kind: "inspection", Detail: "Looks good"}}}, false},
		{"unknown verification", Todo{Content: "Build", Status: TodoStatusPending, Verification: "amazing"}, false},
		{"empty criterion", Todo{Content: "Build", Status: TodoStatusPending, AcceptanceCriteria: []string{" "}}, false},
		{"invalid evidence", Todo{Content: "Build", Status: TodoStatusPending, Evidence: []TodoEvidence{{Kind: "guess", Detail: "Probably fine"}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateTodo(tc.todo)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestTodoQualityJSONCompatibility(t *testing.T) {
	t.Parallel()
	var legacy Todo
	require.NoError(t, json.Unmarshal([]byte(`{"content":"Build","status":"completed","active_form":"Building"}`), &legacy))
	require.NoError(t, ValidateTodo(legacy))
	todo := Todo{Content: "Build", Status: TodoStatusCompleted, AcceptanceCriteria: []string{"Build passes"}, Verification: "passed", Evidence: []TodoEvidence{{Kind: "command", Detail: "go build: exit 0"}}}
	data, err := json.Marshal(todo)
	require.NoError(t, err)
	var restored Todo
	require.NoError(t, json.Unmarshal(data, &restored))
	require.Equal(t, todo, restored)
}

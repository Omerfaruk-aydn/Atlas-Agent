package session

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// TodoEvidence records reported evidence, not independently verified execution.
type TodoEvidence struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// CompactTodo produces a bounded preview without mutating stored evidence.
func CompactTodo(todo Todo, limit int) Todo {
	trim := func(s string) string {
		if len(s) <= limit {
			return s
		}
		n := limit
		for n > 0 && !utf8.ValidString(s[:n]) {
			n--
		}
		return s[:n] + " [truncated]"
	}
	todo.Content = trim(todo.Content)
	todo.ActiveForm = trim(todo.ActiveForm)
	todo.Verification = trim(todo.Verification)
	todo.ID = trim(todo.ID)
	todo.Agent = trim(todo.Agent)
	todo.DependsOn = append([]string(nil), todo.DependsOn[:min(len(todo.DependsOn), 32)]...)
	todo.OwnedPaths = append([]string(nil), todo.OwnedPaths[:min(len(todo.OwnedPaths), 32)]...)
	for i := range todo.OwnedPaths {
		todo.OwnedPaths[i] = trim(todo.OwnedPaths[i])
	}
	todo.AcceptanceCriteria = append([]string(nil), todo.AcceptanceCriteria[:min(len(todo.AcceptanceCriteria), 16)]...)
	for i := range todo.AcceptanceCriteria {
		todo.AcceptanceCriteria[i] = trim(todo.AcceptanceCriteria[i])
	}
	todo.Evidence = append([]TodoEvidence(nil), todo.Evidence[:min(len(todo.Evidence), 16)]...)
	for i := range todo.Evidence {
		todo.Evidence[i].Kind = trim(todo.Evidence[i].Kind)
		todo.Evidence[i].Detail = trim(todo.Evidence[i].Detail)
	}
	return todo
}

// ValidateTodo rejects contradictory acceptance and completion reports.
// Legacy tasks without acceptance criteria remain compatible.
func ValidateTodo(todo Todo) error {
	if strings.TrimSpace(todo.Content) == "" || len(todo.Content) > 4096 {
		return fmt.Errorf("task content must contain 1 to 4096 bytes")
	}
	switch todo.Status {
	case TodoStatusPending, TodoStatusInProgress, TodoStatusCompleted:
	default:
		return fmt.Errorf("invalid task status %q", todo.Status)
	}
	switch todo.Verification {
	case "", "pending", "passed", "failed", "user_confirmed", "not_applicable":
	default:
		return fmt.Errorf("invalid verification status %q", todo.Verification)
	}
	if len(todo.AcceptanceCriteria) > 16 || len(todo.Evidence) > 16 {
		return fmt.Errorf("a task may contain at most 16 criteria and 16 evidence records")
	}
	for _, criterion := range todo.AcceptanceCriteria {
		if strings.TrimSpace(criterion) == "" || len(criterion) > 2048 {
			return fmt.Errorf("acceptance criteria must contain 1 to 2048 bytes")
		}
	}
	hasUser := false
	for _, evidence := range todo.Evidence {
		switch evidence.Kind {
		case "command", "inspection", "user":
		default:
			return fmt.Errorf("invalid evidence kind %q", evidence.Kind)
		}
		if strings.TrimSpace(evidence.Detail) == "" || len(evidence.Detail) > 4096 {
			return fmt.Errorf("evidence details must contain 1 to 4096 bytes")
		}
		hasUser = hasUser || evidence.Kind == "user"
	}
	if todo.Verification == "user_confirmed" && !hasUser {
		return fmt.Errorf("user_confirmed requires an explicit user evidence record")
	}
	if todo.Status == TodoStatusCompleted && len(todo.AcceptanceCriteria) > 0 {
		if len(todo.Evidence) == 0 {
			return fmt.Errorf("completed tasks with acceptance criteria require evidence")
		}
		switch todo.Verification {
		case "passed", "user_confirmed", "not_applicable":
		default:
			return fmt.Errorf("completed tasks require passed, user_confirmed or explained not_applicable verification")
		}
	}
	if todo.Status == TodoStatusCompleted && todo.Verification == "failed" {
		return fmt.Errorf("a task with failed verification cannot be completed")
	}
	if todo.Status == TodoStatusCompleted && todo.Verification == "pending" {
		return fmt.Errorf("a task with pending verification cannot be completed")
	}
	return nil
}

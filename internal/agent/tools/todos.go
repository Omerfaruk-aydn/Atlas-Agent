package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
)

//go:embed todos.md
var todosDescription string

const TodosToolName = "todos"

type TodosParams struct {
	Action string     `json:"action,omitempty" description:"update (default) replaces the task list; list reads one persisted task at offset without changing it."`
	Offset int        `json:"offset,omitempty" description:"Zero-based task index for action list."`
	Todos  []TodoItem `json:"todos,omitempty" description:"The updated todo list; required for update, unused for list."`
}

type TodoItem struct {
	ID                 string                 `json:"id,omitempty" description:"Stable task ID for dependencies and workflow dispatch."`
	DependsOn          []string               `json:"depends_on,omitempty" description:"IDs of tasks that must complete first."`
	Agent              string                 `json:"agent,omitempty" description:"Named specialist for workflow dispatch."`
	OwnedPaths         []string               `json:"owned_paths,omitempty" description:"Literal project-relative files or directories this task may change."`
	Content            string                 `json:"content" description:"What needs to be done (imperative form)"`
	Status             string                 `json:"status" description:"Task status: pending, in_progress, or completed"`
	ActiveForm         string                 `json:"active_form" description:"Present continuous form (e.g., 'Running tests')"`
	AcceptanceCriteria []string               `json:"acceptance_criteria,omitempty" description:"Observable completion criteria for this task, at most 16."`
	Verification       string                 `json:"verification,omitempty" description:"Reported verification: pending, passed, failed, user_confirmed, or not_applicable. Evidence reports do not independently prove execution."`
	Evidence           []session.TodoEvidence `json:"evidence,omitempty" description:"Reported evidence with kind command, inspection or user, and detail containing the actual result or explicit user confirmation."`
}

type TodosResponseMetadata struct {
	IsNew         bool           `json:"is_new"`
	Todos         []session.Todo `json:"todos"`
	JustCompleted []string       `json:"just_completed,omitempty"`
	JustStarted   string         `json:"just_started,omitempty"`
	Completed     int            `json:"completed"`
	Total         int            `json:"total"`
}

func NewTodosTool(sessions session.Service) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		TodosToolName,
		todosDescription,
		func(ctx context.Context, params TodosParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for managing todos")
			}

			currentSession, err := sessions.Get(ctx, sessionID)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("failed to get session: %w", err)
			}
			if params.Action == "list" {
				if params.Offset < 0 || params.Offset > len(currentSession.Todos) {
					return fantasy.NewTextErrorResponse("Task offset is out of range"), nil
				}
				var todo *session.Todo
				if params.Offset < len(currentSession.Todos) {
					item := currentSession.Todos[params.Offset]
					todo = &item
				}
				result := map[string]any{"todo": todo, "offset": params.Offset, "total": len(currentSession.Todos), "truncated": false}
				data, err := json.Marshal(result)
				if err != nil {
					return fantasy.ToolResponse{}, err
				}
				if len(data) > 128*1024 && todo != nil {
					compact := session.CompactTodo(*todo, 256)
					result["todo"] = compact
					result["truncated"] = true
					data, err = json.Marshal(result)
					if err != nil {
						return fantasy.ToolResponse{}, err
					}
				}
				return fantasy.NewTextResponse(string(data)), nil
			}
			if params.Action != "" && params.Action != "update" {
				return fantasy.NewTextErrorResponse("Unknown todo action; use update or list"), nil
			}
			if params.Todos == nil {
				return fantasy.NewTextErrorResponse("update requires todos; use action=list to read existing tasks"), nil
			}

			isNew := len(currentSession.Todos) == 0
			oldStatusByContent := make(map[string]session.TodoStatus)
			oldTodoByContent := make(map[string]session.Todo)
			oldTodoByID := make(map[string]session.Todo)
			for _, todo := range currentSession.Todos {
				oldStatusByContent[todo.Content] = todo.Status
				oldTodoByContent[todo.Content] = todo
				if todo.ID != "" {
					oldTodoByID[todo.ID] = todo
				}
			}
			for i := range params.Todos {
				item := &params.Todos[i]
				old, exists := oldTodoByContent[item.Content]
				if item.ID != "" {
					if byID, ok := oldTodoByID[item.ID]; ok {
						old = byID
						exists = true
					}
				}
				if exists {
					if item.ID == "" {
						item.ID = old.ID
					}
					if item.DependsOn == nil {
						item.DependsOn = old.DependsOn
					}
					if item.Agent == "" {
						item.Agent = old.Agent
					}
					if item.OwnedPaths == nil {
						item.OwnedPaths = old.OwnedPaths
					}
					changed := item.Content != old.Content || !slices.Equal(item.DependsOn, old.DependsOn) || !slices.Equal(item.OwnedPaths, old.OwnedPaths) || (item.AcceptanceCriteria != nil && !slices.Equal(item.AcceptanceCriteria, old.AcceptanceCriteria)) || (old.Status == session.TodoStatusCompleted && item.Status != string(session.TodoStatusCompleted))
					if item.AcceptanceCriteria == nil {
						item.AcceptanceCriteria = old.AcceptanceCriteria
					}
					if item.Verification == "" {
						if changed {
							item.Verification = "pending"
						} else {
							item.Verification = old.Verification
						}
					}
					if item.Evidence == nil && !changed {
						item.Evidence = old.Evidence
					}
				}
			}

			for _, item := range params.Todos {
				if err := session.ValidateTodo(session.Todo{Content: item.Content, Status: session.TodoStatus(item.Status), AcceptanceCriteria: item.AcceptanceCriteria, Verification: item.Verification, Evidence: item.Evidence}); err != nil {
					return fantasy.NewTextErrorResponse(fmt.Sprintf("Invalid task %q: %s", item.Content, err)), nil
				}
			}

			todos := make([]session.Todo, len(params.Todos))
			var justCompleted []string
			var justStarted string
			completedCount := 0

			for i, item := range params.Todos {
				todos[i] = session.Todo{
					ID: item.ID, DependsOn: item.DependsOn, Agent: item.Agent, OwnedPaths: item.OwnedPaths,
					Content:            item.Content,
					Status:             session.TodoStatus(item.Status),
					ActiveForm:         item.ActiveForm,
					AcceptanceCriteria: item.AcceptanceCriteria,
					Verification:       item.Verification,
					Evidence:           item.Evidence,
				}

				newStatus := session.TodoStatus(item.Status)
				oldStatus, existed := oldStatusByContent[item.Content]

				if newStatus == session.TodoStatusCompleted {
					completedCount++
					if existed && oldStatus != session.TodoStatusCompleted {
						justCompleted = append(justCompleted, item.Content)
					}
				}

				if newStatus == session.TodoStatusInProgress {
					if !existed || oldStatus != session.TodoStatusInProgress {
						if item.ActiveForm != "" {
							justStarted = item.ActiveForm
						} else {
							justStarted = item.Content
						}
					}
				}
			}

			if err := session.ValidateTaskGraph(todos); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			currentSession.Todos = todos
			_, err = sessions.Save(ctx, currentSession)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("failed to save todos: %w", err)
			}

			response := "Todo list updated successfully.\n\n"

			pendingCount := 0
			inProgressCount := 0

			for _, todo := range todos {
				switch todo.Status {
				case session.TodoStatusPending:
					pendingCount++
				case session.TodoStatusInProgress:
					inProgressCount++
				}
			}

			response += fmt.Sprintf("Status: %d pending, %d in progress, %d completed\n",
				pendingCount, inProgressCount, completedCount)

			response += "Todos have been modified successfully. Ensure that you continue to use the todo list to track your progress. Please proceed with the current tasks if applicable."

			metadata := TodosResponseMetadata{
				IsNew:         isNew,
				Todos:         todos,
				JustCompleted: justCompleted,
				JustStarted:   justStarted,
				Completed:     completedCount,
				Total:         len(todos),
			}

			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(response), metadata), nil
		},
	)
}

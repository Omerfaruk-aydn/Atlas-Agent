package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
)

func (c *coordinator) reviseControlTask(ctx context.Context, id string, snapshot engineering.WorkflowSnapshot, action engineering.WorkflowControl) error {
	if action.TaskID == "" {
		return fmt.Errorf("select a task")
	}
	if taskHasRunner(snapshot, action.TaskID) {
		return fmt.Errorf("stop and reconcile this task before revision")
	}
	for _, op := range snapshot.Operations {
		if op.TaskID == action.TaskID && (op.Status == "running" || op.Status == "background") {
			return fmt.Errorf("reconcile this task's active operations before revision")
		}
	}
	sess, err := c.sessions.Get(ctx, id)
	if err != nil {
		return err
	}
	before := session.TodosFingerprint(sess.Todos)
	found := false
	for i := range sess.Todos {
		task := &sess.Todos[i]
		if task.ID != action.TaskID {
			continue
		}
		if task.Status == session.TodoStatusCompleted || action.Action != "task_retry" && task.Status != session.TodoStatusPending {
			return fmt.Errorf("only pending tasks may be edited; reconcile interrupted tasks before retry")
		}
		switch action.Action {
		case "task_replan":
			if strings.TrimSpace(action.Text) == "" || len(action.Text) > 8192 {
				return fmt.Errorf("replan requires bounded task text")
			}
			task.Content = strings.TrimSpace(action.Text)
		case "task_scope":
			if len(action.OwnedPaths) == 0 {
				return fmt.Errorf("scope requires literal owned_paths")
			}
			task.OwnedPaths = action.OwnedPaths
		case "task_retry":
			task.Status = session.TodoStatusPending
		}
		task.Verification, task.Evidence = "pending", nil
		found = true
	}
	if !found {
		return fmt.Errorf("task not found")
	}
	if err := session.ValidateTaskGraph(sess.Todos); err != nil {
		return err
	}
	// Invalidate before the graph commit. A racing graph edit can leave checks
	// invalidated, but can never retain old certification for revised work.
	if err := c.engineering.Update(ctx, id, func(st *engineering.State) error { delete(st.RoleExecutions, action.TaskID); return nil }); err != nil {
		return err
	}
	_, err = c.sessions.CompareAndSwapTodos(ctx, id, before, sess.Todos)
	return err
}

package agent

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

func taskHasRunner(snapshot engineering.WorkflowSnapshot, task string) bool {
	return slices.ContainsFunc(snapshot.Runners, func(r engineering.LiveRunner) bool { return r.TaskID == task })
}

func (c *coordinator) controlBoardAction(ctx context.Context, id string, snapshot engineering.WorkflowSnapshot, action engineering.WorkflowControl) error {
	hasTask := func(id string) bool {
		return slices.ContainsFunc(snapshot.Tasks, func(t engineering.WorkflowTask) bool { return t.ID == id && id != "" })
	}
	if action.TaskID != "" && !hasTask(action.TaskID) && !taskHasRunner(snapshot, action.TaskID) {
		return fmt.Errorf("task not found")
	}
	for _, dependency := range action.DependsOn {
		if !hasTask(dependency) {
			return fmt.Errorf("unknown queue dependency %q", dependency)
		}
	}
	switch action.Action {
	case "steer", "queue", "feedback":
		mode := "steer"
		if action.Action == "queue" {
			mode = "next"
		}
		file := strings.ReplaceAll(action.File, `\`, "/")
		if action.Action == "feedback" {
			if file == "" || len(file) > 512 || strings.ContainsAny(file, "*?:") || strings.HasPrefix(file, "/") || path.Clean(file) == ".." || strings.HasPrefix(path.Clean(file), "../") || action.Line < 1 {
				return fmt.Errorf("feedback requires a project-relative file and positive line")
			}
			file = path.Clean(file)
		}
		if err := c.engineering.EnqueueDirective(ctx, id, engineering.UserDirective{TaskID: action.TaskID, Mode: mode, Text: action.Text, DependsOn: action.DependsOn, File: file, Line: action.Line}); err != nil {
			return err
		}
		// Busy sessions consume at the next boundary. Idle queues require an
		// explicit start, so a late instruction never unexpectedly restarts a
		// cancelled or finished session.
		return nil
	case "budget":
		if action.Limits == nil {
			return fmt.Errorf("budget requires limits")
		}
		return c.engineering.SetBudget(ctx, id, action.TaskID, *action.Limits)
	case "task_pause", "task_resume", "cancel_task":
		if action.TaskID == "" {
			return fmt.Errorf("task control requires task_id")
		}
		if action.Action == "cancel_task" && !taskHasRunner(snapshot, action.TaskID) {
			return fmt.Errorf("live task runner unavailable; cancellation was not acknowledged")
		}
		err := c.engineering.UpdateControlBoard(ctx, id, func(board *engineering.ControlBoard) error {
			if action.Action == "task_resume" {
				board.HeldTasks = slices.DeleteFunc(board.HeldTasks, func(t string) bool { return t == action.TaskID })
			} else if !slices.Contains(board.HeldTasks, action.TaskID) {
				board.HeldTasks = append(board.HeldTasks, action.TaskID)
			}
			return nil
		})
		if err != nil {
			return err
		}
		if action.Action == "cancel_task" {
			c.engineering.CancelTask(id, action.TaskID)
		}
		return nil
	case "team_limit":
		if action.MaxAgents < 1 || action.MaxAgents > 16 {
			return fmt.Errorf("agent limit must be between 1 and 16")
		}
		return c.engineering.UpdateControlBoard(ctx, id, func(board *engineering.ControlBoard) error { board.MaxAgents = action.MaxAgents; return nil })
	default:
		if action.Action == "queue_retry" && c.currentAgent == nil {
			return fmt.Errorf("live coordinator unavailable; inspect uncertain delivery in the original session")
		}
		if action.Action == "queue_retry" && (snapshot.Busy || len(snapshot.Runners) > 0) {
			return fmt.Errorf("wait for agents to stop before retrying uncertain delivery")
		}
		return c.engineering.UpdateControlBoard(ctx, id, func(board *engineering.ControlBoard) error {
			i := slices.IndexFunc(board.Directives, func(d engineering.UserDirective) bool { return d.ID == action.DirectiveID })
			if i < 0 {
				return fmt.Errorf("queue entry not found")
			}
			d := &board.Directives[i]
			switch action.Action {
			case "queue_cancel":
				if d.Status != "queued" {
					return fmt.Errorf("only undelivered instructions can be cancelled")
				}
				d.Status = "cancelled"
			case "queue_retry":
				if d.Status != "claimed" && d.Status != "cancelled" {
					return fmt.Errorf("only ambiguous or cancelled instructions can be retried")
				}
				d.Status, d.ReceivedAt = "queued", 0
			case "queue_remove":
				if d.Status != "received" && d.Status != "cancelled" {
					return fmt.Errorf("cancel or reconcile the instruction before removal")
				}
				board.Directives = slices.Delete(board.Directives, i, i+1)
			case "queue_up", "queue_down":
				if d.Status != "queued" {
					return fmt.Errorf("only queued instructions can be reordered")
				}
				j := i - 1
				if action.Action == "queue_down" {
					j = i + 1
				}
				if j >= 0 && j < len(board.Directives) {
					board.Directives[i], board.Directives[j] = board.Directives[j], board.Directives[i]
				}
			default:
				return fmt.Errorf("unknown queue control")
			}
			return nil
		})
	}
}

package agent

import (
	"context"
	"fmt"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
)

type WorkflowController interface {
	WorkflowSnapshot(context.Context, string) (engineering.WorkflowSnapshot, error)
	WorkflowControl(context.Context, string, engineering.WorkflowControl) error
}

// NewWorkflowController provides model-free persistent controls. A live stop
// must use the running coordinator rather than this standalone controller.
func NewWorkflowController(cfg *config.ConfigStore, sessions session.Service, store *engineering.Store) WorkflowController {
	return &coordinator{cfg: cfg, sessions: sessions, engineering: store}
}

func ReadWorkflowSnapshot(ctx context.Context, store *engineering.Store, sessions session.Service, root, id string, busy bool) (engineering.WorkflowSnapshot, error) {
	for range 3 {
		sess, err := sessions.Get(ctx, id)
		if err != nil {
			return engineering.WorkflowSnapshot{}, err
		}
		tasks := make([]engineering.WorkflowTask, 0, len(sess.Todos))
		for _, task := range sess.Todos {
			tasks = append(tasks, engineering.WorkflowTask{ID: task.ID, Content: task.Content, Status: string(task.Status), Agent: task.Agent, SpecFingerprint: session.TaskFingerprint(task), DependsOn: task.DependsOn, OwnedPaths: task.OwnedPaths, AcceptanceCriteria: task.AcceptanceCriteria, Verification: task.Verification})
		}
		out, err := store.Snapshot(ctx, root, id, tasks, session.TodosFingerprint(sess.Todos), busy)
		if err != nil {
			return out, err
		}
		after, err := sessions.Get(ctx, id)
		if err != nil {
			return out, err
		}
		if session.TodosFingerprint(after.Todos) == session.TodosFingerprint(sess.Todos) {
			return out, nil
		}
	}
	return engineering.WorkflowSnapshot{}, fmt.Errorf("task graph changed during snapshot; retry")
}

func (c *coordinator) WorkflowSnapshot(ctx context.Context, id string) (engineering.WorkflowSnapshot, error) {
	out, err := ReadWorkflowSnapshot(ctx, c.engineering, c.sessions, c.cfg.WorkingDir(), id, c.currentAgent != nil && c.IsSessionBusy(id))
	if err != nil {
		return out, err
	}
	out.AgentLimit = 16
	if configured := c.cfg.Config().Options.MaxConcurrentSubAgents; configured > 0 {
		out.AgentLimit = min(out.AgentLimit, configured)
	}
	if out.Board.MaxAgents > 0 {
		out.AgentLimit = min(out.AgentLimit, out.Board.MaxAgents)
	}
	return out, nil
}

func (c *coordinator) WorkflowControl(ctx context.Context, id string, action engineering.WorkflowControl) error {
	if action.ExpectedRevision == "" {
		return fmt.Errorf("workflow control requires expected_revision")
	}
	release, err := c.engineering.WorkflowLock(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	snapshot, err := c.WorkflowSnapshot(ctx, id)
	if err != nil {
		return err
	}
	if snapshot.Revision != action.ExpectedRevision {
		return fmt.Errorf("workflow revision conflict; refresh snapshot")
	}
	switch action.Action {
	case "start_queue":
		return c.startControlQueue(ctx, id)
	case "task_retry", "task_replan", "task_scope":
		return c.reviseControlTask(ctx, id, snapshot, action)
	case "inspect_checkpoint", "resume_checkpoint":
		return c.controlCheckpoint(ctx, id, snapshot, action)
	case "steer", "queue", "feedback", "task_pause", "task_resume", "cancel_task", "queue_cancel", "queue_retry", "queue_remove", "queue_up", "queue_down", "team_limit", "budget":
		return c.controlBoardAction(ctx, id, snapshot, action)
	case "pause", "resume":
		return c.engineering.Update(ctx, id, func(st *engineering.State) error { st.Paused = action.Action == "pause"; return nil })
	case "stop":
		if c.currentAgent == nil {
			return fmt.Errorf("live agent coordinator unavailable; stop was not acknowledged")
		}
		if c.currentAgent != nil {
			c.Cancel(id)
		}
		// Cancellation is a request. Running operation identities remain visible
		// until their runners observe exit or explicit recovery reconciles them.
		return c.engineering.Update(ctx, id, func(st *engineering.State) error { st.Paused = true; return nil })
	case "reassign":
		if action.TaskID == "" || action.Agent == "" {
			return fmt.Errorf("reassignment requires task_id and agent")
		}
		if taskHasRunner(snapshot, action.TaskID) {
			return fmt.Errorf("stop and reconcile this task before reassignment")
		}
		for _, op := range snapshot.Operations {
			if op.TaskID == action.TaskID && (op.Status == "running" || op.Status == "background") {
				return fmt.Errorf("reconcile active operations before reassignment")
			}
		}
		roles := subagents.Discover(c.cfg.Config().Options.SubagentsPaths)
		if _, ok := subagents.Find(roles, action.Agent); !ok {
			return fmt.Errorf("unknown role %q", action.Agent)
		}
		sess, err := c.sessions.Get(ctx, id)
		if err != nil {
			return err
		}
		found := false
		for i := range sess.Todos {
			if sess.Todos[i].ID != action.TaskID {
				continue
			}
			if sess.Todos[i].Status != session.TodoStatusPending {
				return fmt.Errorf("only pending reconciled tasks can be reassigned")
			}
			sess.Todos[i].Agent = action.Agent
			sess.Todos[i].Verification = "pending"
			sess.Todos[i].Evidence = nil
			found = true
		}
		if !found {
			return fmt.Errorf("task not found")
		}
		var before string
		beforeTasks, err := c.sessions.Get(ctx, id)
		if err != nil {
			return err
		}
		before = session.TodosFingerprint(beforeTasks.Todos)
		if snapshot.Tasks == nil {
			return fmt.Errorf("task graph changed")
		}
		latest, err := c.WorkflowSnapshot(ctx, id)
		if err != nil {
			return err
		}
		if latest.Revision != snapshot.Revision {
			return fmt.Errorf("workflow revision conflict")
		}
		_, err = c.sessions.CompareAndSwapTodos(ctx, id, before, sess.Todos)
		return err
	default:
		return fmt.Errorf("unknown workflow control %q", action.Action)
	}
}

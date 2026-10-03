package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agentstate"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/interaction"
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
			out.Context, out.ContextPreferences, err = store.ReadContext(ctx, id)
			if err != nil {
				return out, err
			}
			out.Batches, err = store.AgentBatches(ctx, id)
			if err != nil {
				return out, err
			}
			contextRevision, _ := json.Marshal(out.ContextPreferences)
			if err := interaction.Default.Load(root, id); err != nil {
				return out, err
			}
			out.Interactions = interaction.Default.Snapshot(id)
			out.Revision = engineering.Hash(fmt.Sprintf("%s:%d", out.Revision, out.Interactions.ControlRevision))
			out.Revision = engineering.Hash(out.Revision + string(contextRevision))
			if provider, ok := sessions.(interface{ AgentState() *agentstate.Store }); ok && provider.AgentState() != nil {
				state := provider.AgentState()
				scope := platformScopeForRoot(root)
				out.PlatformJobs, err = agentstate.Jobs(ctx, state, scope+"/agent_jobs")
				if err != nil {
					return out, err
				}
				out.PlatformTasks, err = agentstate.Tasks(ctx, state, scope+"/task_board")
				if err != nil {
					return out, err
				}
				out.SourceMemories, err = agentstate.Memories(ctx, state, scope+"/source_memory", root, "")
				if err != nil {
					return out, err
				}
				if len(out.PlatformJobs) == 0 {
					out.PlatformJobs = nil
				}
				if len(out.PlatformTasks) == 0 {
					out.PlatformTasks = nil
				}
				if len(out.SourceMemories) == 0 {
					out.SourceMemories = nil
				}
				encoded, _ := json.Marshal([]any{out.PlatformJobs, out.PlatformTasks, out.SourceMemories})
				out.Revision = engineering.Hash(out.Revision + string(encoded))
			}

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
	if out.Interactions.LivePreview && ctx.Value(interactionPreviewSuppressed{}) == nil {
		if err := c.captureInteractionPreview(ctx, id); err != nil {
			out.Capabilities["interaction_preview"] = err.Error()
		}
		out.Interactions = interaction.Default.Snapshot(id)
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
	snapshot, err := c.WorkflowSnapshot(context.WithValue(ctx, interactionPreviewSuppressed{}, true), id)
	if err != nil {
		return err
	}
	if snapshot.Revision != action.ExpectedRevision {
		return fmt.Errorf("workflow revision conflict; refresh snapshot")
	}
	switch action.Action {
	case "job_pause", "job_resume", "job_recover":
		if c.stateStore() == nil {
			return fmt.Errorf("automation unavailable")
		}
		return agentstate.ControlJob(ctx, c.stateStore(), c.platformScope()+"/agent_jobs", action.Text, map[string]string{"job_pause": "pause", "job_resume": "resume", "job_recover": "recover"}[action.Action])
	case "board_accept", "board_retry", "board_recover":
		if c.stateStore() == nil {
			return fmt.Errorf("task board unavailable")
		}
		return agentstate.TransitionTask(ctx, c.stateStore(), c.platformScope()+"/task_board", action.Text, map[string]string{"board_accept": "accept", "board_retry": "retry", "board_recover": "recover"}[action.Action], "", nil, "")
	case "interaction_capture":
		return c.captureInteractionPreview(ctx, id)
	case "interaction_preview":
		interaction.Default.SetPreview(id, !snapshot.Interactions.LivePreview)
		return interaction.Default.Record(c.cfg.WorkingDir(), id, interaction.Entry{Resource: "control", Action: "preview", Status: "ready"})
	case "interaction_pause":
		interaction.Default.Pause(id, "User took control from TUI")
		return interaction.Default.Record(c.cfg.WorkingDir(), id, interaction.Entry{Resource: "control", Action: "pause", Status: "paused"})
	case "interaction_resume":
		interaction.Default.Resume(id)
		return interaction.Default.Record(c.cfg.WorkingDir(), id, interaction.Entry{Resource: "control", Action: "resume", Status: "ready"})
	case "batch_retry":
		for _, batch := range snapshot.Batches {
			if batch.ID != action.Text {
				continue
			}
			var request AgentParams
			if err := json.Unmarshal(batch.Request, &request); err != nil {
				return err
			}
			request.RetryFailed = true
			if err := validateAgentBatch(request); err != nil {
				return err
			}
			hasFailed := false
			for _, row := range batch.Rows {
				hasFailed = hasFailed || row.Status == "failed"
			}
			if !hasFailed {
				return fmt.Errorf("batch has no failed rows")
			}
			data, err := json.Marshal(AgentParams{Mode: "batch", BatchID: batch.ID, RetryFailed: true, Prompt: "Retry failed rows of the stored batch definition"})
			if err != nil {
				return err
			}
			return c.engineering.EnqueueDirective(ctx, id, engineering.UserDirective{Mode: "next", Text: "Retry only the failed rows by calling the agent tool with exactly these arguments. Do not rerun successful or interrupted rows.\n" + string(data)})
		}
		return fmt.Errorf("batch not found")
	case "context_pin", "context_unpin", "context_exclude", "context_include":
		return c.engineering.ContextControl(ctx, c.cfg.WorkingDir(), id, action.Action, action.Text)
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

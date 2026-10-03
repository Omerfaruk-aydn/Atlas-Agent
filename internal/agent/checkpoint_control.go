package agent

import (
	"context"
	"fmt"
	"reflect"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
)

func (c *coordinator) controlCheckpoint(ctx context.Context, id string, snapshot engineering.WorkflowSnapshot, action engineering.WorkflowControl) error {
	cp, err := c.engineering.ReadCheckpoint(ctx, c.cfg.WorkingDir(), id, action.CheckpointID)
	if err != nil {
		return err
	}
	sess, err := c.sessions.Get(ctx, id)
	if err != nil {
		return err
	}
	plan, err := c.currentResumePlan(ctx, cp, sess)
	if err != nil {
		return err
	}
	if action.Action == "inspect_checkpoint" {
		return c.engineering.UpdateControlBoard(ctx, id, func(board *engineering.ControlBoard) error { board.ResumePreview = &plan; return nil })
	}
	if snapshot.Busy || len(snapshot.Runners) > 0 {
		return fmt.Errorf("wait for active agents before checkpoint resume")
	}
	if snapshot.Board.ResumePreview == nil || !reflect.DeepEqual(*snapshot.Board.ResumePreview, plan) {
		return fmt.Errorf("checkpoint plan changed; inspect it again before resume")
	}
	if len(plan.AmbiguousOperations) > 0 || len(plan.ReverifyTasks) > 0 {
		return fmt.Errorf("reconcile ambiguous operations and reverify changed tasks before resume")
	}
	latest, err := c.sessions.Get(ctx, id)
	if err != nil {
		return err
	}
	if session.TodosFingerprint(latest.Todos) != session.TodosFingerprint(sess.Todos) {
		return fmt.Errorf("task graph changed before resume")
	}
	return c.engineering.UpdateRevision(ctx, id, plan.StateRevision, func(st *engineering.State) error {
		fresh, err := engineering.SourceFingerprint(ctx, cp.Root, c.engineering.Dir())
		if err != nil {
			return err
		}
		if fresh != plan.SourceFingerprint {
			return fmt.Errorf("source changed before resume")
		}
		st.Resume, st.Paused = &plan, false
		return nil
	})
}

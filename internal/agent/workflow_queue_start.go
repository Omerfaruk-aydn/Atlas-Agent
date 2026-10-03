package agent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
)

type queueContinuationKey struct{}

// startControlQueue uses the same accepted-run lifecycle as interactive user
// messages. It reserves before dispatch so a stop can cancel startup too.
func (c *coordinator) startControlQueue(ctx context.Context, id string) error {
	if c.currentAgent == nil {
		return fmt.Errorf("live coordinator unavailable; queue remains persisted")
	}
	if c.IsSessionBusy(id) {
		return fmt.Errorf("agent is busy; queued work continues at a turn boundary")
	}
	state, err := c.engineering.Read(ctx, id)
	if err != nil {
		return err
	}
	if state.Paused {
		return fmt.Errorf("resume session dispatch before starting the queue")
	}
	if err := c.engineering.Check(ctx, id, ""); err != nil {
		return err
	}
	_, board, err := c.engineering.ReadControlBoard(ctx, id)
	if err != nil {
		return err
	}
	sess, err := c.sessions.Get(ctx, id)
	if err != nil {
		return err
	}
	completed := map[string]bool{}
	for _, task := range sess.Todos {
		completed[task.ID] = task.Status == session.TodoStatusCompleted
	}
	ready := false
	for _, d := range board.Directives {
		if d.Status != "queued" || d.TaskID != "" {
			continue
		}
		eligible := true
		for _, dep := range d.DependsOn {
			if !completed[dep] {
				eligible = false
			}
		}
		if eligible {
			ready = true
			break
		}
	}
	if !ready {
		return fmt.Errorf("no ready coordinator instructions; inspect task targets and dependencies")
	}
	accept := c.BeginAccepted(id)
	runContext := c.lifetime
	if runContext == nil {
		runContext = context.WithoutCancel(ctx)
	}
	runContext = context.WithValue(runContext, queueContinuationKey{}, true)
	go func() {
		defer accept.Close()
		if _, err := c.RunAccepted(runContext, accept, id, "Continue with the next ready user instruction from the persistent workflow queue."); err != nil {
			slog.Warn("Workflow queue run failed", "session_id", id, "error", err)
		}
	}()
	return nil
}

package workspace

import (
	"context"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/pubsub"
)

func (w *AppWorkspace) WorkflowSnapshot(ctx context.Context, id string) (engineering.WorkflowSnapshot, error) {
	if w.app.AgentCoordinator == nil {
		return engineering.WorkflowSnapshot{}, ErrAgentNotInitialized
	}
	return w.app.AgentCoordinator.WorkflowSnapshot(ctx, id)
}

func (w *AppWorkspace) WorkflowControl(ctx context.Context, id string, control engineering.WorkflowControl) error {
	if w.app.AgentCoordinator == nil {
		return ErrAgentNotInitialized
	}
	if err := w.app.AgentCoordinator.WorkflowControl(ctx, id, control); err != nil {
		return err
	}
	snapshot, _ := w.app.AgentCoordinator.WorkflowSnapshot(ctx, id)
	w.app.SendEvent(pubsub.Event[engineering.WorkflowChanged]{Type: pubsub.UpdatedEvent, Payload: engineering.WorkflowChanged{SessionID: id, Revision: snapshot.Revision}})
	return nil
}

func (w *ClientWorkspace) WorkflowSnapshot(ctx context.Context, id string) (engineering.WorkflowSnapshot, error) {
	return w.client.WorkflowSnapshot(ctx, w.workspaceID(), id)
}

func (w *ClientWorkspace) WorkflowControl(ctx context.Context, id string, control engineering.WorkflowControl) error {
	return w.client.WorkflowControl(ctx, w.workspaceID(), id, control)
}

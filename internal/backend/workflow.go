package backend

import (
	"context"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/pubsub"
)

func (b *Backend) AuthorizeWorkflow(workspaceID, clientID string) error {
	if _, err := validateClientID(clientID); err != nil {
		return err
	}
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}
	ws.clientsMu.Lock()
	defer ws.clientsMu.Unlock()
	entry, ok := ws.clients[clientID]
	if !ok || entry.streams == 0 {
		return ErrClientNotAttached
	}
	return nil
}

func (b *Backend) WorkflowSnapshot(ctx context.Context, wid, sid string) (engineering.WorkflowSnapshot, error) {
	ws, err := b.GetWorkspace(wid)
	if err != nil {
		return engineering.WorkflowSnapshot{}, err
	}
	if ws.AgentCoordinator == nil {
		return engineering.WorkflowSnapshot{}, ErrAgentNotInitialized
	}
	return ws.AgentCoordinator.WorkflowSnapshot(ctx, sid)
}

func (b *Backend) WorkflowControl(ctx context.Context, wid, sid string, control engineering.WorkflowControl) error {
	ws, err := b.GetWorkspace(wid)
	if err != nil {
		return err
	}
	if ws.AgentCoordinator == nil {
		return ErrAgentNotInitialized
	}
	if err := ws.AgentCoordinator.WorkflowControl(ctx, sid, control); err != nil {
		return err
	}
	snapshot, _ := ws.AgentCoordinator.WorkflowSnapshot(ctx, sid)
	ws.SendEvent(pubsub.Event[engineering.WorkflowChanged]{Type: pubsub.UpdatedEvent, Payload: engineering.WorkflowChanged{SessionID: sid, Revision: snapshot.Revision}})
	return nil
}

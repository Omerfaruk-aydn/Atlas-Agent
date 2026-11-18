package backend

import (
	"context"
	"errors"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/history"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session/replay"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session/rewind"
)

// CreateSession creates a new session in the given workspace.
func (b *Backend) CreateSession(ctx context.Context, workspaceID, title string) (session.Session, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return session.Session{}, err
	}

	return ws.Sessions.Create(ctx, title)
}

// GetSession retrieves a session by workspace and session ID.
func (b *Backend) GetSession(ctx context.Context, workspaceID, sessionID string) (session.Session, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return session.Session{}, err
	}

	return ws.Sessions.Get(ctx, sessionID)
}

// ListSessions returns all sessions in the given workspace.
func (b *Backend) ListSessions(ctx context.Context, workspaceID string) ([]session.Session, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	return ws.Sessions.List(ctx)
}

// SearchSessions returns sessions in the given workspace with at least one
// matching message, best-matching session first.
func (b *Backend) SearchSessions(ctx context.Context, workspaceID, query string) ([]session.Session, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	ids, err := ws.Messages.SearchSessionIDs(ctx, query)
	if err != nil {
		return nil, err
	}
	sessions := make([]session.Session, 0, len(ids))
	for _, id := range ids {
		s, err := ws.Sessions.Get(ctx, id)
		if err != nil {
			continue
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

// GetAgentSession returns session metadata with the agent's busy
// status.
func (b *Backend) GetAgentSession(ctx context.Context, workspaceID, sessionID string) (proto.AgentSession, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.AgentSession{}, err
	}

	se, err := ws.Sessions.Get(ctx, sessionID)
	if err != nil {
		return proto.AgentSession{}, err
	}

	var isSessionBusy bool
	if ws.AgentCoordinator != nil {
		isSessionBusy = ws.AgentCoordinator.IsSessionBusy(sessionID)
	}

	return proto.AgentSession{
		Session: proto.Session{
			ID:    se.ID,
			Title: se.Title,
		},
		IsBusy: isSessionBusy,
	}, nil
}

// ListSessionMessages returns all messages for a session.
func (b *Backend) ListSessionMessages(ctx context.Context, workspaceID, sessionID string) ([]message.Message, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	// Drain debounced updates so HTTP clients (and the TUI on session
	// switch) observe the latest in-memory state rather than racing the
	// debounce timer in message.Service.
	if err := ws.Messages.FlushAll(ctx); err != nil {
		return nil, err
	}
	return ws.Messages.List(ctx, sessionID)
}

// ListSessionHistory returns the history items for a session.
func (b *Backend) ListSessionHistory(ctx context.Context, workspaceID, sessionID string) ([]history.File, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	return ws.History.ListBySession(ctx, sessionID)
}

// SaveSession updates a session in the given workspace.
func (b *Backend) SaveSession(ctx context.Context, workspaceID string, sess session.Session) (session.Session, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return session.Session{}, err
	}

	return ws.Sessions.Save(ctx, sess)
}

// DeleteSession deletes a session from the given workspace.
func (b *Backend) DeleteSession(ctx context.Context, workspaceID, sessionID string) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}

	return ws.Sessions.Delete(ctx, sessionID)
}

// RewindPreviewSession reports how many files rewinding sourceSessionID to
// upToMessageID would write/delete, without applying anything.
func (b *Backend) RewindPreviewSession(ctx context.Context, workspaceID, sourceSessionID, upToMessageID string) (int, int, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return 0, 0, err
	}

	svc := rewind.NewService(ws.Sessions, ws.Messages, ws.History)
	return svc.Preview(ctx, sourceSessionID, upToMessageID)
}

// RewindSession forks sourceSessionID at upToMessageID (inclusive) and
// restores the workspace's files to their state before it.
func (b *Backend) RewindSession(ctx context.Context, workspaceID, sourceSessionID, upToMessageID string) (rewind.Result, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return rewind.Result{}, err
	}

	svc := rewind.NewService(ws.Sessions, ws.Messages, ws.History)
	return svc.ForkAt(ctx, sourceSessionID, upToMessageID)
}

// RewindSessionBefore forks sourceSessionID just before messageID and
// restores the workspace's files to their state before it.
func (b *Backend) RewindSessionBefore(ctx context.Context, workspaceID, sourceSessionID, messageID string) (rewind.Result, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return rewind.Result{}, err
	}

	svc := rewind.NewService(ws.Sessions, ws.Messages, ws.History)
	return svc.ForkBefore(ctx, sourceSessionID, messageID)
}

// ListUserMessages returns user-role messages for a session.
func (b *Backend) ListUserMessages(ctx context.Context, workspaceID, sessionID string) ([]message.Message, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	return ws.Messages.ListUserMessages(ctx, sessionID)
}

// ListAllUserMessages returns all user-role messages across sessions.
func (b *Backend) ListAllUserMessages(ctx context.Context, workspaceID string) ([]message.Message, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	return ws.Messages.ListAllUserMessages(ctx)
}

// RenameSession changes only a session's title, leaving its usage
// counters, todos and timestamps alone.
func (b *Backend) RenameSession(ctx context.Context, workspaceID, sessionID, title string) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}
	return ws.Sessions.Rename(ctx, sessionID, title)
}

// SideChatTagPrefix marks a branch made as a side chat; the rest is the
// full ID of the session it was forked from.
const SideChatTagPrefix = session.SideChatTagPrefix

// BranchSession copies a session's conversation, up to messageID or up to its
// last complete step when messageID is empty, into a new session tagged as a
// side chat of the source. Files are left alone and the source is not changed.
func (b *Backend) BranchSession(ctx context.Context, workspaceID, sourceSessionID, messageID string) (session.Session, error) {
	return b.branchSession(ctx, workspaceID, sourceSessionID, messageID, false)
}

// BranchConversation is BranchSession for a chat of its own: the copy is
// tagged as a branch of its source, not as a side chat.
func (b *Backend) BranchConversation(ctx context.Context, workspaceID, sourceSessionID, messageID string) (session.Session, error) {
	return b.branchSession(ctx, workspaceID, sourceSessionID, messageID, true)
}

func (b *Backend) branchSession(ctx context.Context, workspaceID, sourceSessionID, messageID string, plain bool) (session.Session, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return session.Session{}, err
	}
	if messageID == "" {
		if err := ws.Messages.FlushAll(ctx); err != nil {
			return session.Session{}, err
		}
		all, err := ws.Messages.List(ctx, sourceSessionID)
		if err != nil {
			return session.Session{}, err
		}
		for i := len(all) - 1; i >= 0; i-- {
			if replay.CheckBranchable(all, i) == nil {
				messageID = all[i].ID
				break
			}
		}
		if messageID == "" {
			return session.Session{}, errors.New("the session has nothing to branch from yet")
		}
	}
	tag := SideChatTagPrefix + sourceSessionID
	if plain {
		tag = rewind.BranchTagPrefix + sourceSessionID
	}
	res, err := rewind.NewBrancher(ws.Sessions, ws.Messages, ws.History).Branch(ctx, rewind.BranchOptions{SourceID: sourceSessionID, MessageID: messageID, ExtraTags: []string{tag}})
	if err != nil {
		return session.Session{}, err
	}
	return res.Session, nil
}

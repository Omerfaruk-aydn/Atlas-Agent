package backend

import (
	"errors"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/question"
)

// ErrInvalidPermissionMode is returned when a client asks for a
// permission mode the permission service does not define.
var ErrInvalidPermissionMode = errors.New("invalid permission mode")

// pendingPermissionLister is implemented by the permission service. It is
// asserted rather than added to [permission.Service] so the many test
// doubles of that interface do not all have to grow a method.
type pendingPermissionLister interface {
	PendingRequests() []permission.PermissionRequest
}

// pendingQuestionLister is the question-service counterpart of
// [pendingPermissionLister].
type pendingQuestionLister interface {
	Pending() []question.Request
}

// PendingPermissions returns the permission requests of a workspace that
// still await a decision. A client that reconnects, or opens after the
// request was published, uses it to show the prompt it missed.
func (b *Backend) PendingPermissions(workspaceID string) ([]permission.PermissionRequest, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	if ws.App == nil || ws.Permissions == nil {
		return nil, nil
	}
	lister, ok := ws.Permissions.(pendingPermissionLister)
	if !ok {
		return nil, nil
	}
	return lister.PendingRequests(), nil
}

// PendingQuestions returns the question batches of a workspace that still
// await an answer, oldest first.
func (b *Backend) PendingQuestions(workspaceID string) ([]question.Request, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	if ws.App == nil || ws.Questions == nil {
		return nil, nil
	}
	lister, ok := ws.Questions.(pendingQuestionLister)
	if !ok {
		return nil, nil
	}
	return lister.Pending(), nil
}

// GetPermissionMode returns the workspace's current permission mode.
func (b *Backend) GetPermissionMode(workspaceID string) (permission.PermissionMode, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return "", err
	}
	return ws.Permissions.Mode(), nil
}

// SetPermissionMode switches the workspace's permission mode. The
// permission service applies it to every later request, whichever client
// started the run.
func (b *Backend) SetPermissionMode(workspaceID string, mode permission.PermissionMode) error {
	switch mode {
	case permission.ModeManual, permission.ModeAutoAcceptEdits, permission.ModePlan, permission.ModeBypass:
	default:
		return ErrInvalidPermissionMode
	}
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}
	ws.Permissions.SetMode(mode)
	return nil
}

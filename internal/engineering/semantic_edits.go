package engineering

import (
	"context"
	"fmt"
)

type SemanticEditState struct {
	ID     string `json:"id"`
	Root   string `json:"root"`
	TaskID string `json:"task_id,omitempty"`
	Status string `json:"status"`
	Record Record `json:"record"`
}

func SemanticEditPending(status string) bool {
	switch status {
	case "preview", "applied", "rolled_back", "rejected":
		return false
	default:
		return true
	}
}

// SemanticEditsReady prevents unresolved multi-file mutations from certifying
// task or stage success. Artifact and command recovery do not clear this gate.
func (s *Store) SemanticEditsReady(ctx context.Context, sessionID string) error {
	state, err := s.Read(ctx, sessionID)
	if err != nil {
		return err
	}
	for id, edit := range state.SemanticEdits {
		if SemanticEditPending(edit.Status) {
			return fmt.Errorf("semantic edit %s requires journal inspection and recovery", id)
		}
	}
	return nil
}

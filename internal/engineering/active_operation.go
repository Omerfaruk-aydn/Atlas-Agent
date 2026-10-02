package engineering

import (
	"context"
	"fmt"
	"time"
)

// CheckOperation checks the remaining resource budget of an already admitted
// operation. Its one reserved tool call is not charged a second time.
func (s *Store) CheckOperation(ctx context.Context, sessionID, operationID string) error {
	state, err := s.Read(ctx, sessionID)
	if err != nil {
		return err
	}
	var operation *Operation
	for i := range state.Operations {
		if state.Operations[i].ID == operationID {
			operation = &state.Operations[i]
			break
		}
	}
	if operation == nil || operation.Status != "running" {
		return fmt.Errorf("scenario operation is no longer active")
	}
	accounts := []TaskAccount{{Limits: state.Limits, Usage: state.Usage}}
	if operation.TaskID != "" {
		accounts = append(accounts, state.Tasks[operation.TaskID])
	}
	for _, account := range accounts {
		account.Usage.ToolCalls = max(0, account.Usage.ToolCalls-1)
		account.Usage.DurationMS += max(0, time.Now().UnixMilli()-operation.StartedAt)
		if err := account.Limits.Check(account.Usage); err != nil {
			return err
		}
	}
	return nil
}

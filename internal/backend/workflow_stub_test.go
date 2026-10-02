package backend

import (
	"context"
	"fmt"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

func (*blockingCoordinator) WorkflowSnapshot(context.Context, string) (engineering.WorkflowSnapshot, error) {
	return engineering.WorkflowSnapshot{}, fmt.Errorf("workflow unavailable in run fixture")
}

func (*blockingCoordinator) WorkflowControl(context.Context, string, engineering.WorkflowControl) error {
	return fmt.Errorf("workflow unavailable in run fixture")
}

func (*errorCoordinator) WorkflowSnapshot(context.Context, string) (engineering.WorkflowSnapshot, error) {
	return engineering.WorkflowSnapshot{}, fmt.Errorf("workflow unavailable in run fixture")
}

func (*errorCoordinator) WorkflowControl(context.Context, string, engineering.WorkflowControl) error {
	return fmt.Errorf("workflow unavailable in run fixture")
}

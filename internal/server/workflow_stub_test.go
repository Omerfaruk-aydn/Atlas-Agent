package server

import (
	"context"
	"fmt"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

func (*runCoordinator) WorkflowSnapshot(context.Context, string) (engineering.WorkflowSnapshot, error) {
	return engineering.WorkflowSnapshot{}, fmt.Errorf("workflow unavailable in run fixture")
}

func (*runCoordinator) WorkflowControl(context.Context, string, engineering.WorkflowControl) error {
	return fmt.Errorf("workflow unavailable in run fixture")
}

func (*stubCoordinator) WorkflowSnapshot(context.Context, string) (engineering.WorkflowSnapshot, error) {
	return engineering.WorkflowSnapshot{}, fmt.Errorf("workflow unavailable in run fixture")
}

func (*stubCoordinator) WorkflowControl(context.Context, string, engineering.WorkflowControl) error {
	return fmt.Errorf("workflow unavailable in run fixture")
}

func (*scriptedCoordinator) WorkflowSnapshot(context.Context, string) (engineering.WorkflowSnapshot, error) {
	return engineering.WorkflowSnapshot{}, fmt.Errorf("workflow unavailable in run fixture")
}

func (*scriptedCoordinator) WorkflowControl(context.Context, string, engineering.WorkflowControl) error {
	return fmt.Errorf("workflow unavailable in run fixture")
}

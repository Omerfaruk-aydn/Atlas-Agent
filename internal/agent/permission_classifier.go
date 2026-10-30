package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
)

type smallModelPermissionApprover struct{ worker *sessionAgent }

func (a smallModelPermissionApprover) ClassifyPermission(ctx context.Context, req permission.CreatePermissionRequest) (permission.ApprovalDecision, error) {
	if a.worker == nil {
		return permission.ApprovalDecision{}, errors.New("permission classifier is not initialized")
	}
	model := a.worker.smallModel.Get()
	if model.Model == nil {
		return permission.ApprovalDecision{}, errors.New("small model is unavailable")
	}
	input, err := json.Marshal(req)
	if err != nil {
		return permission.ApprovalDecision{}, err
	}
	system := `Classify the permission request as safe, risky or unknown. Safe means reversible, local, low-impact and consistent with the user's engineering task. Any destructive, privileged, remote-execution, secret-exposure or ambiguous operation is not safe. Return only JSON: {"class":"safe|risky|unknown","reason":"short concrete reason"}.`
	reply, err := a.worker.runReviewer(ctx, req.SessionID, model, system, string(input), nil, nil, 1)
	if err != nil {
		return permission.ApprovalDecision{}, err
	}
	var decision permission.ApprovalDecision
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(reply)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decision); err != nil {
		return permission.ApprovalDecision{}, fmt.Errorf("invalid classifier response: %w", err)
	}
	if decoder.More() {
		return permission.ApprovalDecision{}, errors.New("classifier returned trailing content")
	}
	return decision, nil
}

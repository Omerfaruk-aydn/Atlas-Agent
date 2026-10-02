package engineering

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

type RepairAttempt struct {
	ID                        string   `json:"id"`
	Hypothesis                string   `json:"hypothesis"`
	EvidenceHash              string   `json:"evidence_hash"`
	SourceFingerprint         string   `json:"source_fingerprint"`
	DiagnosticRunIDs          []string `json:"diagnostic_run_ids"`
	ImplementationRunID       string   `json:"implementation_run_id,omitempty"`
	VerificationRunID         string   `json:"verification_run_id,omitempty"`
	ReviewRunID               string   `json:"review_run_id,omitempty"`
	VerifiedSourceFingerprint string   `json:"verified_source_fingerprint,omitempty"`
	ReviewedSourceFingerprint string   `json:"reviewed_source_fingerprint,omitempty"`
	ReviewPassed              bool     `json:"review_passed"`
	PendingCallID             string   `json:"pending_call_id,omitempty"`
	Status                    string   `json:"status"`
}

type RepairCase struct {
	Reason            string          `json:"reason,omitempty"`
	ID                string          `json:"id"`
	TaskID            string          `json:"task_id"`
	TaskFingerprint   string          `json:"task_fingerprint"`
	FailedOperationID string          `json:"failed_operation_id"`
	Status            string          `json:"status"`
	Attempts          []RepairAttempt `json:"attempts"`
}

// RepairState hydrates only referenced observations, retaining bounded runtime
// state while allowing a long implementation to outlive its recent history.
func (s *Store) RepairState(ctx context.Context, session string, state State, repair RepairCase) (State, error) {
	state.Operations = slices.Clone(state.Operations)
	state.Checks = slices.Clone(state.Checks)
	ids := []string{repair.FailedOperationID}
	for _, attempt := range repair.Attempts {
		ids = append(ids, attempt.DiagnosticRunIDs...)
		ids = append(ids, attempt.ImplementationRunID, attempt.VerificationRunID, attempt.ReviewRunID)
	}
	for _, id := range ids {
		if id == "" {
			continue
		}
		op, err := s.OperationObservation(ctx, session, id, state)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return state, err
		}
		if _, ok := findOperation(state, id); !ok {
			state.Operations = append(state.Operations, op)
		}
		if op.Tool == "verify" && op.Status == "completed" && op.OutcomeObserved {
			_, checks, err := s.VerificationObservation(ctx, session, op.CallID, op.TaskID, state)
			if err != nil {
				return state, err
			}
			// Replace only this run's recent subset with its complete journal.
			state.Checks = slices.DeleteFunc(state.Checks, func(c Check) bool { return c.RunID == op.CallID && c.TaskID == op.TaskID })
			state.Checks = append(state.Checks, checks...)
		}
	}
	return state, nil
}

func (s *Store) ValidateRepair(ctx context.Context, session string, state State, repair RepairCase) error {
	state, err := s.RepairState(ctx, session, state, repair)
	if err != nil {
		return err
	}
	return ValidateRepair(ctx, state, repair)
}

// ValidateRepair checks persisted run identities, never model-reported counts.
func ValidateRepair(ctx context.Context, state State, repair RepairCase) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validArtifactHash(repair.ID) || repair.TaskID == "" || !validArtifactHash(repair.TaskFingerprint) || len(repair.Reason) > 2048 || len(repair.Attempts) > 3 {
		return fmt.Errorf("invalid repair identity or attempt limit")
	}
	if repair.Status != "running" && repair.Status != "blocked" && repair.Status != "unresolved" && repair.Status != "resolved" {
		return fmt.Errorf("invalid repair status")
	}
	role, ok := state.RoleExecutions[repair.TaskID]
	if !ok || role.TaskFingerprint != repair.TaskFingerprint {
		return fmt.Errorf("repair task changed or has no registered assignment")
	}
	ops := map[string]Operation{}
	for _, op := range state.Operations {
		ops[op.ID] = op
	}
	failed, ok := ops[repair.FailedOperationID]
	if !ok || failed.Status != "failed" || !failed.OutcomeObserved || failed.TaskID != repair.TaskID || !validArtifactHash(failed.EvidenceHash) || failed.Tool != "bash" && failed.Tool != "test_run" && failed.Tool != "lint_run" && failed.Tool != "verify" {
		return fmt.Errorf("repair requires an actual observed failed operation for this task")
	}
	allowedEvidence := map[string]bool{failed.EvidenceHash: true}
	seenAttempts, seenEvidence, seenRuns := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, attempt := range repair.Attempts {
		if err := ctx.Err(); err != nil {
			return err
		}
		key := attempt.EvidenceHash + ":" + attempt.SourceFingerprint
		if attempt.ID == "" || seenAttempts[attempt.ID] || strings.TrimSpace(attempt.Hypothesis) == "" || len(attempt.Hypothesis) > 4096 || !validArtifactHash(attempt.SourceFingerprint) || !allowedEvidence[attempt.EvidenceHash] || seenEvidence[key] || len(attempt.DiagnosticRunIDs) > 2 {
			return fmt.Errorf("repair attempt exceeds bounds or repeats unchanged source and evidence")
		}
		seenAttempts[attempt.ID], seenEvidence[key] = true, true
		runs := append([]string{}, attempt.DiagnosticRunIDs...)
		runs = append(runs, attempt.ImplementationRunID, attempt.VerificationRunID, attempt.ReviewRunID)
		for _, id := range runs {
			if id == "" {
				continue
			}
			op, ok := ops[id]
			if !ok || op.TaskID != repair.TaskID || seenRuns[id] {
				return fmt.Errorf("repair references an unknown, foreign or reused run")
			}
			seenRuns[id] = true
		}
		for _, id := range attempt.DiagnosticRunIDs {
			if ops[id].Tool != "verify" {
				return fmt.Errorf("repair diagnostic must reference a verification run")
			}
		}
		if attempt.ImplementationRunID != "" && (ops[attempt.ImplementationRunID].Tool != "agent" || ops[attempt.ImplementationRunID].AgentName != "debug") {
			return fmt.Errorf("repair implementation must reference its debug specialist")
		}
		if op, ok := ops[attempt.VerificationRunID]; ok && op.Status == "failed" && op.OutcomeObserved && validArtifactHash(op.EvidenceHash) {
			allowedEvidence[op.EvidenceHash] = true
		}
	}
	if repair.Status != "resolved" {
		return nil
	}
	if len(repair.Attempts) == 0 {
		return fmt.Errorf("repair cannot resolve without observed verification")
	}
	last := repair.Attempts[len(repair.Attempts)-1]
	verify, verified := ops[last.VerificationRunID]
	review, reviewed := ops[last.ReviewRunID]
	if !verified || verify.Tool != "verify" || verify.Status != "completed" || !verify.OutcomeObserved || !reviewed || review.Tool != "agent" || review.AgentName != "review" || review.Status != "completed" || !last.ReviewPassed || !validArtifactHash(last.VerifiedSourceFingerprint) || last.VerifiedSourceFingerprint != last.ReviewedSourceFingerprint || last.PendingCallID != "" {
		return fmt.Errorf("repair resolution lacks matching verification, independent review or source evidence")
	}
	checks := 0
	for _, check := range state.Checks {
		if check.RunID == verify.CallID && check.TaskID == repair.TaskID {
			if !check.Passed || check.Evidence == "" {
				return fmt.Errorf("repair verification contains an invalid check")
			}
			checks++
		}
	}
	if checks == 0 {
		return fmt.Errorf("repair verification has no machine-observed journal checks")
	}
	return nil
}

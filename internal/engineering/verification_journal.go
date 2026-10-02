package engineering

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type verificationObservation struct {
	SessionID string    `json:"session_id"`
	Operation Operation `json:"operation"`
	Checks    []Check   `json:"checks"`
}

func verificationJournalKey(session, call, task string) (string, string) {
	key := Hash(call + "\x00" + task)
	return "verification-" + Hash(session) + "-" + key[:2], key
}

// retainVerification records machine observations before rolling state can
// evict them. Source, specification and declared inputs remain gate checks.
func (s *Store) retainVerification(ctx context.Context, session string, op Operation, checks []Check) error {
	if op.OutcomeObserved || op.Tool == "agent" && op.Status == "completed" {
		key := Hash(op.ID)
		data, err := json.Marshal(op)
		if err != nil {
			return err
		}
		if _, err := s.PutRecord(ctx, "runtime-operations-"+Hash(session)+"-"+key[:2], key, 0, data); err != nil {
			return err
		}
	}
	if op.Tool != "verify" || !op.OutcomeObserved || op.Status != "completed" || op.EvidenceHash == "" {
		return nil
	}
	proof := verificationObservation{SessionID: session, Operation: op}
	ns, key := verificationJournalKey(session, op.CallID, op.TaskID)
	_, partial, partialErr := s.ReadRecord(ctx, ns+"-checks", key)
	if partialErr == nil {
		if err := json.Unmarshal(partial, &proof.Checks); err != nil {
			return err
		}
	} else if !errors.Is(partialErr, os.ErrNotExist) {
		return partialErr
	} else {
		for _, check := range checks {
			if check.RunID == op.CallID && check.TaskID == op.TaskID {
				proof.Checks = append(proof.Checks, check)
			}
		}
	}
	if len(proof.Checks) == 0 {
		return nil
	}
	if len(proof.Checks) > 12 {
		return fmt.Errorf("verification journal check set exceeds bounds")
	}
	data, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	_, err = s.PutRecord(ctx, ns, key, 0, data)
	return err
}

// AppendVerificationCheck retains every observed step before updating the
// bounded recent list, including checks from concurrently running tasks.
func (s *Store) AppendVerificationCheck(ctx context.Context, session string, check Check) error {
	return s.Update(ctx, session, func(st *State) error {
		ns, key := verificationJournalKey(session, check.RunID, check.TaskID)
		record, data, err := s.ReadRecord(ctx, ns+"-checks", key)
		var checks []Check
		if err == nil {
			if err := json.Unmarshal(data, &checks); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if len(checks) >= 12 {
			return fmt.Errorf("verification check set exceeds bounds")
		}
		checks = append(checks, check)
		data, err = json.Marshal(checks)
		if err != nil {
			return err
		}
		if _, err := s.PutRecordStrict(ctx, ns+"-checks", key, record.Revision, data); err != nil {
			return err
		}
		st.Checks = append(st.Checks, check)
		return nil
	})
}

func (s *Store) CompletedAgentOperation(ctx context.Context, session, id string, st State) (Operation, bool) {
	op, err := s.OperationObservation(ctx, session, id, st)
	return op, err == nil && op.Tool == "agent" && op.Status == "completed"
}

func (s *Store) OperationObservation(ctx context.Context, session, id string, st State) (Operation, error) {
	if op, ok := findOperation(st, id); ok {
		return op, nil
	}
	key := Hash(id)
	_, data, err := s.ReadRecord(ctx, "runtime-operations-"+Hash(session)+"-"+key[:2], key)
	if err != nil {
		return Operation{}, err
	}
	var op Operation
	if json.Unmarshal(data, &op) != nil || op.ID != id || op.Tool == "" || op.Status != "completed" && op.Status != "failed" {
		return Operation{}, fmt.Errorf("operation journal identity mismatch")
	}
	return op, nil
}

func (s *Store) VerificationObservation(ctx context.Context, session, call, task string, st State) (Operation, []Check, error) {
	ns, key := verificationJournalKey(session, call, task)
	_, data, err := s.ReadRecord(ctx, ns, key)
	if err == nil {
		var proof verificationObservation
		if err := json.Unmarshal(data, &proof); err != nil {
			return Operation{}, nil, err
		}
		op := proof.Operation
		if proof.SessionID != session || op.CallID != call || op.TaskID != task || op.Tool != "verify" || op.Status != "completed" || !op.OutcomeObserved || !validArtifactHash(op.EvidenceHash) || len(proof.Checks) == 0 || len(proof.Checks) > 12 {
			return Operation{}, nil, fmt.Errorf("verification journal identity mismatch")
		}
		for _, current := range st.Operations {
			if current.CallID == call && current.TaskID == task && (current.ID != op.ID || current.Status != op.Status || current.EvidenceHash != op.EvidenceHash) {
				return Operation{}, nil, fmt.Errorf("verification journal conflicts with current operation")
			}
		}
		for _, check := range proof.Checks {
			if check.RunID != call || check.TaskID != task {
				return Operation{}, nil, fmt.Errorf("verification check scope mismatch")
			}
		}
		return op, proof.Checks, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Operation{}, nil, err
	}
	// Legacy records are valid only while their actual operation and checks
	// remain in the rolling state. Missing history is never invented.
	var op Operation
	var checks []Check
	for _, value := range st.Operations {
		if value.CallID == call && value.TaskID == task && value.Tool == "verify" {
			op = value
		}
	}
	for _, check := range st.Checks {
		if check.RunID == call && check.TaskID == task {
			checks = append(checks, check)
		}
	}
	return op, checks, nil
}

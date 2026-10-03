package agentstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Job struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Root           string `json:"root"`
	SessionID      string `json:"session_id,omitempty"`
	Prompt         string `json:"prompt"`
	EverySeconds   int64  `json:"every_seconds"`
	TimeoutSeconds int64  `json:"timeout_seconds"`
	MaxRuns        int    `json:"max_runs"`
	Runs           int    `json:"runs"`
	NextAt         int64  `json:"next_at"`
	Paused         bool   `json:"paused"`
	Attempt        string `json:"attempt,omitempty"`
	LeaseUntil     int64  `json:"lease_until,omitempty"`
	LastStatus     string `json:"last_status,omitempty"`
	LastResult     string `json:"last_result,omitempty"`
}

func SaveJob(ctx context.Context, s *Store, ns string, j Job, revision int64) error {
	if j.ID == "" || len(j.Prompt) > 16000 || j.Prompt == "" || (j.Kind != "heartbeat" && j.Kind != "cron") || j.EverySeconds < 60 || j.EverySeconds > 2592000 || j.TimeoutSeconds < 1 || j.TimeoutSeconds > 3600 || j.MaxRuns < 1 || j.MaxRuns > 1000 || j.Root == "" || (j.Kind == "heartbeat" && j.SessionID == "") {
		return errors.New("invalid job: prompt, root, kind, interval 60s-30d, timeout 1s-1h and runs 1-1000 required")
	}
	return s.Put(ctx, ns, j.ID, revision, j)
}

func ClaimJob(ctx context.Context, s *Store, ns, id string, now int64) (Job, error) {
	var claimed Job
	err := Update[Job](ctx, s, ns, id, func(j *Job) error {
		if j.ID == "" || j.Paused || j.Runs >= j.MaxRuns || j.NextAt > now || j.LeaseUntil > now {
			return errors.New("job is not due")
		}
		if j.Attempt != "" {
			return errors.New("interrupted job requires explicit recovery")
		}
		j.Attempt = uuid.NewString()
		j.LeaseUntil = now + j.TimeoutSeconds + 30
		j.Runs++
		j.NextAt = now + j.EverySeconds
		j.LastStatus = "running"
		claimed = *j
		return nil
	})
	return claimed, err
}

func FinishJob(ctx context.Context, s *Store, ns string, j Job, result string, runErr error) error {
	return Update[Job](ctx, s, ns, j.ID, func(current *Job) error {
		if current.Attempt != j.Attempt {
			return ErrConflict
		}
		current.Attempt = ""
		current.LeaseUntil = 0
		current.LastStatus = "completed"
		if runErr != nil {
			current.LastStatus = "failed"
			current.Paused = true
		}
		if len(result) > 8000 {
			result = result[:8000]
		}
		current.LastResult = result
		if current.Runs >= current.MaxRuns {
			current.Paused = true
		}
		return nil
	})
}

func Jobs(ctx context.Context, s *Store, ns string) ([]Job, error) {
	rows, err := s.List(ctx, ns)
	if err != nil {
		return nil, err
	}
	out := []Job{}
	for _, r := range rows {
		var j Job
		if err := json.Unmarshal(r.Payload, &j); err != nil {
			return nil, fmt.Errorf("decode job: %w", err)
		}
		out = append(out, j)
	}
	return out, nil
}

func ControlJob(ctx context.Context, s *Store, ns, id, action string) error {
	return Update[Job](ctx, s, ns, id, func(j *Job) error {
		if j.ID == "" {
			return errors.New("job not found")
		}
		switch action {
		case "pause":
			j.Paused = true
		case "resume":
			if j.Attempt != "" {
				return errors.New("recover interrupted run first")
			}
			if j.Runs >= j.MaxRuns {
				return errors.New("run budget exhausted")
			}
			j.Paused = false
			j.NextAt = time.Now().Unix() + j.EverySeconds
		case "clear":
			j.Paused = true
			j.LastStatus = "removed"
		case "recover":
			if j.LeaseUntil > time.Now().Unix() {
				return errors.New("worker lease still active")
			}
			j.Attempt = ""
			j.LeaseUntil = 0
			j.Paused = true
			j.LastStatus = "recovered"
		default:
			return errors.New("unknown job control")
		}
		return nil
	})
}

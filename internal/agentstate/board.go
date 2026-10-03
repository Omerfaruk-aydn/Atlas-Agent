package agentstate

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

type Task struct {
	ID             string            `json:"id"`
	Title          string            `json:"title"`
	Root           string            `json:"root"`
	Prompt         string            `json:"prompt"`
	SessionID      string            `json:"session_id,omitempty"`
	Status         string            `json:"status"`
	Worker         string            `json:"worker,omitempty"`
	Attempt        string            `json:"attempt,omitempty"`
	LeaseUntil     int64             `json:"lease_until,omitempty"`
	Dependencies   []string          `json:"dependencies,omitempty"`
	Acceptance     []string          `json:"acceptance"`
	Evidence       []string          `json:"evidence,omitempty"`
	EvidenceHashes map[string]string `json:"evidence_hashes,omitempty"`
	Result         string            `json:"result,omitempty"`
}

func AddTask(ctx context.Context, s *Store, ns string, t Task) error {
	if t.ID == "" || t.Root == "" || t.Title == "" || len(t.Title) > 256 || t.Prompt == "" || len(t.Prompt) > 16000 || len(t.Acceptance) == 0 || len(t.Acceptance) > 16 || len(t.Dependencies) > 16 {
		return errors.New("task requires bounded title, prompt, workspace and acceptance criteria")
	}
	for _, id := range t.Dependencies {
		var d Task
		_, err := s.Get(ctx, ns, id, &d)
		if err != nil {
			return err
		}
		if id == t.ID || d.ID == "" {
			return errors.New("dependency must be an existing distinct task")
		}
	}
	t.Status = "ready"
	t.Worker = ""
	t.Attempt = ""
	t.LeaseUntil = 0
	t.Evidence = nil
	t.Result = ""
	return s.Put(ctx, ns, t.ID, 0, t)
}

func ClaimTask(ctx context.Context, s *Store, ns, id, worker string, seconds int64) (Task, error) {
	if worker == "" || len(worker) > 128 || seconds < 30 || seconds > 3600 {
		return Task{}, errors.New("worker and lease 30s-1h required")
	}
	var t Task
	_, err := s.Get(ctx, ns, id, &t)
	if err != nil {
		return t, err
	}
	for _, id := range t.Dependencies {
		var d Task
		if _, err := s.Get(ctx, ns, id, &d); err != nil {
			return t, err
		}
		if d.Status != "done" {
			return t, errors.New("dependency not done")
		}
	}
	var claimed Task
	err = Update[Task](ctx, s, ns, id, func(t *Task) error {
		if t.ID == "" || t.Status != "ready" {
			return errors.New("task is not ready")
		}
		t.Status = "running"
		t.Worker = worker
		t.Attempt = uuid.NewString()
		t.LeaseUntil = time.Now().Unix() + seconds
		claimed = *t
		return nil
	})
	return claimed, err
}

func TransitionTask(ctx context.Context, s *Store, ns, id, action, attempt string, evidence []string, result string) error {
	if len(result) > 8000 || len(evidence) > 16 {
		return errors.New("task result exceeds bounds")
	}
	return Update[Task](ctx, s, ns, id, func(t *Task) error {
		if t.ID == "" {
			return errors.New("task not found")
		}
		switch action {
		case "renew":
			if t.Status != "running" || t.Attempt != attempt || t.LeaseUntil <= time.Now().Unix() {
				return errors.New("worker lease is stale")
			}
			t.LeaseUntil = time.Now().Unix() + 300
		case "submit", "block":
			if t.Status != "running" || t.Attempt != attempt || t.LeaseUntil <= time.Now().Unix() {
				return errors.New("worker lease is stale")
			}
			if action == "submit" && len(evidence) == 0 {
				return errors.New("submit requires evidence files")
			}
			if action == "submit" {
				t.EvidenceHashes = map[string]string{}
				for _, path := range evidence {
					data, err := ReadSource(t.Root, path)
					if err != nil {
						return err
					}
					t.EvidenceHashes[path] = Fingerprint(data)
				}
			}
			t.Status = "review"
			if action == "block" {
				t.Status = "blocked"
			}
			t.Evidence = evidence
			t.Result = result
			t.LeaseUntil = 0
		case "accept":
			if t.Status != "review" || len(t.Evidence) == 0 {
				return errors.New("only evidenced review tasks can be accepted")
			}
			for _, path := range t.Evidence {
				data, err := ReadSource(t.Root, path)
				if err != nil || Fingerprint(data) != t.EvidenceHashes[path] {
					return errors.New("review evidence changed; resubmit before acceptance")
				}
			}
			t.Status = "done"
		case "retry":
			if t.Status != "review" && t.Status != "blocked" {
				return errors.New("retry requires review or blocked task")
			}
			t.Status = "ready"
			t.Attempt = ""
			t.Worker = ""
			t.Evidence = nil
		case "recover":
			if t.Status != "running" || t.LeaseUntil > time.Now().Unix() {
				return errors.New("task has no expired worker lease")
			}
			t.Status = "blocked"
			t.Result = "Worker lease expired; inspect changes before retry."
			t.LeaseUntil = 0
		default:
			return errors.New("unknown task transition")
		}
		return nil
	})
}

func Tasks(ctx context.Context, s *Store, ns string) ([]Task, error) {
	rows, err := s.List(ctx, ns)
	if err != nil {
		return nil, err
	}
	out := []Task{}
	for _, r := range rows {
		var t Task
		if err := json.Unmarshal(r.Payload, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

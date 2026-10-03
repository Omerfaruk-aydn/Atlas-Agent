package engineering

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

type LiveRunner struct {
	SessionID string `json:"session_id"`
	TaskID    string `json:"task_id"`
	Title     string `json:"title"`
	StartedAt int64  `json:"started_at"`
}

type (
	runnerBinding struct {
		info   LiveRunner
		cancel context.CancelFunc
	}
	runnerRegistry struct {
		mu      sync.Mutex
		entries map[string]map[string]runnerBinding
	}
)

func (s *Store) RegisterRunner(ctx context.Context, parent string, runner LiveRunner, cancel context.CancelFunc) (func(), error) {
	return s.RegisterRunnerLimit(ctx, parent, runner, cancel, 16)
}

// RegisterRunnerLimit applies a shared cap across tools and nested specialists.
func (s *Store) RegisterRunnerLimit(ctx context.Context, parent string, runner LiveRunner, cancel context.CancelFunc, configured int) (func(), error) {
	if parent == "" || runner.SessionID == "" || runner.TaskID == "" || cancel == nil {
		return nil, fmt.Errorf("runner requires session, task and cancellation")
	}
	release, err := s.WorkflowLock(ctx, parent)
	if err != nil {
		return nil, err
	}
	defer release()
	state, err := s.Read(ctx, parent)
	if err != nil {
		return nil, err
	}
	if state.Paused {
		return nil, fmt.Errorf("workflow dispatch is paused")
	}
	if err := s.Check(ctx, parent, runner.TaskID); err != nil {
		return nil, err
	}
	_, board, err := s.ReadControlBoard(ctx, parent)
	if err != nil {
		return nil, err
	}
	if slices.Contains(board.HeldTasks, runner.TaskID) {
		return nil, fmt.Errorf("task dispatch is paused")
	}
	s.runners.mu.Lock()
	defer s.runners.mu.Unlock()
	if s.runners.entries == nil {
		s.runners.entries = map[string]map[string]runnerBinding{}
	}
	if s.runners.entries[parent] == nil {
		s.runners.entries[parent] = map[string]runnerBinding{}
	}
	entries := s.runners.entries[parent]
	limit := 16
	if configured > 0 {
		limit = min(limit, configured)
	}
	if board.MaxAgents > 0 {
		limit = min(limit, board.MaxAgents)
	}
	if len(entries) >= limit {
		return nil, fmt.Errorf("session agent limit reached")
	}
	if _, exists := entries[runner.SessionID]; exists {
		return nil, fmt.Errorf("runner already registered")
	}
	runner.StartedAt = time.Now().UnixMilli()
	entries[runner.SessionID] = runnerBinding{info: runner, cancel: cancel}
	var once sync.Once
	return func() {
		once.Do(func() {
			s.runners.mu.Lock()
			defer s.runners.mu.Unlock()
			delete(s.runners.entries[parent], runner.SessionID)
			if len(s.runners.entries[parent]) == 0 {
				delete(s.runners.entries, parent)
			}
		})
	}, nil
}

func (s *Store) LiveRunners(parent string) []LiveRunner {
	s.runners.mu.Lock()
	defer s.runners.mu.Unlock()
	var out []LiveRunner
	for _, runner := range s.runners.entries[parent] {
		out = append(out, runner.info)
	}
	slices.SortFunc(out, func(a, b LiveRunner) int { return strings.Compare(a.SessionID, b.SessionID) })
	return out
}

func (s *Store) CancelTask(parent, task string) bool {
	s.runners.mu.Lock()
	var cancels []context.CancelFunc
	for _, runner := range s.runners.entries[parent] {
		if runner.info.TaskID == task {
			cancels = append(cancels, runner.cancel)
		}
	}
	s.runners.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	return len(cancels) > 0
}

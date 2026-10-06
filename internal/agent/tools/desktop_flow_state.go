package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lock"
)

type desktopFlowRecord struct {
	ID   string `json:"id"`
	Next string `json:"next,omitempty"`
}

// Journals contain fingerprints and status, never text, images or window IDs.
type desktopFlowState struct {
	Version   int                 `json:"version"`
	PlanHash  string              `json:"plan_hash"`
	Next      string              `json:"next,omitempty"`
	Pending   string              `json:"pending,omitempty"`
	Completed []desktopFlowRecord `json:"completed"`
	Attempts  map[string]int      `json:"attempts"`
}

func openDesktopFlow(ctx context.Context, f DesktopFlowParams) (*desktopFlowState, func(*desktopFlowState) error, func(), error) {
	data, err := json.Marshal(f.Nodes)
	if err != nil || len(data) > 128*1024 {
		return nil, nil, nil, fmt.Errorf("flow plan exceeds 128 KiB")
	}
	s := &desktopFlowState{Version: 1, PlanHash: engineering.Hash(string(data)), Next: f.Nodes[0].ID, Attempts: map[string]int{}}
	noop := func() {}
	if f.RunID == "" {
		return s, func(*desktopFlowState) error { return nil }, noop, nil
	}
	dir, _ := ctx.Value(desktopFlowStoreKey{}).(string)
	session := desktopFlowSession(ctx)
	if dir == "" || session == "" {
		return nil, nil, nil, fmt.Errorf("durable flow requires configured storage and session identity")
	}
	dir = filepath.Join(dir, "desktop-runs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, nil, err
	}
	path := filepath.Join(dir, engineering.Hash(session+"\x00"+f.RunID)+".json")
	release, err := lock.TryFile(path + ".lock")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("flow run is unavailable or active: %w", err)
	}
	fail := func(err error) (*desktopFlowState, func(*desktopFlowState) error, func(), error) {
		release()
		return nil, nil, nil, err
	}
	file, readErr := os.Open(path)
	if readErr == nil {
		defer file.Close()
		if !f.Resume {
			return fail(fmt.Errorf("run_exists: use resume:true or a new run_id; completed work cannot be replayed"))
		}
		stored, err := io.ReadAll(io.LimitReader(file, 64*1024+1))
		var old desktopFlowState
		if err != nil || len(stored) > 64*1024 || json.Unmarshal(stored, &old) != nil || old.Version != 1 || old.PlanHash != s.PlanHash || old.Attempts == nil {
			return fail(fmt.Errorf("invalid_journal: damaged or changed flow plan; refusing replay"))
		}
		if err := validateDesktopFlowState(f, old); err != nil {
			return fail(err)
		}
		s = &old
	} else if !os.IsNotExist(readErr) {
		return fail(readErr)
	} else if f.Resume {
		return fail(fmt.Errorf("run_missing: no durable progress for this session and run_id"))
	}
	save := func(state *desktopFlowState) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		encoded, err := json.Marshal(state)
		if err != nil {
			return err
		}
		tmp, err := os.CreateTemp(dir, ".flow-*")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		if err = tmp.Chmod(0o600); err == nil {
			_, err = tmp.Write(encoded)
		}
		if err == nil {
			err = tmp.Sync()
		}
		closeErr := tmp.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		return os.Rename(tmp.Name(), path)
	}
	if !f.Resume {
		if err := save(s); err != nil {
			return fail(err)
		}
	}
	return s, save, release, nil
}

func validateDesktopFlowState(f DesktopFlowParams, s desktopFlowState) error {
	nodes := map[string]DesktopFlowNode{}
	for _, n := range f.Nodes {
		nodes[n.ID] = n
	}
	next := f.Nodes[0].ID
	if len(s.Completed) > len(f.Nodes) {
		return fmt.Errorf("invalid_journal: too many completed nodes")
	}
	for _, r := range s.Completed {
		n, ok := nodes[r.ID]
		if !ok || next == "" || r.ID != next || (n.Kind == "branch" && r.Next != n.Then && r.Next != n.Else) || (n.Kind != "branch" && r.Next != n.Next) {
			return fmt.Errorf("invalid_journal: completion path does not match graph")
		}
		next = r.Next
	}
	if next != s.Next || (s.Pending != "" && (s.Pending != next || !desktopFlowEffect(nodes[next].Kind))) {
		return fmt.Errorf("invalid_journal: invalid resume cursor")
	}
	for id, attempts := range s.Attempts {
		if !desktopFlowEffect(nodes[id].Kind) || attempts < 1 || attempts > 2 {
			return fmt.Errorf("invalid_journal: invalid attempt record")
		}
	}
	if s.Pending != "" && s.Attempts[s.Pending] == 0 {
		return fmt.Errorf("invalid_journal: pending intent lacks attempt record")
	}
	return nil
}

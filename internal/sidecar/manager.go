package sidecar

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Status of a supervised sidecar.
type Status string

const (
	StatusStarting Status = "STARTING"
	StatusRunning  Status = "RUNNING"
	StatusStopped  Status = "STOPPED"
	StatusErrored  Status = "ERRORED"
	StatusBackoff  Status = "BACKOFF"
)

// Info is a sidecar's current state.
type Info struct {
	ID       string `json:"id"`
	Status   Status `json:"status"`
	Restarts int    `json:"restarts"`
	LastErr  string `json:"last_error,omitempty"`
}

// Manager supervises the enabled sidecars of one project.
type Manager struct {
	Project string
	// Exe is the agent executable handed to sidecars as ATLAS_AGENTAPI_EXE.
	Exe string
	// Poll is how often the folder is rescanned (default 2s).
	Poll time.Duration
	// BackoffBase and BackoffMax bound the wait between restarts (1s, 30s).
	BackoffBase, BackoffMax time.Duration
	// StableAfter is how long a process must stay up to reset the backoff.
	StableAfter time.Duration

	mu      sync.Mutex
	running map[string]*supervised
	info    map[string]*Info
}

type supervised struct {
	version string
	cancel  context.CancelFunc
	done    chan struct{}
}

func (m *Manager) defaults() {
	if m.Poll <= 0 {
		m.Poll = 2 * time.Second
	}
	if m.BackoffBase <= 0 {
		m.BackoffBase = time.Second
	}
	if m.BackoffMax <= 0 {
		m.BackoffMax = 30 * time.Second
	}
	if m.StableAfter <= 0 {
		m.StableAfter = time.Minute
	}
	if m.running == nil {
		m.running = map[string]*supervised{}
		m.info = map[string]*Info{}
	}
}

// Status lists every known sidecar's state, by id.
func (m *Manager) Status() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Info, 0, len(m.info))
	for _, i := range m.info {
		out = append(out, *i)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}

func (m *Manager) set(id string, fn func(*Info)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.info[id]
	if i == nil {
		i = &Info{ID: id}
		m.info[id] = i
	}
	fn(i)
}

// Run supervises sidecars until ctx ends. It rescans the folder every Poll:
// a new or enabled sidecar starts, an edited one restarts, a disabled or
// deleted one stops. It returns after every child has exited.
func (m *Manager) Run(ctx context.Context) error {
	m.mu.Lock()
	m.defaults()
	m.mu.Unlock()
	ticker := time.NewTicker(m.Poll)
	defer ticker.Stop()
	for {
		m.reconcile(ctx)
		select {
		case <-ctx.Done():
			m.stopAll()
			return nil
		case <-ticker.C:
		}
	}
}

func (m *Manager) reconcile(ctx context.Context) {
	found, _ := Discover(Dir(m.Project))
	want := map[string]Sidecar{}
	for _, s := range found {
		if s.Enabled {
			want[s.ID] = s
		}
	}
	m.mu.Lock()
	var stop []*supervised
	for id, sup := range m.running {
		if w, ok := want[id]; !ok || w.Version != sup.version {
			stop = append(stop, sup)
			delete(m.running, id)
		}
	}
	m.mu.Unlock()
	for _, sup := range stop {
		sup.cancel()
		<-sup.done
	}
	for id, s := range want {
		m.mu.Lock()
		_, already := m.running[id]
		m.mu.Unlock()
		if already {
			continue
		}
		child, cancel := context.WithCancel(ctx)
		sup := &supervised{version: s.Version, cancel: cancel, done: make(chan struct{})}
		m.mu.Lock()
		m.running[id] = sup
		m.mu.Unlock()
		go func(s Sidecar) {
			defer close(sup.done)
			m.supervise(child, s)
		}(s)
	}
	// A sidecar that is no longer wanted is reported stopped.
	m.mu.Lock()
	for id, i := range m.info {
		if _, ok := m.running[id]; !ok && i.Status != StatusStopped {
			i.Status = StatusStopped
		}
	}
	m.mu.Unlock()
}

func (m *Manager) stopAll() {
	m.mu.Lock()
	all := make([]*supervised, 0, len(m.running))
	for _, s := range m.running {
		all = append(all, s)
	}
	m.running = map[string]*supervised{}
	m.mu.Unlock()
	for _, s := range all {
		s.cancel()
	}
	for _, s := range all {
		<-s.done
	}
}

func token() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Env is what a sidecar process is given on top of the user's environment.
func (m *Manager) Env(s Sidecar) []string {
	rt := RuntimeDir(m.Project, s.ID)
	env := []string{
		"ATLAS_SIDECAR_ID=" + s.ID,
		"ATLAS_SIDECAR_DATA_DIR=" + filepath.Join(rt, "data"),
		"ATLAS_SIDECAR_EVENTS_DIR=" + filepath.Join(rt, "events"),
		"ATLAS_SIDECAR_TOKEN=" + token(),
		"ATLAS_PROJECT_DIR=" + m.Project,
		"ATLAS_AGENTAPI_EXE=" + m.Exe,
	}
	for k, v := range s.Def.Env {
		env = append(env, k+"="+v)
	}
	return env
}

// supervise runs one sidecar, restarting it per its policy.
func (m *Manager) supervise(ctx context.Context, s Sidecar) {
	rt := RuntimeDir(m.Project, s.ID)
	for _, d := range []string{"data", "logs", "events"} {
		_ = os.MkdirAll(filepath.Join(rt, d), 0o755)
	}
	delay := m.BackoffBase
	restarts := 0
	for {
		m.set(s.ID, func(i *Info) { i.Status = StatusStarting; i.Restarts = restarts })
		started := time.Now()
		err := m.runOnce(ctx, s, rt)
		if ctx.Err() != nil {
			m.set(s.ID, func(i *Info) { i.Status = StatusStopped })
			return
		}
		failed := err != nil
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		switch s.Def.Policy() {
		case RestartNever:
			m.set(s.ID, func(i *Info) {
				i.Status, i.LastErr = StatusStopped, msg
				if failed {
					i.Status = StatusErrored
				}
			})
			return
		case RestartOnFailure:
			if !failed {
				m.set(s.ID, func(i *Info) { i.Status, i.LastErr = StatusStopped, "" })
				return
			}
		}
		if time.Since(started) >= m.StableAfter {
			delay = m.BackoffBase
		}
		m.set(s.ID, func(i *Info) { i.Status, i.LastErr = StatusBackoff, msg })
		select {
		case <-ctx.Done():
			m.set(s.ID, func(i *Info) { i.Status = StatusStopped })
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, m.BackoffMax)
		restarts++
	}
}

func (m *Manager) runOnce(ctx context.Context, s Sidecar, rt string) error {
	stamp := time.Now().UTC().Format("20060102-150405")
	logFile, err := os.Create(filepath.Join(rt, "logs", stamp+".log"))
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.CommandContext(ctx, s.Def.Command, s.Def.Args...)
	cmd.Dir = s.Dir
	cmd.Env = append(os.Environ(), m.Env(s)...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	m.set(s.ID, func(i *Info) { i.Status = StatusRunning })
	return cmd.Wait()
}

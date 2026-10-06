// Package interaction coordinates browser and desktop ownership and handoffs.
package interaction

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
)

// Entry contains execution metadata only, never typed text or credentials.
type Entry struct {
	OwnerID    string    `json:"owner_id,omitempty"`
	Before     string    `json:"before,omitempty"`
	After      string    `json:"after,omitempty"`
	Time       time.Time `json:"time"`
	Resource   string    `json:"resource"`
	Action     string    `json:"action"`
	Status     string    `json:"status"`
	DurationMS int64     `json:"duration_ms"`
	Target     string    `json:"target,omitempty"`
	Role       string    `json:"role,omitempty"`
	Name       string    `json:"name,omitempty"`
	URL        string    `json:"url,omitempty"`
	Condition  string    `json:"condition,omitempty"`
}

// State describes current control and the bounded history for one chat.
type State struct {
	ControlRevision uint64   `json:"control_revision"`
	LivePreview     bool     `json:"live_preview"`
	RecordImages    bool     `json:"record_images"`
	Preview         *Preview `json:"preview,omitempty"`
	LastOwner       string   `json:"last_owner,omitempty"`
	LastResource    string   `json:"last_resource,omitempty"`
	Paused          bool     `json:"paused"`
	Reason          string   `json:"reason,omitempty"`
	Active          string   `json:"active,omitempty"`
	History         []Entry  `json:"history"`
}

// Controller serializes a shared desktop and each session's browser.
type Controller struct {
	mu     sync.Mutex
	states map[string]State
	leases map[string]chan struct{}
	loaded map[string]bool
	active map[string]map[string]int
}

var Default = New()

// New constructs an independent controller.
func New() *Controller {
	return &Controller{states: map[string]State{}, leases: map[string]chan struct{}{}, loaded: map[string]bool{}, active: map[string]map[string]int{}}
}

// Acquire waits cancellably, then checks handoff state while holding the lease.
// The release must happen when the driver finishes, including after a timeout.
func (c *Controller) Acquire(ctx context.Context, id, resource string, observe ...bool) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := resource
	if resource != "desktop" && !strings.HasPrefix(resource, "shared/") {
		key += ":" + id
	}
	c.mu.Lock()
	lease := c.leases[key]
	if lease == nil {
		lease = make(chan struct{}, 1)
		c.leases[key] = lease
	}
	c.mu.Unlock()
	observation := len(observe) > 0 && observe[0]
	shared := resource == "desktop" || strings.HasPrefix(resource, "shared/")
	for {
		// New input and observation wait while a run awaits the user's
		// answer, so no capture can include the island. The wait happens
		// before the lease so the user's own input is never held.
		if err := activity.WaitForPrompts(ctx, shared); err != nil {
			return nil, err
		}
		select {
		case lease <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if err := ctx.Err(); err != nil {
			<-lease
			return nil, err
		}
		if !activity.PromptBlocks(ctx, shared) {
			break
		}
		// A request appeared while the lease was contended.
		<-lease
	}
	c.mu.Lock()
	s := c.states[id]
	if !observation && (resource == "desktop" || strings.HasPrefix(resource, "shared/")) {
		for other, state := range c.states {
			if other != id && state.Paused {
				c.mu.Unlock()
				<-lease
				return nil, fmt.Errorf("interaction_paused: another session has handed control to the user")
			}
		}
	}
	if s.Paused && !observation {
		c.mu.Unlock()
		<-lease
		return nil, fmt.Errorf("interaction_paused: %s; user must resume control", s.Reason)
	}
	if c.active[id] == nil {
		c.active[id] = map[string]int{}
	}
	c.active[id][resource]++
	s.Active = c.activeResources(id)
	c.states[id] = s
	c.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			s := c.states[id]
			c.active[id][resource]--
			if c.active[id][resource] == 0 {
				delete(c.active[id], resource)
			}
			s.Active = c.activeResources(id)
			c.states[id] = s
			c.mu.Unlock()
			<-lease
		})
	}, nil
}

// activeResources is called with the controller lock held.
func (c *Controller) activeResources(id string) string {
	var resources []string
	for resource := range c.active[id] {
		resources = append(resources, resource)
	}
	sort.Strings(resources)
	return strings.Join(resources, ", ")
}

// Pause prevents subsequent actions; an in-flight action may still finish.
func (c *Controller) Pause(id, reason string) {
	c.mu.Lock()
	s := c.states[id]
	s.Paused = true
	s.ControlRevision++
	s.Reason = reason
	c.states[id] = s
	c.mu.Unlock()
	activity.ClearSession(id)
}

// StartActivity invalidates visual starts that race with a control handoff.
// The callback runs outside the controller lock so pause remains responsive.
func (c *Controller) StartActivity(id string, begin func() func()) func() {
	before := c.Snapshot(id)
	if before.Paused {
		return func() {}
	}
	finish := begin()
	after := c.Snapshot(id)
	if after.Paused || before.ControlRevision != after.ControlRevision {
		activity.ClearSession(id)
	}
	return finish
}

// Resume returns control to automation after the user's explicit action.
func (c *Controller) Resume(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.states[id]
	s.Paused = false
	s.ControlRevision++
	s.Reason = ""
	c.states[id] = s
}

// Snapshot returns a copy that callers cannot mutate.
func (c *Controller) Snapshot(id string) State {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.states[id]
	s.History = append([]Entry(nil), s.History...)
	if s.Preview != nil {
		preview := *s.Preview
		preview.Pixels = append([]uint32(nil), preview.Pixels...)
		s.Preview = &preview
	}
	return s
}

// ToolSnapshot omits image pixels and bounds routine status to one recent entry.
func (c *Controller) ToolSnapshot(id string, history bool) State {
	s := c.Snapshot(id)
	if s.Preview != nil {
		s.Preview.Pixels = nil
	}
	if !history && len(s.History) > 1 {
		s.History = s.History[len(s.History)-1:]
	}
	return s
}

// Record retains bounded metadata and persists it under the workspace.
func (c *Controller) Record(root, id string, entry Entry) error {
	if entry.Time.IsZero() {
		entry.Time = time.Now()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.states[id]
	s.History = append(s.History, entry)
	if entry.OwnerID != "" {
		s.LastOwner = entry.OwnerID
		s.LastResource = entry.Resource
	}
	if len(s.History) > 200 {
		s.History = s.History[len(s.History)-200:]
	}
	c.states[id] = s
	if root == "" {
		return nil
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if len(data) > 4*1024*1024 {
		return fmt.Errorf("interaction trace exceeds persistence limit")
	}
	c.loaded[root+"\x00"+id] = true
	return saveState(root, id, data)
}

// SetImageRecording enables explicit before/after capture for non-secret actions.
func (c *Controller) SetImageRecording(id string, enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.states[id]
	s.RecordImages = enabled
	s.ControlRevision++
	c.states[id] = s
}

// Load restores redacted history and a paused handoff after a process restart.
func (c *Controller) Load(root, id string) error {
	if root == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key := root + "\x00" + id
	if c.loaded[key] {
		return nil
	}
	fs, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer fs.Close()
	f, err := fs.Open(sessionPath(id))
	if os.IsNotExist(err) {
		c.loaded[key] = true
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 4*1024*1024+1))
	if err != nil {
		return err
	}
	if len(data) > 4*1024*1024 {
		return fmt.Errorf("interaction trace exceeds load limit")
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	s.Active = ""
	if len(s.History) > 200 {
		s.History = s.History[len(s.History)-200:]
	}
	c.states[id] = s
	c.loaded[key] = true
	return nil
}

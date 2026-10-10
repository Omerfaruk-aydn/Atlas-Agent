package nodes

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
)

// MaxPlanSteps bounds one cross-device plan.
const MaxPlanSteps = 16

// PlanStep is one action on one node. Fields are flat so every provider
// can produce them reliably.
type PlanStep struct {
	ID         string   `json:"id"`
	Node       string   `json:"node"`
	Action     string   `json:"action"`
	Argv       []string `json:"argv,omitempty"`
	Cwd        string   `json:"cwd,omitempty"`
	TimeoutMS  int64    `json:"timeout_ms,omitempty"`
	LocalPath  string   `json:"local_path,omitempty"`
	RemotePath string   `json:"remote_path,omitempty"`
	Overwrite  bool     `json:"overwrite,omitempty"`
	DependsOn  []string `json:"depends_on,omitempty"`
}

// StepResult carries the source identity of every result, so outputs,
// paths and exit codes from different machines are never confused.
type StepResult struct {
	ID          string          `json:"id"`
	Node        string          `json:"node"`
	NodeID      string          `json:"node_id,omitempty"`
	OS          string          `json:"os,omitempty"`
	Action      string          `json:"action"`
	OperationID string          `json:"operation_id,omitempty"`
	State       taskstate.State `json:"state"`
	Effect      string          `json:"effect"`
	ExitCode    *int            `json:"exit_code,omitempty"`
	Stdout      string          `json:"stdout,omitempty"`
	Stderr      string          `json:"stderr,omitempty"`
	Path        string          `json:"path,omitempty"`
	SHA256      string          `json:"sha256,omitempty"`
	Error       string          `json:"error,omitempty"`
	ErrorCode   string          `json:"error_code,omitempty"`
	Replayed    bool            `json:"replayed,omitempty"`
	StartedAt   int64           `json:"started_at,omitempty"`
	EndedAt     int64           `json:"ended_at,omitempty"`
}

// PlanResult summarizes a plan.
type PlanResult struct {
	State taskstate.State `json:"state"`
	// Partial is true when some steps completed and others did not.
	Partial bool         `json:"partial"`
	Steps   []StepResult `json:"steps"`
}

// StepRunner executes one step. It must honour ctx cancellation by asking
// the node to cancel and reporting the node's final state.
type StepRunner func(ctx context.Context, step PlanStep) StepResult

var stepID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// ValidatePlan checks ids, references, cycles and per-action fields.
func ValidatePlan(steps []PlanStep) error {
	if len(steps) == 0 || len(steps) > MaxPlanSteps {
		return fmt.Errorf("plan needs 1-%d steps", MaxPlanSteps)
	}
	ids := map[string]bool{}
	for _, s := range steps {
		if !stepID.MatchString(s.ID) || ids[s.ID] {
			return fmt.Errorf("step id %q must be unique, 1-32 lowercase letters, digits, '_' or '-'", s.ID)
		}
		ids[s.ID] = true
		if s.Node != "local" && !ValidName(s.Node) {
			return fmt.Errorf("step %s: node %q is not a paired node name or \"local\"", s.ID, s.Node)
		}
		switch s.Action {
		case "exec":
			if len(s.Argv) == 0 || s.LocalPath != "" || s.RemotePath != "" {
				return fmt.Errorf("step %s: exec needs argv and no file paths", s.ID)
			}
		case "push", "pull":
			if s.Node == "local" {
				return fmt.Errorf("step %s: %s moves files between this computer and a node; node cannot be local", s.ID, s.Action)
			}
			if s.LocalPath == "" || s.RemotePath == "" || len(s.Argv) != 0 {
				return fmt.Errorf("step %s: %s needs local_path and remote_path and no argv", s.ID, s.Action)
			}
		default:
			return fmt.Errorf("step %s: action must be exec, push or pull", s.ID)
		}
	}
	for _, s := range steps {
		for _, d := range s.DependsOn {
			if !ids[d] || d == s.ID {
				return fmt.Errorf("step %s depends on unknown step %q", s.ID, d)
			}
		}
	}
	// Kahn's algorithm detects cycles.
	indeg := map[string]int{}
	for _, s := range steps {
		indeg[s.ID] += 0
		for range s.DependsOn {
			indeg[s.ID]++
		}
	}
	var queue []string
	for _, s := range steps {
		if indeg[s.ID] == 0 {
			queue = append(queue, s.ID)
		}
	}
	seen := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		seen++
		for _, s := range steps {
			if slices.Contains(s.DependsOn, id) {
				indeg[s.ID]--
				if indeg[s.ID] == 0 {
					queue = append(queue, s.ID)
				}
			}
		}
	}
	if seen != len(steps) {
		return fmt.Errorf("plan dependencies contain a cycle")
	}
	return nil
}

// RunPlan executes steps: a step starts once all its dependencies
// completed; a step whose dependency did not complete is canceled with the
// reason; independent steps run in parallel (at most parallel at a time).
// Cancellation of ctx cancels running steps through the runner and marks
// pending steps canceled. onUpdate, when set, sees every state change.
func RunPlan(ctx context.Context, steps []PlanStep, parallel int, run StepRunner, onUpdate func(StepResult)) PlanResult {
	if parallel <= 0 {
		parallel = 4
	}
	results := make(map[string]*StepResult, len(steps))
	for _, s := range steps {
		results[s.ID] = &StepResult{ID: s.ID, Node: s.Node, Action: s.Action, State: taskstate.Queued, Effect: "none"}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, parallel)
	started := map[string]bool{}
	changed := make(chan struct{}, len(steps)+1)
	notify := func(r StepResult) {
		if onUpdate != nil {
			onUpdate(r)
		}
	}
	for {
		mu.Lock()
		progress := false
		pending := 0
		for _, s := range steps {
			r := results[s.ID]
			if started[s.ID] || r.State.Terminal() {
				continue
			}
			pending++
			ready, blocked := true, ""
			for _, d := range s.DependsOn {
				dr := results[d]
				switch {
				case dr.State == taskstate.Completed:
				case dr.State.Terminal():
					blocked = d
				default:
					ready = false
				}
			}
			switch {
			case ctx.Err() != nil:
				r.State, r.Error = taskstate.Canceled, "plan canceled before this step started"
				notify(*r)
				progress = true
			case blocked != "":
				r.State, r.Error = taskstate.Canceled, "dependency "+blocked+" did not complete; step not started"
				notify(*r)
				progress = true
			case ready:
				started[s.ID] = true
				r.State = taskstate.Running
				r.StartedAt = time.Now().UnixMilli()
				notify(*r)
				progress = true
				wg.Add(1)
				go func(step PlanStep) {
					defer wg.Done()
					sem <- struct{}{}
					res := run(ctx, step)
					<-sem
					mu.Lock()
					cur := results[step.ID]
					res.ID, res.Node, res.Action = step.ID, step.Node, step.Action
					if res.StartedAt == 0 {
						res.StartedAt = cur.StartedAt
					}
					if res.EndedAt == 0 {
						res.EndedAt = time.Now().UnixMilli()
					}
					if !res.State.Terminal() {
						res.State = taskstate.Failed
						if res.Error == "" {
							res.Error = "step ended without a terminal state"
						}
					}
					*cur = res
					mu.Unlock()
					notify(res)
					changed <- struct{}{}
				}(s)
			}
		}
		running := len(started) > 0 && slices.ContainsFunc(steps, func(s PlanStep) bool { return started[s.ID] && !results[s.ID].State.Terminal() })
		mu.Unlock()
		if pending == 0 && !running {
			break
		}
		if !progress {
			select {
			case <-changed:
			case <-ctx.Done():
				// Loop once more to mark pending steps canceled; running steps
				// report through their runner.
				select {
				case <-changed:
				case <-time.After(50 * time.Millisecond):
				}
			}
		}
	}
	wg.Wait()
	out := PlanResult{State: taskstate.Completed}
	completed, other := 0, 0
	for _, s := range steps {
		r := *results[s.ID]
		out.Steps = append(out.Steps, r)
		if r.State == taskstate.Completed {
			completed++
			continue
		}
		other++
		if r.State == taskstate.Canceled && out.State != taskstate.Failed {
			out.State = taskstate.Canceled
		}
		if r.State == taskstate.Failed {
			out.State = taskstate.Failed
		}
	}
	out.Partial = completed > 0 && other > 0
	return out
}

// Summary renders a one-line overview for logs and the TUI.
func (p PlanResult) Summary() string {
	var parts []string
	for _, s := range p.Steps {
		parts = append(parts, fmt.Sprintf("%s@%s=%s", s.ID, s.Node, s.State))
	}
	return string(p.State) + ": " + strings.Join(parts, ", ")
}

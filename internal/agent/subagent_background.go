package agent

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/google/uuid"
)

// AgentResultToolName reads or stops a sub-agent started in the background.
const AgentResultToolName = "agent_result"

const (
	// maxBackgroundRuns bounds the background sub-agents one session has
	// running at once, so a model cannot fan out without limit.
	maxBackgroundRuns = 6
	// backgroundKeep is how long a finished run stays readable.
	backgroundKeep = time.Hour
	// defaultResultWait and maxResultWait bound a waiting agent_result call.
	defaultResultWait = 30
	maxResultWait     = 600
	// maxRunLabel bounds the label a listing shows for one run.
	maxRunLabel = 80
)

// backgroundRun is one sub-agent running apart from the turn that started it.
type backgroundRun struct {
	id        string
	sessionID string
	label     string
	started   time.Time
	finished  time.Time
	cancel    context.CancelFunc
	done      chan struct{}
	response  fantasy.ToolResponse
	err       error
	// read is set once the answer has been handed to the session, so
	// waiting for "any" run never returns the same answer twice and the
	// completion gate knows which answers are still unread.
	read bool
}

// backgroundRuns holds the sub-agents started in the background, by id. A run
// is only visible to the session that started it.
type backgroundRuns struct {
	mu   sync.Mutex
	runs map[string]*backgroundRun
	now  func() time.Time
	// changed is closed and replaced whenever a run finishes, waking every
	// caller parked on "the next run to finish".
	changed chan struct{}
	// collected counts, per session, the answers handed over so far. The
	// completion gate compares it between rounds to tell progress from a
	// model that keeps trying to stop.
	collected map[string]int
}

// sharedBackgroundRuns is the registry every agent tool of this process uses,
// so agent_result finds a run however many times the tools were rebuilt.
var sharedBackgroundRuns = newBackgroundRuns()

func newBackgroundRuns() *backgroundRuns {
	return &backgroundRuns{runs: map[string]*backgroundRun{}, now: time.Now, changed: make(chan struct{}), collected: map[string]int{}}
}

// start runs work apart from ctx's cancellation (the turn may end while the
// sub-agent keeps going) and returns the id to read it by. Values on ctx, such
// as the session, are kept.
func (b *backgroundRuns) start(ctx context.Context, sessionID string, work func(context.Context) (fantasy.ToolResponse, error)) (string, error) {
	return b.startNamed(ctx, sessionID, "", work)
}

// startNamed is start with a short label (the agent and its task) that
// listings show next to the id.
func (b *backgroundRuns) startNamed(ctx context.Context, sessionID, label string, work func(context.Context) (fantasy.ToolResponse, error)) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	running := 0
	for id, run := range b.runs {
		select {
		case <-run.done:
			if b.now().Sub(run.finished) > backgroundKeep {
				delete(b.runs, id)
			}
		default:
			if run.sessionID == sessionID {
				running++
			}
		}
	}
	if running >= maxBackgroundRuns {
		return "", fmt.Errorf("%d sub-agents are already running in the background for this session; read or stop one with %s first", running, AgentResultToolName)
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	run := &backgroundRun{id: "agent-" + uuid.NewString()[:8], sessionID: sessionID, label: shortLabel(label), started: b.now(), cancel: cancel, done: make(chan struct{})}
	b.runs[run.id] = run
	go func() {
		defer func() {
			b.mu.Lock()
			run.finished = b.now()
			close(run.done)
			close(b.changed)
			b.changed = make(chan struct{})
			b.mu.Unlock()
		}()
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				run.err = fmt.Errorf("sub-agent panicked: %v", r)
			}
		}()
		run.response, run.err = work(runCtx)
	}()
	return run.id, nil
}

func shortLabel(label string) string {
	label = strings.Join(strings.Fields(label), " ")
	if r := []rune(label); len(r) > maxRunLabel {
		label = string(r[:maxRunLabel-1]) + "…"
	}
	return label
}

func (b *backgroundRuns) get(id, sessionID string) (*backgroundRun, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	run, ok := b.runs[id]
	if !ok || run.sessionID != sessionID {
		return nil, false
	}
	return run, true
}

func finished(run *backgroundRun) bool {
	select {
	case <-run.done:
		return true
	default:
		return false
	}
}

// sessionRuns returns a session's runs, oldest first.
func (b *backgroundRuns) sessionRuns(sessionID string) []*backgroundRun {
	var out []*backgroundRun
	for _, run := range b.runs {
		if run.sessionID == sessionID {
			out = append(out, run)
		}
	}
	slices.SortFunc(out, func(x, y *backgroundRun) int {
		if c := x.started.Compare(y.started); c != 0 {
			return c
		}
		return strings.Compare(x.id, y.id)
	})
	return out
}

// markRead records that a finished run's answer was handed over.
func (b *backgroundRuns) markRead(run *backgroundRun) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !run.read {
		run.read = true
		b.collected[run.sessionID]++
	}
}

// next returns the oldest finished run of the session whose answer has not
// been read, the channel that is closed when another run finishes, and how
// many runs are still going.
func (b *backgroundRuns) next(sessionID string) (*backgroundRun, <-chan struct{}, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	running := 0
	var ready *backgroundRun
	for _, run := range b.sessionRuns(sessionID) {
		switch {
		case !finished(run):
			running++
		case !run.read && ready == nil:
			ready = run
		}
	}
	return ready, b.changed, running
}

// workerState is what the completion gate needs to know about a session's
// background runs.
type workerState struct {
	Running   []string
	Unread    []string
	Collected int
}

func (b *backgroundRuns) pending(sessionID string) workerState {
	b.mu.Lock()
	defer b.mu.Unlock()
	state := workerState{Collected: b.collected[sessionID]}
	for _, run := range b.sessionRuns(sessionID) {
		switch {
		case !finished(run):
			state.Running = append(state.Running, runTitle(run))
		case !run.read:
			state.Unread = append(state.Unread, runTitle(run))
		}
	}
	return state
}

func runTitle(run *backgroundRun) string {
	if run.label == "" {
		return run.id
	}
	return run.id + " (" + run.label + ")"
}

// listing describes every run of a session, one line each.
func (b *backgroundRuns) listing(sessionID string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	runs := b.sessionRuns(sessionID)
	if len(runs) == 0 {
		return "No background sub-agents in this session."
	}
	var sb strings.Builder
	running, unread := 0, 0
	for _, run := range runs {
		var state string
		switch {
		case !finished(run):
			running++
			state = "running for " + b.now().Sub(run.started).Round(time.Second).String()
		case run.err != nil || run.response.IsError:
			state = "failed"
		default:
			state = "finished"
		}
		if finished(run) {
			if run.read {
				state += ", answer read"
			} else {
				unread++
				state += ", answer not read yet"
			}
		}
		fmt.Fprintf(&sb, "- %s: %s\n", runTitle(run), state)
	}
	fmt.Fprintf(&sb, "%d running, %d finished and unread. Call %s with wait=true and no id to collect the next answer.", running, unread, AgentResultToolName)
	return sb.String()
}

// answer returns a finished run's result and marks it read.
func (b *backgroundRuns) answer(run *backgroundRun, withID bool) fantasy.ToolResponse {
	b.markRead(run)
	prefix := ""
	if withID {
		prefix = runTitle(run) + " finished.\n\n"
	}
	if run.err != nil {
		return fantasy.NewTextErrorResponse(prefix + run.err.Error())
	}
	if !withID {
		return run.response
	}
	resp := run.response
	resp.Content = prefix + resp.Content
	return resp
}

// AgentResultParams is the input of agent_result.
type AgentResultParams struct {
	ID          string `json:"id,omitempty" description:"The id returned when the sub-agent was started with run_in_background. Leave it empty to list every background sub-agent of this session, or, with wait=true, to wait for whichever finishes next."`
	Wait        bool   `json:"wait,omitempty" description:"Block until it finishes or wait_timeout elapses. Without an id, block until the next unread sub-agent finishes. Leave false to just check."`
	WaitTimeout int    `json:"wait_timeout,omitempty" description:"Seconds to wait when wait is true (default 30, max 600). The sub-agent keeps running if it elapses."`
	Cancel      bool   `json:"cancel,omitempty" description:"Stop the sub-agent instead of reading it. Needs an id."`
}

const agentResultDescription = "Read the answer of a sub-agent started with agent's run_in_background, wait for the next one, or stop one.\n\n" +
	"With an id and without wait it returns at once: the answer if the sub-agent has finished, otherwise how long it has been running. " +
	"With wait it blocks until it finishes or wait_timeout (seconds, default 30, max 600) passes; the sub-agent keeps running after a timeout, so you can read it again. " +
	"Without an id it lists every background sub-agent of this session and its state; with wait=true it parks until whichever one finishes next and returns that answer, so a coordinator can collect several workers one call at a time in the order they finish. " +
	"cancel stops the sub-agent named by id. Only the session that started a sub-agent can read it."

func newAgentResultTool(runs *backgroundRuns) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		AgentResultToolName,
		agentResultDescription,
		func(ctx context.Context, p AgentResultParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			sessionID := tools.GetSessionFromContext(ctx)
			wait := p.WaitTimeout
			if wait <= 0 {
				wait = defaultResultWait
			}
			wait = min(wait, maxResultWait)
			if p.ID == "" {
				if p.Cancel {
					return fantasy.NewTextErrorResponse("id is required to stop a sub-agent"), nil
				}
				if !p.Wait {
					return fantasy.NewTextResponse(runs.listing(sessionID)), nil
				}
				return waitForNext(ctx, runs, sessionID, time.Duration(wait)*time.Second)
			}
			run, ok := runs.get(p.ID, sessionID)
			if !ok {
				return fantasy.NewTextErrorResponse("no background sub-agent " + p.ID + " in this session"), nil
			}
			if p.Cancel {
				run.cancel()
				select {
				case <-run.done:
				case <-time.After(10 * time.Second):
				case <-ctx.Done():
					return fantasy.ToolResponse{}, ctx.Err()
				}
				runs.markRead(run)
				return fantasy.NewTextResponse("Stopped " + p.ID + "."), nil
			}
			if p.Wait {
				timer := time.NewTimer(time.Duration(wait) * time.Second)
				defer timer.Stop()
				select {
				case <-run.done:
				case <-timer.C:
				case <-ctx.Done():
					return fantasy.ToolResponse{}, ctx.Err()
				}
			}
			if finished(run) {
				return runs.answer(run, false), nil
			}
			return fantasy.NewTextResponse(fmt.Sprintf("%s is still running (%s so far).", p.ID, runs.now().Sub(run.started).Round(time.Second))), nil
		},
	)
}

// waitForNext parks until one of the session's background runs has an
// unread answer and returns it, prefixed with which run it was.
func waitForNext(ctx context.Context, runs *backgroundRuns, sessionID string, wait time.Duration) (fantasy.ToolResponse, error) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		ready, changed, running := runs.next(sessionID)
		if ready != nil {
			return runs.answer(ready, true), nil
		}
		if running == 0 {
			return fantasy.NewTextResponse("No background sub-agent is running or waiting to be read."), nil
		}
		select {
		case <-changed:
		case <-timer.C:
			return fantasy.NewTextResponse(fmt.Sprintf("No sub-agent finished within %s; %d still running. Call again to keep waiting.\n\n%s", wait, running, runs.listing(sessionID))), nil
		case <-ctx.Done():
			return fantasy.ToolResponse{}, ctx.Err()
		}
	}
}

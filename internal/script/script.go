// Package script runs short Starlark programs that orchestrate sub-agents:
// fan out, wait, branch on what came back. It replaces a model writing ten
// tool calls by hand with one program whose control flow is exact.
//
// A script is hermetic. Starlark has no file, network or clock access, and
// the only way out is the agent() builtin, which the host decides how to
// serve. Every run is bounded: by the number of agents it may start, by
// interpreter steps and by wall-clock time.
package script

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.starlark.net/lib/json"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

// AgentRequest is one agent() call as the host sees it.
type AgentRequest struct {
	Prompt string
	// Agent names a configured specialist; empty means the host's default.
	Agent string
	// Schema, when set, requires the reply to be JSON matching it.
	Schema map[string]any
}

// AgentResult is what the host returns for a call. Value is the parsed JSON
// when a schema was given, otherwise nil; Text is always the raw reply.
type AgentResult struct {
	Text  string
	Value any
}

// Host serves agent() calls. It must be safe for concurrent use: parallel()
// calls it from several goroutines. An error fails that call.
type Host interface {
	RunAgent(ctx context.Context, req AgentRequest) (AgentResult, error)
}

// Limits bound one run. Zero fields take the defaults.
type Limits struct {
	MaxAgents int           // Agent calls in total. Default 16.
	MaxSteps  uint64        // Starlark interpreter steps. Default 5,000,000.
	Timeout   time.Duration // Wall clock for the whole script. Default 30 minutes.
	MaxLog    int           // Bytes kept of log() output. Default 16 KiB.
}

func (l Limits) withDefaults() Limits {
	if l.MaxAgents <= 0 {
		l.MaxAgents = 16
	}
	if l.MaxSteps == 0 {
		l.MaxSteps = 5_000_000
	}
	if l.Timeout <= 0 {
		l.Timeout = 30 * time.Minute
	}
	if l.MaxLog <= 0 {
		l.MaxLog = 16 * 1024
	}
	return l
}

// Result is what a finished script produced.
type Result struct {
	// Value is main()'s return value rendered as JSON.
	Value string
	// Log is what the script printed with log() and phase(), in order.
	Log string
	// Agents is how many agent() calls ran.
	Agents int
}

// ErrBudget is returned (wrapped) when a script exceeds a limit.
var ErrBudget = errors.New("script budget exceeded")

// run holds the state of one execution.
type run struct {
	host   Host
	limits Limits
	ctx    context.Context
	agents atomic.Int64

	mu  sync.Mutex
	log strings.Builder
}

var fileOptions = &syntax.FileOptions{Set: true, While: true, Recursion: false, GlobalReassign: false}

// Run executes code. The program must define main() taking no arguments; its
// return value is the result. Top-level code only defines things.
func Run(ctx context.Context, code string, host Host, limits Limits) (Result, error) {
	limits = limits.withDefaults()
	ctx, cancel := context.WithTimeout(ctx, limits.Timeout)
	defer cancel()
	r := &run{host: host, limits: limits, ctx: ctx}

	predeclared := starlark.StringDict{
		"agent":       starlark.NewBuiltin("agent", r.agentBuiltin),
		"parallel":    starlark.NewBuiltin("parallel", r.parallelBuiltin),
		"log":         starlark.NewBuiltin("log", r.logBuiltin),
		"phase":       starlark.NewBuiltin("phase", r.phaseBuiltin),
		"agents_used": starlark.NewBuiltin("agents_used", r.agentsUsed),
		"sum":         starlark.NewBuiltin("sum", sumBuiltin),
		"json":        json.Module,
		"struct":      starlark.NewBuiltin("struct", starlarkstruct.Make),
	}

	thread := r.newThread("main")
	stop := context.AfterFunc(ctx, func() { thread.Cancel(ctx.Err().Error()) })
	defer stop()

	globals, err := starlark.ExecFileOptions(fileOptions, thread, "script.star", code, predeclared)
	if err != nil {
		return r.result(nil), explain(err, ctx)
	}
	mainFn, ok := globals["main"].(starlark.Callable)
	if !ok {
		return r.result(nil), errors.New("the script must define main(), which returns the result")
	}
	value, err := starlark.Call(thread, mainFn, nil, nil)
	if err != nil {
		return r.result(nil), explain(err, ctx)
	}
	return r.result(value), nil
}

func (r *run) newThread(name string) *starlark.Thread {
	t := &starlark.Thread{Name: name, Print: func(_ *starlark.Thread, msg string) { r.appendLog(msg) }}
	t.SetMaxExecutionSteps(r.limits.MaxSteps)
	return t
}

func (r *run) result(value starlark.Value) Result {
	out := Result{Log: r.logText(), Agents: int(r.agents.Load())}
	if value != nil {
		out.Value = render(value)
	}
	return out
}

// explain turns an interpreter error into one the script's author can act on.
func explain(err error, ctx context.Context) error {
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("%w: the script ran out of time", ErrBudget)
		}
		return ctx.Err()
	}
	var eval *starlark.EvalError
	if errors.As(err, &eval) {
		if strings.Contains(eval.Msg, "too many steps") {
			return fmt.Errorf("%w: the script ran too many steps (an endless loop?)", ErrBudget)
		}
		failure := &scriptError{text: eval.Msg + "\n" + strings.TrimSpace(eval.Backtrace())}
		if errors.Is(err, ErrBudget) {
			failure.cause = ErrBudget
		}
		return failure
	}
	return err
}

// scriptError carries an interpreter failure with its traceback while keeping
// ErrBudget visible to errors.Is when the cause was a limit.
type scriptError struct {
	text  string
	cause error
}

func (e *scriptError) Error() string { return e.text }
func (e *scriptError) Unwrap() error { return e.cause }

func (r *run) appendLog(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.log.Len() >= r.limits.MaxLog {
		return
	}
	if r.log.Len()+len(line)+1 > r.limits.MaxLog {
		line = line[:max(0, r.limits.MaxLog-r.log.Len()-1)] + "…"
	}
	r.log.WriteString(line)
	r.log.WriteByte('\n')
}

func (r *run) logText() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.TrimRight(r.log.String(), "\n")
}

func (r *run) logBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(kwargs) > 0 {
		return nil, errors.New("log takes positional arguments only")
	}
	parts := make([]string, len(args))
	for i, a := range args {
		if s, ok := starlark.AsString(a); ok {
			parts[i] = s
		} else {
			parts[i] = a.String()
		}
	}
	r.appendLog(strings.Join(parts, " "))
	return starlark.None, nil
}

func (r *run) phaseBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "name", &name); err != nil {
		return nil, err
	}
	r.appendLog("== " + name + " ==")
	return starlark.None, nil
}

func (r *run) agentsUsed(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	return starlark.MakeInt64(r.agents.Load()), nil
}

// agentBuiltin implements agent(prompt, agent_name="", schema=None). It blocks
// until the agent has answered and returns its reply: a string, or the parsed
// JSON value when a schema was given. A failed run raises, so the script stops
// unless it handles it with parallel(), which turns a failure into a value.
func (r *run) agentBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var prompt, name string
	var schema starlark.Value = starlark.None
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "prompt", &prompt, "agent_name?", &name, "schema?", &schema); err != nil {
		return nil, err
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, errors.New("agent: prompt must not be empty")
	}
	req := AgentRequest{Prompt: prompt, Agent: strings.TrimSpace(name)}
	if schema != starlark.None {
		doc, err := toGo(schema)
		if err != nil {
			return nil, fmt.Errorf("agent: schema: %w", err)
		}
		m, ok := doc.(map[string]any)
		if !ok {
			return nil, errors.New("agent: schema must be a dict")
		}
		req.Schema = m
	}
	if n := r.agents.Add(1); n > int64(r.limits.MaxAgents) {
		r.agents.Add(-1)
		return nil, fmt.Errorf("%w: a script may start at most %d agents", ErrBudget, r.limits.MaxAgents)
	}
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	res, err := r.host.RunAgent(r.ctx, req)
	if err != nil {
		return nil, fmt.Errorf("agent failed: %w", err)
	}
	if req.Schema != nil {
		return toStarlark(res.Value)
	}
	return starlark.String(res.Text), nil
}

// parallelBuiltin implements parallel(calls): each element is a function of
// no arguments, run in its own goroutine, and the results come back in order.
// A branch that fails does not stop its siblings: its slot holds
// struct(error="...") instead, and the script decides what to do.
//
// The functions are frozen first, so a branch cannot change values another
// branch can see; each branch communicates by returning.
func (r *run) parallelBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var calls *starlark.List
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "calls", &calls); err != nil {
		return nil, err
	}
	fns := make([]starlark.Callable, calls.Len())
	for i := range fns {
		fn, ok := calls.Index(i).(starlark.Callable)
		if !ok {
			return nil, fmt.Errorf("parallel: element %d is a %s, not a function; wrap the call: lambda: agent(...)", i, calls.Index(i).Type())
		}
		fn.(starlark.Value).Freeze()
		fns[i] = fn
	}
	out := make([]starlark.Value, len(fns))
	var wg sync.WaitGroup
	for i, fn := range fns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			thread := r.newThread(fmt.Sprintf("parallel-%d", i))
			stop := context.AfterFunc(r.ctx, func() { thread.Cancel(r.ctx.Err().Error()) })
			defer stop()
			v, err := starlark.Call(thread, fn, nil, nil)
			if err != nil {
				var eval *starlark.EvalError
				msg := err.Error()
				if errors.As(err, &eval) {
					msg = eval.Msg
				}
				v = starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{"error": starlark.String(msg)})
			}
			out[i] = v
		}()
	}
	wg.Wait()
	return starlark.NewList(out), nil
}

// sumBuiltin is sum(values, start=0). Starlark omits it, and it is the first
// thing a script reaches for when it merges the answers of several agents.
func sumBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var values starlark.Iterable
	var start starlark.Value = starlark.MakeInt(0)
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "values", &values, "start?", &start); err != nil {
		return nil, err
	}
	iter := values.Iterate()
	defer iter.Done()
	total := start
	var item starlark.Value
	for iter.Next(&item) {
		next, err := starlark.Binary(syntax.PLUS, total, item)
		if err != nil {
			return nil, fmt.Errorf("sum: %w", err)
		}
		total = next
	}
	return total, nil
}

// render turns the script's result into JSON text; values JSON cannot hold
// (a function, a struct) fall back to Starlark's own notation.
func render(v starlark.Value) string {
	if s, ok := v.(starlark.String); ok {
		return string(s)
	}
	goValue, err := toGo(v)
	if err != nil {
		return v.String()
	}
	data, err := marshalIndent(goValue)
	if err != nil {
		return v.String()
	}
	return data
}

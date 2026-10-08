package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/devloop"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/scenarios"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
)

const AppLoopToolName = "app_loop"

//go:embed app_loop.md
var appLoopDescription string

// AppLoopParams keeps fields flat; scenario reuses the scenario tool's
// validated contract and criteria are one level of flat objects.
type AppLoopParams struct {
	Action          string              `json:"action" enum:"plan,start,ready,check,logs,stop,report,status" description:"plan criteria and limits; start an owned app or attach to a running server; ready waits on a probe; check runs a web, isolated desktop, Android or terminal scenario and grades criteria; logs tails output; stop ends only an owned app; report links criteria to evidence."`
	LoopID          string              `json:"loop_id" description:"Stable id for this loop, e.g. checkout-form-fix."`
	Criteria        []devloop.Criterion `json:"criteria,omitempty" description:"plan: observable acceptance criteria {id,text,verify:\"scenario\"|\"unverifiable\",assertions:[1-based scenario assertion indices],reason}."`
	MaxIterations   int                 `json:"max_iterations,omitempty" description:"plan: check runs allowed (default 5, max 10)."`
	DeadlineMinutes int                 `json:"deadline_minutes,omitempty" description:"plan: wall-clock budget (default 30, max 120)."`
	Argv            []string            `json:"argv,omitempty" description:"start: the project's real run command as literal argv, e.g. [\"npm\",\"run\",\"dev\"]."`
	Cwd             string              `json:"cwd,omitempty" description:"start: directory relative to the workspace."`
	Port            int                 `json:"port,omitempty" description:"start: port the app serves on; if already listening, Atlas attaches and never stops it."`
	URL             string              `json:"url,omitempty" description:"start: app URL for reports."`
	ProbeKind       string              `json:"probe_kind,omitempty" enum:"http,tcp,log" description:"ready: http (status 2xx/3xx, optional contains), tcp (port) or log (regex in app output)."`
	ProbeURL        string              `json:"probe_url,omitempty" description:"ready: URL for http probes."`
	ProbeContains   string              `json:"probe_contains,omitempty" description:"ready: text the http body must contain."`
	ProbePort       int                 `json:"probe_port,omitempty" description:"ready: port for tcp probes."`
	ProbePattern    string              `json:"probe_pattern,omitempty" description:"ready: regex for log probes."`
	TimeoutMS       int                 `json:"timeout_ms,omitempty" description:"ready: wait limit (default 60000, max 180000)."`
	Scenario        *scenarios.Scenario `json:"scenario,omitempty" description:"check: version 1 scenario. web needs url/viewport; desktop needs window_id and optional isolated node; android needs serial/package; tui needs argv/terminal dimensions. Native selectors: desktop id:AutomationId or name:ExactName; Android resource-id. Steps click/type/key; assertions text/value/visible/focused."`
	ChangeNote      string              `json:"change_note,omitempty" description:"check: what was changed in code since the previous check."`
	Lines           int                 `json:"lines,omitempty" description:"logs: number of lines (default 80, max 400)."`
}

type appLoopTool struct {
	root        string
	permissions permission.Service
	store       *engineering.Store
	invoke      ToolInvoker
	manager     *devloop.Manager
}

// NewAppLoopTool builds the live development loop tool. stateDir must be
// outside the workspace: app logs written there during a check would
// otherwise change the source fingerprint the scenario evidence binds to.
func NewAppLoopTool(root, stateDir string, store *engineering.Store, permissions permission.Service, invoke ToolInvoker) fantasy.AgentTool {
	t := &appLoopTool{root: root, permissions: permissions, store: store, invoke: invoke, manager: devloop.NewManager(stateDir)}
	return fantasy.NewAgentTool(AppLoopToolName, appLoopDescription, func(ctx context.Context, _ AppLoopParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p AppLoopParams
		if err := strictDecode(call.Input, &p, `{"action":"status","loop_id":"checkout-fix"}`); err != nil {
			return deviceErrorResponse(err), nil
		}
		resp, err := t.run(ctx, p, call)
		if err != nil {
			return deviceErrorResponse(err), nil
		}
		return resp, nil
	})
}

func (t *appLoopTool) approve(ctx context.Context, call fantasy.ToolCall, action, desc string, params any) error {
	if t.permissions == nil {
		return nil
	}
	ok, err := t.permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: GetSessionFromContext(ctx), ToolCallID: call.ID, ToolName: AppLoopToolName, Action: action, Description: desc, Params: params, Path: t.root})
	if err != nil {
		return err
	}
	if !ok {
		return deviceErr("permission_denied", "", "the user denied this action", "")
	}
	return nil
}

func (t *appLoopTool) loop(id string) (devloop.Loop, error) {
	l, err := t.manager.Get(id)
	if err != nil {
		return l, deviceErr("unknown_loop", "loop_id", "no loop "+id+"; call plan first", `{"action":"plan","loop_id":"`+id+`","criteria":[...]}`)
	}
	return l, nil
}

func (t *appLoopTool) run(ctx context.Context, p AppLoopParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	session := GetSessionFromContext(ctx)
	switch p.Action {
	case "plan":
		l, err := t.manager.Plan(p.LoopID, session, p.Criteria, p.MaxIterations, time.Duration(p.DeadlineMinutes)*time.Minute)
		if err != nil {
			return fantasy.ToolResponse{}, deviceErr("invalid_plan", "criteria", err.Error(), "Every criterion must be observable in the real UI (verify:scenario with assertion indices) or explicitly unverifiable with a reason.")
		}
		return deviceResponse(map[string]any{"ok": true, "loop": l}), nil
	case "status":
		l, err := t.loop(p.LoopID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		alive, code := t.manager.Alive(l)
		return deviceResponse(map[string]any{"ok": true, "loop": l, "app_alive": alive, "exit_code": code}), nil
	case "start":
		l, err := t.loop(p.LoopID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if err := t.approve(ctx, call, "start", fmt.Sprintf("Start the app for loop %s: %s", l.ID, strings.Join(p.Argv, " ")), p); err != nil {
			return fantasy.ToolResponse{}, err
		}
		res, err := t.manager.Start(&l, t.root, p.Argv, p.Cwd, p.Port, p.URL)
		if err != nil {
			return fantasy.ToolResponse{}, deviceErr("start_failed", "argv", err.Error(), "Check the project's real run command (package.json scripts, Makefile, README).")
		}
		note := "Wait with ready before checking; never assume a fixed delay."
		if res.Outcome == "attached_existing" {
			note = "A server you did not start already listens on this port. Atlas attached to it and will not stop or restart it; if code changes need a restart, ask the user."
		}
		return deviceResponse(map[string]any{"ok": true, "result": res, "note": note}), nil
	case "ready":
		l, err := t.loop(p.LoopID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		res, err := t.manager.Ready(ctx, l, devloop.Probe{Kind: p.ProbeKind, URL: p.ProbeURL, Contains: p.ProbeContains, Port: p.ProbePort, Pattern: p.ProbePattern}, time.Duration(p.TimeoutMS)*time.Millisecond)
		if err != nil {
			return fantasy.ToolResponse{}, deviceErr("invalid_probe", "probe_kind", err.Error(), "")
		}
		res.LogTail = redactLines(res.LogTail)
		resp := deviceResponse(map[string]any{"ok": res.Ready, "ready": res})
		resp.IsError = !res.Ready
		return resp, nil
	case "logs":
		l, err := t.loop(p.LoopID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if l.Process == nil || l.Process.LogFile == "" {
			return fantasy.ToolResponse{}, deviceErr("no_logs", "", "logs exist only for an app started by app_loop (attached servers log elsewhere)", "")
		}
		lines, err := devloop.TailLines(l.Process.LogFile, min(max(p.Lines, 1), 400))
		if p.Lines == 0 {
			lines, err = devloop.TailLines(l.Process.LogFile, 80)
		}
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "lines": redactLines(lines)}), nil
	case "stop":
		l, err := t.loop(p.LoopID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		outcome, err := t.manager.Stop(&l)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "outcome": outcome}), nil
	case "check":
		// The scenario is re-parsed from the raw input so the scenario
		// contract (duplicate keys, lowercase keys) applies unchanged.
		var raw struct {
			Scenario json.RawMessage `json:"scenario"`
		}
		_ = json.Unmarshal([]byte(call.Input), &raw)
		return t.check(ctx, p, raw.Scenario, call)
	case "report":
		l, err := t.loop(p.LoopID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(t.report(ctx, l)), nil
	}
	return fantasy.ToolResponse{}, deviceErr("invalid_action", "action", fmt.Sprintf("unknown action %q", p.Action), "")
}

// logSecret matches credential-looking assignments and bearer tokens in app
// output, so log excerpts in reports never carry them.
var logSecret = regexp.MustCompile(`(?i)((?:password|passwd|secret|token|api[_-]?key|authorization|cookie|session)[\w-]*["']?\s*[:=]\s*["']?|bearer\s+)([^\s"',;]+)`)

func redactLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = logSecret.ReplaceAllString(l, "${1}•••")
	}
	return out
}

func (t *appLoopTool) check(ctx context.Context, p AppLoopParams, rawScenario json.RawMessage, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	l, err := t.loop(p.LoopID)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if l.State.Terminal() {
		return fantasy.ToolResponse{}, deviceErr("loop_closed", "loop_id", fmt.Sprintf("loop %s is %s (%s)", l.ID, l.State, l.Reason), "Report the result; start a new loop_id for new work.")
	}
	if len(l.Iterations) >= l.MaxIterations || time.Now().UnixMilli() > l.Deadline {
		l.State, l.Reason = taskstate.Failed, fmt.Sprintf("iteration limit (%d) or deadline reached without passing", l.MaxIterations)
		if err := t.manager.Save(l); err != nil {
			return fantasy.ToolResponse{}, err
		}
		return fantasy.ToolResponse{}, deviceErr("loop_limit", "", l.Reason, "Stop fixing; report the remaining failures with their evidence.")
	}
	scenario, err := scenarios.Parse(ctx, rawScenario)
	if err != nil {
		return fantasy.ToolResponse{}, deviceErr("invalid_scenario", "scenario", err.Error(), "Use a web, desktop, android or tui scenario with its explicit target identity.")
	}
	for _, c := range l.Criteria {
		for _, a := range c.Assertions {
			if a < 1 || a > len(scenario.Assertions) {
				return fantasy.ToolResponse{}, deviceErr("invalid_scenario", "scenario", fmt.Sprintf("criterion %s needs assertion #%d but the scenario has %d", c.ID, a, len(scenario.Assertions)), "")
			}
		}
	}
	it := devloop.Iteration{N: len(l.Iterations) + 1, StartedAt: time.Now().UnixMilli(), ChangeNote: p.ChangeNote}
	var logStart int64
	if l.Process != nil && l.Process.LogFile != "" {
		logStart = devloop.LogSize(l.Process.LogFile)
	}
	appDown := ""
	if alive, code := t.manager.Alive(l); !alive {
		appDown = fmt.Sprintf("the app process is not running (exit code %d); restart it before checking", code)
	}
	var run scenarios.ScenarioRun
	var runErr error
	ran := false
	if appDown == "" {
		services := ScenarioServices{Root: t.root, SessionID: GetSessionFromContext(ctx), Store: t.store, Invoke: t.invoke}
		if t.permissions != nil {
			services.Approve = t.permissions.Request
		}
		checkCtx, cancel := context.WithDeadline(ctx, time.UnixMilli(l.Deadline))
		run, runErr = ExecuteScenario(checkCtx, services, scenario, call)
		cancel()
		ran = run.Observed
		it.ScenarioRun = run.ID
		if alive, code := t.manager.Alive(l); !alive {
			appDown = fmt.Sprintf("the app process exited during the check (exit code %d)", code)
		}
	}
	action := fmt.Sprintf("scenario %s target=%s url=%s node=%s window=%s serial=%s package=%s (%d steps, %d assertions)", scenario.ID, scenario.Target, scenario.URL, scenario.Node, scenario.WindowID, scenario.Serial, scenario.Package, len(scenario.Steps), len(scenario.Assertions))
	if runErr == nil && !run.Passed && len(devloop.FailedAssertions(run.Gaps)) == 0 {
		runErr = fmt.Errorf("scenario did not pass: %s", run.Status)
	}
	it.Criteria, it.Passed = devloop.Grade(l.Criteria, runErr, devloop.FailedAssertions(run.Gaps), ran, action, appDown)
	if runErr != nil {
		it.Error = runErr.Error()
	}
	if l.Process != nil && l.Process.LogFile != "" {
		it.LogExcerpt = redactLines(devloop.LinesSince(l.Process.LogFile, logStart, 40))
	}
	it.EndedAt = time.Now().UnixMilli()
	l.Iterations = append(l.Iterations, it)
	if it.Passed {
		l.State, l.Reason = taskstate.Completed, "all verifiable criteria passed in the real UI"
	}
	if err := t.manager.Save(l); err != nil {
		// Never report success that is not durably recorded.
		return fantasy.ToolResponse{}, deviceErr("persist_failed", "", "the check ran but its result could not be saved: "+err.Error(), "")
	}
	out := map[string]any{"ok": it.Passed, "iteration": it, "remaining_iterations": l.MaxIterations - len(l.Iterations), "scenario": map[string]any{"run_id": run.ID, "status": run.Status, "gaps": run.Gaps, "artifacts": run.Artifacts}}
	if !it.Passed {
		out["next"] = "Read the failed criteria and log excerpt, fix the code, then check again with change_note. Do not edit criteria or assertions to make them pass."
	}
	resp := deviceResponse(out)
	resp.IsError = !it.Passed
	return resp, nil
}

func (t *appLoopTool) report(ctx context.Context, l devloop.Loop) map[string]any {
	type row struct {
		Criterion   string `json:"criterion"`
		Text        string `json:"text"`
		Action      string `json:"action"`
		Observation string `json:"observation"`
		Result      string `json:"result"`
		Iteration   int    `json:"iteration"`
	}
	var rows []row
	if n := len(l.Iterations); n > 0 {
		last := l.Iterations[n-1]
		for _, c := range l.Criteria {
			for _, r := range last.Criteria {
				if r.ID == c.ID {
					rows = append(rows, row{Criterion: c.ID, Text: c.Text, Action: r.Action, Observation: r.Observation, Result: r.Result, Iteration: last.N})
				}
			}
		}
	}
	diff := ""
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(cctx, "git", "-C", t.root, "diff", "--stat").Output(); err == nil {
		diff = strings.TrimSpace(string(out))
		if len(diff) > 8000 {
			diff = diff[:8000] + "\n..."
		}
	}
	return map[string]any{"ok": true, "loop_id": l.ID, "state": l.State, "reason": l.Reason, "criteria": rows, "iterations": len(l.Iterations), "max_iterations": l.MaxIterations, "process": l.Process, "diff_stat": diff, "history": l.Iterations}
}

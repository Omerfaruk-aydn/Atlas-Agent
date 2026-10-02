package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/scenarios"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/terminal"
)

var scenarioKeys = map[string]string{"enter": "\r", "tab": "\t", "escape": "\x1b", "backspace": "\x7f", "delete": "\x1b[3~", "arrowup": "\x1b[A", "arrowdown": "\x1b[B", "arrowright": "\x1b[C", "arrowleft": "\x1b[D"}

type scenarioTranscript struct {
	mu      sync.Mutex
	data    []byte
	changed chan struct{}
	done    chan struct{}
	err     error
}

func (t *scenarioTranscript) collect(reader io.Reader) {
	defer close(t.done)
	buffer := make([]byte, 32768)
	for {
		n, err := reader.Read(buffer)
		t.mu.Lock()
		if len(t.data)+n > terminal.MaxTranscriptBytes {
			err = fmt.Errorf("scenario transcript exceeds bounds")
			n = max(0, terminal.MaxTranscriptBytes-len(t.data))
		}
		t.data = append(t.data, buffer[:n]...)
		close(t.changed)
		t.changed = make(chan struct{})
		if err != nil && err != io.EOF {
			t.err = err
		}
		t.mu.Unlock()
		if err != nil {
			return
		}
	}
}

func (t *scenarioTranscript) wait(ctx context.Context, value string) error {
	if value == "" {
		return fmt.Errorf("wait_text requires nonempty text")
	}
	for {
		t.mu.Lock()
		found := strings.Contains(string(t.data), value)
		changed := t.changed
		t.mu.Unlock()
		if found {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.done:
			return fmt.Errorf("terminal exited before expected text")
		case <-changed:
		}
	}
}

func (b *scenarioBackend) terminal(ctx context.Context, scenario scenarios.Scenario, run scenarios.ScenarioRun) (scenarios.ScenarioRun, error) {
	run.Status = "unavailable"
	commandName := append([]string{}, scenario.Argv...)
	commandName[0] = filepath.Base(commandName[0])
	for _, block := range b.CommandPolicy.blockFuncs() {
		if block(scenario.Argv) || block(commandName) {
			return run, fmt.Errorf("terminal command blocked by policy")
		}
	}
	if b.Approve == nil {
		return run, fmt.Errorf("terminal permission service unavailable")
	}
	approved, err := b.Approve(ctx, permission.CreatePermissionRequest{SessionID: b.SessionID, ToolCallID: b.call.ID, ToolName: BashToolName, Action: "execute", Path: b.Root, Description: "Run interaction scenario with literal argv", Params: BashPermissionsParams{Argv: scenario.Argv, WorkingDir: b.Root}})
	if err != nil {
		return run, err
	}
	if !approved {
		return run, fmt.Errorf("terminal scenario permission denied")
	}
	if err := b.Store.CheckOperation(ctx, b.SessionID, b.operationID); err != nil {
		return run, err
	}
	req := execution.Request{ToolCallID: b.call.ID, SessionID: b.SessionID, TaskID: b.saved.TaskID, Root: b.Root, RunID: scenario.RunID, Argv: scenario.Argv, Policy: execution.ExecutionPolicy{Mode: "legacy", TimeoutMS: scenario.TimeoutMS}}
	size := execution.TerminalSize{Width: scenario.Width, Height: scenario.Height}
	var session execution.TerminalSession
	if execution.HasBinding(ctx) {
		_, runner, e := isolatedRunner(ctx)
		if e != nil {
			return run, e
		}
		backend, ok := runner.(execution.TerminalRunner)
		if !ok {
			return run, terminal.ErrUnavailable
		}
		session, err = backend.StartTerminal(ctx, req, size)
	} else {
		session, err = terminal.Start(ctx, req, size)
	}
	if err != nil {
		return run, err
	}
	if session == nil {
		return run, terminal.ErrUnavailable
	}
	defer terminal.Cleanup(session)
	transcript := &scenarioTranscript{changed: make(chan struct{}), done: make(chan struct{})}
	go transcript.collect(session)
	run.Status = "failed"
	for _, step := range scenario.Steps {
		if err = b.Store.CheckOperation(ctx, b.SessionID, b.operationID); err != nil {
			break
		}
		switch step.Action {
		case "input":
			_, err = io.WriteString(session, step.Value)
		case "key":
			key, ok := scenarioKeys[step.Value]
			if !ok {
				err = fmt.Errorf("unsupported terminal key")
			} else {
				_, err = io.WriteString(session, key)
			}
		case "resize":
			err = session.Resize(ctx, execution.TerminalSize{Width: step.Width, Height: step.Height})
		case "wait_text":
			err = transcript.wait(ctx, step.Value)
		case "cancel":
			err = session.Close()
		default:
			err = fmt.Errorf("unsupported terminal action")
		}
		if err != nil {
			break
		}
	}
	if err != nil {
		_ = session.Close()
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	waitCtx := ctx
	if err != nil {
		waitCtx = cleanup
	}
	result, waitErr := session.Wait(waitCtx)
	if !result.Done {
		_ = session.Close()
		result, waitErr = session.Wait(cleanup)
	}
	run.Result = result
	run.Observed = result.Done && result.ExitCode != nil
	select {
	case <-transcript.done:
	case <-cleanup.Done():
		return run, fmt.Errorf("terminal transcript did not finish")
	}
	transcript.mu.Lock()
	data := append([]byte{}, transcript.data...)
	streamErr := transcript.err
	transcript.mu.Unlock()
	ref, storeErr := b.Store.PutArtifact(cleanup, "pty-transcript", data)
	if storeErr != nil {
		return run, storeErr
	}
	run.Artifacts = append(run.Artifacts, ref)
	for _, step := range scenario.Steps {
		if step.Action == "resize" {
			size = execution.TerminalSize{Width: step.Width, Height: step.Height}
		}
	}
	screen, renderErr := terminal.RenderTranscript(cleanup, data, size)
	if renderErr != nil {
		return run, renderErr
	}
	encoded, renderErr := json.Marshal(screen)
	if renderErr != nil {
		return run, renderErr
	}
	renderRef, renderErr := b.Store.PutArtifact(cleanup, "terminal-rendering", encoded)
	if renderErr != nil {
		return run, renderErr
	}
	run.Artifacts = append(run.Artifacts, renderRef)
	if len(screen.Unsupported) > 0 {
		run.Gaps = append(run.Gaps, "Terminal interpretation includes unsupported controls; raw interaction assertions remain separate")
	}
	if err != nil {
		return run, err
	}
	if waitErr != nil {
		return run, waitErr
	}
	if streamErr != nil {
		return run, streamErr
	}
	passed := run.Observed && result.Status != "output_limit"
	for _, a := range scenario.Assertions {
		ok := false
		switch a.Kind {
		case "transcript":
			ok = strings.Contains(string(data), a.Expected)
		case "exit":
			code, e := strconv.Atoi(a.Expected)
			ok = e == nil && result.ExitCode != nil && *result.ExitCode == code
		case "status":
			ok = result.Status == a.Expected
		}
		if !ok {
			passed = false
			run.Gaps = append(run.Gaps, "Assertion failed: "+a.Kind)
		}
	}
	run.Passed = passed
	if passed {
		run.Status = "passed"
	}
	return run, nil
}

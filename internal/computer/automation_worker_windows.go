//go:build windows

package computer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
)

// The automation worker keeps one PowerShell process with the UI Automation
// assemblies loaded for the backend's lifetime, instead of starting a
// process and loading the assemblies for every call. It runs the same
// script as the one-shot path, one request at a time.
//
// Protocol: Go writes one JSON line {"id":N,"request":{...}} per request to
// the worker's stdin. The worker answers with exactly one stdout line
// "@@ATLAS <id> <json>". Any other stdout line is log output and is never
// read as a result; stderr is kept in a bounded buffer for diagnostics.
//
// Safety: a request that may change the desktop (invoke, set_value,
// select, launch_app) is never sent twice. If the worker dies, is canceled
// or times out after such a request was written, the result is
// outcome_unknown. A read-only request may be retried once on a fresh
// worker. Restarts are bounded; past the bound calls use the one-shot path.

const (
	workerResultPrefix = "@@ATLAS "
	workerIdleTimeout  = 2 * time.Minute
	workerMaxRestarts  = 3
	workerRestartSpan  = time.Minute
	workerStderrLimit  = 16 * 1024
)

// utf8BOM may lead the first output line of a PowerShell process.
var utf8BOM = []byte{0xef, 0xbb, 0xbf}

// workerLoop runs inside the worker. It loads the automation script once
// and invokes it per request with $AtlasRequest set; the script's results
// are pipeline output, collected into one line.
const workerLoop = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
[Console]::InputEncoding = [System.Text.UTF8Encoding]::new($false)
$atlasBody = [scriptblock]::Create([System.IO.File]::ReadAllText($env:ATLAS_AUTOMATION_SCRIPT))
Remove-Item -LiteralPath $env:ATLAS_AUTOMATION_SCRIPT -ErrorAction SilentlyContinue
while ($true) {
    $line = [Console]::In.ReadLine()
    if ($null -eq $line) { break }
    $envelope = $line | ConvertFrom-Json
    $AtlasRequest = $envelope.request
    $out = (& $atlasBody) -join ''
    [Console]::Out.WriteLine('@@ATLAS ' + $envelope.id + ' ' + $out)
    [Console]::Out.Flush()
}`

// automationMutates reports requests that can change the desktop.
func automationMutates(action string) bool {
	switch action {
	case "invoke", "set_value", "select", "launch_app":
		return true
	}
	return false
}

type automationWorker struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	cancel   context.CancelFunc
	stdin    io.WriteCloser
	lines    chan string
	exited   chan struct{}
	stderr   *boundedBuffer
	seq      uint64
	idle     *time.Timer
	restarts []time.Time
	disabled bool
	// Started and Reused count worker starts and requests served by an
	// already running worker, for measurement.
	Started, Reused int
}

type boundedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if n >= workerStderrLimit {
		b.buf.Reset()
		p = p[n-workerStderrLimit:]
	} else if excess := b.buf.Len() + n - workerStderrLimit; excess > 0 {
		b.buf.Next(excess)
	}
	_, _ = b.buf.Write(p)
	return n, nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(b.buf.String())
}

var errWorkerUnavailable = errors.New("automation worker unavailable")

// run sends one request. ok=false means the worker could not be used and
// nothing was sent, so the caller may use the one-shot path.
func (w *automationWorker) run(ctx context.Context, p AutomationRequest, data []byte) (json.RawMessage, bool, error) {
	if err := w.lockRequest(ctx); err != nil {
		return nil, true, err
	}
	defer w.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, true, err
	}
	if w.disabled {
		return nil, false, nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, true, err
		}
		reused := w.cmd != nil
		if err := w.ensure(); err != nil {
			slog.Warn("Automation worker could not start; using one-shot automation", "error", err)
			w.disabled = true
			return nil, false, nil
		}
		if reused {
			w.Reused++
		}
		result, sent, err := w.exchange(ctx, data)
		if err == nil {
			w.armIdle()
			return result, true, nil
		}
		w.stop()
		if ctx.Err() != nil {
			if sent && automationMutates(p.Action) {
				return nil, true, fmt.Errorf("outcome_unknown: %s was canceled while the desktop may have applied it; observe before retrying: %w", p.Action, ctx.Err())
			}
			return nil, true, ctx.Err()
		}
		if !sent {
			continue
		}
		if automationMutates(p.Action) {
			// Never resend input whose effect is unknown.
			return nil, true, fmt.Errorf("outcome_unknown: the automation worker stopped during %s (%v); observe whether it took effect before retrying", p.Action, err)
		}
		if attempt == 0 && w.mayRestart() {
			continue
		}
		return nil, true, fmt.Errorf("accessibility_unavailable: automation worker failed (%w); inspect a screenshot or use manual handoff", err)
	}
	return nil, true, fmt.Errorf("accessibility_unavailable: automation worker could not be restarted")
}

// lockRequest lets queued requests honor cancellation while another
// automation call owns the worker. A canceled waiter never dispatches.
func (w *automationWorker) lockRequest(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if w.mu.TryLock() {
			return nil
		}
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// exchange writes one request and waits for its result line. sent reports
// whether the request reached the worker.
func (w *automationWorker) exchange(ctx context.Context, data []byte) (json.RawMessage, bool, error) {
	w.seq++
	id := strconv.FormatUint(w.seq, 10)
	envelope, err := json.Marshal(map[string]any{"id": id, "request": json.RawMessage(data)})
	if err != nil {
		return nil, false, err
	}
	request := append(envelope, '\n')
	n, err := w.stdin.Write(request)
	if err == nil && n != len(request) {
		err = io.ErrShortWrite
	}
	if err != nil {
		// A partially written request has an unknown delivery outcome.
		// Conservatively prevent replay of a mutating request.
		return nil, n > 0, fmt.Errorf("write request: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return nil, true, ctx.Err()
		case <-w.exited:
			return nil, true, fmt.Errorf("worker exited: %s", w.stderr.String())
		case line, ok := <-w.lines:
			if !ok {
				return nil, true, fmt.Errorf("worker output closed: %s", w.stderr.String())
			}
			rest, isResult := strings.CutPrefix(line, workerResultPrefix)
			if !isResult {
				slog.Debug("Automation worker output", "line", line)
				continue
			}
			gotID, body, _ := strings.Cut(rest, " ")
			if gotID != id {
				// A late answer to an abandoned request cannot be ours.
				continue
			}
			if len(body) > 4*1024*1024 {
				return nil, true, fmt.Errorf("accessibility output exceeded limit")
			}
			return json.RawMessage(body), true, nil
		}
	}
}

func (w *automationWorker) mayRestart() bool {
	now := time.Now()
	kept := w.restarts[:0]
	for _, at := range w.restarts {
		if now.Sub(at) < workerRestartSpan {
			kept = append(kept, at)
		}
	}
	w.restarts = kept
	return len(w.restarts) < workerMaxRestarts
}

func (w *automationWorker) ensure() error {
	if w.cmd != nil {
		return nil
	}
	if !w.mayRestart() {
		return errWorkerUnavailable
	}
	w.restarts = append(w.restarts, time.Now())
	file, err := os.CreateTemp("", "atlas-automation-worker-*.ps1")
	if err != nil {
		return err
	}
	path := file.Name()
	if err = file.Chmod(0o600); err == nil {
		_, err = file.WriteString(automationScript)
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	units := utf16.Encode([]rune(workerLoop))
	encoded := make([]byte, len(units)*2)
	for i, v := range units {
		binary.LittleEndian.PutUint16(encoded[i*2:], v)
	}
	// -STA: UI Automation clients and WPF-hosted providers expect a
	// single-threaded apartment; requests are serialized anyway.
	// The worker outlives single requests, so it has its own lifetime
	// context; stop cancels it.
	lifetime, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(lifetime, "powershell.exe", "-STA", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(encoded))
	cmd.Env = append(os.Environ(), "ATLAS_AUTOMATION_SCRIPT="+path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		_ = os.Remove(path)
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = os.Remove(path)
		return err
	}
	w.stderr = &boundedBuffer{}
	cmd.Stderr = w.stderr
	if err := cmd.Start(); err != nil {
		cancel()
		_ = os.Remove(path)
		return err
	}
	lines := make(chan string, 16)
	exited := make(chan struct{})
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 5*1024*1024)
		for scanner.Scan() {
			lines <- strings.TrimPrefix(scanner.Text(), string(utf8BOM))
		}
		close(lines)
	}()
	go func() {
		// Drain stdout before Wait closes its pipe so the final response
		// cannot be lost when the worker exits immediately after writing.
		<-readDone
		_ = cmd.Wait()
		// The script file removes itself once loaded; this covers a
		// worker that died before reading it.
		_ = os.Remove(path)
		close(exited)
	}()
	w.cmd, w.cancel, w.stdin, w.lines, w.exited = cmd, cancel, stdin, lines, exited
	w.Started++
	return nil
}

func (w *automationWorker) armIdle() {
	if w.idle != nil {
		w.idle.Stop()
	}
	cmd := w.cmd
	w.idle = time.AfterFunc(workerIdleTimeout, func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.cmd == cmd {
			w.stop()
		}
	})
}

// stop ends the worker this backend owns: closing stdin ends its loop, and
// a worker that does not exit promptly is killed.
func (w *automationWorker) stop() {
	if w.cmd == nil {
		return
	}
	cancel, exited, stdin, lines := w.cancel, w.exited, w.stdin, w.lines
	w.cmd, w.cancel, w.stdin = nil, nil, nil
	// Late output of the stopped worker is discarded, never read as the
	// answer to a later request.
	go func() {
		for range lines {
		}
	}()
	if w.idle != nil {
		w.idle.Stop()
	}
	_ = stdin.Close()
	select {
	case <-exited:
	case <-time.After(500 * time.Millisecond):
		// Canceling the lifetime context kills the process.
		cancel()
		<-exited
	}
	cancel()
}

// Close stops the worker.
func (w *automationWorker) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stop()
}

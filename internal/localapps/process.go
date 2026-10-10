package localapps

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/procown"
)

// Log limits: the active file rotates to app.log.1 at maxLogBytes, so at
// most two files (about 2 MiB) are kept per app.
const maxLogBytes = 1 << 20

// rotatingLog is an io.Writer that keeps one rotated generation.
type rotatingLog struct {
	mu   sync.Mutex
	path string
	f    *os.File
	n    int64
}

func openRotatingLog(path string) (*rotatingLog, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	info, _ := f.Stat()
	l := &rotatingLog{path: path, f: f}
	if info != nil {
		l.n = info.Size()
	}
	return l, nil
}

func (l *rotatingLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return len(p), nil
	}
	if l.n+int64(len(p)) > maxLogBytes {
		l.f.Close()
		_ = os.Remove(l.path + ".1")
		_ = os.Rename(l.path, l.path+".1")
		f, err := os.OpenFile(l.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			l.f = nil
			return len(p), nil
		}
		l.f, l.n = f, 0
	}
	chunk := p
	if int64(len(chunk)) > maxLogBytes {
		chunk = chunk[len(chunk)-maxLogBytes:]
	}
	n, _ := l.f.Write(chunk)
	l.n += int64(n)
	return len(p), nil
}

func (l *rotatingLog) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		l.f.Close()
		l.f = nil
	}
}

func (l *rotatingLog) marker(text string) {
	fmt.Fprintf(l, "\n=== %s %s ===\n", time.Now().Format(time.RFC3339), text)
}

// TailLog returns up to n last lines of the app's log, older file first.
func (m *Manager) TailLog(id string, n int) []string {
	n = min(max(n, 1), 400)
	var lines []string
	for _, p := range []string{m.LogPath(id) + ".1", m.LogPath(id)} {
		lines = append(lines, readLines(p)...)
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func readLines(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(io.LimitReader(f, 2*maxLogBytes))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}

// resolveArgv swaps the runtime placeholder for the checked executable.
func resolveArgv(argv []string, rt RuntimeInfo, exe string) []string {
	out := make([]string, len(argv))
	copy(out, argv)
	if len(out) > 0 && out[0] == rt.Placeholder {
		out[0] = exe
	}
	return out
}

// appEnv builds the child environment: Atlas's environment, the resolved
// references and the variables Atlas owns.
func (m *Manager) appEnv(rec Record, man Manifest, port int, dataDir string) ([]string, error) {
	env := os.Environ()
	set := func(k, v string) {
		prefix := k + "="
		for i, e := range env {
			if strings.HasPrefix(strings.ToUpper(e), strings.ToUpper(prefix)) {
				env[i] = prefix + v
				return
			}
		}
		env = append(env, prefix+v)
	}
	for name, ref := range man.Run.Env {
		switch {
		case ref.Value != "":
			set(name, ref.Value)
		case ref.FromEnv != "":
			v, ok := os.LookupEnv(ref.FromEnv)
			if !ok {
				return nil, opErr(ErrStartFailed, "Set "+ref.FromEnv+" in the environment Atlas runs in.", "the environment variable %s is not set", ref.FromEnv)
			}
			set(name, v)
		case ref.Secret != "":
			v, err := m.secret(rec.ID, ref.Secret)
			if err != nil {
				return nil, opErr(ErrStartFailed, "Save the secret "+ref.Secret+" for this app first.", "secret %s is not available: %v", ref.Secret, err)
			}
			set(name, v)
		}
	}
	set("PORT", strconv.Itoa(port))
	set("ATLAS_APP_DATA", dataDir)
	set("PYTHONUNBUFFERED", "1")
	set("PYTHONDONTWRITEBYTECODE", "1")
	return env, nil
}

// StartOptions tune a start.
type StartOptions struct {
	// Port overrides the manifest port for this run.
	Port int
}

// StartResult reports a start.
type StartResult struct {
	Outcome string `json:"outcome"` // started, already_running
	URL     string `json:"url"`
	Port    int    `json:"port"`
	PID     int    `json:"pid"`
}

// Start runs the app with its real data and waits until its health check
// passes. It distinguishes a missing runtime, a busy port, a crash before
// readiness and a health timeout.
func (m *Manager) Start(ctx context.Context, id string, opts StartOptions) (StartResult, error) {
	release, err := m.acquire(id, "start")
	if err != nil {
		return StartResult{}, err
	}
	defer release()
	rec, err := m.record(id)
	if err != nil {
		return StartResult{}, err
	}
	m.startJournal(id, "start")
	res, err := m.startLocked(ctx, rec, opts)
	m.finishJournal(id, err)
	return res, err
}

func (m *Manager) startLocked(ctx context.Context, rec Record, opts StartOptions) (StartResult, error) {
	app := m.view(rec, false)
	if app.Running {
		return StartResult{Outcome: "already_running", URL: app.URL, Port: portOf(rec), PID: pidOf(rec)}, nil
	}
	if app.Manifest == nil {
		return StartResult{}, opErr(ErrInvalidState, "Fix the manifest, then start again.", "the app cannot start: %s", cmp.Or(app.ManifestError, strings.Join(app.Diagnostics, " ")))
	}
	man := *app.Manifest
	rt, _ := RuntimeFor(man.Runtime)
	m.stage(rec.ID, StageDependencies, StageRunning, rt.Name)
	status := m.checkRuntime(ctx, man.Runtime)
	if !status.Available {
		m.stage(rec.ID, StageDependencies, StageFailed, status.Problem)
		return StartResult{}, &OpError{Code: ErrRuntimeMissing, Msg: cmp.Or(status.Problem, rt.Name+" is not available"), Remedy: cmp.Or(status.Remedy, rt.Remedy)}
	}
	m.stage(rec.ID, StageDependencies, StagePassed, status.Version)
	port := cmp.Or(opts.Port, man.Port)
	if port == 0 {
		p, err := pickPort(0)
		if err != nil {
			return StartResult{}, err
		}
		port = p
	} else if listening(port) {
		m.stage(rec.ID, StageStarting, StageFailed, "port in use")
		return StartResult{}, opErr(ErrPortInUse, "Close the program using it, or start this app on another port.", "port %d is already used by another program; Atlas will not stop it", port)
	}
	res, _, err := m.launch(ctx, rec, man, rt, status.Executable, port, m.dataDir(rec.Root, man), true)
	return res, err
}

func portOf(rec Record) int {
	if rec.Process != nil {
		return rec.Process.Port
	}
	return 0
}

func pidOf(rec Record) int {
	if rec.Process != nil {
		return rec.Process.Identity.PID
	}
	return 0
}

func (m *Manager) dataDir(root string, man Manifest) string {
	return filepath.Join(root, filepath.FromSlash(man.DataDirs[0]))
}

// launch starts the process and waits for health. persist records it as the
// app's running process; verification runs are never recorded.
func (m *Manager) launch(ctx context.Context, rec Record, man Manifest, rt RuntimeInfo, exe string, port int, dataDir string, persist bool) (StartResult, *procown.Proc, error) {
	m.stage(rec.ID, StageStarting, StageRunning, fmt.Sprintf("port %d", port))
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		m.stage(rec.ID, StageStarting, StageFailed, err.Error())
		return StartResult{}, nil, err
	}
	env, err := m.appEnv(rec, man, port, dataDir)
	if err != nil {
		m.stage(rec.ID, StageStarting, StageFailed, err.Error())
		return StartResult{}, nil, err
	}
	logPath := m.LogPath(rec.ID)
	if !persist {
		logPath = filepath.Join(m.logDir(rec.ID), "verify.log")
	}
	log, err := openRotatingLog(logPath)
	if err != nil {
		return StartResult{}, nil, err
	}
	log.marker(fmt.Sprintf("start on port %d", port))
	cwd := rec.Root
	if man.Run.Cwd != "" && man.Run.Cwd != "." {
		cwd = filepath.Join(rec.Root, filepath.FromSlash(man.Run.Cwd))
	}
	proc, err := procown.Start(procown.Spec{Argv: resolveArgv(man.Run.Argv, rt, exe), Dir: cwd, Env: env, Stdout: log, Stderr: log})
	if err != nil {
		log.Close()
		m.stage(rec.ID, StageStarting, StageFailed, err.Error())
		return StartResult{}, nil, opErr(ErrStartFailed, "Check the run command in the manifest.", "could not start the app: %v", err)
	}
	go func() { <-proc.Done(); log.Close() }()
	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, cmp.Or(man.URLPath, "/"))
	if persist {
		m.mu.Lock()
		m.procs[rec.ID] = &runningApp{proc: proc, port: port, url: url, startedAt: time.Now()}
		m.mu.Unlock()
	}
	if err := m.waitHealthy(ctx, rec.ID, proc, man, port, logPath); err != nil {
		proc.Stop(3 * time.Second)
		if persist {
			m.mu.Lock()
			delete(m.procs, rec.ID)
			m.mu.Unlock()
		}
		m.stage(rec.ID, StageStarting, StageFailed, err.Error())
		return StartResult{}, nil, err
	}
	m.stage(rec.ID, StageStarting, StagePassed, url)
	res := StartResult{Outcome: "started", URL: url, Port: port, PID: proc.Identity.PID}
	if persist {
		_, err := m.mutate(ctx, rec.ID, func(r *Record) error {
			r.Process = &ProcessRecord{Identity: proc.Identity, Port: port, URL: url, StartedAt: time.Now(), AtlasPID: os.Getpid()}
			r.LastError = ""
			return nil
		})
		if err != nil {
			proc.Stop(3 * time.Second)
			m.mu.Lock()
			delete(m.procs, rec.ID)
			m.mu.Unlock()
			return StartResult{}, nil, fmt.Errorf("the app started but its record could not be saved, so it was stopped: %w", err)
		}
	} else {
		res.Outcome = "started_for_verification"
	}
	return res, proc, nil
}

// waitHealthy polls the health endpoint until it passes, the process exits
// or the timeout ends.
func (m *Manager) waitHealthy(ctx context.Context, id string, proc *procown.Proc, man Manifest, port int, logPath string) error {
	timeout := time.Duration(cmp.Or(man.Health.TimeoutSeconds, 20)) * time.Second
	timeout = min(timeout, 2*time.Minute)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, man.Health.Path)
	expect := cmp.Or(man.Health.ExpectStatus, http.StatusOK)
	last := "no answer yet"
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if resp, err := client.Do(req); err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
			if resp.StatusCode == expect && (man.Health.ExpectContains == "" || strings.Contains(string(body), man.Health.ExpectContains)) {
				return nil
			}
			last = fmt.Sprintf("health check answered HTTP %d", resp.StatusCode)
		}
		select {
		case <-proc.Done():
			code, _ := proc.Result()
			e := opErr(ErrEarlyExit, "Read the log lines below, fix the cause and start again.", "the app exited with code %d before it was ready", code)
			e.LogTail = tailFile(logPath, 25)
			return e
		default:
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				return ctx.Err()
			}
			e := opErr(ErrHealthTimeout, "Read the log lines below; the app may be waiting on something or listening on another port.", "the app did not pass its health check within %s (%s)", timeout, last)
			e.LogTail = tailFile(logPath, 25)
			return e
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func tailFile(path string, n int) []string {
	lines := readLines(path)
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// Stop stops the app only if Atlas started it and can still identify the
// process.
func (m *Manager) Stop(ctx context.Context, id string) (string, error) {
	release, err := m.acquire(id, "stop")
	if err != nil {
		return "", err
	}
	defer release()
	rec, err := m.record(id)
	if err != nil {
		return "", err
	}
	return m.stopLocked(ctx, rec)
}

// stopLocked stops an owned process. Caller holds the app's busy lock.
func (m *Manager) stopLocked(ctx context.Context, rec Record) (string, error) {
	m.mu.Lock()
	live := m.procs[rec.ID]
	delete(m.procs, rec.ID)
	m.mu.Unlock()
	outcome := "not_running"
	switch {
	case live != nil:
		select {
		case <-live.proc.Done():
		default:
			live.proc.Stop(5 * time.Second)
			outcome = "stopped"
		}
	case rec.Process != nil:
		if err := procown.KillOwned(rec.Process.Identity); err == nil {
			outcome = "stopped"
		} else if !errors.Is(err, procown.ErrNotOwned) {
			return "", fmt.Errorf("stop the app: %w", err)
		}
	}
	if rec.Process != nil {
		if _, err := m.mutate(ctx, rec.ID, func(r *Record) error {
			r.Process = nil
			return nil
		}); err != nil && !errors.Is(err, ErrNotFound) {
			return outcome, err
		}
	}
	if outcome == "stopped" {
		if l, err := openRotatingLog(m.LogPath(rec.ID)); err == nil {
			l.marker("stopped by Atlas")
			l.Close()
		}
	}
	return outcome, nil
}

// Health checks a running app's health endpoint once.
func (m *Manager) Health(ctx context.Context, id string) (bool, string, error) {
	app, err := m.Get(ctx, id)
	if err != nil {
		return false, "", err
	}
	if !app.Running || app.Manifest == nil {
		return false, "not running", nil
	}
	port := portOf(app.Record)
	m.mu.Lock()
	if live := m.procs[id]; live != nil {
		port = live.port
	}
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d%s", port, app.Manifest.Health.Path), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err.Error(), nil
	}
	resp.Body.Close()
	ok := resp.StatusCode == cmp.Or(app.Manifest.Health.ExpectStatus, http.StatusOK)
	return ok, fmt.Sprintf("HTTP %d", resp.StatusCode), nil
}

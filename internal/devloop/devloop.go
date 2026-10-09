// Package devloop runs the "change code → run the app → exercise the real
// flow → fix → re-run" loop with explicit ownership and limits. It starts
// the project's app as an owned process, checks readiness with bounded
// probes instead of fixed sleeps, never stops a process it did not start,
// and records every iteration so the final report links each acceptance
// criterion to the action, the observation and the result.
package devloop

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/procown"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/transfer"
)

// Limits.
const (
	DefaultMaxIterations = 5
	HardMaxIterations    = 10
	DefaultDeadline      = 30 * time.Minute
	maxLogBytes          = 8 << 20
	maxReadyWait         = 3 * time.Minute
)

// Criterion is one observable acceptance criterion.
type Criterion struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	// Verify is "scenario" (checked by scenario assertions) or
	// "unverifiable" (needs something this run cannot reach).
	Verify string `json:"verify"`
	// Assertions are 1-based indices of scenario assertions that must pass.
	Assertions []int  `json:"assertions,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// CriterionResult links a criterion to evidence.
type CriterionResult struct {
	ID          string `json:"id"`
	Result      string `json:"result"` // passed, failed, unverified
	Action      string `json:"action"`
	Observation string `json:"observation"`
}

// Iteration is one check run.
type Iteration struct {
	N           int               `json:"n"`
	StartedAt   int64             `json:"started_at"`
	EndedAt     int64             `json:"ended_at"`
	ScenarioRun string            `json:"scenario_run_id,omitempty"`
	Passed      bool              `json:"passed"`
	Criteria    []CriterionResult `json:"criteria"`
	LogExcerpt  []string          `json:"log_excerpt,omitempty"`
	ChangeNote  string            `json:"change_note,omitempty"`
	Error       string            `json:"error,omitempty"`
}

// Process is the ownership record of the app under test.
type Process struct {
	Argv      []string         `json:"argv"`
	Cwd       string           `json:"cwd"`
	Identity  procown.Identity `json:"identity"`
	Port      int              `json:"port,omitempty"`
	URL       string           `json:"url,omitempty"`
	LogFile   string           `json:"log_file"`
	StartedAt int64            `json:"started_at"`
	// Attached is true when an existing server (not started by Atlas) is
	// used; Atlas never stops an attached process.
	Attached bool `json:"attached"`
}

// Loop is the persisted state of one dev loop.
type Loop struct {
	ID            string          `json:"id"`
	SessionID     string          `json:"session_id"`
	State         taskstate.State `json:"state"`
	Criteria      []Criterion     `json:"criteria"`
	MaxIterations int             `json:"max_iterations"`
	Deadline      int64           `json:"deadline"`
	Iterations    []Iteration     `json:"iterations"`
	Process       *Process        `json:"process,omitempty"`
	CreatedAt     int64           `json:"created_at"`
	Reason        string          `json:"reason,omitempty"`
}

var loopID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,47}$`)

// Manager keeps loops and their live processes.
type Manager struct {
	dir   string
	mu    sync.Mutex
	procs map[string]*procown.Proc
}

// NewManager stores loops under dir.
func NewManager(dir string) *Manager { return &Manager{dir: dir, procs: map[string]*procown.Proc{}} }

func (m *Manager) path(id string) string { return filepath.Join(m.dir, id+".json") }

// Get reads a loop.
func (m *Manager) Get(id string) (Loop, error) {
	if !loopID.MatchString(id) {
		return Loop{}, fmt.Errorf("invalid loop id %q", id)
	}
	data, err := os.ReadFile(m.path(id))
	if err != nil {
		return Loop{}, err
	}
	var l Loop
	return l, jsonUnmarshal(data, &l)
}

// Save persists a loop; callers report success only after it returns nil.
func (m *Manager) Save(l Loop) error { return taskstate.WriteJSONAtomic(m.path(l.ID), l) }

// Plan creates or replaces a loop's criteria and limits. Replacing is only
// allowed before starting an app or running an iteration. Process ownership
// and evidence must never be discarded by replanning.
func (m *Manager) Plan(id, session string, criteria []Criterion, maxIter int, deadline time.Duration) (Loop, error) {
	if !loopID.MatchString(id) {
		return Loop{}, fmt.Errorf("loop_id must be 1-48 lowercase letters, digits, '_' or '-'")
	}
	if len(criteria) == 0 || len(criteria) > 32 {
		return Loop{}, errors.New("plan needs 1-32 acceptance criteria")
	}
	seen := map[string]bool{}
	for _, c := range criteria {
		if c.ID == "" || seen[c.ID] || strings.TrimSpace(c.Text) == "" {
			return Loop{}, fmt.Errorf("criteria need unique ids and text")
		}
		seen[c.ID] = true
		switch c.Verify {
		case "scenario":
			if len(c.Assertions) == 0 {
				return Loop{}, fmt.Errorf("criterion %s: scenario criteria name the assertions that prove them", c.ID)
			}
		case "unverifiable":
			if c.Reason == "" {
				return Loop{}, fmt.Errorf("criterion %s: say why it cannot be verified here", c.ID)
			}
		default:
			return Loop{}, fmt.Errorf("criterion %s: verify must be scenario or unverifiable", c.ID)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if prev, err := m.Get(id); err == nil {
		if prev.SessionID != session || prev.Process != nil || len(prev.Iterations) > 0 || prev.State.Terminal() {
			return prev, fmt.Errorf("loop %s has already started or belongs to another session; its criteria are fixed. Start a new loop_id for new criteria", id)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Loop{}, fmt.Errorf("read existing loop before replanning: %w", err)
	}
	if maxIter <= 0 {
		maxIter = DefaultMaxIterations
	}
	if deadline <= 0 {
		deadline = DefaultDeadline
	}
	l := Loop{ID: id, SessionID: session, State: taskstate.Running, Criteria: criteria, MaxIterations: min(maxIter, HardMaxIterations), Deadline: time.Now().Add(min(deadline, 2*time.Hour)).UnixMilli(), CreatedAt: time.Now().UnixMilli()}
	return l, m.Save(l)
}

// cappedFile writes up to maxLogBytes.
type cappedFile struct {
	mu sync.Mutex
	f  *os.File
	n  int64
}

func (c *cappedFile) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if room := maxLogBytes - c.n; room > 0 {
		chunk := p
		if int64(len(chunk)) > room {
			chunk = chunk[:room]
		}
		n, _ := c.f.Write(chunk)
		c.n += int64(n)
	}
	return len(p), nil
}

// Listening reports whether something accepts TCP on localhost:port.
func Listening(port int) bool {
	d := net.Dialer{Timeout: 500 * time.Millisecond}
	conn, err := d.DialContext(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// StartResult says what Start did.
type StartResult struct {
	Outcome string   `json:"outcome"` // started, already_running_owned, attached_existing
	Process *Process `json:"process"`
}

// Start launches argv in root/cwd as an owned process. When port is given
// and something already listens there, Atlas attaches instead of starting
// a second copy, and records that it does not own it.
func (m *Manager) Start(l *Loop, root string, argv []string, cwd string, port int, url string) (StartResult, error) {
	if l.State.Terminal() {
		return StartResult{}, fmt.Errorf("loop %s is %s", l.ID, l.State)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if p := m.procs[l.ID]; p != nil {
		select {
		case <-p.Done():
			delete(m.procs, l.ID)
		default:
			return StartResult{Outcome: "already_running_owned", Process: l.Process}, nil
		}
	}
	if port > 0 && Listening(port) {
		if l.Process != nil && !l.Process.Attached && procown.Matches(l.Process.Identity) {
			return StartResult{Outcome: "already_running_owned", Process: l.Process}, nil
		}
		l.Process = &Process{Port: port, URL: url, Attached: true, StartedAt: time.Now().UnixMilli()}
		return StartResult{Outcome: "attached_existing", Process: l.Process}, m.Save(*l)
	}
	if len(argv) == 0 {
		return StartResult{}, errors.New("start needs argv (the project's real run command)")
	}
	dir := root
	if cwd != "" && cwd != "." {
		d, err := transfer.Resolve(root, cwd)
		if err != nil {
			return StartResult{}, err
		}
		dir = d
	}
	logPath := filepath.Join(m.dir, l.ID+".log")
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return StartResult{}, err
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return StartResult{}, err
	}
	w := &cappedFile{f: f}
	proc, err := procown.Start(procown.Spec{Argv: argv, Dir: dir, Stdout: w, Stderr: w})
	if err != nil {
		f.Close()
		return StartResult{}, fmt.Errorf("start app: %w", err)
	}
	go func() { <-proc.Done(); f.Close() }()
	m.procs[l.ID] = proc
	l.Process = &Process{Argv: argv, Cwd: cwd, Identity: proc.Identity, Port: port, URL: url, LogFile: logPath, StartedAt: time.Now().UnixMilli()}
	if err := m.Save(*l); err != nil {
		proc.Stop(3 * time.Second)
		return StartResult{}, err
	}
	return StartResult{Outcome: "started", Process: l.Process}, nil
}

// Probe is a readiness signal.
type Probe struct {
	Kind     string `json:"kind"` // http, tcp, log
	URL      string `json:"url,omitempty"`
	Contains string `json:"contains,omitempty"`
	Port     int    `json:"port,omitempty"`
	Pattern  string `json:"pattern,omitempty"`
}

// ReadyResult reports a readiness wait.
type ReadyResult struct {
	Ready    bool     `json:"ready"`
	Elapsed  string   `json:"elapsed"`
	Detail   string   `json:"detail"`
	Exited   bool     `json:"exited,omitempty"`
	ExitCode int      `json:"exit_code,omitempty"`
	LogTail  []string `json:"log_tail,omitempty"`
}

// Ready polls probe until it passes, the owned process exits, or timeout.
func (m *Manager) Ready(ctx context.Context, l Loop, p Probe, timeout time.Duration) (ReadyResult, error) {
	if timeout <= 0 || timeout > maxReadyWait {
		timeout = min(max(timeout, 0), maxReadyWait)
		if timeout == 0 {
			timeout = 60 * time.Second
		}
	}
	var re *regexp.Regexp
	switch p.Kind {
	case "http":
		if !strings.HasPrefix(p.URL, "http://") && !strings.HasPrefix(p.URL, "https://") {
			return ReadyResult{}, errors.New("http probe needs an http(s) url")
		}
	case "tcp":
		if p.Port <= 0 || p.Port > 65535 {
			return ReadyResult{}, errors.New("tcp probe needs port")
		}
	case "log":
		var err error
		if re, err = regexp.Compile(p.Pattern); err != nil || p.Pattern == "" {
			return ReadyResult{}, fmt.Errorf("log probe needs a valid pattern: %v", err)
		}
		if l.Process == nil || l.Process.LogFile == "" {
			return ReadyResult{}, errors.New("log probe needs an owned process started by app_loop")
		}
	default:
		return ReadyResult{}, errors.New("probe kind must be http, tcp or log")
	}
	m.mu.Lock()
	proc := m.procs[l.ID]
	m.mu.Unlock()
	client := &http.Client{Timeout: 3 * time.Second}
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	last := ""
	for {
		ok := false
		switch p.Kind {
		case "http":
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
			if resp, err := client.Do(req); err == nil {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
				resp.Body.Close()
				ok = resp.StatusCode >= 200 && resp.StatusCode < 400 && (p.Contains == "" || strings.Contains(string(body), p.Contains))
				last = fmt.Sprintf("HTTP %d", resp.StatusCode)
				if !ok && p.Contains != "" && resp.StatusCode < 400 {
					last += "; body does not contain the expected text yet"
				}
			} else {
				last = err.Error()
			}
		case "tcp":
			ok = Listening(p.Port)
			last = "port " + strconv.Itoa(p.Port) + " not accepting connections"
		case "log":
			lines, _ := TailLines(l.Process.LogFile, 400)
			for _, line := range lines {
				if re.MatchString(line) {
					ok, last = true, line
					break
				}
			}
			if !ok {
				last = "pattern not in log yet"
			}
		}
		if ok {
			return ReadyResult{Ready: true, Elapsed: time.Since(start).Round(time.Millisecond).String(), Detail: last}, nil
		}
		if proc != nil {
			select {
			case <-proc.Done():
				code, _ := proc.Result()
				res := ReadyResult{Elapsed: time.Since(start).Round(time.Millisecond).String(), Detail: "the app process exited before becoming ready", Exited: true, ExitCode: code}
				if l.Process != nil {
					res.LogTail, _ = TailLines(l.Process.LogFile, 40)
				}
				return res, nil
			default:
			}
		}
		select {
		case <-ctx.Done():
			res := ReadyResult{Elapsed: time.Since(start).Round(time.Millisecond).String(), Detail: "not ready within " + timeout.String() + ": " + last}
			if l.Process != nil && l.Process.LogFile != "" {
				res.LogTail, _ = TailLines(l.Process.LogFile, 40)
			}
			return res, nil
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// Alive reports whether the owned process still runs, with exit code.
func (m *Manager) Alive(l Loop) (bool, int) {
	m.mu.Lock()
	proc := m.procs[l.ID]
	m.mu.Unlock()
	if proc != nil {
		select {
		case <-proc.Done():
			code, _ := proc.Result()
			return false, code
		default:
			return true, 0
		}
	}
	if l.Process != nil && !l.Process.Attached {
		return procown.Matches(l.Process.Identity), -1
	}
	return true, 0
}

// Stop ends the loop's process only when Atlas owns it. An attached server
// is left alone and reported as such.
func (m *Manager) Stop(l *Loop) (string, error) {
	if l.Process == nil {
		return "no_process", nil
	}
	if l.Process.Attached {
		return "left_running_not_owned", nil
	}
	m.mu.Lock()
	proc := m.procs[l.ID]
	delete(m.procs, l.ID)
	m.mu.Unlock()
	if proc != nil {
		proc.Stop(5 * time.Second)
		return "stopped", nil
	}
	if err := procown.KillOwned(l.Process.Identity); err != nil {
		if errors.Is(err, procown.ErrNotOwned) {
			return "already_exited", nil
		}
		return "", err
	}
	return "stopped", nil
}

// LogSize returns the current size of the app log, used to bracket the
// lines produced during one check.
func LogSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// LinesSince returns up to n lines written after offset.
func LinesSince(path string, offset int64, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil
	}
	var lines []string
	sc := bufio.NewScanner(io.LimitReader(f, 4<<20))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	return lines
}

// TailLines returns the last n lines of a file.
func TailLines(path string, n int) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	off := max(int64(0), info.Size()-(1<<20))
	return LinesSince(path, off, n), nil
}

// Grade turns failed assertion indices into criterion results.
func Grade(criteria []Criterion, scenarioErr error, failed map[int]bool, ran bool, action string, appDown string) ([]CriterionResult, bool) {
	all := true
	var out []CriterionResult
	for _, c := range criteria {
		r := CriterionResult{ID: c.ID, Action: action}
		switch {
		case c.Verify == "unverifiable":
			r.Result, r.Observation = "unverified", "not verifiable in this run: "+c.Reason
		case appDown != "":
			r.Result, r.Observation = "failed", appDown
			all = false
		case scenarioErr != nil:
			r.Result, r.Observation = "failed", "scenario did not complete: "+scenarioErr.Error()
			all = false
		case !ran:
			r.Result, r.Observation = "failed", "no observed scenario run"
			all = false
		default:
			var bad []string
			for _, a := range c.Assertions {
				if failed[a] {
					bad = append(bad, "#"+strconv.Itoa(a))
				}
			}
			if len(bad) > 0 {
				r.Result, r.Observation = "failed", "assertions "+strings.Join(bad, ", ")+" failed in the real UI"
				all = false
			} else {
				r.Result, r.Observation = "passed", fmt.Sprintf("assertions %v passed in the real UI", c.Assertions)
			}
		}
		out = append(out, r)
	}
	return out, all
}

var gapIndex = regexp.MustCompile(`^Assertion failed: \S+ #(\d+)`)

// FailedAssertions parses scenario gap lines into 1-based indices.
func FailedAssertions(gaps []string) map[int]bool {
	out := map[int]bool{}
	for _, g := range gaps {
		if m := gapIndex.FindStringSubmatch(g); m != nil {
			n, _ := strconv.Atoi(m[1])
			out[n] = true
		}
	}
	return out
}

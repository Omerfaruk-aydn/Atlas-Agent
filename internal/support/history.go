package support

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
)

// Event is one history line. It never holds codes, tokens, passwords or
// file contents: commands are reduced to their program name and argument
// count, transfers to path, size and hash.
type Event struct {
	State        string            `json:"state,omitempty"`
	Effect       string            `json:"effect,omitempty"`
	Detail       string            `json:"detail,omitempty"`
	Fields       map[string]string `json:"fields,omitempty"`
	TaskID       string            `json:"task_id,omitempty"`
	AgentSession string            `json:"agent_session,omitempty"`
	WorkflowID   string            `json:"workflow_id,omitempty"`
	StepID       string            `json:"step_id,omitempty"`
	StartedAt    int64             `json:"started_at,omitempty"`
	EndedAt      int64             `json:"ended_at,omitempty"`
	Dispatch     string            `json:"dispatch,omitempty"`
	Verification string            `json:"verification,omitempty"`
}

// History appends events to one JSONL file per session.
type History struct {
	dir string
	mu  sync.Mutex
	now func() time.Time
}

// OpenHistory uses dir; it is created on first write.
func OpenHistory(dir string) *History { return &History{dir: dir, now: time.Now} }

var sessionIDPattern = regexp.MustCompile(`^[a-z0-9-]{8,64}$`)

// Append records e. Failures are returned so callers can surface them,
// but history never blocks or changes an operation's result.
func (h *History) Append(e Event) error {
	if h == nil || !sessionIDPattern.MatchString(e.SessionID) {
		return nil
	}
	if e.At == 0 {
		e.At = h.now().UnixMilli()
	}
	e = sanitizeEvent(e)
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := os.MkdirAll(h.dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(h.dir, e.SessionID+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

func sanitizeEvent(e Event) Event {
	e.Detail = redact(e.Detail)
	if e.Fields != nil {
		fields := make(map[string]string, len(e.Fields))
		for key, value := range e.Fields {
			fields[key] = redact(value)
		}
		e.Fields = fields
	}
	return e
}

// RecentEvents loads a bounded tail without retaining an entire long session.
func (h *History) RecentEvents(sessionID string, n int) ([]Event, error) {
	if h == nil || !sessionIDPattern.MatchString(sessionID) {
		return nil, errors.New("invalid session id")
	}
	n = min(max(n, 1), 1000)
	f, err := os.Open(filepath.Join(h.dir, sessionID+".jsonl"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	out := make([]Event, 0, n)
	for sc.Scan() {
		var e Event
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		if len(out) == n {
			copy(out, out[1:])
			out = out[:n-1]
		}
		out = append(out, sanitizeEvent(e))
	}
	return out, sc.Err()
}

// Events reads one session's history.
func (h *History) Events(sessionID string) ([]Event, error) {
	if !sessionIDPattern.MatchString(sessionID) {
		return nil, errors.New("invalid session id")
	}
	f, err := os.Open(filepath.Join(h.dir, sessionID+".jsonl"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var e Event
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			out = append(out, e)
		}
	}
	return out, sc.Err()
}

// Summary is one session in the history list.
type Summary struct {
	SessionID string `json:"session_id"`
	Role      string `json:"role"`
	Peer      string `json:"peer"`
	Started   int64  `json:"started"`
	Ended     int64  `json:"ended,omitempty"`
	EndState  string `json:"end_state,omitempty"`
	Events    int    `json:"events"`
}

// Sessions lists recorded sessions, newest first.
func (h *History) Sessions() ([]Summary, error) {
	entries, err := os.ReadDir(h.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Summary
	for _, entry := range entries {
		id, ok := strings.CutSuffix(entry.Name(), ".jsonl")
		if !ok {
			continue
		}
		events, err := h.Events(id)
		if err != nil || len(events) == 0 {
			continue
		}
		s := Summary{SessionID: id, Role: events[0].Role, Started: events[0].At, Events: len(events)}
		for _, e := range events {
			if e.Peer != "" {
				s.Peer = e.Peer
			}
			if e.Kind == "session_ended" {
				s.Ended, s.EndState = e.At, e.State
			}
		}
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b Summary) int { return int(b.Started - a.Started) })
	return out, nil
}

var secretLike = regexp.MustCompile(`(?i)(password|passwd|pwd|token|secret|apikey|api_key|bearer)(\s*[=:]\s*|\s+)\S+`)

// redact removes obvious credentials from free text.
func redact(s string) string {
	if len(s) > 512 {
		s = s[:512] + "…"
	}
	return secretLike.ReplaceAllString(s, "$1$2[redacted]")
}

// CommandSummary keeps the program and argument count of a command, which
// identifies what ran without recording arguments that may hold secrets.
func CommandSummary(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	name := filepath.Base(strings.ReplaceAll(argv[0], "\\", "/"))
	if len(argv) == 1 {
		return name
	}
	return name + " (+" + strconv.Itoa(len(argv)-1) + " args)"
}

// sessionRecord persists a host session's lifecycle, so a restarted host
// never revives a session that was revoked, expired or interrupted.
type sessionRecord struct {
	ID                string `json:"id"`
	State             State  `json:"state"`
	Reason            string `json:"reason,omitempty"`
	ControllerName    string `json:"controller_name"`
	ControllerDevice  string `json:"controller_device"`
	ResumeHash        string `json:"resume_hash"`
	ExpiresAt         int64  `json:"expires_at"`
	UpdatedAt         int64  `json:"updated_at"`
	LastGeneration    uint64 `json:"last_generation"`
	CapabilitiesFinal string `json:"capabilities,omitempty"`
}

type sessionStore struct{ dir string }

func (s sessionStore) path(id string) string { return filepath.Join(s.dir, id+".json") }

func (s sessionStore) save(r sessionRecord) error {
	r.UpdatedAt = time.Now().UnixMilli()
	return taskstate.WriteJSONAtomic(s.path(r.ID), r)
}

func (s sessionStore) load(id string) (sessionRecord, bool) {
	if !sessionIDPattern.MatchString(id) {
		return sessionRecord{}, false
	}
	data, err := os.ReadFile(s.path(id))
	if err != nil {
		return sessionRecord{}, false
	}
	var r sessionRecord
	return r, json.Unmarshal(data, &r) == nil
}

// closeInterrupted marks every non-terminal record disconnected. It runs
// when a host starts: a session from a previous process cannot continue.
func (s sessionStore) closeInterrupted() {
	entries, _ := os.ReadDir(s.dir)
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		if r, ok := s.load(id); ok && !r.State.Terminal() {
			r.State, r.Reason = StateDisconnected, "host restarted; the session cannot continue"
			_ = s.save(r)
		}
	}
}

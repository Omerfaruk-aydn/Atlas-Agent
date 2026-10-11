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
	At          int64             `json:"at"`
	SessionID   string            `json:"session_id"`
	Role        string            `json:"role"`
	Kind        string            `json:"kind"`
	Device      string            `json:"device,omitempty"`
	Peer        string            `json:"peer,omitempty"`
	PeerDevice  string            `json:"peer_device,omitempty"`
	Generation  uint64            `json:"generation,omitempty"`
	OperationID string            `json:"operation_id,omitempty"`
	State       string            `json:"state,omitempty"`
	Effect      string            `json:"effect,omitempty"`
	Detail      string            `json:"detail,omitempty"`
	Fields      map[string]string `json:"fields,omitempty"`
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
	e.Detail = redact(e.Detail)
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


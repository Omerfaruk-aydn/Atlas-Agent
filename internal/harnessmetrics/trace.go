package harnessmetrics

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
	"github.com/google/uuid"
)

// TraceEvent contains metadata only. Payloads and raw errors have no field.
type TraceEvent struct {
	Version          int     `json:"version"`
	Time             int64   `json:"time"`
	Session          string  `json:"session"`
	TraceID          string  `json:"trace_id"`
	Turn             int     `json:"turn"`
	Step             int     `json:"step,omitempty"`
	Kind             string  `json:"kind"`
	Name             string  `json:"name,omitempty"`
	Gate             string  `json:"gate,omitempty"`
	CallID           string  `json:"call_id,omitempty"`
	Outcome          string  `json:"outcome,omitempty"`
	Reason           string  `json:"reason,omitempty"`
	DurationMS       int64   `json:"duration_ms,omitempty"`
	WaitMS           int64   `json:"wait_ms,omitempty"`
	Bytes            int     `json:"bytes,omitempty"`
	InputTokens      int64   `json:"input_tokens,omitempty"`
	OutputTokens     int64   `json:"output_tokens,omitempty"`
	TotalTokens      int64   `json:"total_tokens,omitempty"`
	CacheReadTokens  int64   `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64   `json:"cache_write_tokens,omitempty"`
	Cost             float64 `json:"cost_usd,omitempty"`
	CostSource       string  `json:"cost_source,omitempty"`
	Estimated        bool    `json:"estimated,omitempty"`
}

// Trace binds asynchronous observations to the run that started them.
type Trace struct {
	r       *Recorder
	root    string
	Session string
	ID      string
	Turn    int
}

// Tokens preserves providers that report a total without an input/output split.
func (e TraceEvent) Tokens() int64 {
	if e.TotalTokens > 0 {
		return e.TotalTokens
	}
	return max(0, e.InputTokens) + max(0, e.OutputTokens) + max(0, e.CacheReadTokens) + max(0, e.CacheWriteTokens)
}

// Fixed stripes serialize one session across recorders without making unrelated
// sessions wait for each other's disk writes or growing a global lock registry.
var traceLocks [256]sync.Mutex

func traceLock(root, session string) *sync.Mutex {
	return traceFileLock(tracePath(root, session))
}

func traceFileLock(path string) *sync.Mutex {
	sum := sha256.Sum256([]byte(path))
	return &traceLocks[sum[0]]
}

func tracePath(root, session string) string {
	sum := sha256.Sum256([]byte(session))
	return filepath.Join(root, Dir, "traces", hex.EncodeToString(sum[:])+".jsonl")
}

// BeginTrace allocates a durable turn number without changing execution.
func (r *Recorder) BeginTrace(session string) *Trace {
	if r == nil || r.Root() == "" {
		return nil
	}
	now := time.Now()
	last := r.tracePrunedAt.Load()
	if now.Unix()-last >= 3600 && r.tracePrunedAt.CompareAndSwap(last, now.Unix()) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err := PruneTraces(ctx, r.Root(), now, 30*24*time.Hour, 512<<20)
		cancel()
		if err != nil {
			r.traceFailed(err)
		}
	}
	t := &Trace{r: r, root: r.Root(), Session: session, ID: uuid.NewString(), Turn: 1}
	lock := traceLock(t.root, t.Session)
	lock.Lock()
	defer lock.Unlock()
	counter := tracePath(t.root, session) + ".turn"
	data, err := os.ReadFile(counter)
	if err == nil {
		previous, parseErr := strconv.Atoi(string(data))
		if parseErr != nil || previous < 1 {
			r.traceFailed(fmt.Errorf("invalid trace turn counter"))
			return nil
		}
		t.Turn = previous + 1
	} else if errors.Is(err, fs.ErrNotExist) {
		events, readErr := loadTrace(t.root, session, 0)
		if readErr != nil {
			r.traceFailed(readErr)
			return nil
		}
		for _, event := range events {
			t.Turn = max(t.Turn, event.Turn+1)
		}
	} else {
		r.traceFailed(err)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(counter), 0o700); err != nil {
		r.traceFailed(err)
		return nil
	}
	if err := os.WriteFile(counter, []byte(strconv.Itoa(t.Turn)), 0o600); err != nil {
		r.traceFailed(err)
		return nil
	}
	t.recordLocked(TraceEvent{Kind: "turn_start"})
	return t
}

// Record writes a best-effort observation. It never returns a control error.
func (t *Trace) Record(event TraceEvent) {
	if t == nil {
		return
	}
	lock := traceLock(t.root, t.Session)
	lock.Lock()
	defer lock.Unlock()
	t.recordLocked(event)
}

func (t *Trace) recordLocked(event TraceEvent) {
	event.Version, event.Time = 1, time.Now().UnixMilli()
	event.Session, event.TraceID, event.Turn = t.Session, t.ID, t.Turn
	for _, value := range []*string{&event.Session, &event.TraceID, &event.Name, &event.Gate, &event.CallID, &event.Reason, &event.Outcome, &event.Kind} {
		*value = vault.Redact(t.Session, *value)
		if len(*value) > 512 {
			*value = strings.ToValidUTF8((*value)[:512], "")
		}
	}
	line, err := json.Marshal(event)
	if err == nil {
		path := tracePath(t.root, t.Session)
		err = os.MkdirAll(filepath.Dir(path), 0o700)
		if err == nil {
			if info, statErr := os.Stat(path); statErr == nil && info.Size() >= maxFileBytes {
				// Remove only the oldest trace generation, never project files.
				if removeErr := os.Remove(path + ".1"); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
					err = removeErr
				} else {
					err = os.Rename(path, path+".1")
				}
			}
			if err == nil {
				var file *os.File
				file, err = os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
				if err == nil {
					_, err = file.Write(append(line, '\n'))
					err = errors.Join(err, file.Close())
				}
			}
		}
	}
	if err != nil {
		t.r.traceFailed(err)
	}
}

func (r *Recorder) traceFailed(err error) {
	if r.traceFailures.Add(1) == 1 {
		slog.Warn("Failed to write harness trace; execution continues", "error", err)
	}
	r.Record(TraceWriteFailed, "", nil)
}

// TraceFailures counts failed detailed writes even when storage is unavailable.
func (r *Recorder) TraceFailures() uint64 {
	if r == nil {
		return 0
	}
	return r.traceFailures.Load()
}

// LoadTrace reads retained events. Missing old generations cannot be recovered.
func LoadTrace(root, session string, turn int) ([]TraceEvent, error) {
	if turn < 0 {
		return nil, fmt.Errorf("turn must be non-negative")
	}
	lock := traceLock(root, session)
	lock.Lock()
	defer lock.Unlock()
	return loadTrace(root, session, turn)
}

func loadTrace(root, session string, turn int) ([]TraceEvent, error) {
	var events []TraceEvent
	path := tracePath(root, session)
	for _, name := range []string{path + ".1", path} {
		f, err := os.Open(name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4096), 64<<10)
		for scanner.Scan() {
			var event TraceEvent
			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				_ = f.Close()
				return nil, fmt.Errorf("invalid retained trace: %w", err)
			}
			if event.Version != 1 {
				_ = f.Close()
				return nil, fmt.Errorf("unsupported trace version %d", event.Version)
			}
			if turn == 0 || event.Turn == turn {
				events = append(events, event)
			}
		}
		err = errors.Join(scanner.Err(), f.Close())
		if err != nil {
			return nil, err
		}
	}
	return events, nil
}

// GateSummary distinguishes verification failure from deliberate fail-open.
type GateSummary struct {
	Name       string  `json:"name"`
	Triggered  int     `json:"triggered"`
	Passed     int     `json:"passed"`
	SentBack   int     `json:"sent_back"`
	FailOpen   int     `json:"fail_open"`
	Unverified int     `json:"unverified"`
	Failed     int     `json:"failed"`
	Skipped    int     `json:"skipped"`
	Tokens     int64   `json:"tokens"`
	DurationMS int64   `json:"duration_ms"`
	Cost       float64 `json:"cost_usd"`
}

// SummarizeGates aggregates terminal gate observations, not nested model events.
func SummarizeGates(events []TraceEvent) []GateSummary {
	by := map[string]*GateSummary{}
	for _, event := range events {
		if event.Kind != "gate" {
			continue
		}
		s := by[event.Name]
		if s == nil {
			s = &GateSummary{Name: event.Name}
			by[event.Name] = s
		}
		if event.Outcome != "skipped" {
			s.Triggered++
		}
		s.Tokens += event.Tokens()
		s.DurationMS += event.DurationMS
		s.Cost += event.Cost
		switch event.Outcome {
		case "pass":
			s.Passed++
		case "send_back":
			s.SentBack++
		case "fail_open":
			s.FailOpen++
		case "unverified":
			s.Unverified++
		case "skipped":
			s.Skipped++
		default:
			s.Failed++
		}
	}
	out := make([]GateSummary, 0, len(by))
	for _, s := range by {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// FormatTrace renders retained metadata without exposing payloads.
func FormatTrace(events []TraceEvent) string {
	var b strings.Builder
	b.WriteString("Retained trace (bounded history; missing earlier events are not reconstructed):\n")
	for _, e := range events {
		fmt.Fprintf(&b, "turn=%d step=%d %-12s %-24s %-12s %s duration=%dms bytes=%d tokens=%d cache_read=%d cost=%.6f\n", e.Turn, e.Step, e.Kind, e.Name, e.Outcome, e.Reason, e.DurationMS, e.Bytes, e.Tokens(), e.CacheReadTokens, e.Cost)
	}
	return b.String()
}

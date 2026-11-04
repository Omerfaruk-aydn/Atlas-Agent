package voice

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// Direction says who placed a call.
type Direction string

// Directions.
const (
	DirectionInbound  Direction = "inbound"
	DirectionOutbound Direction = "outbound"
)

// CallMetrics are per-call measurements. Latencies are milliseconds from
// the moment the call connected (first transcript, answer, audio) or from
// the end of the user's utterance (answer latency of each turn is folded
// into the first value only).
type CallMetrics struct {
	Turns             int   `json:"turns"`
	ToolCalls         int   `json:"tool_calls"`
	BargeIns          int   `json:"barge_ins"`
	STTReconnects     int   `json:"stt_reconnects"`
	FirstTranscriptMS int64 `json:"first_transcript_ms,omitempty"`
	FirstAnswerMS     int64 `json:"first_answer_ms,omitempty"`
	FirstAudioMS      int64 `json:"first_audio_ms,omitempty"`
	AudioInBytes      int64 `json:"audio_in_bytes"`
	AudioOutBytes     int64 `json:"audio_out_bytes"`
	TTSCharacters     int   `json:"tts_characters"`
}

// TurnRecord is one exchange. Text is kept only when transcripts are on.
type TurnRecord struct {
	TurnID    string    `json:"turn_id"`
	RunID     string    `json:"run_id"`
	At        time.Time `json:"at"`
	User      string    `json:"user,omitempty"`
	Assistant string    `json:"assistant,omitempty"`
	Code      Code      `json:"code,omitempty"`
	Cancelled bool      `json:"cancelled,omitempty"`
}

// CallRecord is the stored view of one call. Peer numbers are masked.
type CallRecord struct {
	ID             string        `json:"id"`
	Direction      Direction     `json:"direction"`
	Transport      string        `json:"transport"`
	Provider       string        `json:"provider"`
	ProviderCallID string        `json:"provider_call_id,omitempty"`
	Peer           string        `json:"peer,omitempty"`
	EventID        string        `json:"event_id,omitempty"`
	Reason         string        `json:"reason,omitempty"`
	Priority       string        `json:"priority,omitempty"`
	State          State         `json:"state"`
	Code           Code          `json:"code,omitempty"`
	SessionID      string        `json:"session_id,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
	AnsweredAt     time.Time     `json:"answered_at,omitzero"`
	EndedAt        time.Time     `json:"ended_at,omitzero"`
	Metrics        CallMetrics   `json:"metrics"`
	Turns          []TurnRecord  `json:"turns,omitempty"`
	Summary        string        `json:"summary,omitempty"`
	History        []StateChange `json:"history,omitempty"`
	Fallback       []string      `json:"fallback,omitempty"`
}

// Seconds is the connected duration, rounded up.
func (r CallRecord) Seconds(now time.Time) int {
	if r.AnsweredAt.IsZero() {
		return 0
	}
	end := r.EndedAt
	if end.IsZero() {
		end = now
	}
	return int((end.Sub(r.AnsweredAt) + time.Second - 1) / time.Second)
}

// SiteState is the persisted state of one monitored site.
type SiteState struct {
	Name             string      `json:"name"`
	Health           SiteHealth  `json:"health"`
	Failures         int         `json:"failures"`
	LastCheck        time.Time   `json:"last_check,omitzero"`
	LastError        string      `json:"last_error,omitempty"`
	LastStatus       int         `json:"last_status,omitempty"`
	ChangedAt        time.Time   `json:"changed_at,omitzero"`
	NotifyOnRecovery bool        `json:"notify_on_recovery,omitempty"`
	Incident         int         `json:"incident"`
	Notified         []string    `json:"notified,omitempty"`
	Checks           []SiteCheck `json:"checks,omitempty"`
}

// SiteCheck is one recorded check result.
type SiteCheck struct {
	At     time.Time `json:"at"`
	Status int       `json:"status,omitempty"`
	Error  string    `json:"error,omitempty"`
	OK     bool      `json:"ok"`
}

type storeData struct {
	Calls  []CallRecord         `json:"calls"`
	Events map[string]string    `json:"events"`
	Sites  map[string]SiteState `json:"sites"`
}

const (
	maxStoredCalls  = 500
	maxStoredEvents = 2000
	maxSiteChecks   = 20
)

// Store persists call records, idempotency keys and monitor state in one
// private JSON file. A single voice service owns it.
type Store struct {
	path string
	mu   sync.Mutex
	data storeData
}

// OpenStore loads or creates the store at dir/calls.json.
func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create voice store: %w", err)
	}
	s := &Store{path: filepath.Join(dir, "calls.json")}
	b, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("read voice store: %w", err)
	default:
		if err := json.Unmarshal(b, &s.data); err != nil {
			return nil, fmt.Errorf("parse voice store: %w", err)
		}
	}
	if s.data.Events == nil {
		s.data.Events = map[string]string{}
	}
	if s.data.Sites == nil {
		s.data.Sites = map[string]SiteState{}
	}
	return s, nil
}

// NewMemoryStore returns a store that never touches disk, for tests.
func NewMemoryStore() *Store {
	return &Store{data: storeData{Events: map[string]string{}, Sites: map[string]SiteState{}}}
}

func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	if len(s.data.Calls) > maxStoredCalls {
		s.data.Calls = s.data.Calls[len(s.data.Calls)-maxStoredCalls:]
	}
	if len(s.data.Events) > maxStoredEvents {
		known := map[string]bool{}
		for _, c := range s.data.Calls {
			known[c.ID] = true
		}
		for k, v := range s.data.Events {
			if !known[v] {
				delete(s.data.Events, k)
			}
		}
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write voice store: %w", err)
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) indexLocked(id string) int {
	for i := len(s.data.Calls) - 1; i >= 0; i-- {
		if s.data.Calls[i].ID == id {
			return i
		}
	}
	return -1
}

// Create stores a new call. It fails with ErrCallExists when a live call
// with the same id or provider call id is stored.
func (s *Store) Create(r CallRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.data.Calls {
		if c.ID == r.ID || (r.ProviderCallID != "" && c.ProviderCallID == r.ProviderCallID) {
			return ErrCallExists
		}
	}
	s.data.Calls = append(s.data.Calls, r)
	return s.saveLocked()
}

// Update changes a stored call.
func (s *Store) Update(id string, fn func(*CallRecord)) (CallRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indexLocked(id)
	if i < 0 {
		return CallRecord{}, fmt.Errorf("call %s not found", id)
	}
	fn(&s.data.Calls[i])
	return s.data.Calls[i], s.saveLocked()
}

// Get returns one call.
func (s *Store) Get(id string) (CallRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indexLocked(id)
	if i < 0 {
		return CallRecord{}, false
	}
	return s.data.Calls[i], true
}

// ByProviderCallID finds a call by the transport's id.
func (s *Store) ByProviderCallID(pid string) (CallRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.data.Calls) - 1; i >= 0; i-- {
		if s.data.Calls[i].ProviderCallID == pid && pid != "" {
			return s.data.Calls[i], true
		}
	}
	return CallRecord{}, false
}

// List returns the newest calls first.
func (s *Store) List(limit int) []CallRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	limit = cmp.Or(limit, 20)
	out := make([]CallRecord, 0, min(limit, len(s.data.Calls)))
	for i := len(s.data.Calls) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.data.Calls[i])
	}
	return out
}

// ClaimEvent records that eventID is handled by callID. When the event was
// already claimed it returns the earlier call id and false.
func (s *Store) ClaimEvent(eventID, callID string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.data.Events[eventID]; ok {
		return prev, false, nil
	}
	s.data.Events[eventID] = callID
	return callID, true, s.saveLocked()
}

// ReleaseEvent forgets a claim whose call was never placed, so a later
// attempt may place it.
func (s *Store) ReleaseEvent(eventID, callID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Events[eventID] == callID {
		delete(s.data.Events, eventID)
	}
	return s.saveLocked()
}

// OutboundSince returns outbound calls created at or after t, oldest first.
func (s *Store) OutboundSince(t time.Time) []CallRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []CallRecord
	for _, c := range s.data.Calls {
		if c.Direction == DirectionOutbound && !c.CreatedAt.Before(t) {
			out = append(out, c)
		}
	}
	return out
}

// SecondsSince sums connected seconds of calls created at or after t.
func (s *Store) SecondsSince(t, now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, c := range s.data.Calls {
		if !c.CreatedAt.Before(t) {
			total += c.Seconds(now)
		}
	}
	return total
}

// Site returns a monitored site's state.
func (s *Store) Site(name string) SiteState {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.data.Sites[name]
	if !ok {
		return SiteState{Name: name, Health: HealthUnknown}
	}
	st.Checks = slices.Clone(st.Checks)
	st.Notified = slices.Clone(st.Notified)
	return st
}

// PutSite stores a monitored site's state.
func (s *Store) PutSite(st SiteState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(st.Checks) > maxSiteChecks {
		st.Checks = st.Checks[len(st.Checks)-maxSiteChecks:]
	}
	s.data.Sites[st.Name] = st
	return s.saveLocked()
}

// Sites returns every monitored site's state.
func (s *Store) Sites() []SiteState {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SiteState, 0, len(s.data.Sites))
	for _, st := range s.data.Sites {
		out = append(out, st)
	}
	slices.SortFunc(out, func(a, b SiteState) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

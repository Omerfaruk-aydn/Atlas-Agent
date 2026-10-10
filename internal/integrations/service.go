package integrations

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/pubsub"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
)

// Adapter turns one subsystem into integration items. Adapters report only
// actions they really implement for each item.
type Adapter interface {
	// Items lists the adapter's integrations. It may read local files and
	// look up executables, but must not use the network or start programs.
	Items(ctx context.Context, fp Fingerprinter) []Item
	// Check runs the cheap, side-effect free check for one item.
	Check(ctx context.Context, item Item) CheckResult
	// Do performs a supported non-check action.
	Do(ctx context.Context, item Item, action Action) (i18n.Message, error)
}

// Fingerprinter hashes the configuration a check result belongs to. The
// salt is random per process, so fingerprints of secrets cannot be
// compared across processes or recovered from anything Atlas stores.
type Fingerprinter interface {
	Fingerprint(parts ...any) string
}

// Errors.
var (
	ErrNotFound    = errors.New("integration not found")
	ErrUnsupported = errors.New("this integration does not support that action")
)

// Options configures a Service.
type Options struct {
	// StatePath stores last check results for the next start, where they
	// are shown as history only. Empty disables persistence.
	StatePath string
	// Timeout bounds one check. Defaults to 15s.
	Timeout time.Duration
	// Concurrency bounds parallel checks. Defaults to 4.
	Concurrency int
}

// Service merges adapter items with check results.
type Service struct {
	adapters  []Adapter
	salt      []byte
	timeout   time.Duration
	sem       chan struct{}
	statePath string
	broker    *pubsub.Broker[Event]
	secrets   *Secrets

	mu       sync.Mutex
	results  map[string]stored
	running  map[string]*inflight
	gen      uint64
	previous map[string]PreviousCheck
	notes    map[string]i18n.Message
}

type stored struct {
	res         CheckResult
	at          time.Time
	fingerprint string
}

type inflight struct {
	gen    uint64
	cancel context.CancelFunc
}

// New builds a service. Adapters are added with Add.
func New(opts Options) *Service {
	salt := make([]byte, 32)
	_, _ = rand.Read(salt)
	s := &Service{
		salt:      salt,
		timeout:   cmp.Or(opts.Timeout, 15*time.Second),
		sem:       make(chan struct{}, max(1, cmp.Or(opts.Concurrency, 4))),
		statePath: opts.StatePath,
		broker:    pubsub.NewBroker[Event](),
		secrets:   &Secrets{},
		results:   map[string]stored{},
		running:   map[string]*inflight{},
		previous:  map[string]PreviousCheck{},
		notes:     map[string]i18n.Message{},
	}
	s.loadPrevious()
	return s
}

// Add registers an adapter.
func (s *Service) Add(a Adapter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adapters = append(s.adapters, a)
}

// Secrets returns the redaction set adapters register resolved secrets in.
func (s *Service) Secrets() *Secrets { return s.secrets }

// Subscribe streams service events.
func (s *Service) Subscribe(ctx context.Context) <-chan pubsub.Event[Event] {
	return s.broker.Subscribe(ctx)
}

// Shutdown cancels running checks and stops the broker.
func (s *Service) Shutdown() {
	s.CancelAll()
	s.broker.Shutdown()
}

// Fingerprint implements Fingerprinter.
func (s *Service) Fingerprint(parts ...any) string {
	h := sha256.New()
	h.Write(s.salt)
	data, _ := json.Marshal(parts)
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))[:24]
}

func (s *Service) adapterList() []Adapter {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.adapters)
}

// raw lists adapter items without check results.
func (s *Service) raw(ctx context.Context) ([]Item, map[string]Adapter) {
	var items []Item
	owner := map[string]Adapter{}
	for _, a := range s.adapterList() {
		for _, it := range safeItems(ctx, a, s) {
			if _, dup := owner[it.ID]; dup {
				continue
			}
			owner[it.ID] = a
			items = append(items, it)
		}
	}
	return items, owner
}

func safeItems(ctx context.Context, a Adapter, fp Fingerprinter) (items []Item) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Integration adapter panicked while listing", "panic", r)
			items = nil
		}
	}()
	return a.Items(ctx, fp)
}

// Snapshot returns every item matching f with its current status.
func (s *Service) Snapshot(ctx context.Context, f Filter) []Item {
	items, _ := s.raw(ctx)
	s.mu.Lock()
	out := make([]Item, 0, len(items))
	for _, it := range items {
		it = s.mergeLocked(it)
		if f.matches(it) {
			out = append(out, it)
		}
	}
	s.mu.Unlock()
	sortItems(out)
	return out
}

// Get returns one item with its current status.
func (s *Service) Get(ctx context.Context, id string) (Item, bool) {
	items, _ := s.raw(ctx)
	for _, it := range items {
		if it.ID == id {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.mergeLocked(it), true
		}
	}
	return Item{}, false
}

// Summarize returns the compact overview for the model.
func (s *Service) Summarize(ctx context.Context) Summary {
	items := s.Snapshot(ctx, Filter{})
	sum := Summary{ByKind: map[Kind]int{}, ByStatus: map[Status]int{}}
	for _, it := range items {
		if it.Status == StatusNotConfigured && it.Kind == KindProvider {
			// The provider catalog is long; unconfigured entries are
			// reachable through list but would only add noise here.
			continue
		}
		sum.Total++
		sum.ByKind[it.Kind]++
		sum.ByStatus[it.Status]++
		if it.Status == StatusUnchecked {
			sum.Unchecked++
		}
		if it.Status.NeedsAttention() && len(sum.Attention) < 12 {
			sum.Attention = append(sum.Attention, it.Brief())
		}
	}
	return sum
}

// Brief returns the one-line view.
func (i Item) Brief() Brief {
	return Brief{ID: i.ID, Name: i.Name, Kind: i.Kind, Status: i.Status, Detail: i.Detail, Remedy: i.Remedy}
}

// mergeLocked combines an adapter item with check state. Caller holds mu.
func (s *Service) mergeLocked(it Item) Item {
	if note := s.notes[it.ID]; !note.IsZero() && it.Stale.IsZero() {
		it.Stale = note
	}
	res, has := s.results[it.ID]
	matches := has && res.fingerprint == it.fingerprint
	if has && !matches && it.Stale.IsZero() {
		it.Stale = i18n.M("The configuration changed after the last check; check again.")
	}
	switch {
	case s.running[it.ID] != nil:
		if matches {
			it.CheckedAt = res.at
		}
		it.Status = StatusChecking
	case it.Status == StatusNotConfigured || it.Status == StatusDisabled:
	case it.Live && it.Kind == KindApp:
		// Process liveness does not supersede an application's health check.
		if matches {
			applyResult(&it, res)
		}
	case it.Live:
		if matches {
			it.CheckedAt = res.at
			// A check that found a missing executable is more specific
			// than the live "error" state of the same failure.
			if res.res.Status == StatusMissingDependency && it.Status != StatusReady {
				applyResult(&it, res)
			}
		}
	case matches:
		applyResult(&it, res)
	default:
		it.Status = StatusUnchecked
		if prev, ok := s.previous[it.ID]; ok {
			p := prev
			it.Previous = &p
		}
	}
	return it
}

func applyResult(it *Item, res stored) {
	it.Status = res.res.Status
	if !res.res.Detail.IsZero() {
		it.Detail = res.res.Detail
	}
	it.LastError = res.res.Err
	if !res.res.Remedy.IsZero() {
		it.Remedy = res.res.Remedy
	}
	if len(res.res.Capabilities) > 0 {
		it.Capabilities = res.res.Capabilities
	}
	it.CheckedAt = res.at
}

func (f Filter) matches(it Item) bool {
	if f.Kind != "" && it.Kind != f.Kind {
		return false
	}
	switch f.Status {
	case "":
	case "attention":
		if !it.Status.NeedsAttention() {
			return false
		}
	default:
		if string(it.Status) != f.Status {
			return false
		}
	}
	if f.Configured && it.Status == StatusNotConfigured {
		return false
	}
	if q := strings.ToLower(strings.TrimSpace(f.Query)); q != "" {
		parts := []string{it.Name, it.ID, string(it.Kind), it.Description.String(), it.Detail.String()}
		for _, c := range it.Capabilities {
			parts = append(parts, c.String())
		}
		hay := strings.ToLower(strings.Join(parts, " "))
		for _, word := range strings.Fields(q) {
			if !strings.Contains(hay, word) {
				return false
			}
		}
	}
	return true
}

// sortItems orders by kind, then configured before catalog entries, then
// name.
func sortItems(items []Item) {
	order := map[Kind]int{}
	for i, k := range Kinds() {
		order[k] = i
	}
	slices.SortStableFunc(items, func(a, b Item) int {
		if c := cmp.Compare(order[a.Kind], order[b.Kind]); c != 0 {
			return c
		}
		ac, bc := a.Status == StatusNotConfigured, b.Status == StatusNotConfigured
		if ac != bc {
			if ac {
				return 1
			}
			return -1
		}
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
}

// Check runs one item's check, bounded by the service timeout and ctx. A
// newer check of the same item supersedes this one, and a result whose
// configuration changed while it ran is discarded instead of stored.
func (s *Service) Check(ctx context.Context, id string) (Item, error) {
	items, owner := s.raw(ctx)
	var item Item
	found := false
	for _, it := range items {
		if it.ID == id {
			item, found = it, true
			break
		}
	}
	if !found {
		return Item{}, ErrNotFound
	}
	if item.Status == StatusNotConfigured || item.Status == StatusDisabled {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.mergeLocked(item), nil
	}
	adapter := owner[id]

	s.mu.Lock()
	s.gen++
	gen := s.gen
	if old := s.running[id]; old != nil {
		old.cancel()
	}
	runCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	s.running[id] = &inflight{gen: gen, cancel: cancel}
	delete(s.notes, id)
	s.mu.Unlock()
	s.broker.Publish(pubsub.UpdatedEvent, Event{Type: EventCheckStarted, ID: id})

	res, err := s.run(runCtx, adapter, item)
	res = s.sanitizeResult(res)

	// Re-read the configuration outside the lock: adapters may do IO.
	fresh, freshOK := s.lookup(ctx, id)

	s.mu.Lock()
	cur := s.running[id]
	superseded := cur == nil || cur.gen != gen
	if !superseded {
		delete(s.running, id)
	}
	switch {
	case superseded:
		s.mu.Unlock()
		s.broker.Publish(pubsub.UpdatedEvent, Event{Type: EventCheckDropped, ID: id})
		it, _ := s.Get(ctx, id)
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return it, context.Canceled
		}
		return it, nil
	case err != nil:
		s.mu.Unlock()
		s.broker.Publish(pubsub.UpdatedEvent, Event{Type: EventCheckDropped, ID: id})
		it, _ := s.Get(ctx, id)
		return it, err
	case !freshOK || fresh.fingerprint != item.fingerprint:
		s.notes[id] = i18n.M("The configuration changed while the check ran; the result was discarded. Check again.")
		s.mu.Unlock()
		s.broker.Publish(pubsub.UpdatedEvent, Event{Type: EventCheckDropped, ID: id})
		it, _ := s.Get(ctx, id)
		return it, nil
	}
	now := time.Now()
	s.results[id] = stored{res: res, at: now, fingerprint: item.fingerprint}
	if res.Status != StatusChecking {
		s.previous[id] = PreviousCheck{Status: res.Status, CheckedAt: now, Detail: res.Detail}
	}
	merged := s.mergeLocked(fresh)
	snapshot := maps.Clone(s.previous)
	s.mu.Unlock()
	s.savePrevious(snapshot)
	s.broker.Publish(pubsub.UpdatedEvent, Event{Type: EventCheckFinished, ID: id, Item: merged})
	return merged, nil
}

// run executes the adapter check under the concurrency limit. A user
// cancellation is returned as an error; a timeout becomes a result.
func (s *Service) run(ctx context.Context, a Adapter, item Item) (res CheckResult, err error) {
	select {
	case s.sem <- struct{}{}:
	case <-ctx.Done():
		return s.timeoutOr(ctx, item)
	}
	defer func() { <-s.sem }()
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Integration check panicked", "id", item.ID, "panic", r)
			res, err = CheckResult{Status: StatusConnectionError, Err: i18n.M("The check failed unexpectedly.")}, nil
		}
	}()
	res = a.Check(ctx, item)
	if ctx.Err() != nil {
		return s.timeoutOr(ctx, item)
	}
	if res.Status == "" {
		res.Status = StatusUnchecked
	}
	return res, nil
}

func (s *Service) timeoutOr(ctx context.Context, item Item) (CheckResult, error) {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return CheckResult{
			Status: StatusConnectionError,
			Err:    i18n.M("No answer within %s.", s.timeout.String()),
			Remedy: i18n.M("Check the network or that the service is running, then test again."),
		}, nil
	}
	return CheckResult{}, context.Canceled
}

func (s *Service) lookup(ctx context.Context, id string) (Item, bool) {
	items, _ := s.raw(ctx)
	for _, it := range items {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

// CheckAsync starts a check and returns immediately. The UI follows
// progress through Snapshot or events.
func (s *Service) CheckAsync(id string) {
	go func() {
		if _, err := s.Check(context.Background(), id); err != nil && !errors.Is(err, context.Canceled) {
			slog.Debug("Integration check failed", "id", id, "error", err)
		}
	}()
}

// CheckMany checks the given items concurrently within the service limit.
func (s *Service) CheckMany(ctx context.Context, ids []string) []Item {
	out := make([]Item, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Go(func() {
			it, _ := s.Check(ctx, id)
			out[i] = it
		})
	}
	wg.Wait()
	return out
}

// Checkable returns ids of items in f that a check can run on.
func (s *Service) Checkable(ctx context.Context, f Filter) []string {
	var ids []string
	for _, it := range s.Snapshot(ctx, f) {
		if it.Status != StatusNotConfigured && it.Status != StatusDisabled && it.Supports(ActionTest) {
			ids = append(ids, it.ID)
		}
	}
	return ids
}

// Cancel stops a running check of id; its result is discarded.
func (s *Service) Cancel(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.running[id]; r != nil {
		r.cancel()
		delete(s.running, id)
		return true
	}
	return false
}

// CancelAll stops every running check.
func (s *Service) CancelAll() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.running)
	for id, r := range s.running {
		r.cancel()
		delete(s.running, id)
	}
	return n
}

// Running reports how many checks are in flight.
func (s *Service) Running() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.running)
}

// Do runs a supported action. Afterwards the item's previous check result
// no longer describes its configuration and is dropped.
func (s *Service) Do(ctx context.Context, id string, action Action) (i18n.Message, error) {
	item, owner, ok := s.find(ctx, id)
	if !ok {
		return i18n.Message{}, ErrNotFound
	}
	if !item.Supports(action) {
		return i18n.Message{}, ErrUnsupported
	}
	msg, err := owner.Do(ctx, item, action)
	if err != nil {
		return i18n.Message{}, errors.New(s.secrets.Redact(err.Error()))
	}
	s.mu.Lock()
	delete(s.results, id)
	delete(s.notes, id)
	s.mu.Unlock()
	s.broker.Publish(pubsub.UpdatedEvent, Event{Type: EventChanged, ID: id})
	return msg.Map(s.secrets.Redact), nil
}

func (s *Service) find(ctx context.Context, id string) (Item, Adapter, bool) {
	items, owner := s.raw(ctx)
	for _, it := range items {
		if it.ID == id {
			s.mu.Lock()
			merged := s.mergeLocked(it)
			s.mu.Unlock()
			return merged, owner[id], true
		}
	}
	return Item{}, nil, false
}

func (s *Service) sanitizeResult(r CheckResult) CheckResult {
	r.Err = r.Err.Map(s.secrets.Redact)
	r.Detail = r.Detail.Map(s.secrets.Redact)
	r.Remedy = r.Remedy.Map(s.secrets.Redact)
	return r
}

type persisted struct {
	Version int                      `json:"version"`
	Checks  map[string]PreviousCheck `json:"checks"`
}

func (s *Service) loadPrevious() {
	if s.statePath == "" {
		return
	}
	data, err := os.ReadFile(s.statePath)
	if err != nil {
		return
	}
	var p persisted
	if err := json.Unmarshal(data, &p); err != nil || p.Version != 1 {
		slog.Warn("Ignoring unreadable integration check history", "path", s.statePath)
		return
	}
	for id, c := range p.Checks {
		if c.Status == StatusChecking || c.Status == "" {
			continue
		}
		c.Detail = c.Detail.Map(s.secrets.Redact)
		s.previous[id] = c
	}
}

func (s *Service) savePrevious(checks map[string]PreviousCheck) {
	if s.statePath == "" {
		return
	}
	if err := taskstate.WriteJSONAtomic(s.statePath, persisted{Version: 1, Checks: checks}); err != nil {
		slog.Warn("Failed to save integration check history", "error", err)
	}
}

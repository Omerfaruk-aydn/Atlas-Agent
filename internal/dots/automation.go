package dots

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/routine"
)

// Limits on automation fields.
const (
	maxTriggerNameRunes   = 60
	maxTriggerPromptRunes = 4000
	maxTriggersPerDot     = 25
	maxRuns               = 300
	// MaxHookBody is how much of a webhook body is handed to the agent.
	MaxHookBody = 64 << 10
)

// Trigger kinds. A file trigger fires when files matching a pattern in the
// dot's workspace change; a page trigger fires when a web page's content
// changes. Both are checked every few minutes.
const (
	KindSchedule = "schedule"
	KindWebhook  = "webhook"
	KindFile     = "file"
	KindPage     = "page"
)

// Check intervals for watch triggers, in minutes.
const (
	minFileEvery     = 1
	minPageEvery     = 5
	defaultFileEvery = 1
	defaultPageEvery = 15
	maxEvery         = 24 * 60
	maxHistoryDays   = 60
)

// Watch reports whether kind is checked for changes rather than fired.
func Watch(kind string) bool { return kind == KindFile || kind == KindPage }

// Run sources.
const (
	SourceSchedule = "schedule"
	SourceWebhook  = "webhook"
	SourceManual   = "manual"
	SourceWatch    = "watch"
)

// Run statuses. Running and waiting_approval are live; the rest are final.
const (
	StatusRunning         = "running"
	StatusWaitingApproval = "waiting_approval"
	StatusComplete        = "complete"
	StatusFailed          = "failed"
	StatusCancelled       = "cancelled"
	StatusBudget          = "budget"
	StatusSkipped         = "skipped"
)

// Live reports whether a run status is not final.
func Live(status string) bool {
	return status == StatusRunning || status == StatusWaitingApproval
}

// Trigger wakes a dot up: on a cron schedule, or when its webhook is
// called. Prompt is what the dot is asked to do when it fires.
type Trigger struct {
	ID       string `json:"id"`
	DotID    string `json:"dot_id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Cron     string `json:"cron,omitempty"`
	Timezone string `json:"timezone,omitempty"`
	Prompt   string `json:"prompt"`
	Enabled  bool   `json:"enabled"`
	Token    string `json:"token,omitempty"`
	// Path is a file trigger's pattern, relative to the dot's workspace.
	Path string `json:"path,omitempty"`
	// URL is the page a page trigger watches.
	URL string `json:"url,omitempty"`
	// Every is how often a watch trigger is checked, in minutes.
	Every int `json:"every,omitempty"`
	// Fingerprint is what a watch trigger saw at its last check.
	Fingerprint string    `json:"fingerprint,omitempty"`
	CheckedAt   time.Time `json:"checked_at,omitzero"`
	CreatedAt   time.Time `json:"created_at"`
	LastFired   time.Time `json:"last_fired_at,omitzero"`
	NextFireAt  time.Time `json:"next_fire_at,omitzero"`
}

// TriggerInput is the editable part of a trigger. Nil fields are left
// unchanged on update.
type TriggerInput struct {
	Name     *string `json:"name"`
	Kind     *string `json:"kind"`
	Cron     *string `json:"cron"`
	Timezone *string `json:"timezone"`
	Prompt   *string `json:"prompt"`
	Enabled  *bool   `json:"enabled"`
	Path     *string `json:"path"`
	URL      *string `json:"url"`
	Every    *int    `json:"every"`
}

// DaySpend is what a dot spent on one local day.
type DaySpend struct {
	Day string  `json:"day"`
	USD float64 `json:"usd"`
}

// Run is one wake-up of a dot, from the moment it starts until it ends.
type Run struct {
	ID          string    `json:"id"`
	DotID       string    `json:"dot_id"`
	TriggerID   string    `json:"trigger_id,omitempty"`
	TriggerName string    `json:"trigger_name,omitempty"`
	Source      string    `json:"source"`
	Goal        string    `json:"goal"`
	Status      string    `json:"status"`
	Error       string    `json:"error,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	SessionID   string    `json:"session_id,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	EndedAt     time.Time `json:"ended_at,omitzero"`
	Cost        float64   `json:"cost"`
}

// ledger is one dot's spending for one local day. Spent is what the dot's
// session had cost when the day began, so the day's spend is the session
// cost now minus Baseline.
type ledger struct {
	Day      string  `json:"day"`
	Baseline float64 `json:"baseline"`
	Session  string  `json:"session"`
	// Carried is what earlier sessions spent on the same day.
	Carried float64 `json:"carried,omitempty"`
}

// Budget is a dot's daily spending cap and what it has used today.
type Budget struct {
	LimitUSD float64 `json:"limit_usd"`
	SpentUSD float64 `json:"spent_usd"`
	Day      string  `json:"day"`
}

// Exceeded reports whether the cap is set and used up.
func (b Budget) Exceeded() bool { return b.LimitUSD > 0 && b.SpentUSD >= b.LimitUSD }

// Automation keeps triggers, run history and daily spending ledgers. It is
// safe for concurrent use.
type Automation struct {
	path string
	now  func() time.Time

	mu       sync.Mutex
	triggers []Trigger
	runs     []Run
	ledgers  map[string]ledger
	limits   map[string]float64
	history  map[string]map[string]float64
}

// automationFile is the on-disk form of [Automation].
type automationFile struct {
	Triggers []Trigger                     `json:"triggers"`
	Runs     []Run                         `json:"runs"`
	Ledgers  map[string]ledger             `json:"ledgers"`
	Limits   map[string]float64            `json:"limits"`
	History  map[string]map[string]float64 `json:"history"`
}

// OpenAutomation loads automation state from dir, creating it when missing.
func OpenAutomation(dir string) (*Automation, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create dots directory: %w", err)
	}
	a := &Automation{path: filepath.Join(dir, "automation.json"), now: time.Now}
	raw, err := os.ReadFile(a.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("read automation: %w", err)
	default:
		var f automationFile
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, fmt.Errorf("parse automation: %w", err)
		}
		a.triggers, a.runs, a.ledgers, a.limits, a.history = f.Triggers, f.Runs, f.Ledgers, f.Limits, f.History
	}
	if a.ledgers == nil {
		a.ledgers = map[string]ledger{}
	}
	if a.history == nil {
		a.history = map[string]map[string]float64{}
	}
	if a.limits == nil {
		a.limits = map[string]float64{}
	}
	// A run that was live when the process stopped can never finish.
	changed := false
	for i := range a.runs {
		if Live(a.runs[i].Status) {
			a.runs[i].Status = StatusFailed
			a.runs[i].Error = "Atlas stopped while this run was in progress."
			a.runs[i].EndedAt = a.now()
			changed = true
		}
	}
	if changed {
		if err := a.save(); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// --- Triggers ---

// TriggersFor returns one dot's triggers, oldest first.
func (a *Automation) TriggersFor(dotID string) []Trigger {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []Trigger{}
	for _, t := range a.triggers {
		if t.DotID == dotID {
			out = append(out, t)
		}
	}
	return out
}

// Trigger returns one trigger.
func (a *Automation) Trigger(dotID, id string) (Trigger, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	i := a.triggerIndex(dotID, id)
	if i < 0 {
		return Trigger{}, ErrTriggerNotFound
	}
	return a.triggers[i], nil
}

// ErrTriggerNotFound reports an unknown trigger.
var ErrTriggerNotFound = errors.New("trigger not found")

// CreateTrigger validates in and adds a trigger to a dot.
func (a *Automation) CreateTrigger(dotID string, in TriggerInput) (Trigger, error) {
	t := Trigger{DotID: dotID, Kind: KindSchedule, Enabled: true, CreatedAt: a.now()}
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return Trigger{}, errors.New("a trigger needs a name")
	}
	if err := a.applyTrigger(&t, in); err != nil {
		return Trigger{}, err
	}
	id, err := randomHex("trg_", 6)
	if err != nil {
		return Trigger{}, err
	}
	t.ID = id
	if t.Kind == KindWebhook {
		if t.Token, err = randomHex("", 24); err != nil {
			return Trigger{}, err
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	count := 0
	for _, x := range a.triggers {
		if x.DotID == dotID {
			count++
		}
	}
	if count >= maxTriggersPerDot {
		return Trigger{}, fmt.Errorf("at most %d triggers per dot", maxTriggersPerDot)
	}
	a.triggers = append(a.triggers, t)
	if err := a.save(); err != nil {
		a.triggers = a.triggers[:len(a.triggers)-1]
		return Trigger{}, err
	}
	return t, nil
}

// UpdateTrigger applies in to a trigger.
func (a *Automation) UpdateTrigger(dotID, id string, in TriggerInput) (Trigger, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	i := a.triggerIndex(dotID, id)
	if i < 0 {
		return Trigger{}, ErrTriggerNotFound
	}
	t := a.triggers[i]
	if in.Kind != nil && *in.Kind != t.Kind {
		return Trigger{}, errors.New("a trigger's kind cannot change")
	}
	if err := a.applyTrigger(&t, in); err != nil {
		return Trigger{}, err
	}
	prev := a.triggers[i]
	a.triggers[i] = t
	if err := a.save(); err != nil {
		a.triggers[i] = prev
		return Trigger{}, err
	}
	return t, nil
}

// DeleteTrigger removes a trigger. Its past runs stay in the history.
func (a *Automation) DeleteTrigger(dotID, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	i := a.triggerIndex(dotID, id)
	if i < 0 {
		return ErrTriggerNotFound
	}
	removed := a.triggers[i]
	a.triggers = slices.Delete(a.triggers, i, i+1)
	if err := a.save(); err != nil {
		a.triggers = slices.Insert(a.triggers, i, removed)
		return err
	}
	return nil
}

// DeleteDot forgets everything automation knows about a deleted dot.
func (a *Automation) DeleteDot(dotID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.triggers = slices.DeleteFunc(a.triggers, func(t Trigger) bool { return t.DotID == dotID })
	a.runs = slices.DeleteFunc(a.runs, func(r Run) bool { return r.DotID == dotID })
	delete(a.ledgers, dotID)
	delete(a.limits, dotID)
	delete(a.history, dotID)
	return a.save()
}

// WebhookTrigger finds the enabled webhook trigger for token, comparing in
// constant time.
func (a *Automation) WebhookTrigger(token string) (Trigger, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var found Trigger
	ok := false
	for _, t := range a.triggers {
		if t.Kind != KindWebhook || t.Token == "" || len(t.Token) != len(token) {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(t.Token), []byte(token)) == 1 {
			found, ok = t, t.Enabled
		}
	}
	return found, ok
}

// Due returns the schedule triggers whose time has come at now, and moves
// each one's next fire time past now so it fires once.
func (a *Automation) Due(now time.Time) []Trigger {
	a.mu.Lock()
	defer a.mu.Unlock()
	var due []Trigger
	changed := false
	for i := range a.triggers {
		t := &a.triggers[i]
		if t.Kind != KindSchedule || !t.Enabled {
			continue
		}
		if t.NextFireAt.IsZero() {
			t.NextFireAt = nextFire(*t, now)
			changed = true
			continue
		}
		if now.Before(t.NextFireAt) {
			continue
		}
		due = append(due, *t)
		t.LastFired = now
		t.NextFireAt = nextFire(*t, now)
		changed = true
	}
	if changed {
		_ = a.save()
	}
	return due
}

// MarkFired records that a webhook trigger fired.
func (a *Automation) MarkFired(dotID, id string, at time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if i := a.triggerIndex(dotID, id); i >= 0 {
		a.triggers[i].LastFired = at
		_ = a.save()
	}
}

func (a *Automation) triggerIndex(dotID, id string) int {
	return slices.IndexFunc(a.triggers, func(t Trigger) bool { return t.ID == id && t.DotID == dotID })
}

func (a *Automation) applyTrigger(t *Trigger, in TriggerInput) error {
	if in.Kind != nil {
		if !slices.Contains([]string{KindSchedule, KindWebhook, KindFile, KindPage}, *in.Kind) {
			return errors.New("kind must be schedule, webhook, file or page")
		}
		t.Kind = *in.Kind
	}
	if in.Path != nil && strings.TrimSpace(*in.Path) != t.Path {
		t.Path, t.Fingerprint = strings.TrimSpace(*in.Path), ""
	}
	if in.URL != nil && strings.TrimSpace(*in.URL) != t.URL {
		t.URL, t.Fingerprint = strings.TrimSpace(*in.URL), ""
	}
	if in.Every != nil {
		t.Every = *in.Every
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" || utf8.RuneCountInString(name) > maxTriggerNameRunes {
			return fmt.Errorf("name must be 1-%d characters", maxTriggerNameRunes)
		}
		t.Name = name
	}
	if in.Prompt != nil {
		p := strings.TrimSpace(*in.Prompt)
		if utf8.RuneCountInString(p) > maxTriggerPromptRunes {
			return fmt.Errorf("the task must be at most %d characters", maxTriggerPromptRunes)
		}
		t.Prompt = p
	}
	if in.Enabled != nil {
		t.Enabled = *in.Enabled
	}
	if in.Cron != nil {
		t.Cron = strings.TrimSpace(*in.Cron)
	}
	if in.Timezone != nil {
		t.Timezone = strings.TrimSpace(*in.Timezone)
	}
	if strings.TrimSpace(t.Prompt) == "" {
		return errors.New("tell the dot what to do when this trigger fires")
	}
	if t.Kind == KindSchedule {
		if t.Cron == "" {
			return errors.New("a schedule needs a cron expression")
		}
		if _, err := routine.ParseSchedule(t.Cron); err != nil {
			return fmt.Errorf("invalid schedule: %w", err)
		}
		if _, err := location(t.Timezone); err != nil {
			return err
		}
		t.NextFireAt = nextFire(*t, a.now())
		t.Path, t.URL, t.Every = "", "", 0
		return nil
	}
	t.Cron, t.Timezone, t.NextFireAt = "", "", time.Time{}
	switch t.Kind {
	case KindFile:
		t.URL = ""
		if err := ValidWatchPath(t.Path); err != nil {
			return err
		}
		if t.Every == 0 {
			t.Every = defaultFileEvery
		}
		if t.Every < minFileEvery || t.Every > maxEvery {
			return fmt.Errorf("check every %d-%d minutes", minFileEvery, maxEvery)
		}
	case KindPage:
		t.Path = ""
		u, err := url.Parse(t.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("a page trigger needs an http or https address")
		}
		if t.Every == 0 {
			t.Every = defaultPageEvery
		}
		if t.Every < minPageEvery || t.Every > maxEvery {
			return fmt.Errorf("check every %d-%d minutes", minPageEvery, maxEvery)
		}
	default:
		t.Path, t.URL, t.Every = "", "", 0
	}
	return nil
}

// ValidWatchPath checks a file trigger's pattern: a non-empty glob that
// stays inside the workspace.
func ValidWatchPath(p string) error {
	if p == "" {
		return errors.New("a file trigger needs a file or pattern, such as inbox/*.txt")
	}
	clean := filepath.ToSlash(filepath.Clean(p))
	if filepath.IsAbs(p) || strings.HasPrefix(clean, "../") || clean == ".." || strings.HasPrefix(clean, "/") {
		return errors.New("the pattern must stay inside the dot's workspace")
	}
	if _, err := filepath.Match(clean, "x"); err != nil {
		return fmt.Errorf("invalid pattern: %w", err)
	}
	return nil
}

// Watched returns the enabled watch triggers whose next check is due at
// now, and moves each one's check time forward.
func (a *Automation) Watched(now time.Time) []Trigger {
	a.mu.Lock()
	defer a.mu.Unlock()
	var due []Trigger
	for i := range a.triggers {
		t := &a.triggers[i]
		if !Watch(t.Kind) || !t.Enabled {
			continue
		}
		if !t.CheckedAt.IsZero() && now.Sub(t.CheckedAt) < time.Duration(t.Every)*time.Minute {
			continue
		}
		t.CheckedAt = now
		due = append(due, *t)
	}
	return due
}

// Rebaseline records fingerprint as a watch trigger's current state
// without firing it, so what a dot changed itself does not wake it again.
func (a *Automation) Rebaseline(dotID, id, fingerprint string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if i := a.triggerIndex(dotID, id); i >= 0 && a.triggers[i].Fingerprint != fingerprint {
		a.triggers[i].Fingerprint = fingerprint
		_ = a.save()
	}
}

// Observe records what a watch trigger saw. It reports true when the
// fingerprint differs from the last one; the first observation only sets
// the baseline.
func (a *Automation) Observe(dotID, id, fingerprint string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	i := a.triggerIndex(dotID, id)
	if i < 0 {
		return false
	}
	t := &a.triggers[i]
	prev := t.Fingerprint
	if prev == fingerprint {
		return false
	}
	t.Fingerprint = fingerprint
	changed := prev != ""
	if changed {
		t.LastFired = a.now()
	}
	_ = a.save()
	return changed
}

func location(name string) (*time.Location, error) {
	if name == "" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("unknown time zone %q", name)
	}
	return loc, nil
}

func nextFire(t Trigger, now time.Time) time.Time {
	if !t.Enabled || t.Kind != KindSchedule {
		return time.Time{}
	}
	sched, err := routine.ParseSchedule(t.Cron)
	if err != nil {
		return time.Time{}
	}
	loc, err := location(t.Timezone)
	if err != nil {
		return time.Time{}
	}
	return sched.Next(now.In(loc))
}

// --- Runs ---

// StartRun records a new live run and returns it.
func (a *Automation) StartRun(r Run) (Run, error) {
	id, err := randomHex("run_", 6)
	if err != nil {
		return Run{}, err
	}
	r.ID = id
	r.StartedAt = a.now()
	if r.Status == "" {
		r.Status = StatusRunning
	}
	if !Live(r.Status) {
		r.EndedAt = r.StartedAt
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	prev := a.runs
	a.runs = append(slices.Clone(a.runs), r)
	a.trimRuns()
	if err := a.save(); err != nil {
		// A run that was never recorded must not look live forever.
		a.runs = prev
		return Run{}, err
	}
	return r, nil
}

// UpdateRun applies fn to a run and saves it.
func (a *Automation) UpdateRun(id string, fn func(*Run)) (Run, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	i := slices.IndexFunc(a.runs, func(r Run) bool { return r.ID == id })
	if i < 0 {
		return Run{}, errors.New("run not found")
	}
	was := a.runs[i].Status
	fn(&a.runs[i])
	if Live(was) && !Live(a.runs[i].Status) && a.runs[i].EndedAt.IsZero() {
		a.runs[i].EndedAt = a.now()
	}
	return a.runs[i], a.save()
}

// RunsFor returns a dot's runs, newest first.
func (a *Automation) RunsFor(dotID string, limit int) []Run {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []Run{}
	for i := len(a.runs) - 1; i >= 0; i-- {
		if a.runs[i].DotID == dotID {
			out = append(out, a.runs[i])
			if limit > 0 && len(out) == limit {
				break
			}
		}
	}
	return out
}

// LiveRun returns the dot's run that has not ended, if any.
func (a *Automation) LiveRun(dotID string) (Run, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := len(a.runs) - 1; i >= 0; i-- {
		if a.runs[i].DotID == dotID && Live(a.runs[i].Status) {
			return a.runs[i], true
		}
	}
	return Run{}, false
}

// trimRuns drops the oldest finished runs beyond maxRuns. The caller holds
// a.mu.
func (a *Automation) trimRuns() {
	for len(a.runs) > maxRuns {
		i := slices.IndexFunc(a.runs, func(r Run) bool { return !Live(r.Status) })
		if i < 0 {
			return
		}
		a.runs = slices.Delete(a.runs, i, i+1)
	}
}

// --- Budget ---

// SetLimit sets a dot's daily spending cap in dollars. Zero removes it.
func (a *Automation) SetLimit(dotID string, usd float64) error {
	if usd < 0 || usd > 100000 {
		return errors.New("the daily budget must be between 0 and 100000")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	prev, had := a.limits[dotID]
	if usd == 0 {
		delete(a.limits, dotID)
	} else {
		a.limits[dotID] = usd
	}
	if err := a.save(); err != nil {
		if had {
			a.limits[dotID] = prev
		} else {
			delete(a.limits, dotID)
		}
		return err
	}
	return nil
}

// Spend reports a dot's budget given the cost its session has accumulated.
// Sessions only ever grow in cost, so the first reading of a day is the
// baseline and later readings are measured from it. A new session resets
// the baseline.
func (a *Automation) Spend(dotID, sessionID string, sessionCost float64) Budget {
	a.mu.Lock()
	defer a.mu.Unlock()
	day := a.now().Format("2006-01-02")
	l, ok := a.ledgers[dotID]
	changed := true
	switch {
	case !ok || l.Day != day:
		// First reading of the day: everything so far was spent earlier.
		l = ledger{Day: day, Session: sessionID, Baseline: sessionCost}
	case l.Session != sessionID:
		// A new session started today: all of its cost is today's, on top
		// of what the earlier ones spent.
		l = ledger{Day: day, Session: sessionID, Carried: a.history[dotID][day]}
	case sessionCost < l.Baseline:
		l.Baseline = sessionCost
	default:
		changed = false
	}
	spent := l.Carried + max(0, sessionCost-l.Baseline)
	h := a.history[dotID]
	if h == nil {
		h = map[string]float64{}
		a.history[dotID] = h
	}
	if h[day] != spent {
		h[day] = spent
		trimHistory(h)
		changed = true
	}
	a.ledgers[dotID] = l
	if changed {
		_ = a.save()
	}
	return Budget{LimitUSD: a.limits[dotID], SpentUSD: spent, Day: day}
}

// trimHistory keeps the most recent days of a spending history.
func trimHistory(h map[string]float64) {
	if len(h) <= maxHistoryDays {
		return
	}
	days := make([]string, 0, len(h))
	for d := range h {
		days = append(days, d)
	}
	slices.Sort(days)
	for _, d := range days[:len(days)-maxHistoryDays] {
		delete(h, d)
	}
}

// History returns what a dot spent on each of the last n days, oldest
// first, with zero for days it spent nothing.
func (a *Automation) History(dotID string, n int) []DaySpend {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	out := make([]DaySpend, 0, n)
	for i := n - 1; i >= 0; i-- {
		day := now.AddDate(0, 0, -i).Format("2006-01-02")
		out = append(out, DaySpend{Day: day, USD: a.history[dotID][day]})
	}
	return out
}

// Limit returns a dot's daily cap.
func (a *Automation) Limit(dotID string) float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.limits[dotID]
}

func (a *Automation) save() error {
	raw, err := json.MarshalIndent(automationFile{a.triggers, a.runs, a.ledgers, a.limits, a.history}, "", "  ")
	if err != nil {
		return err
	}
	if err := writeAtomic(a.path, raw); err != nil {
		return fmt.Errorf("write automation: %w", err)
	}
	return nil
}

func randomHex(prefix string, n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}

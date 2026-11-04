package voice

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"sync"
	"time"
)

// SiteHealth is a monitored site's state.
type SiteHealth string

// Site health states.
const (
	HealthUnknown   SiteHealth = "unknown"
	HealthHealthy   SiteHealth = "healthy"
	HealthDegraded  SiteHealth = "degraded"
	HealthDown      SiteHealth = "down"
	HealthRecovered SiteHealth = "recovered"
)

// SiteEventKind is a notification-worthy transition.
type SiteEventKind string

// Site events. Only these two transitions ever notify.
const (
	SiteWentDown  SiteEventKind = "down"
	SiteRecovered SiteEventKind = "recovered"
)

// SiteEvent is emitted on a down or recovery transition.
type SiteEvent struct {
	Site     string
	Kind     SiteEventKind
	EventID  string
	Detail   string
	Priority string
}

// Advance applies one check result to a site's state and returns the event
// the transition produces, if any. It is pure so the rules can be tested
// on their own:
//
//   - a failure while healthy, unknown or recovered is degraded;
//   - threshold consecutive failures are down, and notify once per incident;
//   - success after down is recovered, and notifies only when
//     notify_on_recovery is set, which the recovery then clears;
//   - success otherwise is healthy.
func Advance(st SiteState, site SiteConfig, check SiteCheck) (SiteState, *SiteEvent) {
	threshold := max(site.FailureThreshold, 1)
	st.Name = site.Name
	st.LastCheck, st.LastStatus, st.LastError = check.At, check.Status, check.Error
	st.Checks = append(st.Checks, check)
	prev := st.Health
	if prev == "" {
		prev = HealthUnknown
	}
	var ev *SiteEvent
	if check.OK {
		st.Failures = 0
		if prev == HealthDown {
			st.Health = HealthRecovered
			if st.NotifyOnRecovery {
				ev = &SiteEvent{Site: site.Name, Kind: SiteRecovered, EventID: fmt.Sprintf("monitor:%s:%d:recovered", site.Name, st.Incident)}
				st.NotifyOnRecovery = false
			}
		} else {
			st.Health = HealthHealthy
		}
	} else {
		st.Failures++
		switch {
		case st.Failures >= threshold && prev != HealthDown:
			st.Health = HealthDown
			st.Incident++
			if site.NotifyOnRecovery {
				st.NotifyOnRecovery = true
			}
			ev = &SiteEvent{Site: site.Name, Kind: SiteWentDown, EventID: fmt.Sprintf("monitor:%s:%d:down", site.Name, st.Incident), Detail: check.Error}
		case prev == HealthDown:
			st.Health = HealthDown
		default:
			st.Health = HealthDegraded
		}
	}
	if st.Health != prev {
		st.ChangedAt = check.At
	}
	if ev != nil {
		ev.Priority = site.Priority
		if slices.Contains(st.Notified, ev.EventID) {
			ev = nil
		} else {
			st.Notified = append(st.Notified, ev.EventID)
			if len(st.Notified) > 20 {
				st.Notified = st.Notified[len(st.Notified)-20:]
			}
		}
	}
	return st, ev
}

// Monitor checks the configured sites and reports transitions. Only URLs
// from configuration are ever fetched.
type Monitor struct {
	sites  []SiteConfig
	conc   int
	store  *Store
	client *http.Client
	notify func(context.Context, SiteEvent)
	now    func() time.Time

	mu      sync.Mutex
	running map[string]bool
}

// NewMonitor builds a monitor. notify is called for each down or recovery
// event; it must be idempotent on the event id.
func NewMonitor(cfg MonitorConfig, store *Store, client *http.Client, notify func(context.Context, SiteEvent), now func() time.Time) *Monitor {
	if now == nil {
		now = time.Now
	}
	if client == nil {
		client = &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("too many redirects")
				}
				return nil
			},
		}
	}
	return &Monitor{sites: cfg.Sites, conc: max(cfg.MaxConcurrency, 1), store: store, client: client, notify: notify, now: now, running: map[string]bool{}}
}

// Site returns the configuration of a monitored site.
func (m *Monitor) Site(name string) (SiteConfig, bool) {
	for _, s := range m.sites {
		if s.Name == name {
			return s, true
		}
	}
	return SiteConfig{}, false
}

// Sites lists configured site names.
func (m *Monitor) Sites() []string {
	out := make([]string, len(m.sites))
	for i, s := range m.sites {
		out[i] = s.Name
	}
	return out
}

// SetNotifyOnRecovery arms (or disarms) a call when the site recovers.
func (m *Monitor) SetNotifyOnRecovery(name string, on bool) (SiteState, error) {
	if _, ok := m.Site(name); !ok {
		return SiteState{}, fmt.Errorf("site %q is not monitored", name)
	}
	st := m.store.Site(name)
	st.NotifyOnRecovery = on
	return st, m.store.PutSite(st)
}

// Run checks due sites until ctx ends.
func (m *Monitor) Run(ctx context.Context) {
	if len(m.sites) == 0 {
		return
	}
	sem := make(chan struct{}, m.conc)
	var wg sync.WaitGroup
	defer wg.Wait()
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		for _, site := range m.sites {
			st := m.store.Site(site.Name)
			if !st.LastCheck.IsZero() && m.now().Sub(st.LastCheck) < time.Duration(site.IntervalSeconds)*time.Second {
				continue
			}
			m.mu.Lock()
			busy := m.running[site.Name]
			if !busy {
				m.running[site.Name] = true
			}
			m.mu.Unlock()
			if busy {
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() {
					m.mu.Lock()
					delete(m.running, site.Name)
					m.mu.Unlock()
				}()
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					return
				}
				if _, err := m.CheckOnce(ctx, site); err != nil && ctx.Err() == nil {
					slog.Warn("Failed to record site check", "site", site.Name, "error", err)
				}
			}()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// CheckOnce checks one site, stores the result and notifies on a
// transition.
func (m *Monitor) CheckOnce(ctx context.Context, site SiteConfig) (SiteState, error) {
	check := m.probe(ctx, site)
	if ctx.Err() != nil {
		return SiteState{}, ctx.Err()
	}
	st, ev := Advance(m.store.Site(site.Name), site, check)
	if err := m.store.PutSite(st); err != nil {
		return st, err
	}
	if ev != nil {
		slog.Info("Monitored site changed state", "site", site.Name, "event", ev.Kind, "detail", ev.Detail)
		if m.notify != nil {
			m.notify(ctx, *ev)
		}
	}
	return st, nil
}

func (m *Monitor) probe(ctx context.Context, site SiteConfig) SiteCheck {
	check := SiteCheck{At: m.now()}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(site.TimeoutSeconds)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, site.URL, nil)
	if err != nil {
		check.Error = "invalid_url"
		return check
	}
	req.Header.Set("User-Agent", "Atlas-Agent-Monitor/1")
	resp, err := m.client.Do(req)
	if err != nil {
		check.Error = classifyNetError(err)
		return check
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	check.Status = resp.StatusCode
	switch {
	case resp.StatusCode >= 500:
		check.Error = fmt.Sprintf("http_%d", resp.StatusCode)
	case slices.Contains(site.DownStatuses, resp.StatusCode):
		check.Error = fmt.Sprintf("http_%d", resp.StatusCode)
	default:
		check.OK = true
	}
	return check
}

func classifyNetError(err error) string {
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var netErr net.Error
	switch {
	case errors.As(err, &dnsErr):
		return "dns"
	case errors.As(err, &certErr):
		return "tls"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return "timeout"
	default:
		return "connection"
	}
}

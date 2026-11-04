package voice_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice/providers/mock"
	"github.com/stretchr/testify/require"
)

// fakeDialer records dials and lets the test decide what each call does.
type fakeDialer struct {
	mu        sync.Mutex
	dials     []voice.DialRequest
	cancelled []string
	onDial    func(req voice.DialRequest, pid string)
	err       error
}

func (d *fakeDialer) Dial(_ context.Context, req voice.DialRequest) (string, error) {
	d.mu.Lock()
	d.dials = append(d.dials, req)
	n := len(d.dials)
	err, onDial := d.err, d.onDial
	d.mu.Unlock()
	if err != nil {
		return "", err
	}
	pid := "CA-out-" + string(rune('0'+n))
	if onDial != nil {
		go onDial(req, pid)
	}
	return pid, nil
}

func (d *fakeDialer) Cancel(_ context.Context, pid string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cancelled = append(d.cancelled, pid)
	return nil
}

func (d *fakeDialer) Dials() []voice.DialRequest {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]voice.DialRequest(nil), d.dials...)
}

type fakeNotifier struct {
	channel string
	mu      sync.Mutex
	sent    []string
	fail    bool
}

func (n *fakeNotifier) Channel() string { return n.channel }

func (n *fakeNotifier) Notify(_ context.Context, text string) error {
	if n.fail {
		return errors.New("down")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sent = append(n.sent, text)
	return nil
}

func (n *fakeNotifier) Sent() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.sent...)
}

func newRouter(t *testing.T, h *harness, d *fakeDialer, notifiers ...voice.Notifier) *voice.Router {
	t.Helper()
	return voice.NewRouter(voice.RouterOptions{
		Config: h.cfg, Gateway: h.g, Dialer: d, Notifiers: notifiers, Store: h.store,
		Budget: voice.NewBudget(h.cfg, h.store, nil), RetryDelay: 10 * time.Millisecond, AnswerGrace: 200 * time.Millisecond,
	})
}

func routerHarness(t *testing.T, mod func(*voice.Config)) *harness {
	return newHarness(t, func(c *voice.Config) {
		c.Outbound = voice.OutboundConfig{RingTimeoutSeconds: 5, Fallback: []string{voice.ChannelTelegram, voice.ChannelSMS}, MaxPerHour: 5, MinIntervalSeconds: 1}
		if mod != nil {
			mod(c)
		}
	})
}

func TestOwnerCallAnsweredSpeaksReason(t *testing.T) {
	t.Parallel()
	h := routerHarness(t, nil)
	legs := make(chan *mock.Leg, 1)
	d := &fakeDialer{onDial: func(req voice.DialRequest, pid string) {
		leg := mock.NewLeg()
		legs <- leg
		_ = h.g.Attach(context.Background(), req.CallID, req.StreamToken, leg)
	}}
	r := newRouter(t, h, d)
	res, err := r.CallOwner(context.Background(), voice.OwnerCallRequest{Reason: "Günün sözü hazır.", EventID: "routine:morning:2026-11-01"})
	require.NoError(t, err)
	require.Equal(t, voice.ResultAnswered, res.Result)
	require.Equal(t, voice.ChannelPSTN, res.Channel)
	require.Len(t, d.Dials(), 1)
	require.Equal(t, ownerNumber, d.Dials()[0].To, "the destination comes from configuration")

	leg := <-legs
	h.eventually(func() bool { return strings.Contains(leg.Heard(), "Günün sözü hazır.") }, "reason spoken")
	leg.Say("teşekkürler")
	h.eventually(func() bool { return len(h.runner.Runs()) == 1 }, "conversation continues")
	require.Contains(t, h.runner.Runs()[0].Prompt, "Reason: Günün sözü hazır.")
	leg.CallerHangup()

	// The same occurrence never rings twice.
	again, err := r.CallOwner(context.Background(), voice.OwnerCallRequest{Reason: "Günün sözü hazır.", EventID: "routine:morning:2026-11-01"})
	require.NoError(t, err)
	require.Equal(t, voice.ResultDuplicate, again.Result)
	require.Equal(t, res.CallID, again.CallID)
	require.Len(t, d.Dials(), 1)
}

func TestConcurrentOwnerCallsForOneEventDialOnce(t *testing.T) {
	t.Parallel()
	h := routerHarness(t, nil)
	d := &fakeDialer{onDial: func(_ voice.DialRequest, pid string) { h.g.HandleStatus(pid, voice.StatusNoAnswer) }}
	r := newRouter(t, h, d)
	var wg sync.WaitGroup
	results := make([]voice.OwnerCallResult, 5)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := r.CallOwner(context.Background(), voice.OwnerCallRequest{Reason: "Site çöktü", EventID: "monitor:shop:1:down", Priority: voice.PriorityUrgent})
			require.NoError(t, err)
			results[i] = res
		}()
	}
	wg.Wait()
	require.Len(t, d.Dials(), 1)
	dup := 0
	for _, r := range results {
		if r.Result == voice.ResultDuplicate {
			dup++
		}
	}
	require.Equal(t, 4, dup)
}

func TestOwnerCallNoAnswerFallsBack(t *testing.T) {
	t.Parallel()
	h := routerHarness(t, nil)
	d := &fakeDialer{onDial: func(_ voice.DialRequest, pid string) {
		h.g.HandleStatus(pid, voice.StatusRinging)
		h.g.HandleStatus(pid, voice.StatusNoAnswer)
		h.g.HandleStatus(pid, voice.StatusNoAnswer) // duplicate callback
		h.g.HandleStatus(pid, voice.StatusCompleted)
	}}
	tg := &fakeNotifier{channel: voice.ChannelTelegram, fail: true}
	sms := &fakeNotifier{channel: voice.ChannelSMS}
	r := newRouter(t, h, d, tg, sms)
	res, err := r.CallOwner(context.Background(), voice.OwnerCallRequest{Reason: "Sunucu yanıt vermiyor"})
	require.NoError(t, err)
	require.Equal(t, voice.ResultNoAnswer, res.Result)
	require.Equal(t, []string{voice.ChannelSMS}, res.Fallback, "failed channels are skipped, one delivery is enough")
	require.Equal(t, []string{"Atlas: Sunucu yanıt vermiyor"}, sms.Sent())
	require.True(t, strings.HasPrefix(res.EventID, "adhoc:"))
	rec, ok := h.store.Get(res.CallID)
	require.True(t, ok)
	require.Equal(t, voice.StateNoAnswer, rec.State)
	require.Equal(t, []string{voice.ChannelSMS}, rec.Fallback)
	require.Len(t, d.Dials(), 1, "normal priority never retries")
}

func TestUrgentRetryOnlyWhenAllowed(t *testing.T) {
	t.Parallel()
	h := routerHarness(t, func(c *voice.Config) { c.Outbound.AllowRetry = true })
	d := &fakeDialer{onDial: func(_ voice.DialRequest, pid string) { h.g.HandleStatus(pid, voice.StatusBusy) }}
	r := newRouter(t, h, d)
	res, err := r.CallOwner(context.Background(), voice.OwnerCallRequest{Reason: "Acil", Priority: voice.PriorityUrgent})
	require.NoError(t, err)
	require.Equal(t, voice.ResultNoAnswer, res.Result)
	require.Equal(t, 2, res.Attempts)
	require.Len(t, d.Dials(), 2, "exactly one controlled retry")
}

func TestAmbiguousDialIsNeverRetried(t *testing.T) {
	t.Parallel()
	h := routerHarness(t, func(c *voice.Config) { c.Outbound.AllowRetry = true })
	d := &fakeDialer{err: &voice.DialError{Code: voice.CodeTransportFailed, Err: errors.New("timeout after send")}}
	r := newRouter(t, h, d)
	res, err := r.CallOwner(context.Background(), voice.OwnerCallRequest{Reason: "Acil", Priority: voice.PriorityUrgent})
	require.NoError(t, err)
	require.Equal(t, voice.ResultFailed, res.Result)
	require.Contains(t, res.Note, "uncertain")
	require.Len(t, d.Dials(), 1)
	again, err := r.CallOwner(context.Background(), voice.OwnerCallRequest{Reason: "Acil", Priority: voice.PriorityUrgent})
	require.NoError(t, err)
	require.Equal(t, voice.ResultDuplicate, again.Result, "an uncertain call keeps its event claim")
}

func TestOwnerCallRateLimitAndValidation(t *testing.T) {
	t.Parallel()
	h := routerHarness(t, func(c *voice.Config) { c.Outbound.MaxPerHour = 1 })
	d := &fakeDialer{onDial: func(_ voice.DialRequest, pid string) { h.g.HandleStatus(pid, voice.StatusNoAnswer) }}
	r := newRouter(t, h, d)
	_, err := r.CallOwner(context.Background(), voice.OwnerCallRequest{Reason: "bir", EventID: "e1"})
	require.NoError(t, err)
	res, err := r.CallOwner(context.Background(), voice.OwnerCallRequest{Reason: "iki", EventID: "e2", Priority: voice.PriorityUrgent})
	require.NoError(t, err)
	require.Equal(t, voice.ResultRateLimited, res.Result)
	require.Len(t, d.Dials(), 1)

	_, err = r.CallOwner(context.Background(), voice.OwnerCallRequest{Reason: " "})
	require.Equal(t, voice.CodeInvalidInput, voice.CodeOf(err))
	_, err = r.CallOwner(context.Background(), voice.OwnerCallRequest{Reason: strings.Repeat("a", 501)})
	require.Equal(t, voice.CodeInvalidInput, voice.CodeOf(err))
	_, err = r.CallOwner(context.Background(), voice.OwnerCallRequest{Reason: "x", Priority: "life-or-death"})
	require.Equal(t, voice.CodeInvalidInput, voice.CodeOf(err))

	wa, err := r.CallOwner(context.Background(), voice.OwnerCallRequest{Reason: "üç", EventID: "e3", PreferredChannel: voice.ChannelWhatsAppVoice})
	require.NoError(t, err)
	require.Equal(t, voice.ChannelPSTN, wa.Channel, "outbound WhatsApp calls route over the phone network")
}

func TestRingTimeoutCancelsCall(t *testing.T) {
	t.Parallel()
	h := routerHarness(t, func(c *voice.Config) { c.Outbound.RingTimeoutSeconds = 1 })
	d := &fakeDialer{}
	r := newRouter(t, h, d)
	res, err := r.CallOwner(context.Background(), voice.OwnerCallRequest{Reason: "zil"})
	require.NoError(t, err)
	require.Equal(t, voice.ResultNoAnswer, res.Result)
	require.Len(t, d.cancelled, 1, "the ringing call is cancelled at the provider")
}

func TestMonitorTransitions(t *testing.T) {
	t.Parallel()
	site := voice.SiteConfig{Name: "shop", FailureThreshold: 2, Priority: voice.PriorityUrgent}
	ok := voice.SiteCheck{OK: true, Status: 200}
	bad := voice.SiteCheck{Error: "timeout"}
	var st voice.SiteState
	var ev *voice.SiteEvent
	st, ev = voice.Advance(st, site, ok)
	require.Equal(t, voice.HealthHealthy, st.Health)
	require.Nil(t, ev)
	st, ev = voice.Advance(st, site, bad)
	require.Equal(t, voice.HealthDegraded, st.Health)
	require.Nil(t, ev, "one failure is not down")
	st, ev = voice.Advance(st, site, bad)
	require.Equal(t, voice.HealthDown, st.Health)
	require.NotNil(t, ev)
	require.Equal(t, voice.SiteWentDown, ev.Kind)
	require.Equal(t, "monitor:shop:1:down", ev.EventID)
	require.Equal(t, "timeout", ev.Detail)
	st, ev = voice.Advance(st, site, bad)
	require.Nil(t, ev, "no notification on every failed check")
	require.Equal(t, voice.HealthDown, st.Health)

	st, ev = voice.Advance(st, site, ok)
	require.Equal(t, voice.HealthRecovered, st.Health)
	require.Nil(t, ev, "recovery is silent unless asked for")

	// Second incident with a recovery request.
	st, _ = voice.Advance(st, site, bad)
	st, ev = voice.Advance(st, site, bad)
	require.Equal(t, "monitor:shop:2:down", ev.EventID)
	st.NotifyOnRecovery = true
	st, ev = voice.Advance(st, site, ok)
	require.NotNil(t, ev)
	require.Equal(t, voice.SiteRecovered, ev.Kind)
	require.Equal(t, "monitor:shop:2:recovered", ev.EventID)
	require.False(t, st.NotifyOnRecovery, "the flag clears after the recovery call")
	st, ev = voice.Advance(st, site, ok)
	require.Equal(t, voice.HealthHealthy, st.Health)
	require.Nil(t, ev)
}

func TestMonitorChecksAndNotifiesOnce(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	setStatus := func(s int) { mu.Lock(); status = s; mu.Unlock() }

	store := voice.NewMemoryStore()
	var events []voice.SiteEvent
	var evMu sync.Mutex
	site := voice.SiteConfig{Name: "shop", URL: srv.URL, TimeoutSeconds: 2, FailureThreshold: 2, DownStatuses: []int{403}}
	m := voice.NewMonitor(voice.MonitorConfig{Sites: []voice.SiteConfig{site}}, store, srv.Client(), func(_ context.Context, ev voice.SiteEvent) {
		evMu.Lock()
		events = append(events, ev)
		evMu.Unlock()
	}, nil)
	ctx := context.Background()
	check := func() voice.SiteState {
		st, err := m.CheckOnce(ctx, site)
		require.NoError(t, err)
		return st
	}
	require.Equal(t, voice.HealthHealthy, check().Health)
	setStatus(http.StatusForbidden)
	require.Equal(t, voice.HealthDegraded, check().Health, "403 counts as a failure by policy")
	require.Equal(t, voice.HealthDown, check().Health)
	check()
	_, err := m.SetNotifyOnRecovery("shop", true)
	require.NoError(t, err)
	_, err = m.SetNotifyOnRecovery("unknown", true)
	require.Error(t, err)
	setStatus(http.StatusNotFound)
	st := check()
	require.Equal(t, voice.HealthRecovered, st.Health, "a 404 not listed in down_statuses is up")
	evMu.Lock()
	defer evMu.Unlock()
	require.Len(t, events, 2)
	require.Equal(t, voice.SiteWentDown, events[0].Kind)
	require.Equal(t, "http_403", events[0].Detail)
	require.Equal(t, voice.SiteRecovered, events[1].Kind)
	require.Len(t, store.Site("shop").Checks, 5)
}

func TestMonitorRecordsNetworkErrors(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	t.Cleanup(srv.Close)
	site := voice.SiteConfig{Name: "slow", URL: srv.URL, TimeoutSeconds: 1, FailureThreshold: 1}
	m := voice.NewMonitor(voice.MonitorConfig{}, voice.NewMemoryStore(), srv.Client(), nil, nil)
	st, err := m.CheckOnce(context.Background(), site)
	require.NoError(t, err)
	require.Equal(t, "timeout", st.LastError)
	require.Equal(t, voice.HealthDown, st.Health)

	dns := voice.SiteConfig{Name: "nx", URL: "http://does-not-exist.invalid", TimeoutSeconds: 5, FailureThreshold: 1}
	st, err = m.CheckOnce(context.Background(), dns)
	require.NoError(t, err)
	require.Contains(t, []string{"dns", "connection"}, st.LastError)
}

func TestControlAPI(t *testing.T) {
	t.Parallel()
	h := routerHarness(t, nil)
	d := &fakeDialer{onDial: func(_ voice.DialRequest, pid string) { h.g.HandleStatus(pid, voice.StatusNoAnswer) }}
	r := newRouter(t, h, d)
	cs := &voice.ControlServer{Config: h.cfg, Router: r, Gateway: h.g, Store: h.store, Budget: voice.NewBudget(h.cfg, h.store, nil), Token: "secret-token"}
	srv := httptest.NewServer(cs.Handler())
	t.Cleanup(srv.Close)

	unauth, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/v1/voice/status", nil)
	resp, err := http.DefaultClient.Do(unauth)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/v1/voice/status", nil)
	req.Host = "attacker.example.com"
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode, "DNS-rebinding hosts are refused")

	client := &voice.Client{BaseURL: srv.URL, Token: "secret-token", HTTP: srv.Client()}
	ctx := context.Background()
	res, err := client.CallOwner(ctx, voice.OwnerCallRequest{Reason: "Test", EventID: "t-1"})
	require.NoError(t, err)
	require.Equal(t, voice.ResultNoAnswer, res.Result)
	rec, err := client.Call(ctx, res.CallID)
	require.NoError(t, err)
	require.Equal(t, voice.StateNoAnswer, rec.State)
	calls, err := client.Calls(ctx, 5)
	require.NoError(t, err)
	require.Len(t, calls, 1)
	st, err := client.Status(ctx)
	require.NoError(t, err)
	require.True(t, st.Enabled)
	require.Empty(t, st.ActiveCalls)

	_, err = client.CallOwner(ctx, voice.OwnerCallRequest{Reason: ""})
	require.Error(t, err)
	require.Equal(t, voice.CodeInvalidInput, voice.CodeOf(err))

	raw, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/v1/voice/owner-calls", strings.NewReader(`{"reason":"x","destination":"+15550000000"}`))
	raw.Header.Set("Authorization", "Bearer secret-token")
	resp, err = http.DefaultClient.Do(raw)
	require.NoError(t, err)
	var apiErr struct{ Error string }
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr))
	resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, "there is no destination field to smuggle a number in")
	require.Len(t, d.Dials(), 1)
}

func TestControlStateFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := voice.DialControl(dir)
	require.ErrorIs(t, err, voice.ErrNotRunning)
	require.NoError(t, voice.WriteControlState(voice.ControlStatePath(dir), voice.ControlState{Addr: "127.0.0.1:1", Token: "t"}))
	c, err := voice.DialControl(dir)
	require.NoError(t, err)
	_, err = c.Status(context.Background())
	require.ErrorIs(t, err, voice.ErrNotRunning)
	_, _, err = voice.ServeControl(context.Background(), "0.0.0.0:0", http.NotFoundHandler())
	require.Error(t, err)
}

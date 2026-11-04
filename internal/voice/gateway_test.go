package voice_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice/providers/mock"
	"github.com/stretchr/testify/require"
)

const ownerNumber = "+905551112233"

// fakeRunner stands in for the Atlas coordinator.
type fakeRunner struct {
	mu       sync.Mutex
	runs     []voice.TurnRequest
	sessions int
	answer   func(ctx context.Context, req voice.TurnRequest) (voice.TurnResult, error)
	active   atomic.Int32
}

func (f *fakeRunner) NewSession(context.Context, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions++
	return "session-1", nil
}

func (f *fakeRunner) RunTurn(ctx context.Context, req voice.TurnRequest) (voice.TurnResult, error) {
	f.mu.Lock()
	f.runs = append(f.runs, req)
	answer := f.answer
	f.mu.Unlock()
	f.active.Add(1)
	defer f.active.Add(-1)
	if answer != nil {
		return answer(ctx, req)
	}
	return voice.TurnResult{Text: "Cevap: " + req.Text}, nil
}

func (f *fakeRunner) setAnswer(fn func(ctx context.Context, req voice.TurnRequest) (voice.TurnResult, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answer = fn
}

func (f *fakeRunner) Runs() []voice.TurnRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]voice.TurnRequest(nil), f.runs...)
}

type harness struct {
	t      *testing.T
	g      *voice.Gateway
	store  *voice.Store
	prov   *mock.Provider
	runner *fakeRunner
	cfg    voice.Config
}

func newHarness(t *testing.T, mod func(*voice.Config)) *harness {
	t.Helper()
	cfg := voice.Config{
		Enabled:               true,
		Language:              "tr-TR",
		Owner:                 voice.OwnerConfig{PhoneNumber: ownerNumber},
		SilenceTimeoutSeconds: 30,
		MaxCallSeconds:        120,
		TurnTimeoutSeconds:    10,
	}
	if mod != nil {
		mod(&cfg)
	}
	store := voice.NewMemoryStore()
	prov := mock.New()
	runner := &fakeRunner{}
	g, err := voice.NewGateway(voice.GatewayOptions{
		Config: cfg, Store: store, Recognizer: prov.Recognizer, Synthesizer: prov.Synthesizer, Runner: runner,
		Policy:        voice.NewToolPolicy(cfg.ToolPolicy, func(n string) bool { return n == "view" }),
		ThinkingDelay: time.Hour, ReconnectBackoff: 10 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		g.Shutdown(ctx)
	})
	return &harness{t: t, g: g, store: store, prov: prov, runner: runner, cfg: cfg}
}

// call admits an inbound call and attaches a mock leg. done is closed when
// Attach returns.
func (h *harness) call(pid string) (string, *mock.Leg, chan struct{}) {
	h.t.Helper()
	id, token, err := h.g.AdmitInbound(pid, ownerNumber)
	require.NoError(h.t, err)
	leg := mock.NewLeg()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = h.g.Attach(context.Background(), id, token, leg)
	}()
	h.eventually(func() bool { return strings.Contains(leg.Heard(), "Merhaba") }, "greeting")
	return id, leg, done
}

func (h *harness) eventually(cond func() bool, what string) {
	h.t.Helper()
	require.Eventually(h.t, cond, 5*time.Second, 5*time.Millisecond, "waiting for %s", what)
}

func (h *harness) wait(done chan struct{}) {
	h.t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		h.t.Fatal("call did not end")
	}
}

func TestInboundCallEndToEnd(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	id, leg, done := h.call("CA1")

	leg.Say("Son e-postamı özetle")
	h.eventually(func() bool { return strings.Contains(leg.Heard(), "Cevap: Son e-postamı özetle") }, "agent reply spoken")

	runs := h.runner.Runs()
	require.Len(t, runs, 1)
	require.Equal(t, id, runs[0].CallID)
	require.Equal(t, id+":1", runs[0].RunID)
	require.Equal(t, "session-1", runs[0].SessionID)
	require.Contains(t, runs[0].Prompt, "untrusted data")
	require.Contains(t, runs[0].Prompt, "The caller said: Son e-postamı özetle")

	leg.CallerHangup()
	h.wait(done)
	rec, ok := h.store.Get(id)
	require.True(t, ok)
	require.Equal(t, voice.StateEnded, rec.State)
	require.Equal(t, 1, rec.Metrics.Turns)
	require.Positive(t, rec.Metrics.FirstTranscriptMS)
	require.Len(t, rec.Turns, 1)
	require.Equal(t, "Son e-postamı özetle", rec.Turns[0].User)
	require.Contains(t, rec.Summary, "1 turn(s), 1 answered")
	require.Equal(t, "+90********33", rec.Peer, "numbers are stored masked")
	require.Zero(t, h.prov.Recognizer.OpenStreams(), "recognition stream closed")
	require.Equal(t, 1, leg.Hangups())
}

func TestDuplicateFinalTranscriptRunsOneTurn(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	_, leg, done := h.call("CA2")
	stream := h.prov.Recognizer.Latest()
	stream.Push(voice.Event{Type: voice.EventTranscriptFinal, Text: "Hava nasıl", Final: true})
	stream.Push(voice.Event{Type: voice.EventTranscriptFinal, Text: "Hava nasıl", Final: true})
	stream.Push(voice.Event{Type: voice.EventTranscriptFinal, Text: "  ", Final: true})
	h.eventually(func() bool { return strings.Contains(leg.Heard(), "Cevap: Hava nasıl") }, "reply")
	time.Sleep(100 * time.Millisecond)
	require.Len(t, h.runner.Runs(), 1, "duplicate and empty finals start no extra turn")
	leg.CallerHangup()
	h.wait(done)
}

func TestSameRunIDNeverRunsTwice(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	release := make(chan struct{})
	h.runner.setAnswer(func(ctx context.Context, req voice.TurnRequest) (voice.TurnResult, error) {
		<-release
		return voice.TurnResult{Text: "tek"}, nil
	})
	req := voice.TextTurnRequest{CallID: "c", SessionID: "s", TurnID: "t1", RunID: "c:1", Text: "merhaba"}
	var wg sync.WaitGroup
	results := make([]string, 3)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			text, err := h.g.RunTextTurn(context.Background(), req)
			require.NoError(t, err)
			results[i] = text
		}()
	}
	h.eventually(func() bool { return len(h.runner.Runs()) == 1 }, "first run")
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	require.Len(t, h.runner.Runs(), 1)
	require.Equal(t, []string{"tek", "tek", "tek"}, results)
	_, err := h.g.RunTextTurn(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, h.runner.Runs(), 1, "a finished run id returns the cached result")
}

func TestBargeInStopsPlaybackAndDropsStaleAudio(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	_, leg, done := h.call("CA3")
	h.prov.Synthesizer.ChunkDelay.Store(int64(20 * time.Millisecond))
	long := strings.Repeat("Bu çok uzun bir cevap cümlesi. ", 20)
	h.runner.setAnswer(func(ctx context.Context, req voice.TurnRequest) (voice.TurnResult, error) {
		if req.Text == "ilk" {
			return voice.TurnResult{Text: long}, nil
		}
		return voice.TurnResult{Text: "ikinci cevap"}, nil
	})
	leg.Say("ilk")
	h.eventually(func() bool { return strings.Contains(leg.Heard(), "Bu çok uzun") }, "long reply starts")
	leg.Raw("partial:dur")
	h.eventually(func() bool { return leg.Clears() >= 1 }, "playback cleared")
	heardAtBarge := len(leg.Heard())
	leg.Say("ikinci")
	h.eventually(func() bool { return strings.Contains(leg.Heard(), "ikinci cevap") }, "second reply")
	time.Sleep(150 * time.Millisecond)

	for _, f := range leg.SentFrames()[0:] {
		require.NotEmpty(t, f.TurnID)
	}
	tail := leg.Heard()[heardAtBarge:]
	require.NotContains(t, tail, "uzun bir cevap cümlesi. Bu çok uzun bir cevap cümlesi. Bu çok uzun bir cevap cümlesi. Bu çok uzun bir cevap cümlesi. Bu çok uzun bir cevap cümlesi. Bu çok uzun bir cevap cümlesi.", "the abandoned reply did not keep playing")
	for _, f := range leg.SentFrames() {
		if strings.Contains(string(f.Data), "ikin") {
			require.Equal(t, "t2", f.TurnID)
		}
	}
	leg.CallerHangup()
	h.wait(done)
	rec, _ := h.store.Get(h.store.List(1)[0].ID)
	require.GreaterOrEqual(t, rec.Metrics.BargeIns, 1)
}

func TestNewUtteranceCancelsSlowTurn(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	_, leg, done := h.call("CA4")
	cancelled := make(chan struct{})
	h.runner.setAnswer(func(ctx context.Context, req voice.TurnRequest) (voice.TurnResult, error) {
		if req.Text == "yavaş" {
			<-ctx.Done()
			close(cancelled)
			return voice.TurnResult{Text: "eski cevap"}, ctx.Err()
		}
		return voice.TurnResult{Text: "yeni cevap"}, nil
	})
	leg.Say("yavaş")
	h.eventually(func() bool { return h.runner.active.Load() == 1 }, "slow turn running")
	leg.Say("vazgeç başka bir şey")
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("slow turn was not cancelled")
	}
	h.eventually(func() bool { return strings.Contains(leg.Heard(), "yeni cevap") }, "new reply")
	require.NotContains(t, leg.Heard(), "eski cevap")
	leg.CallerHangup()
	h.wait(done)
}

func TestHangupDuringToolCancelsEverything(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	id, leg, done := h.call("CA5")
	toolCancelled := make(chan struct{})
	h.runner.setAnswer(func(ctx context.Context, req voice.TurnRequest) (voice.TurnResult, error) {
		<-ctx.Done()
		close(toolCancelled)
		return voice.TurnResult{}, ctx.Err()
	})
	leg.Say("uzun süren iş")
	h.eventually(func() bool { return h.runner.active.Load() == 1 }, "turn running")
	leg.CallerHangup()
	select {
	case <-toolCancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("agent turn was not cancelled on hangup")
	}
	h.wait(done)
	require.Zero(t, h.runner.active.Load())
	require.Zero(t, h.prov.Recognizer.OpenStreams())
	rec, _ := h.store.Get(id)
	require.True(t, rec.State.Terminal())
	require.True(t, rec.Turns[0].Cancelled)
	require.Empty(t, h.g.ActiveCalls())
}

// confirmingRunner simulates the agent calling a tool that needs
// confirmation, through the policy carried on the context.
func confirmingAnswer(result *atomic.Value) func(ctx context.Context, req voice.TurnRequest) (voice.TurnResult, error) {
	return func(ctx context.Context, req voice.TurnRequest) (voice.TurnResult, error) {
		turn, ok := voice.TurnFrom(ctx)
		if !ok {
			return voice.TurnResult{}, errors.New("no voice turn on context")
		}
		auth, err := turn.Authorize(ctx, "mcp_calendar_create_event", `{"summary":"Ahmet ile toplantı","start":"2026-11-02T10:00","duration":"30m","attendees":["Ahmet"]}`)
		result.Store(auth)
		if err != nil {
			return voice.TurnResult{}, err
		}
		if auth.Run {
			return voice.TurnResult{Text: "Toplantı oluşturuldu."}, nil
		}
		return voice.TurnResult{Text: "Toplantıyı oluşturmadım."}, nil
	}
}

func TestSpokenConfirmation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		answer  string
		run     bool
		spoken  string
		confirm bool
	}{
		{"evet onaylıyorum", true, "Toplantı oluşturuldu.", true},
		{"hayır", false, "Toplantıyı oluşturmadım.", false},
		{"bilmiyorum", false, "Toplantıyı oluşturmadım.", false},
	} {
		t.Run(tc.answer, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, nil)
			var result atomic.Value
			h.runner.setAnswer(confirmingAnswer(&result))
			_, leg, done := h.call("CA-" + tc.answer)
			leg.Say("Yarın saat 10'da Ahmet ile toplantı oluştur")
			h.eventually(func() bool { return strings.Contains(leg.Heard(), "Onay gerekiyor") }, "confirmation prompt")
			heard := leg.Heard()
			for _, want := range []string{"Ahmet ile toplantı", "2026-11-02T10:00", "30m", "attendees Ahmet"} {
				require.Contains(t, heard, want)
			}
			leg.Say(tc.answer)
			h.eventually(func() bool { return strings.Contains(leg.Heard(), tc.spoken) }, "result spoken")
			auth := result.Load().(voice.Authorization)
			require.Equal(t, tc.run, auth.Run)
			require.Equal(t, tc.confirm, auth.Confirmed)
			require.Len(t, h.runner.Runs(), 1, "the answer is consumed by the confirmation, not a new turn")
			leg.CallerHangup()
			h.wait(done)
		})
	}
}

func TestHangupWhileAwaitingConfirmationRunsNothing(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	var result atomic.Value
	h.runner.setAnswer(confirmingAnswer(&result))
	_, leg, done := h.call("CA6")
	leg.Say("toplantı oluştur")
	h.eventually(func() bool { return strings.Contains(leg.Heard(), "Onay gerekiyor") }, "confirmation prompt")
	leg.CallerHangup()
	h.wait(done)
	h.eventually(func() bool { return result.Load() != nil }, "authorization finished")
	require.False(t, result.Load().(voice.Authorization).Run)
}

func TestSilenceTimeoutEndsCall(t *testing.T) {
	t.Parallel()
	h := newHarness(t, func(c *voice.Config) { c.SilenceTimeoutSeconds = 1 })
	id, leg, done := h.call("CA7")
	h.wait(done)
	require.Contains(t, leg.Heard(), "Seni duyamıyorum")
	rec, _ := h.store.Get(id)
	require.Equal(t, voice.StateEnded, rec.State)
	require.Equal(t, voice.CodeTimeout, rec.Code)
}

func TestMaxCallDuration(t *testing.T) {
	t.Parallel()
	h := newHarness(t, func(c *voice.Config) { c.MaxCallSeconds = 1 })
	id, token, err := h.g.AdmitInbound("CA8", ownerNumber)
	require.NoError(t, err)
	leg := mock.NewLeg()
	// The daily budget makes the call at least 15s, so shorten through the
	// per-call limit only: Admit uses min(max_call_seconds, remaining).
	done := make(chan struct{})
	go func() { defer close(done); _ = h.g.Attach(context.Background(), id, token, leg) }()
	h.wait(done)
	rec, _ := h.store.Get(id)
	// One second is below the useful minimum, so admission refuses it.
	require.Equal(t, voice.StateBudgetExceeded, rec.State)
	require.Equal(t, 1, leg.Hangups())
}

func TestCallTimeLimitSpeaksAndEnds(t *testing.T) {
	t.Parallel()
	h := newHarness(t, func(c *voice.Config) { c.MaxCallSeconds = 16 })
	start := time.Now()
	id, leg, done := h.call("CA9")
	_ = leg
	select {
	case <-done:
	case <-time.After(25 * time.Second):
		t.Fatal("call exceeded its time limit")
	}
	require.GreaterOrEqual(t, time.Since(start), 15*time.Second)
	rec, _ := h.store.Get(id)
	require.Equal(t, voice.StateBudgetExceeded, rec.State)
	require.Contains(t, leg.Heard(), "süresi doldu")
}

func TestRecognitionReconnects(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	id, leg, done := h.call("CA10")
	leg.Raw("drop")
	h.eventually(func() bool { return h.prov.Recognizer.Opened() == 2 }, "reconnect")
	leg.Say("tekrar bağlandın mı")
	h.eventually(func() bool { return strings.Contains(leg.Heard(), "Cevap: tekrar bağlandın mı") }, "reply after reconnect")
	leg.CallerHangup()
	h.wait(done)
	rec, _ := h.store.Get(id)
	require.Equal(t, 1, rec.Metrics.STTReconnects)
	require.Equal(t, voice.StateEnded, rec.State)
}

func TestFatalRecognitionErrorEndsCall(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	id, leg, done := h.call("CA11")
	leg.Raw("fatal")
	h.wait(done)
	require.Contains(t, leg.Heard(), "Ses hizmetinde bir sorun")
	rec, _ := h.store.Get(id)
	require.Equal(t, voice.StateFailed, rec.State)
	require.Equal(t, 1, h.prov.Recognizer.Opened(), "permanent errors are not retried")
}

func TestAgentErrorIsSpokenSafely(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	h.runner.setAnswer(func(ctx context.Context, req voice.TurnRequest) (voice.TurnResult, error) {
		if req.Text == "izin" {
			return voice.TurnResult{}, voice.Errorf(voice.CodePermissionRequired, "turn", errors.New("token=abc123 at https://internal"))
		}
		return voice.TurnResult{}, errors.New("provider 500: api_key=sk-secret")
	})
	id, leg, done := h.call("CA12")
	leg.Say("bozuk")
	h.eventually(func() bool { return strings.Contains(leg.Heard(), "tamamlayamadım") }, "generic error phrase")
	leg.Say("izin")
	h.eventually(func() bool { return strings.Contains(leg.Heard(), "izin gerekiyor") }, "permission phrase")
	require.NotContains(t, leg.Heard(), "sk-secret")
	require.NotContains(t, leg.Heard(), "abc123")
	leg.CallerHangup()
	h.wait(done)
	rec, _ := h.store.Get(id)
	require.Equal(t, voice.CodeAgentFailed, rec.Turns[0].Code)
	require.Equal(t, voice.CodePermissionRequired, rec.Turns[1].Code)
}

func TestTTSFailureEndsCall(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	id, leg, done := h.call("CA13")
	h.prov.Synthesizer.Fail.Store(true)
	leg.Say("bir")
	h.eventually(func() bool { return len(h.runner.Runs()) == 1 }, "first turn")
	time.Sleep(50 * time.Millisecond)
	leg.Say("iki")
	h.wait(done)
	rec, _ := h.store.Get(id)
	require.Equal(t, voice.StateFailed, rec.State)
}

func TestInboundAdmission(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	_, _, err := h.g.AdmitInbound("CA-x", "+15550000000")
	require.ErrorIs(t, err, voice.ErrNotAllowed)
	rejected := h.store.List(1)[0]
	require.Equal(t, voice.StateRejected, rejected.State)

	id1, tok1, err := h.g.AdmitInbound("CA-dup", ownerNumber)
	require.NoError(t, err)
	id2, tok2, err := h.g.AdmitInbound("CA-dup", ownerNumber)
	require.NoError(t, err)
	require.Equal(t, id1, id2, "a repeated webhook maps to the same call")
	require.Equal(t, tok1, tok2)

	require.ErrorIs(t, h.g.Attach(context.Background(), id1, "wrong-token", mock.NewLeg()), voice.ErrNotAllowed)

	_, _, err = h.g.AdmitInbound("CA-other", ownerNumber)
	require.ErrorIs(t, err, voice.ErrBudgetExceeded, "one concurrent call by default, pending counts")

	leg := mock.NewLeg()
	done := make(chan struct{})
	go func() { defer close(done); _ = h.g.Attach(context.Background(), id1, tok1, leg) }()
	h.eventually(func() bool { return len(h.g.ActiveCalls()) == 1 }, "active")
	require.ErrorIs(t, h.g.Attach(context.Background(), id1, tok1, mock.NewLeg()), voice.ErrCallExists, "no second session for one call")
	leg.CallerHangup()
	h.wait(done)
}

func TestShutdownEndsActiveCalls(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	_, _, done := h.call("CA14")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h.g.Shutdown(ctx)
	h.wait(done)
	require.Zero(t, h.prov.Recognizer.OpenStreams())
	_, _, err := h.g.AdmitInbound("CA15", ownerNumber)
	require.ErrorIs(t, err, voice.ErrServiceDisabled)
}

func TestStatusBeforeBindIsApplied(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	callID, _, outcome, err := h.g.PrepareOutbound(voice.OutboundRequest{Reason: "x", EventID: "e"})
	require.NoError(t, err)
	// The provider reports on the call before its create request returned.
	h.g.HandleStatus("CA-early", voice.StatusNoAnswer)
	h.g.BindProvider(callID, "CA-early")
	select {
	case o := <-outcome:
		require.Equal(t, voice.StateNoAnswer, o.State)
	case <-time.After(5 * time.Second):
		t.Fatal("early status was lost")
	}
	rec, _ := h.store.Get(callID)
	require.Equal(t, voice.StateNoAnswer, rec.State)
	require.Equal(t, "CA-early", rec.ProviderCallID)
}

func TestSlowTurnGetsOneHoldingPhrase(t *testing.T) {
	t.Parallel()
	store := voice.NewMemoryStore()
	prov := mock.New()
	runner := &fakeRunner{}
	runner.setAnswer(func(ctx context.Context, req voice.TurnRequest) (voice.TurnResult, error) {
		select {
		case <-time.After(300 * time.Millisecond):
		case <-ctx.Done():
		}
		return voice.TurnResult{Text: "Hazır."}, nil
	})
	cfg := voice.Config{Enabled: true, Owner: voice.OwnerConfig{PhoneNumber: ownerNumber}, SilenceTimeoutSeconds: 30}
	g, err := voice.NewGateway(voice.GatewayOptions{Config: cfg, Store: store, Recognizer: prov.Recognizer, Synthesizer: prov.Synthesizer, Runner: runner, ThinkingDelay: 50 * time.Millisecond})
	require.NoError(t, err)
	t.Cleanup(func() { g.Shutdown(context.Background()) })
	h := &harness{t: t, g: g, store: store, prov: prov, runner: runner, cfg: cfg}
	_, leg, done := h.call("CA-slow")
	leg.Say("rapor hazırla")
	h.eventually(func() bool { return strings.Contains(leg.Heard(), "Hazır.") }, "answer")
	heard := leg.Heard()
	require.Equal(t, 1, strings.Count(heard, "Bir saniye"))
	require.Less(t, strings.Index(heard, "Bir saniye"), strings.Index(heard, "Hazır."))
	leg.CallerHangup()
	h.wait(done)
}

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice/providers/mock"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice/transport/whatsapp"
	"github.com/stretchr/testify/require"
)

type runner struct{}

func (runner) NewSession(context.Context, string) (string, error) { return "s", nil }

func (runner) RunTurn(context.Context, voice.TurnRequest) (voice.TurnResult, error) {
	return voice.TurnResult{Text: "ok"}, nil
}

type noAnswerDialer struct{ gw func() *voice.Gateway }

func (d noAnswerDialer) Dial(_ context.Context, req voice.DialRequest) (string, error) {
	go d.gw().HandleStatus("CA-"+req.CallID, voice.StatusNoAnswer)
	return "CA-" + req.CallID, nil
}

func (noAnswerDialer) Cancel(context.Context, string) error { return nil }

func TestServiceBuildsWhatsAppTransport(t *testing.T) {
	t.Parallel()
	cfg := voice.Config{
		Enabled: true, Transport: "whatsapp",
		Owner:    voice.OwnerConfig{PhoneNumber: "+905551112233"},
		Listen:   voice.ListenConfig{ControlAddr: "127.0.0.1:0"},
		WhatsApp: voice.WhatsAppConfig{AccessToken: "$WA_TOKEN", AppSecret: "$WA_SECRET", VerifyToken: "$WA_VERIFY", PhoneNumberID: "104560001"},
	}
	env := map[string]string{"WA_TOKEN": "token", "WA_SECRET": "secret", "WA_VERIFY": "verify-me"}
	opts := Options{
		Config: cfg, DataDir: t.TempDir(), Runner: runner{},
		Providers: voice.Providers{"elevenlabs": mock.New().Voice()},
		Env:       func(k string) string { return env[k] },
	}
	svc, err := New(opts)
	require.NoError(t, err)
	require.NotNil(t, svc.Transport, "the whatsapp transport serves the public listener")

	srv := httptest.NewServer(svc.Transport.Handler())
	t.Cleanup(srv.Close)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+whatsapp.PathWebhook+"?hub.mode=subscribe&hub.verify_token=verify-me&hub.challenge=abc", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "abc", string(body))

	delete(env, "WA_SECRET")
	opts.DataDir = t.TempDir()
	_, err = New(opts)
	require.Error(t, err, "a missing app secret stops the service from starting")
	require.NotContains(t, err.Error(), "secret-value")
}

func TestServiceLifecycle(t *testing.T) {
	t.Parallel()
	_, err := New(Options{Config: voice.Config{}, Runner: runner{}})
	require.ErrorIs(t, err, voice.ErrServiceDisabled, "disabled by default")

	_, err = New(Options{Config: voice.Config{Enabled: true}, Runner: runner{}, Env: func(string) string { return "" }})
	require.Error(t, err, "an incomplete enabled configuration does not start")

	dir := t.TempDir()
	prov := mock.New()
	var svc *Service
	svc, err = New(Options{
		Config: voice.Config{
			Enabled: true, Owner: voice.OwnerConfig{PhoneNumber: "+905551112233"},
			Listen:   voice.ListenConfig{ControlAddr: "127.0.0.1:0"},
			Outbound: voice.OutboundConfig{RingTimeoutSeconds: 5},
		},
		DataDir: dir, Runner: runner{},
		Providers: voice.Providers{"elevenlabs": prov.Voice()},
		Dialer:    noAnswerDialer{gw: func() *voice.Gateway { return svc.Gateway }},
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()

	var client *voice.Client
	require.Eventually(t, func() bool {
		client, err = voice.DialControl(dir)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond)
	st, err := client.Status(context.Background())
	require.NoError(t, err)
	require.True(t, st.Enabled)
	require.Equal(t, 30, st.RemainingMinutes)

	res, err := client.CallOwner(context.Background(), voice.OwnerCallRequest{Reason: "Günün sözü", EventID: "routine:morning-call:2026-11-01"})
	require.NoError(t, err)
	require.Equal(t, voice.ResultNoAnswer, res.Result)
	again, err := client.CallOwner(context.Background(), voice.OwnerCallRequest{Reason: "Günün sözü", EventID: "routine:morning-call:2026-11-01"})
	require.NoError(t, err)
	require.Equal(t, voice.ResultDuplicate, again.Result, "a second run of the same morning routine does not ring")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("service did not stop")
	}
	_, err = os.Stat(voice.ControlStatePath(dir))
	require.True(t, os.IsNotExist(err), "control state is removed on shutdown")

	// Call records survive a restart.
	store, err := voice.OpenStore(voice.StateDir(dir))
	require.NoError(t, err)
	rec, ok := store.Get(res.CallID)
	require.True(t, ok)
	require.Equal(t, voice.StateNoAnswer, rec.State)
}

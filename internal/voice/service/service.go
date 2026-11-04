// Package service assembles the voice assistant from configuration: the
// call store, budget, speech provider, phone transport, gateway, router,
// site monitor and the two HTTP listeners (public webhooks behind TLS and
// the loopback control API). It is the only place that knows which vendor
// implements which interface.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice/providers/elevenlabs"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice/transport/twilio"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice/transport/whatsapp"
)

// Options configures a [Service].
type Options struct {
	Config  voice.Config
	DataDir string
	Runner  voice.TurnRunner
	// ReadOnly reports tools Atlas classifies as read-only.
	ReadOnly func(string) bool
	Env      func(string) string
	// Providers overrides the speech providers built from configuration,
	// for tests.
	Providers voice.Providers
	// Dialer overrides the transport's dialer, for tests.
	Dialer voice.Dialer
}

// PublicTransport is a call transport that serves webhooks on the public
// listener.
type PublicTransport interface {
	Handler() http.Handler
	Wait()
}

// Service is a running voice assistant.
type Service struct {
	cfg       voice.Config
	dataDir   string
	Store     *voice.Store
	Budget    *voice.Budget
	Gateway   *voice.Gateway
	Router    *voice.Router
	Monitor   *voice.Monitor
	Transport PublicTransport
	token     string
	notifyWG  sync.WaitGroup
}

// New validates the configuration and builds the service. It contacts no
// external service.
func New(opts Options) (*Service, error) {
	env := opts.Env
	if env == nil {
		env = os.Getenv
	}
	cfg := opts.Config.WithDefaults()
	if !cfg.Enabled {
		return nil, voice.ErrServiceDisabled
	}
	if err := cfg.Validate(env); err != nil && opts.Providers == nil {
		return nil, fmt.Errorf("voice configuration: %w", err)
	}
	if opts.Runner == nil {
		return nil, errors.New("voice service needs a turn runner")
	}
	store, err := voice.OpenStore(voice.StateDir(opts.DataDir))
	if err != nil {
		return nil, err
	}
	budget := voice.NewBudget(cfg, store, nil)

	providers := opts.Providers
	if providers == nil {
		el, err := elevenlabs.FromConfig(cfg.ElevenLabs, env)
		if err != nil {
			return nil, err
		}
		providers = voice.Providers{"elevenlabs": el}
	}
	stt, tts, err := providers.Resolve(cfg)
	if err != nil {
		return nil, err
	}

	s := &Service{cfg: cfg, dataDir: opts.DataDir, Store: store, Budget: budget}
	gw, err := voice.NewGateway(voice.GatewayOptions{
		Config: cfg, Store: store, Budget: budget, Recognizer: stt, Synthesizer: tts,
		Runner: opts.Runner, Policy: voice.NewToolPolicy(cfg.ToolPolicy, opts.ReadOnly),
	})
	if err != nil {
		return nil, err
	}
	s.Gateway = gw

	var notifiers []voice.Notifier
	dialer := opts.Dialer
	if dialer == nil && cfg.Transport == "whatsapp" {
		t, err := newWhatsApp(cfg, env, gw)
		if err != nil {
			return nil, err
		}
		s.Transport, dialer = t, t
	} else if dialer == nil {
		sid, err := cfg.Twilio.AccountSID.Resolve(env)
		if err != nil {
			return nil, voice.Errorf(voice.CodeNotConfigured, "twilio", err)
		}
		authToken, err := cfg.Twilio.AuthToken.Resolve(env)
		if err != nil {
			return nil, voice.Errorf(voice.CodeNotConfigured, "twilio", err)
		}
		client := &twilio.Client{AccountSID: sid, AuthToken: authToken, BaseURL: cfg.Twilio.APIBaseURL}
		t := twilio.New(twilio.Options{Config: cfg.Twilio, AuthToken: authToken, Client: client, Gateway: gw})
		s.Transport, dialer = t, t
		owner, _ := cfg.OwnerNumber(env)
		notifiers = append(notifiers,
			&twilio.Notifier{Client: client, Kind: voice.ChannelSMS, From: cfg.Twilio.PhoneNumber, OwnerTo: owner})
		if cfg.Twilio.WhatsAppFrom != "" {
			notifiers = append(notifiers,
				&twilio.Notifier{Client: client, Kind: voice.ChannelWhatsAppText, From: cfg.Twilio.WhatsAppFrom, OwnerTo: owner})
		}
	}
	if tok, err := cfg.Telegram.BotToken.Resolve(env); err == nil && cfg.Telegram.ChatID != "" {
		notifiers = append(notifiers, &voice.TelegramNotifier{Token: tok, ChatID: cfg.Telegram.ChatID})
	}
	s.Router = voice.NewRouter(voice.RouterOptions{
		Config: cfg, Gateway: gw, Dialer: dialer, Notifiers: notifiers, Store: store, Budget: budget, Env: env,
	})
	if len(cfg.Monitor.Sites) > 0 {
		s.Monitor = voice.NewMonitor(cfg.Monitor, store, nil, s.onSiteEvent, nil)
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	s.token = hex.EncodeToString(b)
	return s, nil
}

func newWhatsApp(cfg voice.Config, env func(string) string, gw *voice.Gateway) (*whatsapp.Transport, error) {
	secret := func(name string, ref voice.SecretRef) (voice.Secret, error) {
		s, err := ref.Resolve(env)
		if err != nil {
			return voice.Secret{}, voice.Errorf(voice.CodeNotConfigured, "whatsapp "+name, err)
		}
		return s, nil
	}
	token, err := secret("access token", cfg.WhatsApp.AccessToken)
	if err != nil {
		return nil, err
	}
	appSecret, err := secret("app secret", cfg.WhatsApp.AppSecret)
	if err != nil {
		return nil, err
	}
	verify, err := secret("verify token", cfg.WhatsApp.VerifyToken)
	if err != nil {
		return nil, err
	}
	client := &whatsapp.Client{
		AccessToken: token, PhoneNumberID: cfg.WhatsApp.PhoneNumberID,
		BaseURL: cfg.WhatsApp.APIBaseURL, Version: cfg.WhatsApp.GraphVersion,
	}
	return whatsapp.New(whatsapp.Options{
		Config: cfg.WhatsApp, AppSecret: appSecret, VerifyToken: verify, Client: client, Gateway: gw,
	}), nil
}

// onSiteEvent calls the owner about a site transition. The event id makes
// a repeated notification a no-op in the router.
func (s *Service) onSiteEvent(ctx context.Context, ev voice.SiteEvent) {
	p := voice.PhrasesFor(s.cfg.Language)
	reason := fmt.Sprintf(p.SiteRecovered, ev.Site)
	if ev.Kind == voice.SiteWentDown {
		reason = fmt.Sprintf(p.SiteDown, ev.Site, ev.Detail)
	}
	s.notifyWG.Add(1)
	go func() {
		defer s.notifyWG.Done()
		res, err := s.Router.CallOwner(context.WithoutCancel(ctx), voice.OwnerCallRequest{Reason: reason, Priority: ev.Priority, EventID: ev.EventID})
		if err != nil {
			slog.Warn("Failed to call owner about site", "site", ev.Site, "code", voice.CodeOf(err))
			return
		}
		slog.Info("Called owner about site", "site", ev.Site, "event", ev.Kind, "result", res.Result)
	}()
}

// ControlHandler returns the loopback API handler.
func (s *Service) ControlHandler() http.Handler {
	return (&voice.ControlServer{
		Config: s.cfg, Router: s.Router, Gateway: s.Gateway, Store: s.Store,
		Budget: s.Budget, Monitor: s.Monitor, Token: s.token,
	}).Handler()
}

// Run serves until ctx ends, then shuts every part down: calls are ended,
// listeners closed and the control state file removed.
func (s *Service) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	addr, controlErr, err := voice.ServeControl(ctx, s.cfg.Listen.ControlAddr, s.ControlHandler())
	if err != nil {
		return err
	}
	statePath := voice.ControlStatePath(s.dataDir)
	if err := voice.WriteControlState(statePath, voice.ControlState{Addr: addr.String(), Token: s.token, PID: os.Getpid(), StartedAt: time.Now()}); err != nil {
		return fmt.Errorf("write voice control state: %w", err)
	}
	defer os.Remove(statePath)

	var publicErr chan error
	var public *http.Server
	if s.Transport != nil {
		ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.cfg.Listen.PublicAddr)
		if err != nil {
			return fmt.Errorf("listen on voice public address: %w", err)
		}
		public = &http.Server{
			Handler:           s.Transport.Handler(),
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			MaxHeaderBytes:    16 << 10,
		}
		publicErr = make(chan error, 1)
		go func() {
			err := public.Serve(ln)
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
			publicErr <- err
		}()
		slog.Info("Voice webhooks listening", "addr", ln.Addr().String())
	}
	slog.Info("Voice control API listening", "addr", addr.String())

	var monitorDone chan struct{}
	if s.Monitor != nil {
		monitorDone = make(chan struct{})
		go func() {
			defer close(monitorDone)
			s.Monitor.Run(ctx)
		}()
	}

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-controlErr:
	case runErr = <-publicErr:
	}
	cancel()

	sctx, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	s.Gateway.Shutdown(sctx)
	if public != nil {
		if err := public.Shutdown(sctx); err != nil {
			_ = public.Close()
		}
		s.Transport.Wait()
	}
	if monitorDone != nil {
		<-monitorDone
	}
	s.notifyWG.Wait()
	return runErr
}

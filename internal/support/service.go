package support

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
)

// ServiceConfig is read on first use of each end.
type ServiceConfig struct {
	Name  string
	Relay *RelayConfig
	// LANListen is the local network listen address; empty means any
	// interface on a random port.
	LANListen     string
	DiscoveryPort int
	// Language is the interface language for the on-screen banner.
	Language string
	// Overlay replaces the on-screen activity banner (tests); nil uses
	// the shared activity overlay.
	Overlay         Overlay
	ExecutorFactory func(string, computer.Backend) nodes.RemoteToolExecutor
}

// Service owns this Atlas process's support ends: at most one Host (this
// computer receiving support) and one Controller (this computer supporting
// another). The /support screen and the remote_support tool share it, so
// there is exactly one session state, permission system and history.
type Service struct {
	dir    string
	config func() ServiceConfig
	logger *slog.Logger

	mu         sync.Mutex
	host       *Host
	controller *Controller
	listeners  []chan struct{}
	closed     bool
}

// NewService keeps state under dir (identity, journal, history).
func NewService(dir string, config func() ServiceConfig, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if config == nil {
		config = func() ServiceConfig { return ServiceConfig{} }
	}
	return &Service{dir: dir, config: config, logger: logger}
}

var errServiceClosed = errors.New("remote support is shutting down")

// Changes returns a channel that receives a value after any visible
// change on either end. Slow readers miss intermediate signals, never the
// latest one.
func (s *Service) Changes() <-chan struct{} {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.listeners = append(s.listeners, ch)
	s.mu.Unlock()
	return ch
}

func (s *Service) changed() {
	s.mu.Lock()
	listeners := s.listeners
	s.mu.Unlock()
	for _, ch := range listeners {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Host returns this computer's host end, starting it on first use.
func (s *Service) Host(ctx context.Context) (*Host, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errServiceClosed
	}
	cfg := s.config()
	if s.host != nil {
		s.host.setRelay(cfg.Relay)
		return s.host, nil
	}
	var overlay Overlay = &ActivityOverlay{Language: cfg.Language}
	if cfg.Overlay != nil {
		overlay = cfg.Overlay
	}
	desk, reason := computer.OpenOrNil(), ""
	if desk == nil {
		reason = "desktop control is not available on this computer or session"
	}
	var executor nodes.RemoteToolExecutor
	if cfg.ExecutorFactory != nil {
		executor = cfg.ExecutorFactory(filepath.Join(s.dir, "host"), desk)
	}
	h, err := StartHost(context.WithoutCancel(ctx), HostOptions{
		StateDir: filepath.Join(s.dir, "host"), Name: cfg.Name, Relay: cfg.Relay, DiscoveryPort: cfg.DiscoveryPort, LANListen: cfg.LANListen,
		Desktop: desk, DesktopReason: reason, Overlay: overlay, Logger: s.logger, OnChange: s.changed, Executor: executor,
	})
	if err != nil {
		return nil, err
	}
	s.host = h
	return h, nil
}

// Controller returns this computer's supporting end, creating it on first
// use.
func (s *Service) Controller(ctx context.Context) (*Controller, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errServiceClosed
	}
	cfg := s.config()
	if s.controller != nil {
		s.controller.setRelay(cfg.Relay)
		return s.controller, nil
	}
	c, err := NewController(context.WithoutCancel(ctx), ControllerOptions{
		StateDir: filepath.Join(s.dir, "controller"), Name: cfg.Name, Relay: cfg.Relay, DiscoveryPort: cfg.DiscoveryPort, Logger: s.logger, OnChange: s.changed,
	})
	if err != nil {
		return nil, err
	}
	s.controller = c
	return c, nil
}

// ActiveController returns the controller only if one exists, without
// creating it; the agent tool uses it so a model call never creates
// identities or sessions.
func (s *Service) ActiveController() *Controller {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.controller
}

// ActiveHost returns the host only if one exists.
func (s *Service) ActiveHost() *Host {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.host
}

// Close ends every session and releases listeners.
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	h, c := s.host, s.controller
	s.mu.Unlock()
	if h != nil {
		h.Close()
	}
	if c != nil {
		c.Close()
	}
}

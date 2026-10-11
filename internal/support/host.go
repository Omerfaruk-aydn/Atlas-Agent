package support

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"golang.org/x/net/http2"
)

const (
	maxCodeAttempts        = 5
	defaultCodeTTL         = 10 * time.Minute
	defaultSessionDuration = 2 * time.Hour
	defaultAcceptTimeout   = 3 * time.Minute
	defaultReconnectWindow = 2 * time.Minute
	// DefaultMaxFileBytes bounds one support transfer; chunked uploads keep
	// memory use small regardless of the size.
	DefaultMaxFileBytes int64 = 4 << 30
)

// Overlay registers the connection at Start and displays indicators during
// Operation, using the same banner and cursor as local computer use. An idle
// connection must not claim the cursor. End must remove every indicator.
type Overlay interface {
	Start(sessionID, peer string, stop func())
	Operation(action string, x, y int, point bool) func()
	End()
}

// HostOptions configures the computer that receives support.
type HostOptions struct {
	// StateDir holds the host identity, operation journal, uploads,
	// session records and history. Each host needs its own.
	StateDir string
	// Name is shown to the supporter; default is the computer name.
	Name string
	// LANListen is the TCP address for local network mode, ":0" by default.
	LANListen string
	// DiscoveryPort answers LAN discovery; 0 means the default, negative
	// disables discovery (the supporter then needs CODE@address).
	DiscoveryPort int
	// Relay enables internet mode through a relay you operate.
	Relay *RelayConfig
	// Executor runs the host's browser, computer and long workflows.
	Executor nodes.RemoteToolExecutor
	// Desktop is the interactive session backend, nil when unavailable.
	Desktop       computer.Backend
	DesktopReason string
	Overlay       Overlay
	MaxFileBytes  int64
	Logger        *slog.Logger
	// OnChange is called after every visible change; it must not block.
	OnChange func()
	// Timeouts, overridable for tests.
	AcceptTimeout   time.Duration
	ReconnectWindow time.Duration
}

// OfferOptions is what the owner chose before creating a code.
type OfferOptions struct {
	Capabilities    []Capability
	Roots           []nodes.Root
	CodeTTL         time.Duration
	SessionDuration time.Duration
	LAN             bool
	Internet        bool
}

// OfferInfo is shown to the owner while a code is valid.
type OfferInfo struct {
	Code         string    `json:"code"`
	ExpiresAt    time.Time `json:"expires_at"`
	Link         string    `json:"link"`
	LANAddresses []string  `json:"lan_addresses,omitempty"`
	RelayURL     string    `json:"relay_url,omitempty"`
	// RelayStatus is registered, connecting or an error; never assumed.
	RelayStatus string `json:"relay_status,omitempty"`
}

// JoinRequest is a verified connection request awaiting the owner.
type JoinRequest struct {
	ID           string    `json:"id"`
	Peer         PeerInfo  `json:"peer"`
	Verification string    `json:"verification"`
	Network      string    `json:"network"`
	RemoteAddr   string    `json:"remote_addr"`
	At           time.Time `json:"at"`
	decision     chan joinDecision
}

type joinDecision struct {
	accept bool
	caps   []Capability
}

// PermissionRequest is a supporter's request the owner must answer:
// more capabilities or a restart. ID and Revision bind an answer to the
// exact request; a late answer to a canceled request changes nothing.
type PermissionRequest struct {
	ID           string       `json:"id"`
	Revision     int          `json:"revision"`
	Kind         string       `json:"kind"`
	Capabilities []Capability `json:"capabilities,omitempty"`
	Reason       string       `json:"reason"`
	State        string       `json:"state"`
	CreatedAt    time.Time    `json:"created_at"`
	// SessionID binds the request to the session that asked.
	SessionID string `json:"session_id"`
}

// OperationView is one recent operation for the owner's view.
type OperationView struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Summary    string `json:"summary"`
	State      string `json:"state"`
	Effect     string `json:"effect"`
	Generation uint64 `json:"generation"`
	StartedAt  int64  `json:"started_at"`
	EndedAt    int64  `json:"ended_at,omitempty"`
}

// SessionView is the owner's view of the active session.
type SessionView struct {
	ID           string          `json:"id"`
	Peer         PeerInfo        `json:"peer"`
	Capabilities []Capability    `json:"capabilities"`
	Roots        []nodes.Root    `json:"roots"`
	ExpiresAt    time.Time       `json:"expires_at"`
	StartedAt    time.Time       `json:"started_at"`
	AgentPaused  bool            `json:"agent_paused"`
	InputPaused  bool            `json:"input_paused"`
	Network      string          `json:"network"`
	Active       *OperationView  `json:"active,omitempty"`
	Recent       []OperationView `json:"recent,omitempty"`
}

// HostStatus is everything the owner's screen shows.
type HostStatus struct {
	State      State              `json:"state"`
	Reason     string             `json:"reason,omitempty"`
	Generation uint64             `json:"generation"`
	NodeID     string             `json:"node_id"`
	Name       string             `json:"name"`
	Offer      *OfferInfo         `json:"offer,omitempty"`
	Pending    *JoinRequest       `json:"pending,omitempty"`
	Session    *SessionView       `json:"session,omitempty"`
	Permission *PermissionRequest `json:"permission,omitempty"`
}

type hostSession struct {
	id          string
	peer        PeerInfo
	caps        []Capability
	roots       []nodes.Root
	tokenHash   [32]byte
	resumeHash  [32]byte
	resumeToken string
	started     time.Time
	expires     time.Time
	agentPaused bool
	inputPaused bool
	network     string
	active      map[string]*OperationView
	recent      []OperationView
	opEnds      map[string]func()
	firstToken  string
	expiry      *time.Timer
	reconnect   *time.Timer
	cancel      context.CancelFunc
}

type connInfo struct {
	generation uint64
	session    string
}

type connKey struct{}

// Host is the computer receiving support.
type Host struct {
	opts    HostOptions
	id      nodes.Identity
	node    *nodes.Server
	handler http.Handler
	history *History
	store   sessionStore
	owner   string
	ctx     context.Context
	cancel  context.CancelFunc
	logger  *slog.Logger

	mu          sync.Mutex
	machine     *Machine
	offer       *OfferOptions
	offerInfo   *OfferInfo
	code        PairingCode
	codeKey     []byte
	codeUsed    bool
	attempts    int
	offerCancel context.CancelFunc
	listener    net.Listener
	discovery   *discoveryResponder
	pending     *JoinRequest
	session     *hostSession
	conns       map[uint64]net.Conn
	permission  *PermissionRequest
	relayStatus string
	handshakes  chan struct{}
	linger      *time.Timer
	// work tracks connection goroutines so Close returns only after they
	// stopped writing state.
	work      sync.WaitGroup
	workMu    sync.Mutex
	closed    bool
	closeOnce sync.Once
}

// StartHost prepares the host identity and node API. It offers nothing
// until Offer is called.
func StartHost(ctx context.Context, opts HostOptions) (*Host, error) {
	if opts.StateDir == "" {
		return nil, errors.New("support host needs a state directory")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Name == "" {
		opts.Name, _ = os.Hostname()
	}
	opts.Name = sanitizeName(opts.Name, "computer")
	if opts.MaxFileBytes <= 0 {
		opts.MaxFileBytes = DefaultMaxFileBytes
	}
	if opts.AcceptTimeout <= 0 {
		opts.AcceptTimeout = defaultAcceptTimeout
	}
	if opts.ReconnectWindow <= 0 {
		opts.ReconnectWindow = defaultReconnectWindow
	}
	if opts.LANListen == "" {
		opts.LANListen = ":0"
	}
	if opts.DiscoveryPort == 0 {
		opts.DiscoveryPort = DefaultDiscoveryPort
	}
	root := filepath.Join(opts.StateDir, "empty-root")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	h := &Host{opts: opts, history: OpenHistory(filepath.Join(opts.StateDir, "history")), store: sessionStore{dir: filepath.Join(opts.StateDir, "sessions")}, machine: NewMachine(), conns: map[uint64]net.Conn{}, logger: opts.Logger, handshakes: make(chan struct{}, 8)}
	h.store.closeInterrupted()
	if u, err := user.Current(); err == nil {
		h.owner = u.Username
	}
	node, err := nodes.NewServer(nodes.ServerOptions{StateDir: filepath.Join(opts.StateDir, "node"), Root: root, Name: opts.Name, Capabilities: []nodes.Capability{nodes.CapExec, nodes.CapFiles, nodes.CapDesktop}, Desktop: opts.Desktop, DesktopReason: opts.DesktopReason, MaxFileBytes: opts.MaxFileBytes, Logger: opts.Logger, Access: h})
	if err != nil {
		return nil, err
	}
	h.node, h.id = node, node.Identity()
	h.handler = h.routes(node.Handler())
	h.ctx, h.cancel = context.WithCancel(ctx)
	return h, nil
}

// setRelay applies a changed relay setting to the next offer.
func (h *Host) setRelay(cfg *RelayConfig) {
	h.mu.Lock()
	h.opts.Relay = cfg
	h.mu.Unlock()
}

func (h *Host) relayConfig() *RelayConfig {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.opts.Relay
}

// NodeID is the host's stable device id.
func (h *Host) NodeID() string { return h.id.NodeID }

// Fingerprint is the host certificate fingerprint.
func (h *Host) Fingerprint() string { return h.id.Fingerprint }

// History exposes the owner's session history.
func (h *Host) History() *History { return h.history }

func (h *Host) notify() {
	if h.opts.OnChange != nil {
		h.opts.OnChange()
	}
}

var rootLabel = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} ._-]{0,39}$`)

// Offer creates a one-time code and starts listening on the chosen paths.
func (h *Host) Offer(o OfferOptions) (OfferInfo, error) {
	if !h.beginWork() {
		return OfferInfo{}, Errorf(CodeSessionRevoked, "the support host is closed")
	}
	defer h.work.Done()
	o.Capabilities = Normalize(o.Capabilities)
	if len(o.Capabilities) == 0 {
		return OfferInfo{}, errors.New("choose at least one permission")
	}
	if !o.LAN && !o.Internet {
		return OfferInfo{}, errors.New("choose local network, internet or both")
	}
	if o.Internet && !h.opts.Relay.Configured() {
		return OfferInfo{}, Errorf(CodeRelayNotConfigured, "internet mode needs a support relay you operate; set support_relay_url, or use local network mode")
	}
	if o.CodeTTL <= 0 {
		o.CodeTTL = defaultCodeTTL
	}
	o.CodeTTL = min(o.CodeTTL, 30*time.Minute)
	if o.SessionDuration <= 0 {
		o.SessionDuration = defaultSessionDuration
	}
	o.SessionDuration = min(o.SessionDuration, 8*time.Hour)
	seen := map[string]bool{}
	for i, r := range o.Roots {
		if !rootLabel.MatchString(r.Label) || seen[r.Label] {
			return OfferInfo{}, fmt.Errorf("folder label %q must be unique, 1-40 letters, digits, spaces, '.', '_' or '-'", r.Label)
		}
		seen[r.Label] = true
		if !filepath.IsAbs(r.Path) {
			return OfferInfo{}, fmt.Errorf("folder %q must be an absolute path", r.Path)
		}
		info, err := os.Stat(r.Path)
		if err != nil || !info.IsDir() {
			return OfferInfo{}, fmt.Errorf("folder %q does not exist", r.Path)
		}
		o.Roots[i].Path = filepath.Clean(r.Path)
	}
	code, err := NewPairingCode()
	if err != nil {
		return OfferInfo{}, err
	}
	h.mu.Lock()
	h.workMu.Lock()
	closed := h.closed
	h.workMu.Unlock()
	if closed {
		h.mu.Unlock()
		return OfferInfo{}, Errorf(CodeSessionRevoked, "the support host is closed")
	}
	if h.session != nil || h.pending != nil {
		h.mu.Unlock()
		return OfferInfo{}, errors.New("a support session is already active; end it first")
	}
	h.stopOfferLocked()
	h.machine = NewMachine()
	if err := h.machine.Move(StateHosting, "code created"); err != nil {
		h.mu.Unlock()
		return OfferInfo{}, err
	}
	h.code, h.codeKey, h.codeUsed, h.attempts = code, code.key(), false, 0
	h.offer = &o
	ctx, cancel := context.WithCancel(h.ctx)
	h.offerCancel = cancel
	info := OfferInfo{Code: code.String(), ExpiresAt: time.Now().Add(o.CodeTTL)}
	var lanAddr string
	if o.LAN {
		if err := h.listenLocked(); err != nil {
			h.mu.Unlock()
			cancel()
			return OfferInfo{}, fmt.Errorf("local network listener: %w", err)
		}
		bound := h.listener.Addr().(*net.TCPAddr)
		if bound.IP.IsUnspecified() {
			for _, ip := range LocalAddresses() {
				info.LANAddresses = append(info.LANAddresses, net.JoinHostPort(ip, strconv.Itoa(bound.Port)))
			}
		} else {
			// A listener bound to one address is reachable only there.
			info.LANAddresses = []string{bound.String()}
		}
		if len(info.LANAddresses) > 0 {
			lanAddr = info.LANAddresses[0]
		}
	}
	if o.Internet {
		info.RelayURL = h.opts.Relay.URL
		h.relayStatus = "connecting"
		h.goWork(func() { h.relayOfferLoop(ctx, code.SlotID()) })
	}
	info.Link = JoinLink(code, info.RelayURL, lanAddr)
	h.offerInfo = &info
	machine := h.machine
	h.mu.Unlock()
	h.afterFunc(o.CodeTTL, func() {
		if st, _, _ := machine.State(); st == StateHosting {
			_ = machine.Move(StateExpired, "code expired")
			h.mu.Lock()
			// The listener stays open so a late join hears pairing_expired
			// instead of a refused connection; the relay registration and
			// discovery end now.
			if h.machine == machine && h.session == nil && h.pending == nil && h.offerCancel != nil {
				h.offerCancel()
				h.offerCancel = nil
				h.relayStatus = ""
			}
			h.mu.Unlock()
			h.notify()
		}
	})
	_ = h.history.Append(Event{SessionID: "offer-" + code.SlotID()[:16], Role: "host", Kind: "code_created", Device: h.id.NodeID, Fields: map[string]string{"capabilities": strings.Join(Strings(o.Capabilities), ","), "lan": strconv.FormatBool(o.LAN), "internet": strconv.FormatBool(o.Internet)}})
	h.notify()
	return info, nil
}

// CancelOffer withdraws a code that has not been used.
func (h *Host) CancelOffer() {
	h.mu.Lock()
	if h.session == nil && h.pending == nil {
		_ = h.machine.Move(StateRevoked, "code withdrawn by the owner")
		h.stopOfferLocked()
	}
	h.mu.Unlock()
	h.notify()
}

func (h *Host) listenLocked() error {
	if h.listener != nil {
		return nil
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(h.ctx, "tcp", h.opts.LANListen)
	if err != nil {
		return err
	}
	h.listener = ln
	h.goWork(func() { h.acceptLoop(ln) })
	if h.opts.DiscoveryPort > 0 {
		port := ln.Addr().(*net.TCPAddr).Port
		d, err := startDiscovery(h.opts.DiscoveryPort, port, h.id.NodeID, h.discoverySlots)
		if err != nil {
			// Discovery is a convenience; CODE@address still works.
			h.logger.Warn("Support discovery unavailable", "error", err)
		} else {
			h.discovery = d
		}
	}
	return nil
}

func (h *Host) discoverySlots() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if st, _, _ := h.machine.State(); st == StateHosting && !h.codeUsed {
		return []string{h.code.SlotID()}
	}
	return nil
}

// lingerLocked keeps the session's resume paths open for a short while
// after it ended, so a supporter that is reconnecting learns that the
// session was revoked or expired instead of timing out as disconnected.
// Nothing can resume: every resume is answered from the terminal state.
func (h *Host) lingerLocked(stopResume context.CancelFunc) {
	if h.discovery != nil {
		h.discovery.Close()
		h.discovery = nil
	}
	if h.linger != nil {
		h.linger.Stop()
	}
	machine := h.machine
	h.linger = h.afterFunc(min(h.opts.ReconnectWindow, time.Minute), func() {
		if stopResume != nil {
			stopResume()
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		// A new offer owns the listener now.
		if h.machine != machine || h.session != nil || h.pending != nil {
			return
		}
		if st, _, _ := h.machine.State(); st.Terminal() {
			h.stopOfferLocked()
		}
	})
}

// stopOfferLocked closes listeners and relay registrations of the offer.
func (h *Host) stopOfferLocked() {
	if h.offerCancel != nil {
		h.offerCancel()
		h.offerCancel = nil
	}
	if h.listener != nil {
		_ = h.listener.Close()
		h.listener = nil
	}
	if h.discovery != nil {
		h.discovery.Close()
		h.discovery = nil
	}
	h.offerInfo = nil
	h.relayStatus = ""
}

// LANAddress returns the listening address, for tests and the CLI.
func (h *Host) LANAddress() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.listener == nil {
		return ""
	}
	return h.listener.Addr().String()
}

func (h *Host) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		select {
		case h.handshakes <- struct{}{}:
			if !h.goWork(func() {
				defer func() { <-h.handshakes }()
				h.serveConn(conn, "lan")
			}) {
				<-h.handshakes
				_ = conn.Close()
			}
		default:
			_ = conn.Close()
		}
	}
}

func (h *Host) relayOfferLoop(ctx context.Context, slot string) {
	delay := time.Second
	for ctx.Err() == nil {
		h.mu.Lock()
		active := !h.codeUsed
		h.mu.Unlock()
		if st, _, _ := h.machine.State(); st != StateHosting || !active {
			return
		}
		h.setRelayStatus("registered")
		conn, err := relayHost(ctx, h.relayConfig(), slot)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			h.setRelayStatus("error: " + describe(err))
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			delay = min(delay*2, 30*time.Second)
			continue
		}
		delay = time.Second
		if !h.goWork(func() { h.serveConn(conn, "relay") }) {
			_ = conn.Close()
		}
	}
}

func (h *Host) setRelayStatus(s string) {
	h.mu.Lock()
	changed := h.relayStatus != s
	h.relayStatus = s
	h.mu.Unlock()
	if changed {
		h.notify()
	}
}

// relayResumeLoop keeps a relay registration for the session's resume
// slot, so the supporter can reconnect through the relay.
func (h *Host) relayResumeLoop(ctx context.Context, slot string) {
	delay := time.Second
	for ctx.Err() == nil {
		conn, err := relayHost(ctx, h.relayConfig(), slot)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			delay = min(delay*2, 30*time.Second)
			continue
		}
		delay = time.Second
		if !h.goWork(func() { h.serveConn(conn, "relay") }) {
			_ = conn.Close()
		}
	}
}

func (h *Host) serveConn(raw net.Conn, network string) {
	tconn := tls.Server(raw, hostTLSConfig(h.id))
	clear := handshakeDeadline(tconn, 30*time.Second)
	if err := tconn.HandshakeContext(h.ctx); err != nil {
		_ = raw.Close()
		return
	}
	exp, err := exporter(tconn)
	if err != nil {
		_ = tconn.Close()
		return
	}
	var hello clientHello
	if err := readFrame(tconn, &hello); err != nil {
		_ = tconn.Close()
		return
	}
	if hello.Protocol != ProtocolVersion {
		_ = writeFrame(tconn, errFrame(Errorf(CodeProtocol, "support protocol %d is not supported; update Atlas on both computers", hello.Protocol)))
		_ = tconn.Close()
		return
	}
	clear()
	switch hello.Type {
	case "join":
		h.join(tconn, hello, exp, network)
	case "resume":
		h.resume(tconn, hello, network)
	default:
		_ = writeFrame(tconn, errFrame(Errorf(CodeProtocol, "unknown handshake")))
		_ = tconn.Close()
	}
}

// offerErrorLocked explains why the current offer cannot accept a join.
func (h *Host) offerErrorLocked() *Error {
	st, _, _ := h.machine.State()
	switch {
	case h.attempts >= maxCodeAttempts:
		return Errorf(CodePairingRateLimited, "this code was disabled after too many wrong attempts")
	case h.codeUsed || st == StateAwaitingAcceptance || st.Active() || st == StateReconnecting:
		return Errorf(CodePairingUsed, "this code was already used")
	case st == StateExpired:
		return Errorf(CodePairingExpired, "this code has expired")
	case st != StateHosting:
		return Errorf(CodePairingExpired, "this computer is not offering support now")
	}
	return nil
}


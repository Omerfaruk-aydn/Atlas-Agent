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

func (h *Host) join(conn *tls.Conn, hello clientHello, exp []byte, network string) {
	fail := func(e *Error) {
		_ = writeFrame(conn, errFrame(e))
		_ = conn.Close()
	}
	h.mu.Lock()
	if e := h.offerErrorLocked(); e != nil {
		h.mu.Unlock()
		fail(e)
		return
	}
	key := h.codeKey
	h.mu.Unlock()
	if !hmac.Equal(proof(key, "controller", exp), hello.Proof) {
		h.mu.Lock()
		h.attempts++
		burned := h.attempts >= maxCodeAttempts
		if burned {
			_ = h.machine.Move(StateFailed, "code disabled after too many wrong attempts")
			h.stopOfferLocked()
		}
		h.mu.Unlock()
		h.notify()
		if burned {
			fail(Errorf(CodePairingRateLimited, "too many wrong codes; this code is disabled"))
			return
		}
		fail(Errorf(CodePairingInvalid, "the code is wrong"))
		return
	}
	peer := PeerInfo{Name: sanitizeName(hello.Name, "supporter"), OS: sanitizeName(hello.OS, ""), Arch: sanitizeName(hello.Arch, ""), Fingerprint: peerFingerprint(conn.ConnectionState())}
	req := &JoinRequest{ID: randomID(8), Peer: peer, Verification: VerificationCode(exp), Network: network, RemoteAddr: conn.RemoteAddr().String(), At: time.Now(), decision: make(chan joinDecision, 1)}
	h.mu.Lock()
	if e := h.offerErrorLocked(); e != nil {
		h.mu.Unlock()
		fail(e)
		return
	}
	// The code is single use from here, whatever the owner decides.
	h.codeUsed = true
	if err := h.machine.Move(StateAwaitingAcceptance, "verified request from "+peer.Name); err != nil {
		h.mu.Unlock()
		fail(Errorf(CodePairingExpired, "%v", err))
		return
	}
	h.pending = req
	if h.discovery != nil {
		h.discovery.Close()
		h.discovery = nil
	}
	h.mu.Unlock()
	hostPeer := PeerInfo{NodeID: h.id.NodeID, Name: h.opts.Name, OS: runtime.GOOS, Arch: runtime.GOARCH, Fingerprint: h.id.Fingerprint}
	if err := writeFrame(conn, serverMsg{Type: "challenge_ok", Proof: proof(key, "host", exp), Host: &hostPeer, Verification: req.Verification}); err != nil {
		h.abandonPending(req, StateFailed, "supporter disconnected")
		_ = conn.Close()
		return
	}
	_ = writeFrame(conn, serverMsg{Type: "pending"})
	h.notify()

	// Watch for the supporter leaving while the owner decides. The
	// supporter sends nothing until it has read "accepted" and then sends
	// exactly one "ready" frame, so the watcher's single read ends cleanly
	// before the connection switches to HTTP/2. Read deadlines are not used
	// because they would poison a relayed stream.
	type readyResult struct {
		msg clientHello
		err error
	}
	gone := make(chan readyResult, 1)
	go func() {
		var m clientHello
		err := readFrame(conn, &m)
		gone <- readyResult{msg: m, err: err}
	}()
	var d joinDecision
	decided, left := false, false
	timer := time.NewTimer(h.opts.AcceptTimeout)
	defer timer.Stop()
	select {
	case d = <-req.decision:
		decided = true
	case <-gone:
		left = true
	case <-timer.C:
	case <-h.ctx.Done():
	}
	switch {
	case left:
		h.abandonPending(req, StateFailed, "the supporter left before a decision")
		_ = conn.Close()
		return
	case !decided:
		h.abandonPending(req, StateRevoked, "no decision in time; the request was declined")
		fail(Errorf(CodePairingRejected, "the computer owner did not accept in time"))
		return
	case !d.accept:
		h.abandonPending(req, StateRevoked, "the owner declined "+peer.Name)
		_ = h.history.Append(Event{SessionID: "offer-" + h.code.SlotID()[:16], Role: "host", Kind: "join_rejected", Peer: peer.Name, PeerDevice: peer.Fingerprint})
		fail(Errorf(CodePairingRejected, "the computer owner declined the connection"))
		return
	}
	s, gen, err := h.startSession(req, d.caps)
	if err != nil {
		fail(Errorf(CodeProtocol, "%v", err))
		return
	}
	info := h.sessionInfo(s, gen)
	if err := writeFrame(conn, serverMsg{Type: "accepted", Session: &info, Token: s.token(), ResumeToken: s.resumeToken}); err != nil {
		_ = conn.Close()
		h.connClosed(gen, s.id)
		return
	}
	readyTimer := time.NewTimer(30 * time.Second)
	defer readyTimer.Stop()
	select {
	case r := <-gone:
		if r.err != nil || r.msg.Type != "ready" {
			_ = conn.Close()
			h.connClosed(gen, s.id)
			return
		}
	case <-readyTimer.C:
		_ = conn.Close()
		h.connClosed(gen, s.id)
		return
	case <-h.ctx.Done():
		_ = conn.Close()
		h.connClosed(gen, s.id)
		return
	}
	h.serveH2(conn, gen, s.id)
}

// token returns the plaintext session token once; afterwards only its
// hash remains, so the token never sits in memory longer than needed.
func (s *hostSession) token() string {
	t := s.firstToken
	s.firstToken = ""
	return t
}

func (h *Host) abandonPending(req *JoinRequest, state State, reason string) {
	h.mu.Lock()
	if h.pending == req {
		h.pending = nil
		_ = h.machine.Move(state, reason)
		h.stopOfferLocked()
	}
	h.mu.Unlock()
	h.notify()
}

// Decide answers the pending join request. The capabilities may narrow
// what was offered; nil keeps the offer.
func (h *Host) Decide(requestID string, accept bool, caps []Capability) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pending == nil || h.pending.ID != requestID {
		return errors.New("that connection request is no longer pending")
	}
	if caps == nil && h.offer != nil {
		caps = h.offer.Capabilities
	}
	select {
	case h.pending.decision <- joinDecision{accept: accept, caps: Normalize(caps)}:
		return nil
	default:
		return errors.New("that connection request was already answered")
	}
}

func (h *Host) startSession(req *JoinRequest, caps []Capability) (*hostSession, uint64, error) {
	token, resume := randomID(32), randomID(32)
	s := &hostSession{
		id: "ss-" + randomID(10), peer: req.Peer, caps: caps, network: req.Network,
		tokenHash: sha256.Sum256([]byte(token)), resumeHash: sha256.Sum256([]byte(resume)), resumeToken: resume,
		started: time.Now(), active: map[string]*OperationView{}, opEnds: map[string]func(){}, firstToken: token,
	}
	h.mu.Lock()
	if h.pending != req {
		h.mu.Unlock()
		return nil, 0, errors.New("request is no longer pending")
	}
	s.roots = slices.Clone(h.offer.Roots)
	s.expires = s.started.Add(h.offer.SessionDuration)
	h.pending = nil
	gen, err := h.machine.Connect("accepted by the owner")
	if err != nil {
		h.mu.Unlock()
		return nil, 0, err
	}
	h.session = s
	ctx, cancel := context.WithCancel(h.ctx)
	s.cancel = cancel
	s.expiry = h.afterFunc(time.Until(s.expires), func() { h.endSession(s.id, StateExpired, "the session time ended") })
	if h.offerCancel != nil {
		// The code's relay registration ends; the session gets its own.
		h.offerCancel()
		h.offerCancel = nil
	}
	h.offerInfo = nil
	h.relayStatus = ""
	h.mu.Unlock()
	if req.Network == "relay" && h.relayConfig().Configured() {
		h.goWork(func() { h.relayResumeLoop(ctx, RelaySlotFor(s.id, resume)) })
	}
	_ = h.store.save(sessionRecord{ID: s.id, State: StateConnected, ControllerName: s.peer.Name, ControllerDevice: s.peer.Fingerprint, ResumeHash: hex.EncodeToString(s.resumeHash[:]), ExpiresAt: s.expires.UnixMilli(), LastGeneration: gen, CapabilitiesFinal: strings.Join(Strings(caps), ",")})
	var labels []string
	for _, r := range s.roots {
		labels = append(labels, r.Label)
	}
	_ = h.history.Append(Event{SessionID: s.id, Role: "host", Kind: "session_started", Device: h.id.NodeID, Peer: s.peer.Name, PeerDevice: s.peer.Fingerprint, Generation: gen, Fields: map[string]string{"capabilities": strings.Join(Strings(caps), ","), "folders": strings.Join(labels, ","), "expires_at": s.expires.Format(time.RFC3339), "network": s.network}})
	if h.opts.Overlay != nil {
		id := s.id
		h.opts.Overlay.Start(s.id, s.peer.Name, func() {
			// A native banner callback may outlive the connection goroutine.
			// Shutdown must wait for its history and session writes as well.
			if h.beginWork() {
				defer h.work.Done()
				h.endSession(id, StateRevoked, "the owner stopped remote support from the screen banner")
			}
		})
	}
	h.notify()
	return s, gen, nil
}

func (h *Host) sessionInfo(s *hostSession, gen uint64) SessionInfo {
	var labels []string
	for _, r := range s.roots {
		labels = append(labels, r.Label)
	}
	slices.Sort(labels)
	st, _, _ := h.machine.State()
	return SessionInfo{
		SessionID: s.id, RemoteDeviceID: h.id.NodeID, RemoteName: h.opts.Name, RemoteFingerprint: h.id.Fingerprint,
		OS: runtime.GOOS, Arch: runtime.GOARCH, Generation: gen, Capabilities: slices.Clone(s.caps), Roots: labels,
		OwnerIdentity: h.owner, ExpiresAt: s.expires.UnixMilli(), Desktop: h.opts.Desktop != nil, DesktopReason: h.opts.DesktopReason,
		Interactive: h.opts.Desktop != nil, Network: s.network, State: st, AgentPaused: s.agentPaused, InputPaused: s.inputPaused,
	}
}

func (h *Host) resume(conn *tls.Conn, hello clientHello, network string) {
	fail := func(e *Error) {
		_ = writeFrame(conn, errFrame(e))
		_ = conn.Close()
	}
	fp := peerFingerprint(conn.ConnectionState())
	got := sha256.Sum256([]byte(hello.ResumeToken))
	h.mu.Lock()
	s := h.session
	if s == nil || s.id != hello.SessionID {
		h.mu.Unlock()
		if rec, ok := h.store.load(hello.SessionID); ok {
			if rec.State == StateExpired {
				fail(Errorf(CodeSessionExpired, "the session time ended"))
				return
			}
			fail(Errorf(CodeSessionRevoked, "the session ended: %s", rec.Reason))
			return
		}
		fail(Errorf(CodeSessionRevoked, "this computer has no such support session"))
		return
	}
	if subtle.ConstantTimeCompare(got[:], s.resumeHash[:]) != 1 || fp != s.peer.Fingerprint {
		h.mu.Unlock()
		fail(Errorf(CodeWrongTarget, "this device is not part of the support session"))
		return
	}
	st, gen, _ := h.machine.State()
	if st.Terminal() {
		h.mu.Unlock()
		if st == StateExpired {
			fail(Errorf(CodeSessionExpired, "the session time ended"))
		} else {
			fail(Errorf(CodeSessionRevoked, "the computer owner ended the session"))
		}
		return
	}
	if hello.Generation != gen {
		h.mu.Unlock()
		fail(Errorf(CodeStaleConnection, "connection %d is older than the current connection %d", hello.Generation, gen))
		return
	}
	old := h.conns[gen]
	delete(h.conns, gen)
	if st != StateReconnecting {
		_ = h.machine.MoveIf(gen, StateReconnecting, "supporter reconnected")
	}
	if s.reconnect != nil {
		s.reconnect.Stop()
		s.reconnect = nil
	}
	newGen, err := h.machine.Connect("reconnected")
	if err != nil {
		h.mu.Unlock()
		fail(Errorf(CodeSessionRevoked, "%v", err))
		return
	}
	if s.agentPaused {
		_ = h.machine.Move(StatePaused, "the owner paused the support task")
	}
	info := h.sessionInfo(s, newGen)
	h.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	_ = h.history.Append(Event{SessionID: s.id, Role: "host", Kind: "reconnected", Generation: newGen, Detail: network})
	_ = h.store.save(sessionRecord{ID: s.id, State: StateConnected, ControllerName: s.peer.Name, ControllerDevice: s.peer.Fingerprint, ResumeHash: hex.EncodeToString(s.resumeHash[:]), ExpiresAt: s.expires.UnixMilli(), LastGeneration: newGen, CapabilitiesFinal: strings.Join(Strings(s.caps), ",")})
	h.notify()
	if err := writeFrame(conn, serverMsg{Type: "resumed", Session: &info}); err != nil {
		_ = conn.Close()
		h.connClosed(newGen, s.id)
		return
	}
	h.serveH2(conn, newGen, s.id)
}

func (h *Host) serveH2(conn *tls.Conn, gen uint64, sessionID string) {
	h.mu.Lock()
	if h.session == nil || h.session.id != sessionID {
		h.mu.Unlock()
		_ = conn.Close()
		return
	}
	h.conns[gen] = conn
	h.mu.Unlock()
	ctx := context.WithValue(h.ctx, connKey{}, connInfo{generation: gen, session: sessionID})
	srv := &http2.Server{MaxConcurrentStreams: 64, MaxReadFrameSize: 1 << 20, ReadIdleTimeout: 30 * time.Second, PingTimeout: 15 * time.Second}
	srv.ServeConn(conn, &http2.ServeConnOpts{Context: ctx, Handler: h.handler, BaseConfig: &http.Server{ReadHeaderTimeout: 30 * time.Second}})
	_ = conn.Close()
	h.connClosed(gen, sessionID)
}

// connClosed handles a dropped connection: the session waits for the
// supporter to reconnect, then ends. Operations already running keep
// their journaled outcome for reconciliation.
func (h *Host) connClosed(gen uint64, sessionID string) {
	h.mu.Lock()
	if h.conns[gen] != nil {
		delete(h.conns, gen)
	}
	s := h.session
	if s == nil || s.id != sessionID {
		h.mu.Unlock()
		return
	}
	if err := h.machine.MoveIf(gen, StateReconnecting, "connection lost"); err != nil {
		h.mu.Unlock()
		return
	}
	if s.reconnect != nil {
		s.reconnect.Stop()
	}
	s.reconnect = h.afterFunc(h.opts.ReconnectWindow, func() {
		if st, cur, _ := h.machine.State(); st == StateReconnecting && cur == gen {
			h.endSession(sessionID, StateDisconnected, "the connection was lost and not restored")
		}
	})
	h.mu.Unlock()
	_ = h.history.Append(Event{SessionID: sessionID, Role: "host", Kind: "connection_lost", Generation: gen})
	h.notify()
}

// Disconnect ends the active session at the owner's request. New input is
// refused before this returns; running commands are stopped and recorded.
func (h *Host) Disconnect(reason string) {
	h.mu.Lock()
	s := h.session
	pending := h.pending
	h.mu.Unlock()
	if s != nil {
		h.endSession(s.id, StateRevoked, reason)
		return
	}
	if pending != nil {
		_ = h.Decide(pending.ID, false, nil)
		return
	}
	h.CancelOffer()
}

// endSession moves to a terminal state first, closes connections at once,
// then stops running work and records what happened to it.
func (h *Host) endSession(sessionID string, state State, reason string) {
	h.mu.Lock()
	s := h.session
	if s == nil || s.id != sessionID {
		h.mu.Unlock()
		return
	}
	_ = h.machine.Move(state, reason)
	_, gen, _ := h.machine.State()
	conns := h.conns
	h.conns = map[uint64]net.Conn{}
	h.session = nil
	perm := h.permission
	h.permission = nil
	if s.expiry != nil {
		s.expiry.Stop()
	}
	if s.reconnect != nil {
		s.reconnect.Stop()
	}
	ends := s.opEnds
	s.opEnds = map[string]func(){}
	h.lingerLocked(s.cancel)
	h.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
	if perm != nil {
		perm.State = "canceled"
	}
	for _, end := range ends {
		end()
	}
	if h.opts.Overlay != nil {
		h.opts.Overlay.End()
	}
	if h.opts.Executor != nil {
		h.opts.Executor.Close(s.id)
	}
	g := h.grant(s, gen)
	for _, op := range h.node.CancelRunning(g) {
		_ = h.history.Append(Event{SessionID: s.id, Role: "host", Kind: "operation_stopped", OperationID: op.ID, State: string(op.State), Effect: op.Effect, Detail: op.Error})
	}
	_ = h.store.save(sessionRecord{ID: s.id, State: state, Reason: reason, ControllerName: s.peer.Name, ControllerDevice: s.peer.Fingerprint, ResumeHash: hex.EncodeToString(s.resumeHash[:]), ExpiresAt: s.expires.UnixMilli(), LastGeneration: gen, CapabilitiesFinal: strings.Join(Strings(s.caps), ",")})
	_ = h.history.Append(Event{SessionID: s.id, Role: "host", Kind: "session_ended", State: string(state), Detail: reason, Generation: gen})
	h.notify()
}

// PauseAgent stops (or resumes) every change the supporter's agent may
// make; reading status stays available.
func (h *Host) PauseAgent(paused bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.session
	if s == nil {
		return errors.New("no active session")
	}
	st, _, _ := h.machine.State()
	switch {
	case paused && st == StateConnected:
		if err := h.machine.Move(StatePaused, "the owner paused the support task"); err != nil {
			return err
		}
	case !paused && st == StatePaused:
		if err := h.machine.Move(StateConnected, "the owner resumed the support task"); err != nil {
			return err
		}
	}
	s.agentPaused = paused
	_ = h.history.Append(Event{SessionID: s.id, Role: "host", Kind: map[bool]string{true: "agent_paused", false: "agent_resumed"}[paused]})
	go h.notify()
	return nil
}

// PauseInput stops (or allows) mouse, keyboard and launches only.
func (h *Host) PauseInput(paused bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.session == nil {
		return errors.New("no active session")
	}
	h.session.inputPaused = paused
	_ = h.history.Append(Event{SessionID: h.session.id, Role: "host", Kind: map[bool]string{true: "input_paused", false: "input_resumed"}[paused]})
	go h.notify()
	return nil
}

// SetCapabilities changes what the active session may do, effective for
// the next request.
func (h *Host) SetCapabilities(caps []Capability) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.session == nil {
		return errors.New("no active session")
	}
	h.session.caps = Normalize(caps)
	_ = h.history.Append(Event{SessionID: h.session.id, Role: "host", Kind: "permissions_changed", Fields: map[string]string{"capabilities": strings.Join(Strings(h.session.caps), ",")}})
	go h.notify()
	return nil
}

// Status is the owner's current view.
func (h *Host) Status() HostStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	st, gen, reason := h.machine.State()
	out := HostStatus{State: st, Reason: reason, Generation: gen, NodeID: h.id.NodeID, Name: h.opts.Name}
	if h.offerInfo != nil {
		info := *h.offerInfo
		info.RelayStatus = h.relayStatus
		out.Offer = &info
	}
	if h.pending != nil {
		p := *h.pending
		out.Pending = &p
	}
	if s := h.session; s != nil {
		view := &SessionView{ID: s.id, Peer: s.peer, Capabilities: slices.Clone(s.caps), Roots: slices.Clone(s.roots), ExpiresAt: s.expires, StartedAt: s.started, AgentPaused: s.agentPaused, InputPaused: s.inputPaused, Network: s.network, Recent: slices.Clone(s.recent)}
		for _, op := range s.active {
			o := *op
			view.Active = &o
		}
		out.Session = view
	}
	if h.permission != nil && h.permission.State == "pending" {
		p := *h.permission
		out.Permission = &p
	}
	return out
}

// Close ends any session, withdraws the offer and releases listeners.
func (h *Host) Close() {
	h.closeOnce.Do(h.close)
}

func (h *Host) close() {
	h.workMu.Lock()
	h.closed = true
	h.workMu.Unlock()
	h.mu.Lock()
	s := h.session
	h.mu.Unlock()
	if s != nil {
		h.endSession(s.id, StateDisconnected, "Atlas was closed on this computer")
	}
	h.mu.Lock()
	if h.pending != nil {
		select {
		case h.pending.decision <- joinDecision{}:
		default:
		}
	}
	if h.linger != nil {
		h.linger.Stop()
		h.linger = nil
	}
	h.stopOfferLocked()
	h.mu.Unlock()
	h.cancel()
	done := make(chan struct{})
	go func() {
		h.work.Wait()
		h.node.WaitOperations()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		h.logger.Warn("Support host connections did not stop in time")
	}
}

// goWork runs fn as a tracked connection goroutine.
func (h *Host) goWork(fn func()) bool {
	if !h.beginWork() {
		return false
	}
	go func() {
		defer h.work.Done()
		fn()
	}()
	return true
}

// beginWork prevents new writers from racing with shutdown's Wait.
func (h *Host) beginWork() bool {
	h.workMu.Lock()
	defer h.workMu.Unlock()
	if h.closed {
		return false
	}
	h.work.Add(1)
	return true
}

// afterFunc tracks callbacks that have started and refuses them after Close.
func (h *Host) afterFunc(delay time.Duration, fn func()) *time.Timer {
	return time.AfterFunc(delay, func() { h.goWork(fn) })
}

// sessionPath returns the live session view to the controller, including
// pauses and narrowed permissions the owner changed after acceptance.
const sessionPath = "/v1/support/session"

func (h *Host) routes(node http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", node)
	mux.HandleFunc("POST /v1/support/tool", h.handleTool)
	mux.HandleFunc("POST "+permissionsPath, h.handleRequestPermission)
	mux.HandleFunc("GET "+permissionsPath+"/{id}", h.handlePermissionStatus)
	mux.HandleFunc("DELETE "+permissionsPath+"/{id}", h.handleCancelPermission)
	mux.HandleFunc("GET "+sessionPath, func(w http.ResponseWriter, r *http.Request) {
		g, apiErr, status := h.Authorize(r, nodes.NeedInfo)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if apiErr != nil {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(apiErr)
			return
		}
		h.mu.Lock()
		s := h.session
		var info SessionInfo
		if s != nil && s.id == g.SessionID {
			info = h.sessionInfo(s, g.Generation)
		}
		h.mu.Unlock()
		_ = json.NewEncoder(w).Encode(info)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.beginWork() {
			writeSupportJSON(w, http.StatusServiceUnavailable, apiErr2(CodeSessionRevoked, "the support host is shutting down"))
			return
		}
		defer h.work.Done()
		mux.ServeHTTP(w, r)
	})
}

func (h *Host) grant(s *hostSession, gen uint64) nodes.Grant {
	return nodes.Grant{Device: nodes.Device{ID: "support-" + s.id, Name: s.peer.Name}, SessionID: s.id, Generation: gen, Roots: slices.Clone(s.roots), Permissions: Strings(s.caps)}
}

func apiErr(code Code, format string, args ...any) *nodes.APIError {
	e := Errorf(code, format, args...)
	return &nodes.APIError{Code: string(code), Message: e.Message, Effect: "none"}
}

// Authorize implements nodes.Access: every request is checked against the
// session, its connection generation, the owner's pauses and the granted
// capabilities, on this computer. Nothing the supporter's side decides
// (including its own auto-approve mode) can widen it.
func (h *Host) Authorize(r *http.Request, need nodes.Need) (nodes.Grant, *nodes.APIError, int) {
	ci, _ := r.Context().Value(connKey{}).(connInfo)
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.session
	st, gen, _ := h.machine.State()
	if s == nil || ci.session != s.id {
		if st == StateExpired {
			return nodes.Grant{}, apiErr(CodeSessionExpired, "the session time ended"), http.StatusUnauthorized
		}
		return nodes.Grant{}, apiErr(CodeSessionRevoked, "the computer owner ended the session"), http.StatusUnauthorized
	}
	if !st.Active() {
		return nodes.Grant{}, apiErr(CodeSessionRevoked, "the session is %s", st), http.StatusUnauthorized
	}
	if ci.generation != gen {
		return nodes.Grant{}, apiErr(CodeStaleConnection, "request arrived on connection %d; the current connection is %d", ci.generation, gen), http.StatusConflict
	}
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	sum := sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare(sum[:], s.tokenHash[:]) != 1 {
		return nodes.Grant{}, apiErr(CodeSessionRevoked, "invalid session token"), http.StatusUnauthorized
	}
	if r.Header.Get("X-Atlas-Session") != s.id || r.Header.Get("X-Atlas-Target") != h.id.NodeID {
		return nodes.Grant{}, apiErr(CodeWrongTarget, "the request names another session or device"), http.StatusConflict
	}
	if g := r.Header.Get("X-Atlas-Generation"); g != strconv.FormatUint(gen, 10) {
		return nodes.Grant{}, apiErr(CodeStaleConnection, "request was prepared for connection %s; the current connection is %d", g, gen), http.StatusConflict
	}
	if time.Now().After(s.expires) {
		return nodes.Grant{}, apiErr(CodeSessionExpired, "the session time ended"), http.StatusUnauthorized
	}
	if mutating(need) && (s.agentPaused || st == StatePaused) {
		return nodes.Grant{}, apiErr(CodeSessionPaused, "the computer owner paused the support task"), http.StatusLocked
	}
	if (need == nodes.NeedDesktopInput || need == nodes.NeedDesktopLaunch || need == nodes.NeedBrowserInput) && s.inputPaused {
		return nodes.Grant{}, apiErr(CodeControlPaused, "the computer owner paused screen control"), http.StatusLocked
	}
	if required := requiredFor(need); len(required) > 0 && !slices.ContainsFunc(required, func(c Capability) bool { return slices.Contains(s.caps, c) }) {
		return nodes.Grant{}, apiErr(CodeCapabilityDenied, "the computer owner did not grant %q", required[0]), http.StatusForbidden
	}
	return h.grant(s, gen), nil, 0
}

// CheckExec implements nodes.ExecPolicy with the narrower capabilities a
// command or launch touches.
func (h *Host) CheckExec(g nodes.Grant, kind string, argv []string) *nodes.APIError {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.session
	if s == nil || s.id != g.SessionID {
		return apiErr(CodeSessionRevoked, "the session ended")
	}
	need := ExtraCapabilities(argv)
	if IsBrowser(argv) {
		need = append(need, CapBrowser)
	}
	if kind == "launch" && !IsBrowser(argv) {
		need = append(need, CapInput)
	}
	for _, c := range need {
		if !slices.Contains(s.caps, c) {
			return apiErr(CodeCapabilityDenied, "this command needs %q, which the computer owner did not grant", c)
		}
	}
	return nil
}


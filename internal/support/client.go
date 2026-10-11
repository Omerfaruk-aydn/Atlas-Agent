package support

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"golang.org/x/net/http2"
)

const (
	defaultDiscoveryWait = 2 * time.Second
	// joinWait covers the owner's decision time plus a margin.
	joinWait = defaultAcceptTimeout + 30*time.Second
)

// ControllerOptions configures the supporting computer.
type ControllerOptions struct {
	// StateDir holds this device's identity and history; each controller
	// needs its own.
	StateDir string
	// Name is shown to the computer owner as "name sent by the device".
	Name string
	// Relay is the relay this user configured; a join link that names a
	// different relay is refused instead of followed.
	Relay           *RelayConfig
	DiscoveryPort   int
	DiscoveryWait   time.Duration
	ReconnectWindow time.Duration
	Logger          *slog.Logger
	// OnChange is called after every visible change; it must not block.
	OnChange func()
}

// JoinPrompt is shown to the supporter while the owner decides: the
// verification code must match the one on the owner's screen.
type JoinPrompt struct {
	Host         PeerInfo
	Verification string
	Network      string
	Address      string
}

// Remote is the typed target of the active session. Every remote call is
// addressed with it, never with a free-form name.
type Remote struct {
	SessionInfo
	// Address is the LAN address, empty for relay sessions.
	Address string `json:"address,omitempty"`
}

// ControllerStatus is the supporter's view.
type ControllerStatus struct {
	State      State       `json:"state"`
	Reason     string      `json:"reason,omitempty"`
	Generation uint64      `json:"generation"`
	DeviceID   string      `json:"device_id"`
	Name       string      `json:"name"`
	Remote     *Remote     `json:"remote,omitempty"`
	Pending    *JoinPrompt `json:"pending,omitempty"`
}

// Controller is the supporting computer's end of one session at a time.
type Controller struct {
	opts    ControllerOptions
	id      nodes.Identity
	history *History

	mu        sync.Mutex
	machine   *Machine
	remote    *Remote
	pending   *JoinPrompt
	token     string
	resume    string
	relay     *RelayConfig
	conn      *tls.Conn
	cc        *http2.ClientConn
	client    *nodes.Client
	cancel    context.CancelFunc
	ctx       context.Context
	stopping  bool
	closed    bool
	work      sync.WaitGroup
	closeOnce sync.Once
}

// NewController loads or creates this device's support identity.
func NewController(ctx context.Context, opts ControllerOptions) (*Controller, error) {
	if opts.StateDir == "" {
		return nil, errors.New("support controller needs a state directory")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Name == "" {
		opts.Name, _ = os.Hostname()
	}
	opts.Name = sanitizeName(opts.Name, "supporter")
	if opts.DiscoveryPort == 0 {
		opts.DiscoveryPort = DefaultDiscoveryPort
	}
	if opts.DiscoveryWait <= 0 {
		opts.DiscoveryWait = defaultDiscoveryWait
	}
	if opts.ReconnectWindow <= 0 {
		opts.ReconnectWindow = defaultReconnectWindow
	}
	id, err := nodes.LoadOrCreateIdentity(filepath.Join(opts.StateDir, "identity"), nil)
	if err != nil {
		return nil, err
	}
	c := &Controller{opts: opts, id: id, history: OpenHistory(filepath.Join(opts.StateDir, "history")), machine: NewMachine()}
	c.ctx, c.cancel = context.WithCancel(ctx)
	return c, nil
}

// setRelay applies a changed relay setting to the next join.
func (c *Controller) setRelay(cfg *RelayConfig) {
	c.mu.Lock()
	c.opts.Relay = cfg
	c.mu.Unlock()
}

// Fingerprint is this device's verified identity as the owner sees it.
func (c *Controller) Fingerprint() string { return c.id.Fingerprint }

// History exposes the supporter's session history.
func (c *Controller) History() *History { return c.history }

func (c *Controller) notify() {
	if c.opts.OnChange != nil {
		c.opts.OnChange()
	}
}

// Status is the supporter's current view.
func (c *Controller) Status() ControllerStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	st, gen, reason := c.machine.State()
	out := ControllerStatus{State: st, Reason: reason, Generation: gen, DeviceID: c.id.NodeID, Name: c.opts.Name}
	if c.remote != nil {
		r := *c.remote
		r.Capabilities = slices.Clone(r.Capabilities)
		r.Roots = slices.Clone(r.Roots)
		out.Remote = &r
	}
	if c.pending != nil {
		p := *c.pending
		out.Pending = &p
	}
	return out
}

// Target returns the active remote, or a no_support_target error. A
// session that is reconnecting is still the target, but calls fail with
// remote_offline until it is back.
func (c *Controller) Target() (Remote, *nodes.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st, _, reason := c.machine.State()
	if c.remote == nil {
		return Remote{}, nil, Errorf(CodeNoTarget, "no remote computer is connected")
	}
	switch {
	case st == StateExpired:
		return *c.remote, nil, Errorf(CodeSessionExpired, "the support session ended: %s", reason)
	case st == StateRevoked:
		return *c.remote, nil, Errorf(CodeSessionRevoked, "the support session ended: %s", reason)
	case st.Terminal():
		return *c.remote, nil, Errorf(CodeRemoteOffline, "the support session ended: %s", reason)
	case st == StateReconnecting:
		return *c.remote, nil, Errorf(CodeRemoteOffline, "the connection to %s is being restored", c.remote.RemoteName)
	}
	return *c.remote, c.client, nil
}

// Join connects with a code, a CODE@address or a join link. onPending is
// called once the host proved it knows the code, with the verification code
// to compare; Join then waits for the owner's decision.
func (c *Controller) Join(ctx context.Context, input string, onPending func(JoinPrompt)) (Remote, error) {
	target, err := ParseTarget(input)
	if err != nil {
		return Remote{}, err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return Remote{}, Errorf(CodeSessionRevoked, "the support controller is closed")
	}
	if st, _, _ := c.machine.State(); st.Active() || st == StateReconnecting || st == StateConnecting || st == StateAwaitingAcceptance {
		c.mu.Unlock()
		return Remote{}, errors.New("a support session is already active; disconnect it first")
	}
	c.machine = NewMachine()
	c.remote, c.pending, c.token, c.resume = nil, nil, "", ""
	_ = c.machine.Move(StateConnecting, "connecting")
	c.work.Add(1)
	c.mu.Unlock()
	defer c.work.Done()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(c.ctx, cancel)
	defer stop()
	c.notify()
	remote, err := c.join(ctx, target, onPending)
	if err != nil {
		state := StateFailed
		switch CodeOf(err) {
		case CodePairingRejected:
			state = StateRevoked
		case CodePairingExpired, CodePairingUsed:
			state = StateExpired
		}
		c.mu.Lock()
		c.pending = nil
		_ = c.machine.Move(state, describe(err))
		c.mu.Unlock()
		c.notify()
		return Remote{}, err
	}
	return remote, nil
}

func (c *Controller) join(ctx context.Context, target Target, onPending func(JoinPrompt)) (Remote, error) {
	relay, err := c.relayFor(target)
	if err != nil {
		return Remote{}, err
	}
	raw, network, address, err := c.dialJoin(ctx, target, relay)
	if err != nil {
		return Remote{}, err
	}
	tconn := tls.Client(raw, controllerTLSConfig(c.id, ""))
	stop := contextConn(ctx, tconn)
	defer stop()
	clear := handshakeDeadline(tconn, 30*time.Second)
	if err := tconn.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return Remote{}, Errorf(CodeRemoteOffline, "secure connection to the other computer failed: %v", err)
	}
	exp, err := exporter(tconn)
	if err != nil {
		_ = tconn.Close()
		return Remote{}, err
	}
	key := target.Code.key()
	hello := clientHello{Type: "join", Protocol: ProtocolVersion, Name: c.opts.Name, OS: runtime.GOOS, Arch: runtime.GOARCH, Proof: proof(key, "controller", exp)}
	if err := writeFrame(tconn, hello); err != nil {
		_ = tconn.Close()
		return Remote{}, Errorf(CodeRemoteOffline, "the other computer closed the connection")
	}
	var msg serverMsg
	if err := readFrame(tconn, &msg); err != nil {
		_ = tconn.Close()
		return Remote{}, frameErr(err)
	}
	if msg.Type == "error" && msg.Error != nil {
		_ = tconn.Close()
		return Remote{}, msg.Error
	}
	if msg.Type != "challenge_ok" || msg.Host == nil || !hmac.Equal(proof(key, "host", exp), msg.Proof) {
		_ = tconn.Close()
		return Remote{}, Errorf(CodeWrongTarget, "the computer that answered did not prove it created this code")
	}
	host := *msg.Host
	host.Fingerprint = peerFingerprint(tconn.ConnectionState())
	if VerificationCode(exp) != msg.Verification {
		_ = tconn.Close()
		return Remote{}, Errorf(CodeWrongTarget, "verification codes differ; the connection may be intercepted")
	}
	clear()
	prompt := JoinPrompt{Host: host, Verification: msg.Verification, Network: network, Address: address}
	c.mu.Lock()
	c.pending = &prompt
	err = c.machine.Move(StateAwaitingAcceptance, "waiting for "+host.Name+" to accept")
	c.mu.Unlock()
	if err != nil {
		_ = tconn.Close()
		return Remote{}, err
	}
	if onPending != nil {
		onPending(prompt)
	}
	c.notify()
	_ = tconn.SetReadDeadline(time.Now().Add(joinWait))
	for {
		msg = serverMsg{}
		if err := readFrame(tconn, &msg); err != nil {
			_ = tconn.Close()
			if ctx.Err() != nil {
				return Remote{}, Errorf(CodePairingRejected, "the connection request was canceled")
			}
			return Remote{}, frameErr(err)
		}
		if msg.Type == "pending" {
			continue
		}
		break
	}
	_ = tconn.SetReadDeadline(time.Time{})
	if msg.Type == "error" && msg.Error != nil {
		_ = tconn.Close()
		return Remote{}, msg.Error
	}
	if msg.Type != "accepted" || msg.Session == nil || msg.Token == "" || msg.ResumeToken == "" {
		_ = tconn.Close()
		return Remote{}, Errorf(CodeProtocol, "unexpected answer from the other computer")
	}
	if msg.Session.RemoteFingerprint != host.Fingerprint {
		_ = tconn.Close()
		return Remote{}, Errorf(CodeWrongTarget, "the session names a different device than the one connected")
	}
	if err := writeFrame(tconn, clientHello{Type: "ready", Protocol: ProtocolVersion}); err != nil {
		_ = tconn.Close()
		return Remote{}, Errorf(CodeConnectionLost, "the connection dropped right after the owner accepted; ask for a new code")
	}
	stop()
	remote := Remote{SessionInfo: *msg.Session, Address: address}
	c.mu.Lock()
	c.pending = nil
	c.token, c.resume, c.relay = msg.Token, msg.ResumeToken, relay
	c.remote = &remote
	if _, err := c.machine.Connect("accepted by " + host.Name); err != nil {
		c.mu.Unlock()
		_ = tconn.Close()
		return Remote{}, err
	}
	if err := c.attachLocked(tconn, remote.Generation); err != nil {
		c.mu.Unlock()
		_ = tconn.Close()
		return Remote{}, err
	}
	c.mu.Unlock()
	_ = c.history.Append(Event{SessionID: remote.SessionID, Role: "supporter", Kind: "session_started", Device: c.id.NodeID, Peer: remote.RemoteName, PeerDevice: remote.RemoteDeviceID, Generation: remote.Generation, Fields: map[string]string{"capabilities": strings.Join(Strings(remote.Capabilities), ","), "folders": strings.Join(remote.Roots, ","), "network": network, "fingerprint": remote.RemoteFingerprint}})
	c.notify()
	return remote, nil
}

func frameErr(err error) error {
	if e := AsError(err); e != nil {
		return e
	}
	return Errorf(CodeRemoteOffline, "the other computer closed the connection")
}

// relayFor picks the relay for a target. A link may name a relay, but only
// the one this user configured is ever contacted.
func (c *Controller) relayFor(t Target) (*RelayConfig, error) {
	c.mu.Lock()
	configured := c.opts.Relay
	c.mu.Unlock()
	if t.Relay == "" {
		if configured.Configured() {
			return configured, nil
		}
		return nil, nil
	}
	if !configured.Configured() {
		return nil, Errorf(CodeRelayNotConfigured, "the link uses the relay %s, which is not configured on this computer; add it as support_relay_url only if you trust it", t.Relay)
	}
	if strings.TrimRight(t.Relay, "/") != strings.TrimRight(configured.URL, "/") {
		return nil, Errorf(CodeRelayNotConfigured, "the link uses the relay %s, but this computer is configured for %s; Atlas does not contact unconfigured relays", t.Relay, configured.URL)
	}
	return configured, nil
}

// dialJoin finds the host: an explicit address, then LAN discovery, then
// the configured relay. Nothing reports a connection that did not happen.
func (c *Controller) dialJoin(ctx context.Context, t Target, relay *RelayConfig) (net.Conn, string, string, error) {
	dial := func(addr string) (net.Conn, error) {
		d := net.Dialer{Timeout: 10 * time.Second}
		return d.DialContext(ctx, "tcp", addr)
	}
	if t.Address != "" {
		addr := t.Address
		if _, _, err := net.SplitHostPort(addr); err != nil {
			return nil, "", "", Errorf(CodePairingInvalid, "address %q must be host:port", addr)
		}
		conn, err := dial(addr)
		if err != nil {
			return nil, "", "", Errorf(CodeRemoteOffline, "cannot reach %s on the local network: %v", addr, err)
		}
		return conn, "lan", addr, nil
	}
	if c.opts.DiscoveryPort > 0 {
		found, _ := discover(ctx, t.Code.SlotID(), c.opts.DiscoveryPort, c.opts.DiscoveryWait)
		for _, addr := range found {
			if conn, err := dial(addr); err == nil {
				return conn, "lan", addr, nil
			}
		}
	}
	if relay.Configured() {
		conn, err := relayJoin(ctx, relay, t.Code.SlotID())
		if err != nil {
			return nil, "", "", err
		}
		return conn, "relay", "", nil
	}
	return nil, "", "", Errorf(CodeRemoteOffline, "no computer on this local network offers this code. For a computer on another network, both computers need the same support relay (support_relay_url); or enter CODE@address")
}

// attachLocked starts an HTTP/2 client over the authenticated connection.
func (c *Controller) attachLocked(tconn *tls.Conn, generation uint64) error {
	if c.closed {
		return Errorf(CodeSessionRevoked, "the support controller is shutting down")
	}
	tr := &http2.Transport{ReadIdleTimeout: 30 * time.Second, PingTimeout: 15 * time.Second}
	cc, err := tr.NewClientConn(tconn)
	if err != nil {
		return err
	}
	c.conn, c.cc = tconn, cc
	sessionID, deviceID := c.remote.SessionID, c.remote.RemoteDeviceID
	rt := &sessionTransport{cc: cc, session: sessionID, target: deviceID, generation: generation}
	c.client = &nodes.Client{BaseURL: "https://atlas-support", Fingerprint: c.remote.RemoteFingerprint, Token: c.token, HTTP: &http.Client{Transport: rt}}
	c.work.Add(1)
	go func() { defer c.work.Done(); c.watch(cc, generation) }()
	return nil
}

// sessionTransport stamps every request with the session, the target
// device and the connection generation it was prepared for. A request is
// never moved to another connection.
type sessionTransport struct {
	cc         *http2.ClientConn
	session    string
	target     string
	generation uint64
}

func (t *sessionTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if !t.cc.CanTakeNewRequest() {
		// Classified as "not sent" by the node client.
		return nil, &net.OpError{Op: "dial", Net: "support", Err: errors.New("support connection is closed")}
	}
	r = r.Clone(r.Context())
	r.Header.Set("X-Atlas-Session", t.session)
	r.Header.Set("X-Atlas-Target", t.target)
	r.Header.Set("X-Atlas-Generation", strconv.FormatUint(t.generation, 10))
	return t.cc.RoundTrip(r)
}

// watch notices a dropped connection and tries to resume it within the
// reconnect window. Requests in flight on the old connection are not
// resent; their callers see an unknown outcome and reconcile.
func (c *Controller) watch(cc *http2.ClientConn, generation uint64) {
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
		if cc.State().Closed || !cc.CanTakeNewRequest() {
			break
		}
	}
	c.mu.Lock()
	if c.cc != cc || c.stopping || c.closed {
		c.mu.Unlock()
		return
	}
	if err := c.machine.Move(StateReconnecting, "connection lost"); err != nil {
		c.mu.Unlock()
		return
	}
	remote := *c.remote
	c.mu.Unlock()
	_ = c.history.Append(Event{SessionID: remote.SessionID, Role: "supporter", Kind: "connection_lost", Generation: generation})
	c.notify()
	c.reconnect(remote, generation)
}

func (c *Controller) reconnect(remote Remote, generation uint64) {
	deadline := time.Now().Add(c.opts.ReconnectWindow)
	delay := 500 * time.Millisecond
	for time.Now().Before(deadline) && c.ctx.Err() == nil {
		err := c.resumeOnce(remote, generation)
		if err == nil {
			return
		}
		switch CodeOf(err) {
		case CodeSessionRevoked, CodeSessionExpired, CodeWrongTarget, CodeStaleConnection:
			c.end(remote.SessionID, terminalFor(CodeOf(err)), describe(err))
			return
		}
		select {
		case <-c.ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, 5*time.Second)
	}
	c.end(remote.SessionID, StateDisconnected, "the connection was lost and not restored")
}

func terminalFor(code Code) State {
	switch code {
	case CodeSessionExpired:
		return StateExpired
	case CodeSessionRevoked:
		return StateRevoked
	}
	return StateFailed
}


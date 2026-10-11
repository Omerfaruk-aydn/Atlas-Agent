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


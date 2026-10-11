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


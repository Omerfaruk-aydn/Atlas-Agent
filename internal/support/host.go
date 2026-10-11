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


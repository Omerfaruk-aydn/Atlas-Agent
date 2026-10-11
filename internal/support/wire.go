package support

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
)

// ProtocolVersion is bumped on incompatible handshake changes.
const ProtocolVersion = 1

const (
	alpnProtocol  = "atlas-support/1"
	exporterLabel = "EXPORTER-atlas-support-v1"
	maxFrame      = 64 << 10
)

// PeerInfo describes one end as the other end verified it. Fingerprint is
// the SHA-256 of the TLS certificate the peer proved possession of; it is
// the verified device identity, unlike the self-chosen name.
type PeerInfo struct {
	NodeID      string `json:"node_id,omitempty"`
	Name        string `json:"name"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	Fingerprint string `json:"fingerprint"`
}

// SessionInfo is what the controller learns once the owner accepts.
type SessionInfo struct {
	SessionID string `json:"session_id"`
	// RemoteDeviceID is the host's stable node id.
	RemoteDeviceID    string       `json:"remote_device_id"`
	RemoteName        string       `json:"remote_name"`
	RemoteFingerprint string       `json:"remote_fingerprint"`
	OS                string       `json:"os"`
	Arch              string       `json:"arch"`
	Generation        uint64       `json:"generation"`
	Capabilities      []Capability `json:"capabilities"`
	Roots             []string     `json:"roots"`
	// OwnerIdentity is the account signed in on the host.
	OwnerIdentity string `json:"owner_identity"`
	ExpiresAt     int64  `json:"expires_at"`
	Desktop       bool   `json:"desktop"`
	DesktopReason string `json:"desktop_reason,omitempty"`
	// Interactive reports whether a user desktop session backs the host.
	Interactive bool   `json:"interactive"`
	Network     string `json:"network"`
	State       State  `json:"state"`
	AgentPaused bool   `json:"agent_paused,omitempty"`
	InputPaused bool   `json:"input_paused,omitempty"`
}

type clientHello struct {
	Type     string `json:"type"`
	Protocol int    `json:"protocol"`
	// Join fields.
	Name  string `json:"name,omitempty"`
	OS    string `json:"os,omitempty"`
	Arch  string `json:"arch,omitempty"`
	Proof []byte `json:"proof,omitempty"`
	// Resume fields.
	SessionID   string `json:"session_id,omitempty"`
	ResumeToken string `json:"resume_token,omitempty"`
	Generation  uint64 `json:"generation,omitempty"`
}

type serverMsg struct {
	Type         string       `json:"type"`
	Proof        []byte       `json:"proof,omitempty"`
	Host         *PeerInfo    `json:"host,omitempty"`
	Verification string       `json:"verification,omitempty"`
	Session      *SessionInfo `json:"session,omitempty"`
	Token        string       `json:"token,omitempty"`
	ResumeToken  string       `json:"resume_token,omitempty"`
	Error        *Error       `json:"error,omitempty"`
}

func writeFrame(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(data) > maxFrame {
		return errors.New("handshake frame too large")
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(data)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}


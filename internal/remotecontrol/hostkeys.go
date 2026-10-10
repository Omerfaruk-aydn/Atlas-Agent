package remotecontrol

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// HostKeys checks SSH host keys against Atlas' own known_hosts file and,
// unless disabled per profile, reads the user's ~/.ssh/known_hosts. Only
// Atlas' file is ever written, and only after an explicit user decision.
type HostKeys struct {
	// Path is Atlas' known_hosts file.
	Path string
	// UserPath is the user's OpenSSH known_hosts file; read only.
	UserPath string
	mu       sync.Mutex
}

// HostKeyUnknownError is returned on the first connection to a server: the
// user must compare the fingerprint and decide.
type HostKeyUnknownError struct {
	Address     string
	Algorithm   string
	Fingerprint string
	key         ssh.PublicKey
}

func (e *HostKeyUnknownError) Error() string {
	return fmt.Sprintf("the SSH host key of %s is not trusted yet (%s %s)", e.Address, e.Algorithm, e.Fingerprint)
}

// HostKeyChangedError is returned when the server presents a key that
// differs from the trusted one. The connection is not used.
type HostKeyChangedError struct {
	Address     string
	Algorithm   string
	Fingerprint string
	Previous    []string
	// InUserFile is true when the trusted key came from ~/.ssh/known_hosts,
	// which Atlas does not modify.
	InUserFile bool
	Revoked    bool
	key        ssh.PublicKey
}

func (e *HostKeyChangedError) Error() string {
	return fmt.Sprintf("the SSH host key of %s changed (now %s %s)", e.Address, e.Algorithm, e.Fingerprint)
}

// HostAddress is the known_hosts address for host and port.
func HostAddress(host string, port int) string {
	return knownhosts.Normalize(net.JoinHostPort(host, strconv.Itoa(port)))
}

// Callback returns the host key callback for one connection.
func (h *HostKeys) Callback(useUserFile bool) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		address := knownhosts.Normalize(hostname)
		state, previous, revoked, err := checkFile(h.Path, hostname, remote, key)
		if err != nil {
			return fmt.Errorf("the Atlas known_hosts file is unreadable: %w", err)
		}
		switch state {
		case keyMatch:
			return nil
		case keyMismatch:
			return &HostKeyChangedError{Address: address, Algorithm: key.Type(), Fingerprint: ssh.FingerprintSHA256(key), Previous: previous, Revoked: revoked, key: key}
		}
		if useUserFile && h.UserPath != "" {
			// An unreadable user file is ignored rather than trusted: the
			// user is then asked to confirm the key as on a first visit.
			state, previous, revoked, err = checkFile(h.UserPath, hostname, remote, key)
			if err == nil {
				switch state {
				case keyMatch:
					return nil
				case keyMismatch:
					return &HostKeyChangedError{Address: address, Algorithm: key.Type(), Fingerprint: ssh.FingerprintSHA256(key), Previous: previous, InUserFile: true, Revoked: revoked, key: key}
				}
			}
		}
		return &HostKeyUnknownError{Address: address, Algorithm: key.Type(), Fingerprint: ssh.FingerprintSHA256(key), key: key}
	}
}

type keyState int

const (
	keyUnknown keyState = iota
	keyMatch
	keyMismatch
)

func checkFile(path, hostname string, remote net.Addr, key ssh.PublicKey) (keyState, []string, bool, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return keyUnknown, nil, false, nil
	}
	cb, err := knownhosts.New(path)
	if err != nil {
		return keyUnknown, nil, false, err
	}
	err = cb(hostname, remote, key)
	var keyErr *knownhosts.KeyError
	var revoked *knownhosts.RevokedError
	switch {
	case err == nil:
		return keyMatch, nil, false, nil
	case errors.As(err, &revoked):
		return keyMismatch, []string{ssh.FingerprintSHA256(revoked.Revoked.Key)}, true, nil
	case errors.As(err, &keyErr) && len(keyErr.Want) == 0:
		return keyUnknown, nil, false, nil
	case errors.As(err, &keyErr):
		var previous []string
		for _, want := range keyErr.Want {
			previous = append(previous, want.Key.Type()+" "+ssh.FingerprintSHA256(want.Key))
		}
		return keyMismatch, previous, false, nil
	}
	return keyUnknown, nil, false, err
}

// Trust records a key after the user compared its fingerprint. For an
// unknown key it appends a line; for a changed key it replaces only the
// lines Atlas wrote for that exact address. fingerprint must match the key
// the user saw, so a decision cannot apply to a different key.
func (h *HostKeys) Trust(address string, key ssh.PublicKey, fingerprint string) error {
	if key == nil || ssh.FingerprintSHA256(key) != fingerprint {
		return errors.New("the host key decision does not match the presented key")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	data, err := os.ReadFile(h.Path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var out bytes.Buffer
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == address {
			continue
		}
		out.WriteString(line + "\n")
	}
	out.WriteString(knownhosts.Line([]string{address}, key) + "\n")
	return taskstate.WriteFileAtomic(h.Path, out.Bytes(), 0o600)
}

// HostKeyDecision is the presented key the UI asks about.
type HostKeyDecision struct {
	Address     string
	Algorithm   string
	Fingerprint string
	Previous    []string
	Changed     bool
	InUserFile  bool
	Revoked     bool
	key         ssh.PublicKey
}

// DecisionFrom extracts a pending host key decision from an error.
func DecisionFrom(err error) (HostKeyDecision, bool) {
	var unknown *HostKeyUnknownError
	var changed *HostKeyChangedError
	switch {
	case errors.As(err, &unknown):
		return HostKeyDecision{Address: unknown.Address, Algorithm: unknown.Algorithm, Fingerprint: unknown.Fingerprint, key: unknown.key}, true
	case errors.As(err, &changed):
		return HostKeyDecision{Address: changed.Address, Algorithm: changed.Algorithm, Fingerprint: changed.Fingerprint, Previous: changed.Previous, Changed: true, InUserFile: changed.InUserFile, Revoked: changed.Revoked, key: changed.key}, true
	}
	return HostKeyDecision{}, false
}

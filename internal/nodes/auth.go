package nodes

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
)

// Pairing and authentication errors are classified for the API.
var (
	ErrPairingInvalid = errors.New("pairing code is invalid, expired or already used")
	ErrPairingLocked  = errors.New("pairing is locked after repeated failures; start a new pairing on the node")
	ErrUnauthorized   = errors.New("device token is missing, unknown or revoked")
)

const (
	pairingTTL         = 10 * time.Minute
	maxPairingFailures = 5
	tokenPrefix        = "atn_"
)

// Device is a controller paired with this node. Only the token hash is kept.
type Device struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	TokenHash string `json:"token_hash"`
	CreatedAt int64  `json:"created_at"`
	LastSeen  int64  `json:"last_seen,omitempty"`
	Revoked   bool   `json:"revoked,omitempty"`
}

type pairing struct {
	code    string
	expires time.Time
	used    bool
}

// Auth holds pairing codes and paired devices for one node.
type Auth struct {
	path     string
	mu       sync.Mutex
	devices  []Device
	pairings []*pairing
	failures int
	now      func() time.Time
}

// LoadAuth reads the device list from dir/devices.json.
func LoadAuth(dir string) (*Auth, error) {
	a := &Auth{path: filepath.Join(dir, "devices.json"), now: time.Now}
	data, err := os.ReadFile(a.path)
	if errors.Is(err, os.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &a.devices); err != nil {
		return nil, fmt.Errorf("corrupt device list: %w", err)
	}
	return a, nil
}

const codeAlphabet = "ABCDEFGHJKMNPQRSTVWXYZ23456789"

// NewPairingCode creates a single-use code valid for ten minutes. Creating a
// code also clears a lockout, since the node operator acted deliberately.
func (a *Auth) NewPairingCode() (string, time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
	}
	code := string(b[:4]) + "-" + string(b[4:])
	p := &pairing{code: code, expires: a.now().Add(pairingTTL)}
	a.pairings = append(a.pairings, p)
	a.failures = 0
	return code, p.expires
}

// Pair redeems a code and returns the new device and its plaintext token,
// which is shown to the controller exactly once.
func (a *Auth) Pair(code, name string) (Device, string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.failures >= maxPairingFailures {
		return Device{}, "", ErrPairingLocked
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	var match *pairing
	for _, p := range a.pairings {
		if subtle.ConstantTimeCompare([]byte(p.code), []byte(code)) == 1 {
			match = p
		}
	}
	if match == nil || match.used || a.now().After(match.expires) {
		a.failures++
		if a.failures >= maxPairingFailures {
			for _, p := range a.pairings {
				p.used = true
			}
		}
		return Device{}, "", ErrPairingInvalid
	}
	match.used = true
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		name = "controller"
	}
	token := tokenPrefix + randomHex(32)
	d := Device{ID: "dev-" + randomHex(6), Name: name, TokenHash: hashToken(token), CreatedAt: a.now().UnixMilli()}
	a.devices = append(a.devices, d)
	if err := a.saveLocked(); err != nil {
		a.devices = a.devices[:len(a.devices)-1]
		return Device{}, "", fmt.Errorf("persist paired device: %w", err)
	}
	return d, token, nil
}

// Provision adds a device with a token chosen by a trusted local creator
// (the isolation manager writing into the guest's exchange folder).
func (a *Auth) Provision(name, token string) (Device, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !strings.HasPrefix(token, tokenPrefix) || len(token) != len(tokenPrefix)+64 {
		return Device{}, errors.New("provisioned token has an invalid format")
	}
	d := Device{ID: "dev-" + randomHex(6), Name: name, TokenHash: hashToken(token), CreatedAt: a.now().UnixMilli()}
	a.devices = append(a.devices, d)
	return d, a.saveLocked()
}

// Authenticate maps a bearer token to a live device.
func (a *Auth) Authenticate(token string) (Device, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !strings.HasPrefix(token, tokenPrefix) {
		return Device{}, ErrUnauthorized
	}
	h := hashToken(token)
	for i := range a.devices {
		d := &a.devices[i]
		if subtle.ConstantTimeCompare([]byte(d.TokenHash), []byte(h)) == 1 {
			if d.Revoked {
				return Device{}, ErrUnauthorized
			}
			d.LastSeen = a.now().UnixMilli()
			return *d, nil
		}
	}
	return Device{}, ErrUnauthorized
}

// Revoke disables a device by id. Revocation is persisted before it is
// reported, so a crash cannot resurrect the device.
func (a *Auth) Revoke(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.devices {
		if a.devices[i].ID == id {
			if a.devices[i].Revoked {
				return nil
			}
			a.devices[i].Revoked = true
			if err := a.saveLocked(); err != nil {
				a.devices[i].Revoked = false
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("device %s not found", id)
}

// Devices lists paired devices (hashes included, never tokens).
func (a *Auth) Devices() []Device {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]Device(nil), a.devices...)
}

func (a *Auth) saveLocked() error { return taskstate.WriteJSONAtomic(a.path, a.devices) }

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewToken returns a fresh device token for provisioning.
func NewToken() string { return tokenPrefix + randomHex(32) }

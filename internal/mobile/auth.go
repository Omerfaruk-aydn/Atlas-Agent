// Package mobile is the phone control surface: a small HTTPS web app (PWA)
// served by the running Atlas process that lists tasks, streams their state,
// answers permission requests and questions, and pauses, resumes or stops
// runs. It is off by default and binds to loopback unless the user opts
// into the local network.
//
// Pairing never puts a credential in the QR code: the QR holds a
// short-lived single-use nonce; the phone redeems it for a pending device
// with a six-digit code shown on both screens; only the desktop user's
// explicit acceptance issues the session cookie.
package mobile

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
)

// Lifetimes.
const (
	NonceTTL        = 2 * time.Minute
	PendingTTL      = 5 * time.Minute
	SessionIdle     = 12 * time.Hour
	SessionAbsolute = 7 * 24 * time.Hour
)

// Errors returned by pairing and authentication.
var (
	ErrNonceInvalid = errors.New("pairing link is invalid, expired or already used; show a new QR code on the computer")
	ErrNotPending   = errors.New("pairing request is unknown, expired or already decided")
	ErrNoSession    = errors.New("not signed in")
)

// Device is a paired phone.
type Device struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CreatedAt   int64  `json:"created_at"`
	LastSeen    int64  `json:"last_seen"`
	SessionHash string `json:"session_hash"`
	CSRF        string `json:"csrf"`
	// Previews allows low-resolution screen previews for this device; it is
	// off by default and set only from the desktop.
	Previews bool `json:"previews"`
	// UserAgent helps the user recognize the device in the list.
	UserAgent string `json:"user_agent,omitempty"`
}

// Pending is a pairing waiting for desktop acceptance.
type Pending struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Code      string    `json:"code"`
	UserAgent string    `json:"user_agent"`
	Remote    string    `json:"remote"`
	Expires   time.Time `json:"expires"`
	pollHash  string
	state     string // pending, accepted, rejected
	session   string // plaintext session, handed to the poller once
	deviceID  string
}

type nonce struct {
	hash    string
	expires time.Time
	used    bool
}

// Auth owns pairing and device sessions.
type Auth struct {
	path    string
	mu      sync.Mutex
	devices []Device
	nonces  []*nonce
	pending map[string]*Pending
	now     func() time.Time
	// OnPending is called when a phone redeemed a nonce, so the desktop UI
	// can ask the user.
	OnPending func(Pending)
	// OnRevoke is called after devices were removed, so open event streams
	// can end without waiting for their next periodic check.
	OnRevoke func()
	// OnRevokeDevices receives the removed device IDs, so state bound to a
	// device (such as a password it supplied) is withdrawn at once.
	OnRevokeDevices func(ids []string)
	transfers       map[string]*transfer
}

// transfer lets an already paired browser hand its access to the same
// phone's Home Screen app, which iOS gives a separate cookie store.
type transfer struct {
	hash     string
	deviceID string
	expires  time.Time
}

// TransferTTL bounds a Home Screen transfer code.
const TransferTTL = 3 * time.Minute

const transferAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// LoadAuth reads dir/devices.json.
func LoadAuth(dir string) (*Auth, error) {
	a := &Auth{path: filepath.Join(dir, "devices.json"), pending: map[string]*Pending{}, now: time.Now}
	data, err := os.ReadFile(a.path)
	if errors.Is(err, os.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &a.devices); err != nil {
		return nil, fmt.Errorf("corrupt phone device list: %w", err)
	}
	return a, nil
}

func random(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// NewNonce returns a single-use pairing nonce valid for two minutes.
// Earlier unused nonces are invalidated, so only the QR on screen works.
func (a *Auth) NewNonce() (string, time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, n := range a.nonces {
		n.used = true
	}
	v := random(24)
	n := &nonce{hash: hash(v), expires: a.now().Add(NonceTTL)}
	a.nonces = append(a.nonces[:0], n)
	return v, n.expires
}

// Redeem consumes a nonce and creates a pending device. It returns the
// pending record and a poll token only the redeeming phone holds.
func (a *Auth) Redeem(value, name, userAgent, remote string) (Pending, string, error) {
	a.mu.Lock()
	h := hash(value)
	var match *nonce
	for _, n := range a.nonces {
		if subtle.ConstantTimeCompare([]byte(n.hash), []byte(h)) == 1 {
			match = n
		}
	}
	if match == nil || match.used || a.now().After(match.expires) {
		a.mu.Unlock()
		return Pending{}, "", ErrNonceInvalid
	}
	match.used = true
	code, _ := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if len(name) > 48 {
		name = name[:48]
	}
	if name == "" {
		name = "Phone"
	}
	poll := random(24)
	p := &Pending{ID: "pp-" + random(9), Name: name, Code: fmt.Sprintf("%06d", code.Int64()), UserAgent: truncate(userAgent, 160), Remote: remote, Expires: a.now().Add(PendingTTL), pollHash: hash(poll), state: "pending"}
	a.pending[p.ID] = p
	cb := a.OnPending
	out := *p
	a.mu.Unlock()
	if cb != nil {
		cb(out)
	}
	return out, poll, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// PendingList returns undecided pairings.
func (a *Auth) PendingList() []Pending {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []Pending
	for _, p := range a.pending {
		if p.state == "pending" && a.now().Before(p.Expires) {
			out = append(out, *p)
		}
	}
	slices.SortFunc(out, func(x, y Pending) int { return x.Expires.Compare(y.Expires) })
	return out
}

// Decide accepts or rejects a pending pairing from the desktop.
func (a *Auth) Decide(id string, accept bool) (Device, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.pending[id]
	if !ok || p.state != "pending" || a.now().After(p.Expires) {
		return Device{}, ErrNotPending
	}
	if !accept {
		p.state = "rejected"
		return Device{}, nil
	}
	session := random(32)
	d := Device{ID: "phone-" + random(6), Name: p.Name, CreatedAt: a.now().UnixMilli(), LastSeen: a.now().UnixMilli(), SessionHash: hash(session), CSRF: random(24), UserAgent: p.UserAgent}
	a.devices = append(a.devices, d)
	if err := a.saveLocked(); err != nil {
		a.devices = a.devices[:len(a.devices)-1]
		return Device{}, err
	}
	p.state, p.session, p.deviceID = "accepted", session, d.ID
	return d, nil
}

// Cancel lets the redeeming phone withdraw its own pending request, so the
// computer no longer offers it for acceptance. Only the poll token holder
// can cancel, and only while the request is still pending.
func (a *Auth) Cancel(id, poll string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.pending[id]
	if !ok || subtle.ConstantTimeCompare([]byte(p.pollHash), []byte(hash(poll))) != 1 {
		return ErrNotPending
	}
	if p.state != "pending" || a.now().After(p.Expires) {
		return ErrNotPending
	}
	p.state = "cancelled"
	return nil
}

// Poll lets the redeeming phone learn the decision. On acceptance it
// returns the session secret exactly once.
func (a *Auth) Poll(id, poll string) (state, session string, d Device, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.pending[id]
	if !ok || subtle.ConstantTimeCompare([]byte(p.pollHash), []byte(hash(poll))) != 1 {
		return "", "", Device{}, ErrNotPending
	}
	if p.state == "pending" && a.now().After(p.Expires) {
		p.state = "expired"
	}
	state = p.state
	if state == "accepted" && p.session != "" {
		session, p.session = p.session, ""
		for _, dev := range a.devices {
			if dev.ID == p.deviceID {
				d = dev
			}
		}
		delete(a.pending, id)
	}
	return state, session, d, nil
}

// Authenticate maps a session cookie to a device, enforcing idle and
// absolute lifetimes.
func (a *Auth) Authenticate(session string) (Device, error) {
	if session == "" {
		return Device{}, ErrNoSession
	}
	h := hash(session)
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	for i := range a.devices {
		d := &a.devices[i]
		if subtle.ConstantTimeCompare([]byte(d.SessionHash), []byte(h)) != 1 {
			continue
		}
		if now.Sub(time.UnixMilli(d.CreatedAt)) > SessionAbsolute || now.Sub(time.UnixMilli(d.LastSeen)) > SessionIdle {
			return Device{}, errors.New("session expired; pair the phone again")
		}
		if now.Sub(time.UnixMilli(d.LastSeen)) > time.Minute {
			d.LastSeen = now.UnixMilli()
			_ = a.saveLocked()
		}
		return *d, nil
	}
	return Device{}, ErrNoSession
}

// Devices lists paired phones.
func (a *Auth) Devices() []Device {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.devices)
}

// Revoke removes a device; its session stops working immediately.
func (a *Auth) Revoke(id string) error {
	return a.revoke(func(d Device) bool { return d.ID == id }, fmt.Errorf("no phone %s", id))
}

// RevokeAll removes every paired device and cancels undecided pairings.
// It returns how many devices were removed.
func (a *Auth) RevokeAll() (int, error) {
	a.mu.Lock()
	n := len(a.devices)
	for _, p := range a.pending {
		if p.state == "pending" {
			p.state = "rejected"
		}
	}
	a.mu.Unlock()
	if n == 0 {
		return 0, nil
	}
	return n, a.revoke(func(Device) bool { return true }, nil)
}

func (a *Auth) revoke(match func(Device) bool, notFound error) error {
	a.mu.Lock()
	n := len(a.devices)
	kept := slices.DeleteFunc(slices.Clone(a.devices), match)
	if len(kept) == n {
		a.mu.Unlock()
		return notFound
	}
	prev := a.devices
	a.devices = kept
	if err := a.saveLocked(); err != nil {
		a.devices = prev
		a.mu.Unlock()
		return err
	}
	var removed []string
	for _, d := range prev {
		if match(d) {
			removed = append(removed, d.ID)
		}
	}
	for k, t := range a.transfers {
		if slices.Contains(removed, t.deviceID) {
			delete(a.transfers, k)
		}
	}
	cb, cbDevices := a.OnRevoke, a.OnRevokeDevices
	a.mu.Unlock()
	if cbDevices != nil {
		cbDevices(removed)
	}
	if cb != nil {
		cb()
	}
	return nil
}

// NewTransfer issues a one-time code that lets the same phone's Home
// Screen app join without a new QR code. Only an authenticated device can
// ask, and a new code replaces that device's previous one.
func (a *Auth) NewTransfer(deviceID string) (string, time.Time, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !slices.ContainsFunc(a.devices, func(d Device) bool { return d.ID == deviceID }) {
		return "", time.Time{}, ErrNoSession
	}
	if a.transfers == nil {
		a.transfers = map[string]*transfer{}
	}
	now := a.now()
	for k, t := range a.transfers {
		if t.deviceID == deviceID || now.After(t.expires) {
			delete(a.transfers, k)
		}
	}
	b := make([]byte, 8)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(transferAlphabet))))
		if err != nil {
			return "", time.Time{}, err
		}
		b[i] = transferAlphabet[n.Int64()]
	}
	code := string(b)
	t := &transfer{hash: hash(code), deviceID: deviceID, expires: now.Add(TransferTTL)}
	a.transfers[t.hash] = t
	return code, t.expires, nil
}

// RedeemTransfer turns a transfer code into a new device session for the
// Home Screen app. The code works once; the new device keeps the source
// device's preview setting because it is the same phone.
func (a *Auth) RedeemTransfer(code, userAgent string) (Device, string, error) {
	code = strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(code), " ", ""), "-", ""))
	a.mu.Lock()
	defer a.mu.Unlock()
	t, ok := a.transfers[hash(code)]
	if !ok || len(code) != 8 {
		return Device{}, "", ErrNonceInvalid
	}
	delete(a.transfers, t.hash)
	if a.now().After(t.expires) {
		return Device{}, "", ErrNonceInvalid
	}
	var src *Device
	for i := range a.devices {
		if a.devices[i].ID == t.deviceID {
			src = &a.devices[i]
		}
	}
	if src == nil {
		return Device{}, "", ErrNonceInvalid
	}
	name := src.Name
	if !strings.HasSuffix(name, " (Home Screen)") {
		name = truncate(name, 34) + " (Home Screen)"
	}
	session := random(32)
	d := Device{ID: "phone-" + random(6), Name: name, CreatedAt: a.now().UnixMilli(), LastSeen: a.now().UnixMilli(), SessionHash: hash(session), CSRF: random(24), UserAgent: truncate(userAgent, 160), Previews: src.Previews}
	a.devices = append(a.devices, d)
	if err := a.saveLocked(); err != nil {
		a.devices = a.devices[:len(a.devices)-1]
		return Device{}, "", err
	}
	return d, session, nil
}

// SetPreviews toggles screen previews for a device (desktop side only).
func (a *Auth) SetPreviews(id string, on bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.devices {
		if a.devices[i].ID == id {
			a.devices[i].Previews = on
			return a.saveLocked()
		}
	}
	return fmt.Errorf("no phone %s", id)
}

func (a *Auth) saveLocked() error { return taskstate.WriteJSONAtomic(a.path, a.devices) }

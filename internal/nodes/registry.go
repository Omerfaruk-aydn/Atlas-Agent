package nodes

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
)

// Network classifies how a node is reached.
type Network string

const (
	NetworkLoopback Network = "loopback"
	NetworkLAN      Network = "lan"
	NetworkInternet Network = "internet"
)

// ClassifyHost reports the network class of a node URL host.
func ClassifyHost(host string) Network {
	h := strings.ToLower(strings.Trim(host, "[]"))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return NetworkLoopback
	}
	ip := net.ParseIP(h)
	if ip == nil {
		if strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".lan") || strings.HasSuffix(h, ".home.arpa") || !strings.Contains(h, ".") {
			return NetworkLAN
		}
		return NetworkInternet
	}
	switch {
	case ip.IsLoopback():
		return NetworkLoopback
	case ip.IsPrivate(), ip.IsLinkLocalUnicast():
		return NetworkLAN
	}
	// 100.64.0.0/10 (carrier-grade NAT, also used by overlay VPNs) is not
	// the public internet but is not the local LAN either; treat as internet
	// so the user must opt in explicitly.
	return NetworkInternet
}

// Entry is one paired node as stored by a controller.
type Entry struct {
	Name         string       `json:"name"`
	URL          string       `json:"url"`
	Fingerprint  string       `json:"fingerprint"`
	NodeID       string       `json:"node_id"`
	DeviceID     string       `json:"device_id"`
	OS           string       `json:"os"`
	Arch         string       `json:"arch"`
	Root         string       `json:"root"`
	Capabilities []Capability `json:"capabilities"`
	Network      Network      `json:"network"`
	Isolated     bool         `json:"isolated,omitempty"`
	MaxFileBytes int64        `json:"max_file_bytes,omitempty"`
	PairedAt     int64        `json:"paired_at"`
	// SealedToken is the token protected with the platform credential
	// store; Protection names the mechanism.
	SealedToken string `json:"sealed_token"`
	Protection  string `json:"protection"`
}

var nodeName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// ValidName reports whether name can address a node in tool calls.
func ValidName(name string) bool { return nodeName.MatchString(name) && name != "local" }

// Registry is the controller's list of paired nodes.
type Registry struct {
	path string
	mu   sync.Mutex
}

// OpenRegistry uses dir/nodes.json.
func OpenRegistry(dir string) *Registry { return &Registry{path: filepath.Join(dir, "nodes.json")} }

func (r *Registry) load() ([]Entry, error) {
	data, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Entry
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("corrupt node registry: %w", err)
	}
	return out, nil
}

// List returns all entries without tokens.
func (r *Registry) List() ([]Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries, err := r.load()
	for i := range entries {
		entries[i].SealedToken = ""
	}
	return entries, err
}

// Get returns an entry and its plaintext token.
func (r *Registry) Get(name string) (Entry, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries, err := r.load()
	if err != nil {
		return Entry{}, "", err
	}
	for _, e := range entries {
		if e.Name == name {
			token, err := unsealToken(e)
			return e, token, err
		}
	}
	return Entry{}, "", fmt.Errorf("no paired node named %q", name)
}

// Put adds or replaces an entry, sealing the token.
func (r *Registry) Put(e Entry, token string) error {
	if !ValidName(e.Name) {
		return fmt.Errorf("node name %q must be 1-32 lowercase letters, digits or '-', and not \"local\"", e.Name)
	}
	sealed, protection, err := sealToken(token)
	if err != nil {
		return err
	}
	e.SealedToken, e.Protection = sealed, protection
	if e.PairedAt == 0 {
		e.PairedAt = time.Now().UnixMilli()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entries, err := r.load()
	if err != nil {
		return err
	}
	entries = slices.DeleteFunc(entries, func(x Entry) bool { return x.Name == e.Name })
	entries = append(entries, e)
	return taskstate.WriteJSONAtomic(r.path, entries)
}

// Remove deletes an entry.
func (r *Registry) Remove(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries, err := r.load()
	if err != nil {
		return err
	}
	n := len(entries)
	entries = slices.DeleteFunc(entries, func(x Entry) bool { return x.Name == name })
	if len(entries) == n {
		return fmt.Errorf("no paired node named %q", name)
	}
	return taskstate.WriteJSONAtomic(r.path, entries)
}

func sealToken(token string) (string, string, error) {
	sealed, err := vault.Seal([]byte(token))
	if err == nil {
		mech := "dpapi"
		if runtime.GOOS != "windows" {
			mech = "atlas_vault_key"
		}
		return base64.StdEncoding.EncodeToString(sealed), mech, nil
	}
	if runtime.GOOS == "windows" {
		return "", "", err
	}
	// Without ATLAS_VAULT_KEY the token relies on owner-only file
	// permissions, like an SSH private key. The protection field says so.
	return base64.StdEncoding.EncodeToString([]byte(token)), "file_permissions", nil
}

func unsealToken(e Entry) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(e.SealedToken)
	if err != nil {
		return "", err
	}
	if e.Protection == "file_permissions" {
		return string(raw), nil
	}
	plain, err := vault.Unseal(raw)
	if err != nil {
		return "", fmt.Errorf("cannot unseal the token for node %s: %w", e.Name, err)
	}
	return string(plain), nil
}

// ClientFor builds a pinned client for a registered node.
func (r *Registry) ClientFor(name string) (*Client, Entry, error) {
	e, token, err := r.Get(name)
	if err != nil {
		return nil, Entry{}, err
	}
	c, err := NewClient(e.URL, e.Fingerprint, token)
	return c, e, err
}

// HostOf extracts the host of a node URL.
func HostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

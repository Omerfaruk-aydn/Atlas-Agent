package remotecontrol

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
)

// Secrets keeps SSH passwords and key passphrases sealed with the
// operating-system protection the credential vault uses (DPAPI on Windows,
// ATLAS_VAULT_KEY elsewhere). Profiles store only the reference. When
// sealing is unavailable a secret is kept in memory for this run only and
// Persisted reports false.
type Secrets struct {
	Dir    string
	mu     sync.Mutex
	memory map[string][]byte
	// seal and unseal default to the vault's protection; tests replace them.
	seal, unseal func([]byte) ([]byte, error)
}

var refPattern = regexp.MustCompile(`^sr-[0-9a-f]{24}$`)

// NewSecretRef returns a fresh secret reference.
func NewSecretRef() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "sr-" + hex.EncodeToString(b[:])
}

func (s *Secrets) path(ref string) (string, error) {
	if !refPattern.MatchString(ref) {
		return "", errors.New("invalid secret reference")
	}
	return filepath.Join(s.Dir, ref+".sealed"), nil
}

func (s *Secrets) sealer() (func([]byte) ([]byte, error), func([]byte) ([]byte, error)) {
	if s.seal != nil {
		return s.seal, s.unseal
	}
	return vault.Seal, vault.Unseal
}

// Put stores value under ref. persisted is false when the platform could
// not seal it; the value then lives only in this process.
func (s *Secrets) Put(ref string, value []byte) (persisted bool, err error) {
	path, err := s.path(ref)
	if err != nil {
		return false, err
	}
	if len(value) == 0 || len(value) > 4096 {
		return false, errors.New("invalid secret length")
	}
	seal, _ := s.sealer()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.memory == nil {
		s.memory = map[string][]byte{}
	}
	sealed, sealErr := seal(value)
	if sealErr != nil {
		clear(s.memory[ref])
		s.memory[ref] = append([]byte(nil), value...)
		_ = os.Remove(path)
		return false, nil
	}
	if err := taskstate.WriteFileAtomic(path, sealed, 0o600); err != nil {
		return false, errors.New("could not save the sealed secret")
	}
	clear(s.memory[ref])
	delete(s.memory, ref)
	return true, nil
}

// Get returns a copy of the secret; callers clear it after use.
func (s *Secrets) Get(ref string) ([]byte, error) {
	path, err := s.path(ref)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if v, ok := s.memory[ref]; ok {
		s.mu.Unlock()
		return append([]byte(nil), v...), nil
	}
	s.mu.Unlock()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrSecretMissing
	}
	if err != nil {
		return nil, errors.New("could not read the sealed secret")
	}
	_, unseal := s.sealer()
	plain, err := unseal(data)
	if err != nil {
		return nil, errors.New("the sealed secret cannot be opened by this user account")
	}
	return plain, nil
}

// Delete removes the secret from disk and memory.
func (s *Secrets) Delete(ref string) error {
	path, err := s.path(ref)
	if err != nil {
		return err
	}
	s.mu.Lock()
	clear(s.memory[ref])
	delete(s.memory, ref)
	s.mu.Unlock()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// ErrSecretMissing means a profile references a secret that is not stored,
// for example after a restart when sealing was unavailable.
var ErrSecretMissing = errors.New("the saved SSH secret is missing; enter it again in the profile")

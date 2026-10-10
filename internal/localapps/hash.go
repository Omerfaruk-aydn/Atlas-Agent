package localapps

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
)

// sourceHash fingerprints the app's files (not its data), so verification
// and acceptance evidence can be tied to the exact code they ran against.
func sourceHash(root string, dataDirs []string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}
		if e.IsDir() {
			if slices.Contains(skipSourceDirs, e.Name()) || isDataDir(rel, dataDirs) || strings.HasPrefix(e.Name(), ".atlas-") {
				return filepath.SkipDir
			}
			return nil
		}
		if e.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00", filepath.ToSlash(rel))
		_, err = io.Copy(h, f)
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		h.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("fingerprint app source: %w", err)
	}
	// The version separates complete fingerprints from legacy truncated ones.
	return "v2:" + hex.EncodeToString(h.Sum(nil)), nil
}

// SetSecret seals value in the per-user secret store for the app. The
// manifest only names the secret; the value never enters the manifest,
// the registry, logs or the model context.
func (m *Manager) SetSecret(id, name, value string) error {
	if !ValidID(id) || !envNamePattern.MatchString(name) {
		return errors.New("invalid app id or secret name")
	}
	if value == "" || len(value) > 8192 {
		return errors.New("secret value must be 1-8192 bytes")
	}
	sealed, err := vault.Seal([]byte(value))
	if err != nil {
		return fmt.Errorf("seal the secret: %w", err)
	}
	return writeFileAtomic(filepath.Join(m.secretDir(id), name+".sealed"), sealed, 0o600)
}

// HasSecret reports whether a secret is stored.
func (m *Manager) HasSecret(id, name string) bool {
	if !ValidID(id) || !envNamePattern.MatchString(name) {
		return false
	}
	_, err := os.Stat(filepath.Join(m.secretDir(id), name+".sealed"))
	return err == nil
}

func (m *Manager) secret(id, name string) (string, error) {
	if !ValidID(id) || !envNamePattern.MatchString(name) {
		return "", errors.New("invalid secret reference")
	}
	data, err := os.ReadFile(filepath.Join(m.secretDir(id), name+".sealed"))
	if err != nil {
		return "", err
	}
	plain, err := vault.Unseal(data)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

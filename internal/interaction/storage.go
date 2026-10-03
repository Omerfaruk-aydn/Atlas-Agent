package interaction

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func sessionPath(id string) string {
	hash := sha256.Sum256([]byte(id))
	return filepath.Join(".atlas", "interactions", fmt.Sprintf("%x.json", hash))
}

// storageRoot confines all runtime writes using OS-backed root handles.
func storageRoot(root string) (*os.Root, error) {
	fs, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	for _, path := range []string{".atlas", filepath.Join(".atlas", "interactions")} {
		info, err := fs.Lstat(path)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			fs.Close()
			return nil, fmt.Errorf("interaction storage must not be a symlink")
		}
		if err != nil && !os.IsNotExist(err) {
			fs.Close()
			return nil, err
		}
	}
	if err := fs.MkdirAll(filepath.Join(".atlas", "interactions"), 0o700); err != nil {
		fs.Close()
		return nil, err
	}
	return fs, nil
}

func saveState(root, id string, data []byte) error {
	fs, err := storageRoot(root)
	if err != nil {
		return err
	}
	defer fs.Close()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	name := filepath.Join(".atlas", "interactions", fmt.Sprintf("trace-%x.tmp", random))
	f, err := fs.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer fs.Remove(name)
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return fs.Rename(name, sessionPath(id))
}

func saveCapture(root string, data []byte) (string, error) {
	fs, err := storageRoot(root)
	if err != nil {
		return "", err
	}
	defer fs.Close()
	dir := filepath.Join(".atlas", "interactions", "captures")
	if info, err := fs.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("capture storage must not be a symlink")
	}
	if err := fs.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	name := filepath.Join(dir, fmt.Sprintf("%x.png", hash))
	if existing, err := fs.Lstat(name); err == nil {
		if !existing.Mode().IsRegular() {
			return "", fmt.Errorf("capture cache entry must be a regular file")
		}
		previous, err := fs.ReadFile(name)
		if err != nil {
			return "", err
		}
		if sha256.Sum256(previous) != hash {
			return "", fmt.Errorf("capture cache entry is corrupted")
		}
		return filepath.Join(root, name), nil
	}
	directory, err := fs.Open(dir)
	if err != nil {
		return "", err
	}
	entries, err := directory.ReadDir(1025)
	directory.Close()
	if err != nil && err != io.EOF {
		return "", err
	}
	if len(entries) >= 1024 {
		return "", fmt.Errorf("capture cache reached 1024 files; remove unneeded local captures")
	}
	var total int64
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return "", err
		}
		total += info.Size()
	}
	if total+int64(len(data)) > 512*1024*1024 {
		return "", fmt.Errorf("capture cache reached 512 MiB; remove unneeded local captures")
	}
	f, err := fs.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		_ = fs.Remove(name)
		return "", err
	}
	if closeErr != nil {
		_ = fs.Remove(name)
		return "", closeErr
	}
	return filepath.Join(root, name), nil
}

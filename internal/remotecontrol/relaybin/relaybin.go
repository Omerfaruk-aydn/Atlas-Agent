// Package relaybin carries the standalone mobile relay for Linux servers
// inside the Atlas executable, so /rc never downloads a binary on the
// server. The files are produced by `go generate` (see gen.go) from this
// repository's cmd/atlas-mobile-relay; each artifact's SHA-256 is recorded
// at build time and verified again on the server after upload.
package relaybin

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
)

//go:generate go run gen.go

//go:embed files
var files embed.FS

// Artifact describes one bundled relay binary.
type Artifact struct {
	Arch    string `json:"-"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	Version string `json:"-"`
}

type manifest struct {
	Version   string              `json:"version"`
	Artifacts map[string]Artifact `json:"artifacts"`
}

// ErrNotBundled means this Atlas build was compiled without running
// `go generate ./internal/remotecontrol/relaybin`.
var ErrNotBundled = errors.New("this Atlas build does not include the Linux relay; rebuild after go generate ./internal/remotecontrol/relaybin")

func readManifest() (manifest, error) {
	data, err := files.ReadFile("files/manifest.json")
	if err != nil {
		return manifest{}, ErrNotBundled
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return manifest{}, fmt.Errorf("corrupt relay manifest: %w", err)
	}
	return m, nil
}

// Available lists the bundled architectures (amd64, arm64).
func Available() []string {
	m, err := readManifest()
	if err != nil {
		return nil
	}
	var out []string
	for arch := range m.Artifacts {
		out = append(out, arch)
	}
	slices.Sort(out)
	return out
}

// Describe returns the artifact metadata without decompressing it.
func Describe(arch string) (Artifact, error) {
	m, err := readManifest()
	if err != nil {
		return Artifact{}, err
	}
	a, ok := m.Artifacts[arch]
	if !ok {
		return Artifact{}, fmt.Errorf("no bundled relay for linux/%s", arch)
	}
	a.Arch, a.Version = arch, m.Version
	return a, nil
}

// Lookup returns the artifact and its bytes after checking the recorded
// size and SHA-256.
func Lookup(arch string) (Artifact, []byte, error) {
	a, err := Describe(arch)
	if err != nil {
		return Artifact{}, nil, err
	}
	compressed, err := files.ReadFile("files/atlas-mobile-relay-linux-" + arch + ".gz")
	if err != nil {
		return Artifact{}, nil, ErrNotBundled
	}
	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return Artifact{}, nil, fmt.Errorf("corrupt bundled relay: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(zr, 64<<20))
	if err != nil {
		return Artifact{}, nil, fmt.Errorf("corrupt bundled relay: %w", err)
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) != a.Size || hex.EncodeToString(sum[:]) != a.SHA256 {
		return Artifact{}, nil, errors.New("bundled relay failed its integrity check")
	}
	return a, data, nil
}

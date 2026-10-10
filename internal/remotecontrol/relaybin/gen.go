//go:build ignore

// gen builds cmd/atlas-mobile-relay for linux/amd64 and linux/arm64,
// compresses each binary and records its SHA-256 in files/manifest.json.
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type artifact struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "relaybin:", err)
		os.Exit(1)
	}
}

func run() error {
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return err
	}
	root := filepath.Dir(strings.TrimSpace(string(out)))
	dir, err := filepath.Abs("files")
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "atlas-relaybin-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	version := "dev"
	if v, err := exec.Command("git", "-C", root, "describe", "--always", "--dirty").Output(); err == nil {
		version = strings.TrimSpace(string(v))
	}
	m := struct {
		Version   string              `json:"version"`
		Artifacts map[string]artifact `json:"artifacts"`
	}{Version: version, Artifacts: map[string]artifact{}}
	for _, arch := range []string{"amd64", "arm64"} {
		bin := filepath.Join(tmp, "relay-"+arch)
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w -buildid=", "-o", bin, "./cmd/atlas-mobile-relay")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+arch, "CGO_ENABLED=0", "GOEXPERIMENT=greenteagc")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("build linux/%s: %w", arch, err)
		}
		data, err := os.ReadFile(bin)
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		if _, err := zw.Write(data); err != nil {
			return err
		}
		if err := zw.Close(); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "atlas-mobile-relay-linux-"+arch+".gz"), buf.Bytes(), 0o644); err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		m.Artifacts[arch] = artifact{SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), append(data, '\n'), 0o644)
}

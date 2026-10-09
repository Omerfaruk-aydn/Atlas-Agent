package clibridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Kind classifies how an executable would be started.
type Kind string

const (
	// KindNative is a binary the operating system loads directly.
	KindNative Kind = "native"
	// KindScript is a Unix script with a shebang line; the kernel starts
	// the named interpreter with the literal argv.
	KindScript Kind = "script"
	// KindBatch is a Windows .cmd/.bat file. cmd.exe re-parses its command
	// line with its own quoting rules, so literal argv cannot be guaranteed.
	KindBatch Kind = "batch"
	// KindLauncher is a Windows App Execution Alias or similar zero-byte
	// reparse point whose real target cannot be identified or hashed.
	KindLauncher Kind = "launcher"
	// KindUnsupported is anything else, including text files without a
	// shebang that a shell would run as shell source.
	KindUnsupported Kind = "unsupported"
)

// Status is an executable's availability.
type Status string

const (
	StatusAvailable   Status = "available"
	StatusUnavailable Status = "unavailable"
	StatusUnsupported Status = "unsupported"
	StatusChanged     Status = "changed"
	StatusUnpinned    Status = "unpinned"
)

// maxHashBytes bounds hashing. Larger executables are refused instead of
// being trusted without an identity.
const maxHashBytes = 1 << 30

// Identity binds an executable path to observed content.
type Identity struct {
	Requested   string `json:"requested"`
	Path        string `json:"path"`
	Kind        Kind   `json:"kind"`
	Interpreter string `json:"interpreter,omitempty"`
	Size        int64  `json:"size"`
	ModTime     int64  `json:"mod_time"`
	SHA256      string `json:"sha256"`
	PinnedAt    int64  `json:"pinned_at,omitempty"`
}

// ExecutableError explains why an executable cannot be used.
type ExecutableError struct {
	Status Status
	Kind   Kind
	Reason string
}

func (e *ExecutableError) Error() string { return string(e.Status) + ": " + e.Reason }

// StatusOf extracts the availability status of an executable error.
func StatusOf(err error) Status {
	if err == nil {
		return StatusAvailable
	}
	if e, ok := errors.AsType[*ExecutableError](err); ok {
		return e.Status
	}
	return StatusUnavailable
}

// LookPath resolves a bare program name through PATH exactly once. On
// Windows PATHEXT order applies and the current directory is not searched.
func LookPath(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", &ExecutableError{Status: StatusUnavailable, Reason: fmt.Sprintf("%s was not found on PATH; install it or set executable.path", name)}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", &ExecutableError{Status: StatusUnavailable, Reason: err.Error()}
	}
	return abs, nil
}

// Identify inspects and hashes a file without running it. Unsupported
// kinds return an identity together with an *ExecutableError.
func Identify(ctx context.Context, requested, path string) (Identity, error) {
	identity := Identity{Requested: requested, Path: path}
	if !filepath.IsAbs(path) {
		return identity, &ExecutableError{Status: StatusUnavailable, Reason: "executable path must be absolute"}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return identity, &ExecutableError{Status: StatusUnavailable, Reason: fmt.Sprintf("%s does not exist or cannot be read", path)}
	}
	if runtime.GOOS == "windows" && info.Size() == 0 && info.Mode()&os.ModeIrregular != 0 || runtime.GOOS == "windows" && info.Size() == 0 && strings.Contains(strings.ToLower(path), `\windowsapps\`) {
		identity.Kind = KindLauncher
		return identity, &ExecutableError{Status: StatusUnsupported, Kind: KindLauncher, Reason: "Windows App Execution Aliases are launchers whose target cannot be identified or hashed; set executable.path to the real program inside its installation directory"}
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return identity, &ExecutableError{Status: StatusUnavailable, Reason: fmt.Sprintf("cannot resolve %s: %v", path, err)}
	}
	if resolved, err = filepath.Abs(resolved); err != nil {
		return identity, &ExecutableError{Status: StatusUnavailable, Reason: err.Error()}
	}
	identity.Path = resolved
	info, err = os.Stat(resolved)
	if err != nil {
		return identity, &ExecutableError{Status: StatusUnavailable, Reason: err.Error()}
	}
	if !info.Mode().IsRegular() {
		return identity, &ExecutableError{Status: StatusUnsupported, Kind: KindUnsupported, Reason: resolved + " is not a regular file"}
	}
	if info.Size() > maxHashBytes {
		return identity, &ExecutableError{Status: StatusUnsupported, Kind: KindUnsupported, Reason: "executable exceeds the 1 GiB identity limit"}
	}
	identity.Size, identity.ModTime = info.Size(), info.ModTime().UnixMilli()
	kind, interpreter, reason := classify(resolved, info)
	identity.Kind, identity.Interpreter = kind, interpreter
	if reason != "" {
		return identity, &ExecutableError{Status: StatusUnsupported, Kind: kind, Reason: reason}
	}
	if err := ctx.Err(); err != nil {
		return identity, err
	}
	sum, err := fileSHA256(resolved, maxHashBytes)
	if err != nil {
		return identity, &ExecutableError{Status: StatusUnavailable, Reason: "cannot hash executable: " + err.Error()}
	}
	identity.SHA256 = sum
	return identity, nil
}

// classify decides how a file would start, without starting it.
func classify(path string, info os.FileInfo) (Kind, string, string) {
	head := make([]byte, 256)
	f, err := os.Open(path)
	if err != nil {
		return KindUnsupported, "", err.Error()
	}
	n, _ := io.ReadFull(f, head)
	f.Close()
	head = head[:n]
	ext := strings.ToLower(filepath.Ext(path))
	if runtime.GOOS == "windows" {
		switch {
		case ext == ".bat" || ext == ".cmd":
			return KindBatch, "", "batch files are run by cmd.exe, which re-parses arguments with its own quoting rules, so a literal argv cannot be guaranteed; point the profile at the real program (for npm shims: node.exe with the script as a fixed argument)"
		case bytes.HasPrefix(head, []byte("MZ")):
			return KindNative, "", ""
		case bytes.HasPrefix(head, []byte("#!")):
			return KindScript, shebang(head), "scripts are not directly executable on Windows; set executable.path to the interpreter and pass the script as a fixed argument"
		default:
			return KindUnsupported, "", fmt.Sprintf("%s is not a Windows executable image (%s files are not run natively)", filepath.Base(path), cmpOr(ext, "extensionless"))
		}
	}
	if info.Mode().Perm()&0o111 == 0 {
		return KindUnsupported, "", path + " is not marked executable"
	}
	switch {
	case bytes.HasPrefix(head, []byte{0x7f, 'E', 'L', 'F'}),
		bytes.HasPrefix(head, []byte{0xfe, 0xed, 0xfa, 0xce}), bytes.HasPrefix(head, []byte{0xfe, 0xed, 0xfa, 0xcf}),
		bytes.HasPrefix(head, []byte{0xce, 0xfa, 0xed, 0xfe}), bytes.HasPrefix(head, []byte{0xcf, 0xfa, 0xed, 0xfe}),
		bytes.HasPrefix(head, []byte{0xca, 0xfe, 0xba, 0xbe}):
		return KindNative, "", ""
	case bytes.HasPrefix(head, []byte("#!")):
		interpreter := shebang(head)
		if !filepath.IsAbs(strings.Fields(interpreter + " x")[0]) {
			return KindScript, interpreter, "the shebang interpreter must be an absolute path"
		}
		return KindScript, interpreter, ""
	}
	return KindUnsupported, "", "file has no executable header or shebang and would be interpreted as shell source"
}

func shebang(head []byte) string {
	line, _, _ := bytes.Cut(head[2:], []byte("\n"))
	return strings.TrimSpace(string(bytes.TrimSuffix(line, []byte("\r"))))
}

func fileSHA256(path string, limit int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, limit+1))
	if err != nil {
		return "", err
	}
	if n > limit {
		return "", fmt.Errorf("file exceeds hashing limit")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Pin resolves a profile's executable now, for Import or an explicit
// re-pin. It never runs the program.
func Pin(ctx context.Context, e Executable) (Identity, error) {
	requested, path := e.Path, e.Path
	if e.Name != "" {
		requested = e.Name
		found, err := LookPath(e.Name)
		if err != nil {
			return Identity{Requested: requested}, err
		}
		path = found
	}
	identity, err := Identify(ctx, requested, path)
	if err != nil {
		return identity, err
	}
	if e.SHA256 != "" && e.SHA256 != identity.SHA256 {
		return identity, &ExecutableError{Status: StatusChanged, Kind: identity.Kind, Reason: fmt.Sprintf("%s has sha256 %s but the profile requires %s", identity.Path, identity.SHA256, e.SHA256)}
	}
	identity.PinnedAt = time.Now().UnixMilli()
	return identity, nil
}

// Current verifies that the pinned executable is still the same file.
// PATH is not consulted: a changed PATH cannot silently select a
// different program.
func Current(ctx context.Context, installed Installed) (Identity, error) {
	if installed.Pin == nil {
		return Identity{Requested: installed.Profile.Executable.Name}, &ExecutableError{Status: StatusUnpinned, Reason: "the executable was unavailable when the profile was imported; install it, then run `atlas-agent cli-bridge pin " + installed.Profile.ID + "`"}
	}
	pin := *installed.Pin
	identity, err := Identify(ctx, pin.Requested, pin.Path)
	if err != nil {
		if StatusOf(err) == StatusUnavailable {
			return identity, &ExecutableError{Status: StatusUnavailable, Reason: fmt.Sprintf("pinned executable %s is missing; reinstall it or re-pin with `atlas-agent cli-bridge pin %s`", pin.Path, installed.Profile.ID)}
		}
		return identity, err
	}
	identity.PinnedAt = pin.PinnedAt
	if identity.SHA256 != pin.SHA256 || identity.Path != pin.Path {
		return identity, &ExecutableError{Status: StatusChanged, Kind: identity.Kind, Reason: fmt.Sprintf("%s changed since it was pinned (sha256 %s, now %s); review the new program and re-pin with `atlas-agent cli-bridge pin %s`", pin.Path, shortHash(pin.SHA256), shortHash(identity.SHA256), installed.Profile.ID)}
	}
	return identity, nil
}

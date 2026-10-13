// Package tsfunnel connects the phone gateway to the internet with the
// user's own Tailscale Funnel. Atlas only drives the installed Tailscale
// CLI: it never embeds credentials, never runs "reset" and never edits
// Serve or Funnel entries it did not create.
//
// The Funnel runs in the CLI's foreground mode. tailscaled stores such an
// entry under ServeConfig.Foreground, keyed by the CLI's IPN bus session,
// and deletes it when that session ends. The CLI process is owned by Atlas
// (a Windows job object kills it with Atlas), so a crash cannot leave a
// public route to a loopback port another program may reuse later.
package tsfunnel

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/procown"
)

// CommandTimeout bounds every short CLI call.
const CommandTimeout = 15 * time.Second

// maxOutput bounds what Atlas reads from one short CLI call.
const maxOutput = 4 << 20

var (
	// ErrNotInstalled means no Tailscale CLI was found.
	ErrNotInstalled = errors.New("tailscale is not installed")
	// ErrServiceUnavailable means the CLI could not reach tailscaled.
	ErrServiceUnavailable = errors.New("the Tailscale service is not running")
	// ErrMalformed means the CLI printed JSON Atlas cannot use.
	ErrMalformed = errors.New("tailscale returned unexpected output")
	// ErrTimeout means a CLI call did not finish in time.
	ErrTimeout = errors.New("tailscale did not answer in time")
)

// Exec runs the Tailscale CLI. The real implementation runs the installed
// binary; tests substitute a fake daemon.
type Exec interface {
	// Path returns the CLI path, or ErrNotInstalled.
	Path() (string, error)
	// Output runs a short command and returns its standard output.
	Output(ctx context.Context, args ...string) ([]byte, error)
	// Start runs a long-lived command owned by Atlas. Each output line is
	// passed to onLine from another goroutine.
	Start(args []string, onLine func(string)) (Process, error)
}

// Process is a long-lived CLI command.
type Process interface {
	Identity() procown.Identity
	Done() <-chan struct{}
	Result() (int, error)
	Stop(grace time.Duration)
}

// CLI runs the installed tailscale binary.
type CLI struct {
	// Binary overrides discovery; empty finds the installed CLI.
	Binary string
	// Timeout bounds each short command; zero means CommandTimeout.
	Timeout time.Duration
}

var _ Exec = CLI{}

// Path finds tailscale on PATH, then in the default Windows location.
func (c CLI) Path() (string, error) {
	if c.Binary != "" {
		if _, err := os.Stat(c.Binary); err != nil {
			return "", ErrNotInstalled
		}
		return c.Binary, nil
	}
	if p, err := exec.LookPath("tailscale"); err == nil {
		return p, nil
	}
	if runtime.GOOS == "windows" {
		for _, env := range []string{"ProgramFiles", "ProgramW6432"} {
			if dir := os.Getenv(env); dir != "" {
				p := filepath.Join(dir, "Tailscale", "tailscale.exe")
				if _, err := os.Stat(p); err == nil {
					return p, nil
				}
			}
		}
	}
	return "", ErrNotInstalled
}

// Output runs a short command, bounded by CommandTimeout and ctx.
func (c CLI) Output(ctx context.Context, args ...string) ([]byte, error) {
	path, err := c.Path()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, orDefault(c.Timeout, CommandTimeout))
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	hideWindow(cmd)
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr limitedBuffer
	stdout.max, stderr.max = maxOutput, 16<<10
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return nil, ErrTimeout
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case stdout.overflow:
		return nil, fmt.Errorf("%w: output too large", ErrMalformed)
	case err != nil:
		return stdout.Bytes(), classify(err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// classify turns a failed CLI run into an error the dialog can explain.
// The CLI's own words are kept, shortened, as detail.
func classify(err error, stderr string) error {
	detail := Sanitize(stderr)
	lower := strings.ToLower(stderr)
	for _, hint := range []string{"failed to connect to local tailscale", "is tailscale running", "tailscaled not running", "the system cannot find the file specified", "connect: connection refused", "no such file or directory"} {
		if strings.Contains(lower, hint) {
			return fmt.Errorf("%w: %s", ErrServiceUnavailable, detail)
		}
	}
	if detail == "" {
		return err
	}
	return fmt.Errorf("%s: %w", detail, err)
}

// Start launches a long-lived command as an owned process.
func (c CLI) Start(args []string, onLine func(string)) (Process, error) {
	path, err := c.Path()
	if err != nil {
		return nil, err
	}
	r, w := io.Pipe()
	p, err := procown.Start(procown.Spec{Argv: append([]string{path}, args...), Stdout: w, Stderr: w})
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	go func() {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 4096), 64<<10)
		for sc.Scan() {
			onLine(sc.Text())
		}
		_, _ = io.Copy(io.Discard, r)
	}()
	go func() {
		<-p.Done()
		_ = w.Close()
	}()
	return ownedProcess{p}, nil
}

type ownedProcess struct{ p *procown.Proc }

func (o ownedProcess) Identity() procown.Identity { return o.p.Identity }
func (o ownedProcess) Done() <-chan struct{}      { return o.p.Done() }
func (o ownedProcess) Result() (int, error)       { return o.p.Result() }
func (o ownedProcess) Stop(grace time.Duration)   { o.p.Stop(grace) }

// limitedBuffer keeps at most max bytes and records an overflow.
type limitedBuffer struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	max      int
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := b.max - b.buf.Len(); len(p) > room {
		b.overflow = true
		b.buf.Write(p[:max(0, room)])
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.buf.Bytes())
}

func (b *limitedBuffer) String() string { return string(b.Bytes()) }

// Sanitize shortens CLI text for display and logs. It drops control
// characters and anything that looks like an auth key; Atlas never passes
// one, but the user's environment might.
func Sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	fields := strings.Fields(s)
	for i, f := range fields {
		if strings.HasPrefix(f, "tskey-") || strings.Contains(f, "--auth-key") || strings.Contains(f, "--authkey") {
			fields[i] = "[redacted]"
		}
	}
	s = strings.Join(fields, " ")
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

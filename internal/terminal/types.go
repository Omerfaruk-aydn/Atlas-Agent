// Package terminal runs real local pseudo-terminal sessions.
package terminal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/google/uuid"
)

var ErrUnavailable = errors.New("real terminal backend unavailable")

const MaxTranscriptBytes = 16 * 1024 * 1024

type processTerminal struct {
	stream io.ReadWriteCloser
	resize func(execution.TerminalSize) error
	kill   func() error
	wait   func() (int, error)
	finish func()
}

type session struct {
	process    processTerminal
	identity   execution.RunHandle
	mu         sync.Mutex
	buffer     []byte
	changed    chan struct{}
	outputDone chan struct{}
	done       chan struct{}
	stopOnce   sync.Once
	status     string
	streamErr  error
	result     execution.Result
	waitErr    error
}

func validSize(size execution.TerminalSize) bool {
	return size.Width >= 20 && size.Width <= 512 && size.Height >= 5 && size.Height <= 256
}

// Start never escapes a required execution backend through a native terminal.
// Callers must use their runner's TerminalRunner when isolation is required.
func Start(ctx context.Context, req execution.Request, size execution.TerminalSize) (execution.TerminalSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.Policy.Mode != "" && req.Policy.Mode != "legacy" || execution.HasBinding(ctx) {
		return nil, fmt.Errorf("%w: required backend has no native PTY fallback", ErrUnavailable)
	}
	if execution.IsReadOnly(ctx) {
		return nil, fmt.Errorf("read-only execution cannot start a terminal")
	}
	if req.Policy.TimeoutMS < 0 || req.Policy.TimeoutMS > 600000 {
		return nil, fmt.Errorf("terminal timeout exceeds bounds")
	}
	if !validSize(size) || !filepath.IsAbs(req.Root) || len(req.Argv) == 0 || len(req.Argv) > 128 || req.Argv[0] == "" {
		return nil, fmt.Errorf("invalid terminal size, root or argv")
	}
	info, err := os.Stat(req.Root)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("terminal root is unavailable")
	}
	bytes := 0
	for _, arg := range req.Argv {
		bytes += len(arg)
		if len(arg) > 4096 || bytes > 16*1024 || strings.ContainsRune(arg, 0) {
			return nil, fmt.Errorf("terminal argv exceeds bounds")
		}
	}
	if req.RunID == "" {
		req.RunID = uuid.NewString()
	}
	if _, err := uuid.Parse(req.RunID); err != nil {
		return nil, fmt.Errorf("terminal run identity must be a UUID")
	}
	if req.Env == nil {
		req.Env = os.Environ()
	}
	// Automated PTYs retain the ordinary Atlas tool provenance marker. They
	// must not be mistaken for direct user input by interactive CLI controls.
	req.Env = append(append([]string{}, req.Env...), "ATLAS_TOOL_CALL_ID="+req.RunID, "AI_AGENT=atlas-terminal")
	process, err := startProcess(req, size)
	if err != nil {
		return nil, err
	}
	s := &session{process: process, identity: execution.RunHandle{RunID: req.RunID, Backend: "pty-" + runtime.GOOS}, changed: make(chan struct{}), outputDone: make(chan struct{}), done: make(chan struct{})}
	go s.collect()
	go s.observe()
	timeout := 10 * time.Minute
	if req.Policy.TimeoutMS > 0 {
		timeout = time.Duration(req.Policy.TimeoutMS) * time.Millisecond
	}
	lifetime, cancel := context.WithTimeout(ctx, timeout)
	go func() {
		defer cancel()
		select {
		case <-lifetime.Done():
			_ = s.Close()
		case <-s.done:
		}
	}()
	return s, nil
}

func (s *session) signal() { close(s.changed); s.changed = make(chan struct{}) }

func (s *session) collect() {
	defer close(s.outputDone)
	hash := sha256.New()
	buffer := make([]byte, 32*1024)
	total := 0
	for {
		n, err := s.process.stream.Read(buffer)
		s.mu.Lock()
		remaining := MaxTranscriptBytes - total
		accepted := min(n, remaining)
		if accepted > 0 {
			hash.Write(buffer[:accepted])
			s.buffer = append(s.buffer, buffer[:accepted]...)
			total += accepted
		}
		overflow := n > remaining
		if overflow {
			s.status = "output_limit"
			s.streamErr = fmt.Errorf("terminal transcript exceeds 16 MiB")
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && s.streamErr == nil {
				s.streamErr = err
			}
			s.result.OutputHash = hex.EncodeToString(hash.Sum(nil))
			s.signal()
			s.mu.Unlock()
			return
		}
		s.signal()
		s.mu.Unlock()
		if overflow {
			_ = s.stop("output_limit")
		}
	}
}

func (s *session) observe() {
	code, err := s.process.wait()
	s.process.finish()
	<-s.outputDone
	_ = s.process.stream.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.status
	if status == "" {
		status = "succeeded"
		if code != 0 || err != nil || s.streamErr != nil {
			status = "failed"
		}
	}
	s.result.RunID, s.result.Backend = s.identity.RunID, s.identity.Backend
	s.result.HostOS, s.result.ExecutionOS = runtime.GOOS, runtime.GOOS
	s.result.Status, s.result.Done, s.result.ExitCode = status, true, &code
	s.waitErr = err
	if status != "cancelled" {
		s.waitErr = errors.Join(err, s.streamErr)
	}
	close(s.done)
	s.signal()
}

func (s *session) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	for {
		s.mu.Lock()
		if len(s.buffer) > 0 {
			n := copy(buffer, s.buffer)
			s.buffer = s.buffer[n:]
			if len(s.buffer) == 0 {
				s.buffer = nil
			}
			s.mu.Unlock()
			return n, nil
		}
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-s.outputDone:
			s.mu.Lock()
			empty := len(s.buffer) == 0
			s.mu.Unlock()
			if empty {
				return 0, io.EOF
			}
		case <-changed:
		}
	}
}

func (s *session) Write(data []byte) (int, error) {
	if len(data) > 4096 {
		return 0, fmt.Errorf("terminal input exceeds 4096 bytes")
	}
	select {
	case <-s.done:
		return 0, io.ErrClosedPipe
	default:
	}
	return s.process.stream.Write(data)
}

func (s *session) Resize(ctx context.Context, size execution.TerminalSize) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validSize(size) {
		return fmt.Errorf("invalid terminal size")
	}
	select {
	case <-s.done:
		return io.ErrClosedPipe
	default:
	}
	return s.process.resize(size)
}

func (s *session) stop(status string) error {
	var err error
	s.stopOnce.Do(func() {
		s.mu.Lock()
		select {
		case <-s.done:
			s.mu.Unlock()
			return
		default:
		}
		s.status = status
		s.mu.Unlock()
		err = s.process.kill()
	})
	return err
}

func (s *session) Close() error                  { return s.stop("cancelled") }
func (s *session) Identity() execution.RunHandle { return s.identity }
func (s *session) Wait(ctx context.Context) (execution.Result, error) {
	select {
	case <-ctx.Done():
		return execution.Result{RunID: s.identity.RunID, Backend: s.identity.Backend, Status: "running"}, ctx.Err()
	case <-s.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.result, s.waitErr
	}
}

// Cleanup waits only for this session's owned process identity.
func Cleanup(s execution.TerminalSession) error {
	if err := s.Close(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.Wait(ctx)
	return err
}

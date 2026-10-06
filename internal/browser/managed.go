package browser

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"time"
)

type retiredSession struct {
	driver Session
}

type pendingSession struct {
	done chan struct{}
	err  error
}

// SessionContext creates one session per chat. Concurrent callers share the
// same launch; cancellation cannot leave an untracked acknowledged session.
func (m *Manager) SessionContext(ctx context.Context, id string) (Session, error) {
	return m.sessionContext(ctx, id, "")
}

// SessionContextURL carries the first authorized URL into connection setup.
// Existing sessions are returned unchanged and navigate through the tool.
func (m *Manager) SessionContextURL(ctx context.Context, id, initialURL string) (Session, error) {
	parsed, err := url.Parse(initialURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return nil, errors.New("initial browser URL must be an HTTP(S) destination without embedded credentials")
	}
	return m.sessionContext(ctx, id, initialURL)
}

func (m *Manager) sessionContext(ctx context.Context, id, initialURL string) (Session, error) {
	if id == "" {
		return nil, errors.New("browser session ID is required")
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		m.mu.Lock()
		retired := m.reapLocked()
		if len(retired) > 0 {
			m.mu.Unlock()
			m.releaseSessions(retired)
			continue
		}
		if waiting := m.pending[id]; waiting != nil {
			m.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-waiting.done:
				if waiting.err != nil {
					return nil, waiting.err
				}
				continue
			}
		}
		if s := m.sessions[id]; s != nil {
			m.lastUsed[id] = time.Now()
			m.mu.Unlock()
			return s, nil
		}
		opts, generation, epoch := m.opts, m.generation, m.epochs[id]
		opts.initialURL = initialURL
		waiting := &pendingSession{done: make(chan struct{})}
		m.pending[id] = waiting
		m.mu.Unlock()
		s, err := m.launch(ctx, id, opts)
		m.mu.Lock()
		if err == nil && (ctx.Err() != nil || generation != m.generation || epoch != m.epochs[id]) {
			err = ctx.Err()
			if err == nil {
				err = errors.New("browser configuration or session changed while connecting; retry with a fresh observation")
			}
		}
		if err == nil {
			m.sessions[id] = s
			m.lastUsed[id] = time.Now()
		}
		delete(m.pending, id)
		waiting.err = err
		close(waiting.done)
		m.mu.Unlock()
		if err != nil {
			m.releaseSessions([]retiredSession{{driver: s}})
			return nil, err
		}
		return s, nil
	}
}

func (m *Manager) launch(ctx context.Context, id string, opts Options) (Session, error) {
	timeout := opts.ActionTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if opts.UseRealProfile && opts.UserDataDir == "" {
		opts.UserDataDir = defaultProfileDir()
	}
	if opts.IsolateProfiles && opts.RemoteURL == "" && opts.UserDataDir != "" {
		hash := sha256.Sum256([]byte(id))
		dest := filepath.Join(opts.UserDataDir, "atlas-sessions", fmt.Sprintf("%x", hash[:16]))
		if !opts.UseRealProfile {
			if err := seedSessionProfile(opts.UserDataDir, dest); err != nil {
				return nil, err
			}
		}
		opts.UserDataDir = dest
	}
	return m.connect(ctx, opts)
}

func (m *Manager) dropLocked(id string) retiredSession {
	retired := retiredSession{driver: m.sessions[id]}
	delete(m.sessions, id)
	delete(m.lastUsed, id)
	return retired
}

func (m *Manager) releaseSessions(sessions []retiredSession) {
	for _, retired := range sessions {
		if retired.driver != nil {
			retired.driver.Close()
		}
	}
}

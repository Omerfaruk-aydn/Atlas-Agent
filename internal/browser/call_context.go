package browser

import (
	"context"
	"errors"
)

// ErrBrowserDisconnected reports that the browser behind a session went
// away, for example because its window was closed. The manager retires such
// a session, so the next browser action starts a fresh one.
var ErrBrowserDisconnected = errors.New("browser was closed or disconnected; the next browser action starts a fresh browser session")

// disconnected reports whether the browser or the tab this session drives is
// gone. chromedp cancels these contexts when Chrome exits or the target is
// destroyed, after which no action on the session can succeed again.
//
// A selected secondary tab that closed (a popup closing itself) leaves the
// browser usable: that is tab_closed, recovered by selecting another tab,
// never a reason to discard the browser and its other tabs.
func (s *chromedpSession) disconnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rootCtx != nil && s.rootCtx.Err() != nil {
		return true
	}
	return s.ctx != nil && s.ctx.Err() != nil && (s.rootCtx == nil || s.ctx == s.rootCtx)
}

// ErrTabClosed reports that the selected tab is gone while the browser is
// still connected.
var ErrTabClosed = errors.New("tab_closed: the controlled tab no longer exists; list tabs, then tab_select or tab_return. The browser is still running")

func (s *chromedpSession) tabClosed() bool {
	s.mu.Lock()
	rootAlive := s.rootCtx != nil && s.rootCtx.Err() == nil
	s.mu.Unlock()
	return rootAlive && s.currentTabGone()
}

// explainFailure replaces an error caused by a lost browser with
// ErrBrowserDisconnected. A cancellation requested by the caller is kept
// as is, so stopping a turn is never reported as a browser failure.
func (s *chromedpSession) explainFailure(err error) error {
	s.mu.Lock()
	callerCanceled := s.callContext != nil && s.callContext.Err() != nil
	s.mu.Unlock()
	if !callerCanceled && s.disconnected() {
		return ErrBrowserDisconnected
	}
	if !callerCanceled && s.tabClosed() {
		return ErrTabClosed
	}
	return err
}

func (s *chromedpSession) currentContext() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ctx
}

func (s *chromedpSession) operationContext() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.callContext != nil {
		return s.callContext
	}
	return s.ctx
}

// BindContext makes legacy actions respect their caller's cancellation.
// Callers hold the browser ownership lease until the returned restore runs.
func (s *chromedpSession) BindContext(ctx context.Context) func() {
	s.mu.Lock()
	previous := s.callContext
	s.callContext = ctx
	s.mu.Unlock()
	return func() { s.mu.Lock(); s.callContext = previous; s.mu.Unlock() }
}

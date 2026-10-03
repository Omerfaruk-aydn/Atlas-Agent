package browser

import "context"

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

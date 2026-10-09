package browser

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// inputDispatchFailure preserves the dispatch error and cleanup outcome.
type inputDispatchFailure struct {
	cause            error
	cleanupErr       error
	cleanupAttempted bool
	pending          []inputEvent
}

func (e *inputDispatchFailure) Error() string {
	return errors.Join(e.cause, e.cleanupErr).Error()
}

func (e *inputDispatchFailure) Unwrap() []error {
	if e.cleanupErr == nil {
		return []error{e.cause}
	}
	return []error{e.cause, e.cleanupErr}
}

// releaseInput attempts every release even when one fails. Only failed
// releases remain pending; successful key-ups must not be replayed.
func releaseInput(ctx context.Context, send inputSender, releases []inputEvent) ([]inputEvent, error) {
	var pending []inputEvent
	var failures []error
	for _, release := range releases {
		if err := send(ctx, release.method, release.params); err != nil {
			pending = append(pending, release)
			failures = append(failures, err)
		}
	}
	if len(failures) > 0 {
		return pending, fmt.Errorf("%w: %w", ErrInputCleanup, errors.Join(failures...))
	}
	return nil, nil
}

// inputRecovery retains the original target context and sender so changing
// tabs cannot redirect cleanup to an unrelated page.
type inputRecovery struct {
	ctx      context.Context
	send     inputSender
	releases []inputEvent
}

func (s *chromedpSession) recoverInputLocked(parent context.Context) error {
	if s.inputRecovery == nil {
		return nil
	}
	if err := parent.Err(); err != nil {
		return fmt.Errorf("%w: recovery interrupted: %w", ErrInputCleanup, err)
	}
	r := s.inputRecovery
	ctx, cancel := context.WithTimeout(r.ctx, 2*time.Second)
	defer cancel()
	stop := context.AfterFunc(parent, cancel)
	defer stop()
	pending, err := releaseInput(ctx, r.send, r.releases)
	if err != nil {
		r.releases = pending
		return err
	}
	s.inputRecovery = nil
	return nil
}

// recoverInput protects legacy and semantic input from an unfinished
// typed gesture. Observations remain available while recovery is pending.
func (s *chromedpSession) recoverInput(parent context.Context) error {
	s.inputMu.Lock()
	defer s.inputMu.Unlock()
	return s.recoverInputLocked(parent)
}

func (s *chromedpSession) dispatchSessionInput(ctx context.Context, send inputSender, events []inputEvent) (int, error) {
	s.inputMu.Lock()
	defer s.inputMu.Unlock()
	if err := s.recoverInputLocked(ctx); err != nil {
		return 0, err
	}
	n, err := dispatchInput(ctx, send, events)
	var failure *inputDispatchFailure
	if errors.As(err, &failure) && len(failure.pending) > 0 {
		s.inputRecovery = &inputRecovery{ctx: context.WithoutCancel(ctx), send: send, releases: failure.pending}
	}
	return n, err
}

func inputFailureResult(result InputResult, err error) InputResult {
	result.CleanupStatus = "not_needed"
	var failure *inputDispatchFailure
	if errors.Is(err, ErrInputCleanup) {
		result.CleanupStatus = "pending"
		result.Next = "Held input release could not be confirmed. Further input is blocked until the pending releases succeed on their original tab. Observe the page; do not replay the partial gesture. If the target is unavailable, close and restart the browser session."
		return result
	}
	if errors.As(err, &failure) && failure.cleanupAttempted {
		result.CleanupStatus = "released"
		result.Next = "Input was interrupted; held input was released. Observe the page before retrying so a partial gesture is not repeated."
		return result
	}
	result.Next = "Input was interrupted before any held input needed cleanup. Observe the page before retrying."
	return result
}

package browser

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDispatchInputReportsFailedCleanupAndPreservesCauses(t *testing.T) {
	t.Parallel()
	dispatchErr := errors.New("drag interrupted")
	cleanupErr := errors.New("release transport failed")
	events, err := planInput(Request{Action: InputPointerDrag, ViewportID: "vp-1", X: 10, Y: 10, ToX: 30, ToY: 30, Steps: 2}, testViewport)
	require.NoError(t, err)
	var calls int
	n, err := dispatchInput(t.Context(), func(ctx context.Context, method string, params map[string]any) error {
		calls++
		if params["type"] == "mouseReleased" {
			return cleanupErr
		}
		if calls == 3 {
			return dispatchErr
		}
		return nil
	}, events)
	require.Equal(t, 2, n)
	require.ErrorIs(t, err, dispatchErr)
	require.ErrorIs(t, err, cleanupErr)
	require.ErrorIs(t, err, ErrInputCleanup)
	var failure *inputDispatchFailure
	require.ErrorAs(t, err, &failure)
	require.Len(t, failure.pending, 1)
	result := inputFailureResult(InputResult{}, err)
	require.Equal(t, "pending", result.CleanupStatus)
	require.NotContains(t, result.Next, "was released")
}

func TestDispatchInputCleanupContinuesAfterKeyReleaseFailure(t *testing.T) {
	t.Parallel()
	events, err := planInput(Request{Action: InputKeyCombo, Key: "a", Modifiers: []string{"ctrl", "shift"}}, Viewport{})
	require.NoError(t, err)
	var releases []string
	n, err := dispatchInput(t.Context(), func(ctx context.Context, method string, params map[string]any) error {
		if params["type"] == "keyUp" {
			code := params["code"].(string)
			releases = append(releases, code)
			if code == "KeyA" {
				return errors.New("key release failed")
			}
		}
		return nil
	}, events)
	require.Equal(t, 3, n)
	require.ErrorIs(t, err, ErrInputCleanup)
	require.Equal(t, []string{"KeyA", "KeyA", "ControlLeft", "ShiftLeft"}, releases)
	var failure *inputDispatchFailure
	require.ErrorAs(t, err, &failure)
	require.Len(t, failure.pending, 1, "successful modifier releases must not remain pending")
	require.Equal(t, "KeyA", failure.pending[0].params["code"])
}

func TestDispatchInputCanceledCleanupFailurePreservesCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	events, err := planInput(Request{Action: InputKeyCombo, Key: "a", Modifiers: []string{"ctrl"}}, Viewport{})
	require.NoError(t, err)
	_, err = dispatchInput(ctx, func(cleanup context.Context, method string, params map[string]any) error {
		if params["type"] == "rawKeyDown" {
			cancel()
		}
		if params["type"] == "keyUp" {
			require.NoError(t, cleanup.Err(), "cleanup must survive the caller's cancellation")
			return errors.New("release failed")
		}
		return nil
	}, events)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, ErrInputCleanup)
}

func TestInputRecoveryBlocksNewInputAndRetriesOnlyOriginalReleases(t *testing.T) {
	t.Parallel()
	type targetKey struct{}
	original := context.WithValue(t.Context(), targetKey{}, "original-tab")
	next := context.WithValue(t.Context(), targetKey{}, "next-tab")
	s := &chromedpSession{}
	events, err := planInput(Request{Action: InputPointerDrag, ViewportID: "vp-1", X: 10, Y: 10, ToX: 30, ToY: 30, Steps: 2}, testViewport)
	require.NoError(t, err)
	allowRelease := false
	var originalCalls int
	var releaseAttempts int
	originalSend := func(ctx context.Context, method string, params map[string]any) error {
		require.Equal(t, "original-tab", ctx.Value(targetKey{}), "recovery must stay on the original target")
		originalCalls++
		if params["type"] == "mouseReleased" {
			releaseAttempts++
			if !allowRelease {
				return errors.New("release unavailable")
			}
		} else if originalCalls == 3 {
			return errors.New("drag interrupted")
		}
		return nil
	}
	_, err = s.dispatchSessionInput(original, originalSend, events)
	require.ErrorIs(t, err, ErrInputCleanup)
	require.NotNil(t, s.inputRecovery)
	text, err := planInput(Request{Action: InputInsertText, Text: "new action"}, Viewport{})
	require.NoError(t, err)
	var newCalls int
	newSend := func(ctx context.Context, method string, params map[string]any) error {
		newCalls++
		require.Equal(t, cdpInsertText, method)
		return nil
	}
	n, err := s.dispatchSessionInput(next, newSend, text)
	require.ErrorIs(t, err, ErrInputCleanup)
	require.Zero(t, n)
	require.Zero(t, newCalls, "no new input before confirmed release")
	allowRelease = true
	n, err = s.dispatchSessionInput(next, newSend, text)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, 1, newCalls)
	require.Equal(t, 3, releaseAttempts)
	require.Nil(t, s.inputRecovery)
	require.Equal(t, 6, originalCalls, "only release commands may be retried, never the drag")
}

func TestInputCleanupStatusOnlyClaimsConfirmedRelease(t *testing.T) {
	t.Parallel()
	events, err := planInput(Request{Action: InputKeyCombo, Key: "a", Modifiers: []string{"ctrl"}}, Viewport{})
	require.NoError(t, err)
	_, send := recordingSender(2, errors.New("input interrupted"), 0, nil)
	_, err = dispatchInput(t.Context(), send, events)
	require.Error(t, err)
	require.Equal(t, "released", inputFailureResult(InputResult{}, err).CleanupStatus)
	_, err = dispatchInput(t.Context(), func(context.Context, string, map[string]any) error {
		return errors.New("text input failed")
	}, []inputEvent{{method: cdpInsertText, params: map[string]any{"text": "hello"}}})
	require.Error(t, err)
	result := inputFailureResult(InputResult{}, err)
	require.Equal(t, "not_needed", result.CleanupStatus)
	require.NotContains(t, result.Next, "was released")
}

func TestReleaseHeldPointerUsesActualButtonAndRetainsFailures(t *testing.T) {
	t.Parallel()
	for _, button := range []string{"left", "right", "middle"} {
		t.Run(button, func(t *testing.T) {
			t.Parallel()
			s := &chromedpSession{pointerHeld: true, pointerButton: button, pointerX: 20, pointerY: 30}
			fail := true
			calls := 0
			send := func(ctx context.Context, method string, params map[string]any) error {
				calls++
				require.Equal(t, "mouseReleased", params["type"])
				require.Equal(t, button, params["button"])
				if fail {
					return errors.New("release failed")
				}
				s.presentMouseEvent(nil, 0, params)
				return nil
			}
			require.ErrorIs(t, s.releaseHeldPointer(t.Context(), send), ErrInputCleanup)
			require.NotNil(t, s.inputRecovery)
			fail = false
			require.NoError(t, s.releaseHeldPointer(t.Context(), send))
			require.Nil(t, s.inputRecovery)
			require.Equal(t, 2, calls)
			require.False(t, s.pointerHeld)
		})
	}
}

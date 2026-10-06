package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/stretchr/testify/require"
)

type changingDesktopBackend struct {
	efficientDesktopBackend
	watch func(context.Context, string) (<-chan struct{}, func(), error)
}

func (b *changingDesktopBackend) WatchChanges(ctx context.Context, id string) (<-chan struct{}, func(), error) {
	return b.watch(ctx, id)
}

func TestDesktopAssertionSubscribesBeforeReadAndVerifiesEvent(t *testing.T) {
	events := make(chan struct{}, 1)
	subscribed, released, calls := false, false, 0
	b := &changingDesktopBackend{}
	b.watch = func(context.Context, string) (<-chan struct{}, func(), error) {
		subscribed = true
		return events, func() { released = true }, nil
	}
	b.call = func(context.Context, computer.AutomationRequest) (json.RawMessage, error) {
		require.True(t, subscribed)
		calls++
		if calls == 1 {
			events <- struct{}{}
			return json.RawMessage(`{"passed":false,"actual":"loading"}`), nil
		}
		return json.RawMessage(`{"passed":true,"actual":"ready"}`), nil
	}
	s := &computerToolState{backend: b}
	started := time.Now()
	r, err := s.runAutomation(t.Context(), "assert", ComputerParams{Automation: computer.AutomationRequest{WindowID: "11", WaitMS: 500}})
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Equal(t, 2, calls, "An event must cause an actual native read")
	require.True(t, released)
	require.Less(t, time.Since(started), 200*time.Millisecond)
}

func TestDesktopAssertionEventFailureFallsBackAndCancelsSubscription(t *testing.T) {
	for _, supported := range []bool{false, true} {
		t.Run(map[bool]string{true: "deadline", false: "fallback"}[supported], func(t *testing.T) {
			b := &changingDesktopBackend{}
			released, calls := false, 0
			b.watch = func(context.Context, string) (<-chan struct{}, func(), error) {
				cleanup := func() { released = true }
				if !supported {
					return nil, cleanup, errors.New("provider lacks event support")
				}
				return make(chan struct{}), cleanup, nil
			}
			b.call = func(context.Context, computer.AutomationRequest) (json.RawMessage, error) {
				calls++
				if !supported && calls == 2 {
					return json.RawMessage(`{"passed":true}`), nil
				}
				return json.RawMessage(`{"passed":false}`), nil
			}
			wait := 500
			if supported {
				wait = 20
			}
			s := &computerToolState{backend: b}
			r, err := s.runAutomation(t.Context(), "assert", ComputerParams{Automation: computer.AutomationRequest{WindowID: "11", WaitMS: wait}})
			require.NoError(t, err)
			require.Equal(t, supported, r.IsError)
			require.True(t, released)
			require.Equal(t, map[bool]int{true: 1, false: 2}[supported], calls)
		})
	}
}

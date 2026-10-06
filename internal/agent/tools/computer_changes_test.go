package tools

import (
	"context"
	"encoding/json"

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

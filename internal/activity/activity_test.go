package activity

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type testRenderer struct {
	mu     sync.Mutex
	events []Event
}

func (r *testRenderer) Render(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}
func (r *testRenderer) Close() {}
func (r *testRenderer) last() Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) == 0 {
		return Event{}
	}
	return r.events[len(r.events)-1]
}

func TestUnusedManagerDoesNotStartBackgroundLoop(t *testing.T) {
	m := New(&testRenderer{})
	require.False(t, m.started)
	m.Close()
	require.False(t, m.Snapshot().Visible)
	select {
	case <-m.done:
	default:
		t.Fatal("Closing an unused manager must complete without a worker")
	}
}

func TestLifecycleIgnoresStaleFinishAndSuspends(t *testing.T) {
	r := &testRenderer{}
	m := New(r)
	defer m.Close()
	first := m.Begin(context.Background(), Event{Session: "one", Action: "click"})
	second := m.Begin(context.Background(), Event{Session: "two", Action: "type"})
	require.Eventually(t, func() bool { return r.last().Session == "two" }, time.Second, time.Millisecond)
	first()
	require.Equal(t, "two", m.Snapshot().Session)
	restore := m.Suspend()
	require.False(t, m.Snapshot().Visible)
	restore()
	require.True(t, m.Snapshot().Visible)
	second()
	require.False(t, m.Snapshot().Visible)
}

func TestCancellationAndPauseClearOnlyTheirSession(t *testing.T) {
	r := &testRenderer{}
	m := New(r)
	defer m.Close()
	ctx, cancel := context.WithCancel(context.Background())
	m.Begin(ctx, Event{Session: "one", Action: "click"})
	m.Clear("other")
	require.True(t, m.Snapshot().Visible)
	cancel()
	require.Eventually(t, func() bool { return !m.Snapshot().Visible }, time.Second, time.Millisecond)
	m.Begin(context.Background(), Event{Session: "two", Action: "type"})
	m.Clear("two")
	require.False(t, m.Snapshot().Visible)
	m.Close()
	m.Begin(context.Background(), Event{Session: "three"})
	require.False(t, m.Snapshot().Visible)
}

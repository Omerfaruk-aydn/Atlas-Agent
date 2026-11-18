package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Unconfigured means unlimited, and a nil limiter has to be usable so the
// call site needs no branch.
func TestNilLimiterIsUnlimited(t *testing.T) {
	var l *concurrencyLimiter
	require.Nil(t, newConcurrencyLimiter(0))
	require.Nil(t, newConcurrencyLimiter(-1))

	require.NoError(t, l.acquire(t.Context()))
	l.release()
}

func TestLimiterBoundsConcurrentHolders(t *testing.T) {
	const limit = 2
	l := newConcurrencyLimiter(limit)

	var running, peak atomic.Int64
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			require.NoError(t, l.acquire(t.Context()))
			defer l.release()

			now := running.Add(1)
			for {
				was := peak.Load()
				if now <= was || peak.CompareAndSwap(was, now) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			running.Add(-1)
		}()
	}
	wg.Wait()

	require.LessOrEqual(t, peak.Load(), int64(limit))
	require.Positive(t, peak.Load())
}

// A cancelled caller must not sit in the queue waiting for work nobody wants
// any more.
func TestLimiterAcquireHonoursCancellation(t *testing.T) {
	l := newConcurrencyLimiter(1)
	require.NoError(t, l.acquire(t.Context()))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, l.acquire(ctx), context.Canceled)

	// The held slot is still held: cancelling a waiter frees nothing.
	l.release()
	require.NoError(t, l.acquire(t.Context()))
}

// Releasing without holding a slot happens if acquire failed and a defer
// fires anyway; it must not block.
func TestLimiterReleaseWithoutAcquireDoesNotBlock(t *testing.T) {
	l := newConcurrencyLimiter(1)

	done := make(chan struct{})
	go func() {
		l.release()
		l.release()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("release blocked")
	}

	// The limiter still admits exactly its configured number.
	require.NoError(t, l.acquire(t.Context()))
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	require.Error(t, l.acquire(ctx))
}

// The agent, orchestrate, delegate and debate tools used to build one limiter
// each, so the effective cap was four times the setting. They now take the
// coordinator's single limiter.
func TestSharedSubagentLimiterIsOnePool(t *testing.T) {
	c := &coordinator{}
	a := c.sharedSubagentLimiter(2)
	b := c.sharedSubagentLimiter(2)
	require.Same(t, a, b, "same capacity must return the same pool")

	var running, peak atomic.Int64
	var wg sync.WaitGroup
	for i := range 12 {
		// Alternate handles to stand in for different tools.
		l := a
		if i%2 == 1 {
			l = b
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			require.NoError(t, l.acquire(t.Context()))
			defer l.release()
			n := running.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			running.Add(-1)
		}()
	}
	wg.Wait()
	require.LessOrEqual(t, peak.Load(), int64(2))
}

func TestSharedSubagentLimiterFollowsConfig(t *testing.T) {
	c := &coordinator{}
	first := c.sharedSubagentLimiter(2)
	require.NotNil(t, first)
	second := c.sharedSubagentLimiter(5)
	require.NotSame(t, first, second, "a new capacity builds a new pool")
	require.Nil(t, c.sharedSubagentLimiter(0), "zero means unlimited")
}

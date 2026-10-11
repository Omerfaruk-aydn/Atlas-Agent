package support

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHostCloseWaitsForBannerStopCallback(t *testing.T) {
	t.Parallel()
	var block atomic.Bool
	var releaseOnce sync.Once
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	overlay := &fakeOverlay{}
	h := startHost(t, func(opts *HostOptions) {
		opts.Overlay = overlay
		opts.OnChange = func() {
			if block.CompareAndSwap(true, false) {
				close(entered)
				<-release
			}
		}
	})
	c := startController(t, nil)
	connect(t, h, c, Presets["diagnose"])
	overlay.mu.Lock()
	stop := overlay.stop
	overlay.mu.Unlock()
	block.Store(true)
	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Banner stop did not enter its completion callback")
	}
	closed := make(chan struct{})
	go func() { h.Close(); close(closed) }()
	select {
	case <-closed:
		t.Error("Close returned while the banner stop callback was still running")
	case <-time.After(30 * time.Millisecond):
	}
	unblock()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not finish after the banner callback returned")
	}
	<-stopped
}

func TestHostCloseWaitsForStartedTimerCallback(t *testing.T) {
	t.Parallel()
	h := startHost(t, nil)
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	h.afterFunc(time.Millisecond, func() {
		close(started)
		<-release
		require.NoError(t, h.history.Append(Event{SessionID: "ss-timer-test", Kind: "timer_done"}))
		close(finished)
	})
	<-started
	closed := make(chan struct{})
	go func() { h.Close(); close(closed) }()
	select {
	case <-closed:
		t.Error("Close returned while a timer callback could still write state")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not finish")
	}
	<-finished
	called := make(chan struct{}, 1)
	h.afterFunc(time.Millisecond, func() { called <- struct{}{} })
	select {
	case <-called:
		t.Fatal("a timer ran after Close")
	case <-time.After(30 * time.Millisecond):
	}
}

func TestClosedSupportEndpointsCannotRestart(t *testing.T) {
	t.Parallel()
	h := startHost(t, nil)
	h.Close()
	_, err := h.Offer(OfferOptions{Capabilities: []Capability{CapSystemInfo}, LAN: true})
	require.Equal(t, CodeSessionRevoked, CodeOf(err))
	c := startController(t, nil)
	c.Close()
	_, err = c.Join(t.Context(), "ABCD-EFGH-JKMN@127.0.0.1:12345", nil)
	require.Error(t, err)
}

func TestControllerCloseCancelsPendingJoin(t *testing.T) {
	t.Parallel()
	h := startHost(t, nil)
	info := offerLAN(t, h, []Capability{CapSystemInfo})
	c := startController(t, nil)
	done := startJoin(c, info.Code+"@"+h.LANAddress())
	waitPending(t, h)
	closed := make(chan struct{})
	go func() { c.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not cancel an unaccepted connection")
	}
	require.Error(t, (<-done).err)
}

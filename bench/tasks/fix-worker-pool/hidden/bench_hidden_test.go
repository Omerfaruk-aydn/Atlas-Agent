package pool

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// hiddenWithin fails the test instead of hanging when f does not return.
func hiddenWithin(t *testing.T, what string, d time.Duration, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not finish within %v", what, d)
	}
}

func TestHiddenSubmitAfterCloseReturnsErrClosed(t *testing.T) {
	p := New(2)
	p.Close()
	var ran atomic.Bool
	err := p.Submit(func() { ran.Store(true) })
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("Submit after Close = %v, want ErrClosed", err)
	}
	time.Sleep(20 * time.Millisecond)
	if ran.Load() {
		t.Fatal("a task submitted after Close ran")
	}
}

func TestHiddenCloseIsIdempotentAndConcurrentSafe(t *testing.T) {
	p := New(3)
	hiddenWithin(t, "concurrent Close", 3*time.Second, func() {
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				p.Close()
			}()
		}
		wg.Wait()
		p.Close()
	})
}

func TestHiddenSubmitNeverBlocks(t *testing.T) {
	p := New(1)
	gate := make(chan struct{})
	started := make(chan struct{})
	if err := p.Submit(func() { close(started); <-gate }); err != nil {
		t.Fatal(err)
	}
	<-started // the only worker is now busy
	var ran atomic.Int64
	hiddenWithin(t, "submitting 1000 tasks to a busy pool", 3*time.Second, func() {
		for range 1000 {
			if err := p.Submit(func() { ran.Add(1) }); err != nil {
				t.Error(err)
			}
		}
	})
	close(gate)
	hiddenWithin(t, "Close", 3*time.Second, p.Close)
	if ran.Load() != 1000 {
		t.Fatalf("ran %d queued tasks, want 1000", ran.Load())
	}
}

func TestHiddenWaitCoversTasksSubmittedByTasks(t *testing.T) {
	p := New(2)
	defer p.Close()
	var ran atomic.Int64
	var spawn func(depth int)
	spawn = func(depth int) {
		ran.Add(1)
		if depth == 0 {
			return
		}
		for range 3 {
			d := depth - 1
			if err := p.Submit(func() { spawn(d) }); err != nil {
				t.Error(err)
			}
		}
	}
	if err := p.Submit(func() { spawn(3) }); err != nil {
		t.Fatal(err)
	}
	hiddenWithin(t, "Wait", 5*time.Second, p.Wait)
	// 1 + 3 + 9 + 27 tasks
	if got := ran.Load(); got != 40 {
		t.Fatalf("ran %d tasks after Wait, want 40", got)
	}
}

func TestHiddenWaitDoesNotReturnEarly(t *testing.T) {
	p := New(4)
	defer p.Close()
	var done atomic.Int64
	for range 20 {
		if err := p.Submit(func() {
			time.Sleep(15 * time.Millisecond)
			done.Add(1)
		}); err != nil {
			t.Fatal(err)
		}
	}
	p.Wait()
	if got := done.Load(); got != 20 {
		t.Fatalf("%d tasks finished when Wait returned, want 20", got)
	}
}

func TestHiddenCloseWaitsForRunningTasks(t *testing.T) {
	p := New(1)
	started := make(chan struct{})
	var finished atomic.Bool
	if err := p.Submit(func() {
		close(started)
		time.Sleep(80 * time.Millisecond)
		finished.Store(true)
	}); err != nil {
		t.Fatal(err)
	}
	<-started
	p.Close()
	if !finished.Load() {
		t.Fatal("Close returned while a task was still running")
	}
}

func TestHiddenCloseDrainsAcceptedTasks(t *testing.T) {
	p := New(1)
	gate := make(chan struct{})
	var ran atomic.Int64
	if err := p.Submit(func() { <-gate }); err != nil {
		t.Fatal(err)
	}
	for range 50 {
		if err := p.Submit(func() { ran.Add(1) }); err != nil {
			t.Fatal(err)
		}
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		close(gate)
	}()
	hiddenWithin(t, "Close", 3*time.Second, p.Close)
	if got := ran.Load(); got != 50 {
		t.Fatalf("%d of 50 accepted tasks ran before Close returned", got)
	}
}

func TestHiddenPanicIsRecovered(t *testing.T) {
	p := New(1)
	var ran atomic.Int64
	if err := p.Submit(func() { panic("boom") }); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if err := p.Submit(func() { ran.Add(1) }); err != nil {
			t.Fatal(err)
		}
	}
	hiddenWithin(t, "Wait after a panic", 3*time.Second, p.Wait)
	p.Close()
	if ran.Load() != 5 {
		t.Fatalf("ran %d tasks after the panic, want 5", ran.Load())
	}
	if p.Panics() != 1 {
		t.Fatalf("Panics = %d, want 1", p.Panics())
	}
}

func TestHiddenWorkerCount(t *testing.T) {
	// Three tasks that only finish together prove three run at once.
	p := New(3)
	var arrived sync.WaitGroup
	arrived.Add(3)
	release := make(chan struct{})
	var passed atomic.Int64
	for range 3 {
		if err := p.Submit(func() {
			arrived.Done()
			<-release
			passed.Add(1)
		}); err != nil {
			t.Fatal(err)
		}
	}
	hiddenWithin(t, "three tasks to run at the same time", 3*time.Second, arrived.Wait)
	close(release)
	p.Close()
	if passed.Load() != 3 {
		t.Fatalf("passed = %d", passed.Load())
	}

	// A count below one still gets a worker.
	z := New(0)
	var ran atomic.Bool
	if err := z.Submit(func() { ran.Store(true) }); err != nil {
		t.Fatal(err)
	}
	hiddenWithin(t, "Wait on New(0)", 3*time.Second, z.Wait)
	z.Close()
	if !ran.Load() {
		t.Fatal("New(0) never ran the task")
	}
}

func TestHiddenSubmitRacingClose(t *testing.T) {
	for range 50 {
		p := New(2)
		var accepted, ran atomic.Int64
		var wg sync.WaitGroup
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range 50 {
					err := p.Submit(func() { ran.Add(1) })
					switch {
					case err == nil:
						accepted.Add(1)
					case errors.Is(err, ErrClosed):
					default:
						t.Errorf("Submit = %v", err)
					}
				}
			}()
		}
		time.Sleep(time.Millisecond)
		p.Close()
		wg.Wait()
		p.Close()
		if accepted.Load() != ran.Load() {
			t.Fatalf("accepted %d tasks but %d ran", accepted.Load(), ran.Load())
		}
	}
}

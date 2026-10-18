// Package pool runs submitted functions on a fixed number of workers.
package pool

import (
	"errors"
	"sync"
)

// ErrClosed is returned by Submit once the pool has been closed.
var ErrClosed = errors.New("pool: closed")

// Pool runs tasks on a fixed set of worker goroutines.
type Pool struct {
	mu      sync.Mutex
	work    *sync.Cond // signaled when the queue gains a task or the pool closes
	idle    *sync.Cond // signaled when no task is queued or running
	queue   []func()
	pending int // queued plus running
	closed  bool
	panics  int
	workers sync.WaitGroup
}

// New starts a pool with the given number of workers. A count below 1 is
// treated as 1.
func New(workers int) *Pool {
	workers = max(workers, 1)
	p := &Pool{}
	p.work = sync.NewCond(&p.mu)
	p.idle = sync.NewCond(&p.mu)
	for i := 0; i < workers; i++ {
		p.workers.Add(1)
		go p.run()
	}
	return p
}

func (p *Pool) run() {
	defer p.workers.Done()
	for {
		p.mu.Lock()
		for len(p.queue) == 0 && !p.closed {
			p.work.Wait()
		}
		if len(p.queue) == 0 {
			p.mu.Unlock()
			return
		}
		task := p.queue[0]
		p.queue[0] = nil
		p.queue = p.queue[1:]
		p.mu.Unlock()

		p.execute(task)

		p.mu.Lock()
		p.pending--
		if p.pending == 0 {
			p.idle.Broadcast()
		}
		p.mu.Unlock()
	}
}

func (p *Pool) execute(task func()) {
	defer func() {
		if recover() != nil {
			p.mu.Lock()
			p.panics++
			p.mu.Unlock()
		}
	}()
	task()
}

// Submit queues task to run on a worker and returns nil. The queue is
// unbounded: Submit never blocks the caller, however many tasks are waiting.
// Tasks may call Submit themselves. After Close, Submit returns ErrClosed and
// the task is not run; it never panics.
func (p *Pool) Submit(task func()) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrClosed
	}
	p.queue = append(p.queue, task)
	p.pending++
	p.work.Signal()
	return nil
}

// Wait blocks until every task submitted so far has finished, including tasks
// that other tasks submitted while they ran. It does not close the pool.
func (p *Pool) Wait() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for p.pending > 0 {
		p.idle.Wait()
	}
}

// Close stops the pool from accepting tasks, then blocks until every task it
// had already accepted has finished and the workers have exited. It is safe to
// call more than once and from several goroutines at the same time.
func (p *Pool) Close() {
	p.mu.Lock()
	p.closed = true
	p.work.Broadcast()
	p.mu.Unlock()
	p.workers.Wait()
}

// Panics returns how many tasks have panicked. A panicking task is recovered:
// it does not stop its worker, later tasks still run, and Wait and Close treat
// it as finished.
func (p *Pool) Panics() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.panics
}

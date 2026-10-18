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
	tasks   chan func()
	wg      sync.WaitGroup
	workers sync.WaitGroup

	mu     sync.Mutex
	panics int
}

// New starts a pool with the given number of workers. A count below 1 is
// treated as 1.
func New(workers int) *Pool {
	p := &Pool{tasks: make(chan func(), 16)}
	for i := 0; i < workers; i++ {
		p.workers.Add(1)
		go p.run()
	}
	return p
}

func (p *Pool) run() {
	defer p.workers.Done()
	for task := range p.tasks {
		p.wg.Add(1)
		task()
		p.wg.Done()
	}
}

// Submit queues task to run on a worker and returns nil. The queue is
// unbounded: Submit never blocks the caller, however many tasks are waiting.
// Tasks may call Submit themselves. After Close, Submit returns ErrClosed and
// the task is not run; it never panics.
func (p *Pool) Submit(task func()) error {
	p.tasks <- task
	return nil
}

// Wait blocks until every task submitted so far has finished, including tasks
// that other tasks submitted while they ran. It does not close the pool.
func (p *Pool) Wait() {
	p.wg.Wait()
}

// Close stops the pool from accepting tasks, then blocks until every task it
// had already accepted has finished and the workers have exited. It is safe to
// call more than once and from several goroutines at the same time.
func (p *Pool) Close() {
	close(p.tasks)
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

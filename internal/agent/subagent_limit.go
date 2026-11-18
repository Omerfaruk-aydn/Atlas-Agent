package agent

import "context"

// concurrencyLimiter caps how many sub-agents run at once. The agent tool is
// a parallel tool, so a model that fans out ten tasks otherwise starts ten
// sessions against the provider at the same moment -- fine on a paid
// endpoint, a wall of 429s on a rate-limited one.
//
// A nil *concurrencyLimiter is unlimited, so the unconfigured case needs no
// branch at the call site.
type concurrencyLimiter struct {
	slots chan struct{}
}

// newConcurrencyLimiter returns a limiter for n concurrent holders, or nil
// when n is zero or negative -- "no limit configured", not "no one may run".
func newConcurrencyLimiter(n int) *concurrencyLimiter {
	if n <= 0 {
		return nil
	}
	return &concurrencyLimiter{slots: make(chan struct{}, n)}
}

// sharedSubagentLimiter returns the coordinator-wide limiter for n
// concurrent sub-agents. Every tool that starts sub-agents (agent,
// orchestrate, delegate, debate) takes its limiter here, so the cap is a
// real bound on the sub-agents running at once instead of a bound per tool
// -- four tools with their own limiters allowed four times the setting.
//
// The limiter is replaced only when the configured capacity changes; tool
// instances built earlier keep releasing into the limiter they acquired from.
func (c *coordinator) sharedSubagentLimiter(n int) *concurrencyLimiter {
	c.subLimiterMu.Lock()
	defer c.subLimiterMu.Unlock()
	if c.subLimiter == nil || c.subLimiterN != n {
		c.subLimiter = newConcurrencyLimiter(n)
		c.subLimiterN = n
	}
	return c.subLimiter
}

// acquire waits for a slot. It returns the context's error if the caller is
// cancelled while waiting, so a cancelled sub-agent does not sit in the
// queue for work nobody wants any more.
func (l *concurrencyLimiter) acquire(ctx context.Context) error {
	if l == nil {
		return nil
	}
	select {
	case l.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *concurrencyLimiter) release() {
	if l == nil {
		return
	}
	select {
	case <-l.slots:
	default:
		// Releasing without holding a slot would otherwise block
		// forever; drop it instead, since the alternative is a
		// deadlock in a defer.
	}
}

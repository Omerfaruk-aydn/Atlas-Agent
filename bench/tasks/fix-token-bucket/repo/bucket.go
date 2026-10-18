// Package bucket implements a token bucket rate limiter.
package bucket

import "time"

// Bucket holds up to burst tokens and refills continuously at rate tokens
// per second, including fractions of a token. It starts full. It is not safe
// for concurrent use.
type Bucket struct {
	rate   float64
	burst  int
	tokens float64
	last   time.Time
	now    func() time.Time
}

// New returns a full bucket that reads time from now.
func New(rate float64, burst int, now func() time.Time) *Bucket {
	return &Bucket{rate: rate, burst: burst, tokens: float64(burst), last: now(), now: now}
}

// Allow takes n tokens and reports whether it could. It takes nothing when
// it returns false. A request for fewer than 1 token, or for more than the
// bucket can ever hold, is always refused.
func (b *Bucket) Allow(n int) bool {
	b.refill()
	if float64(n) > b.tokens {
		return false
	}
	b.tokens -= float64(n)
	return true
}

// Tokens returns the number of tokens available now.
func (b *Bucket) Tokens() float64 {
	b.refill()
	return b.tokens
}

func (b *Bucket) refill() {
	now := b.now()
	elapsed := now.Sub(b.last)
	b.tokens += float64(int(elapsed.Seconds())) * b.rate
	b.last = now
}

package bucket

import (
	"math"
	"testing"
	"time"
)

type hiddenClock struct{ t time.Time }

func (c *hiddenClock) now() time.Time          { return c.t }
func (c *hiddenClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func hiddenNewBucket(rate float64, burst int) (*Bucket, *hiddenClock) {
	c := &hiddenClock{t: time.Unix(1_000_000, 0)}
	return New(rate, burst, c.now), c
}

func hiddenNear(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestHiddenStartsFull(t *testing.T) {
	b, _ := hiddenNewBucket(1, 3)
	for i := range 3 {
		if !b.Allow(1) {
			t.Fatalf("request %d refused", i)
		}
	}
	if b.Allow(1) {
		t.Fatal("fourth request should be refused")
	}
}

func TestHiddenFractionalRefill(t *testing.T) {
	b, c := hiddenNewBucket(2, 10) // two tokens per second
	if !b.Allow(10) {
		t.Fatal("full bucket should allow its burst")
	}
	c.advance(250 * time.Millisecond)
	if !hiddenNear(b.Tokens(), 0.5) {
		t.Fatalf("after 250ms Tokens = %v, want 0.5", b.Tokens())
	}
	if b.Allow(1) {
		t.Fatal("half a token is not enough for a request")
	}
	c.advance(250 * time.Millisecond)
	if !b.Allow(1) {
		t.Fatal("two quarter seconds should have refilled one token")
	}
}

func TestHiddenManySmallStepsRefillLikeOneBigStep(t *testing.T) {
	b, c := hiddenNewBucket(1, 100)
	b.Allow(100)
	for range 1000 {
		c.advance(10 * time.Millisecond) // ten seconds in total
		b.Tokens()
	}
	if !hiddenNear(b.Tokens(), 10) {
		t.Fatalf("Tokens = %v, want 10", b.Tokens())
	}
}

func TestHiddenBurstCap(t *testing.T) {
	b, c := hiddenNewBucket(5, 4)
	c.advance(24 * time.Hour)
	if got := b.Tokens(); !hiddenNear(got, 4) {
		t.Fatalf("Tokens after a long idle = %v, want the burst of 4", got)
	}
	allowed := 0
	for range 10 {
		if b.Allow(1) {
			allowed++
		}
	}
	if allowed != 4 {
		t.Fatalf("allowed %d requests, want 4", allowed)
	}
}

func TestHiddenRefusedRequestTakesNothing(t *testing.T) {
	b, _ := hiddenNewBucket(1, 5)
	if b.Allow(6) {
		t.Fatal("more than the burst can never be allowed")
	}
	if !hiddenNear(b.Tokens(), 5) {
		t.Fatalf("a refused request changed Tokens to %v", b.Tokens())
	}
}

func TestHiddenInvalidRequestSizes(t *testing.T) {
	b, _ := hiddenNewBucket(1, 5)
	for _, n := range []int{0, -1, -100} {
		if b.Allow(n) {
			t.Errorf("Allow(%d) = true, want false", n)
		}
	}
	if !hiddenNear(b.Tokens(), 5) {
		t.Fatalf("invalid requests changed Tokens to %v", b.Tokens())
	}
}

func TestHiddenClockGoingBackwardsDoesNotDrain(t *testing.T) {
	b, c := hiddenNewBucket(1, 5)
	b.Allow(5)
	c.advance(2 * time.Second)
	c.advance(-10 * time.Second)
	if got := b.Tokens(); got < 0 {
		t.Fatalf("Tokens = %v after the hiddenClock moved backwards", got)
	}
}

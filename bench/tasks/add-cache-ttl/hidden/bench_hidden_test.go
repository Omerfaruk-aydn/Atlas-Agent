package cache

import (
	"testing"
	"time"
)

type hiddenClock struct{ t time.Time }

func (c *hiddenClock) now() time.Time          { return c.t }
func (c *hiddenClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func hiddenNewClock() *hiddenClock {
	return &hiddenClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func TestHiddenEvictsLeastRecentlyUsed(t *testing.T) {
	c := New(2)
	c.Set("a", 1)
	c.Set("b", 2)
	c.Get("a")
	c.Set("c", 3)
	if _, ok := c.Get("b"); ok {
		t.Fatal("b should have been evicted")
	}
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatal("a should still be cached")
	}
}

func TestHiddenEntryExpires(t *testing.T) {
	clk := hiddenNewClock()
	c := NewWithClock(4, clk.now)
	c.SetWithTTL("k", "v", time.Minute)
	clk.advance(time.Minute - time.Nanosecond)
	if v, ok := c.Get("k"); !ok || v != "v" {
		t.Fatalf("before expiry Get = %v, %v", v, ok)
	}
	clk.advance(time.Nanosecond)
	if _, ok := c.Get("k"); ok {
		t.Fatal("entry should be expired exactly at set time + ttl")
	}
	if c.Len() != 0 {
		t.Fatalf("Len = %d, want 0 after the expired entry was removed", c.Len())
	}
}

func TestHiddenLenIgnoresExpired(t *testing.T) {
	clk := hiddenNewClock()
	c := NewWithClock(4, clk.now)
	c.SetWithTTL("a", 1, time.Second)
	c.Set("b", 2)
	clk.advance(2 * time.Second)
	if c.Len() != 1 {
		t.Fatalf("Len = %d, want 1", c.Len())
	}
}

func TestHiddenSetNeverExpires(t *testing.T) {
	clk := hiddenNewClock()
	c := NewWithClock(2, clk.now)
	c.Set("a", 1)
	clk.advance(1000 * time.Hour)
	if _, ok := c.Get("a"); !ok {
		t.Fatal("Set entries never expire")
	}
}

func TestHiddenResetReplacesExpiry(t *testing.T) {
	clk := hiddenNewClock()
	c := NewWithClock(2, clk.now)
	c.SetWithTTL("a", 1, time.Second)
	c.Set("a", 2)
	clk.advance(time.Hour)
	if v, ok := c.Get("a"); !ok || v != 2 {
		t.Fatalf("Get = %v, %v; re-Set should clear the expiry", v, ok)
	}
	c.SetWithTTL("a", 3, time.Second)
	clk.advance(2 * time.Second)
	if _, ok := c.Get("a"); ok {
		t.Fatal("re-set with a ttl should expire")
	}
}

func TestHiddenExpiredEntriesAreRemovedBeforeEvicting(t *testing.T) {
	clk := hiddenNewClock()
	c := NewWithClock(2, clk.now)
	c.SetWithTTL("short", 1, time.Second)
	c.Set("live", 2)
	c.Get("short") // live is now the least recently used entry
	clk.advance(2 * time.Second)
	c.Set("new", 3) // short is expired, so it goes and live must survive
	if v, ok := c.Get("live"); !ok || v != 2 {
		t.Fatal("a live entry was evicted while an expired entry was still held")
	}
	if v, ok := c.Get("new"); !ok || v != 3 {
		t.Fatal("new entry missing")
	}
}

func TestHiddenStillEvictsLRUWhenNothingExpired(t *testing.T) {
	clk := hiddenNewClock()
	c := NewWithClock(2, clk.now)
	c.SetWithTTL("a", 1, time.Hour)
	c.SetWithTTL("b", 2, time.Hour)
	c.Get("a")
	c.Set("c", 3)
	if _, ok := c.Get("b"); ok {
		t.Fatal("b should have been evicted")
	}
}

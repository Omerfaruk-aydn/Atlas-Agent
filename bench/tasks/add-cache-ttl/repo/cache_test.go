package cache

import "testing"

func TestEvictsLeastRecentlyUsed(t *testing.T) {
	c := New(2)
	c.Set("a", 1)
	c.Set("b", 2)
	c.Get("a")
	c.Set("c", 3)
	if _, ok := c.Get("b"); ok {
		t.Fatal("b should have been evicted")
	}
	if _, ok := c.Get("a"); !ok {
		t.Fatal("a should still be cached")
	}
}

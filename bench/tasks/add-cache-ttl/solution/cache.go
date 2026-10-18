// Package cache implements a fixed-capacity least-recently-used cache.
package cache

import (
	"container/list"
	"time"
)

type entry struct {
	key     string
	value   any
	expires time.Time // zero means the entry never expires
}

// Cache holds at most capacity entries and evicts the least recently used
// one when a new key does not fit. It is not safe for concurrent use.
type Cache struct {
	capacity int
	order    *list.List // front is the most recently used
	items    map[string]*list.Element
	now      func() time.Time
}

// New returns an empty cache. A capacity below 1 is treated as 1.
func New(capacity int) *Cache { return NewWithClock(capacity, time.Now) }

// NewWithClock is New with an injected clock.
func NewWithClock(capacity int, now func() time.Time) *Cache {
	if capacity < 1 {
		capacity = 1
	}
	return &Cache{capacity: capacity, order: list.New(), items: map[string]*list.Element{}, now: now}
}

func (e *entry) expired(now time.Time) bool {
	return !e.expires.IsZero() && !now.Before(e.expires)
}

func (c *Cache) remove(el *list.Element) {
	c.order.Remove(el)
	delete(c.items, el.Value.(*entry).key)
}

func (c *Cache) purgeExpired() {
	now := c.now()
	for el := c.order.Front(); el != nil; {
		next := el.Next()
		if el.Value.(*entry).expired(now) {
			c.remove(el)
		}
		el = next
	}
}

// Get returns the value for key and marks it most recently used.
func (c *Cache) Get(key string) (any, bool) {
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	if el.Value.(*entry).expired(c.now()) {
		c.remove(el)
		return nil, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*entry).value, true
}

// Set stores value under key without an expiry, replacing any existing value.
func (c *Cache) Set(key string, value any) { c.set(key, value, time.Time{}) }

// SetWithTTL stores value under key until ttl has elapsed.
func (c *Cache) SetWithTTL(key string, value any, ttl time.Duration) {
	c.set(key, value, c.now().Add(ttl))
}

func (c *Cache) set(key string, value any, expires time.Time) {
	if el, ok := c.items[key]; ok {
		e := el.Value.(*entry)
		e.value, e.expires = value, expires
		c.order.MoveToFront(el)
		return
	}
	if c.order.Len() >= c.capacity {
		c.purgeExpired()
	}
	if c.order.Len() >= c.capacity {
		c.remove(c.order.Back())
	}
	c.items[key] = c.order.PushFront(&entry{key: key, value: value, expires: expires})
}

// Len returns the number of live entries.
func (c *Cache) Len() int {
	c.purgeExpired()
	return c.order.Len()
}

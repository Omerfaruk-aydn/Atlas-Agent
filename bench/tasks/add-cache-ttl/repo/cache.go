// Package cache implements a fixed-capacity least-recently-used cache.
package cache

import "container/list"

type entry struct {
	key   string
	value any
}

// Cache holds at most capacity entries and evicts the least recently used
// one when a new key does not fit. It is not safe for concurrent use.
type Cache struct {
	capacity int
	order    *list.List // front is the most recently used
	items    map[string]*list.Element
}

// New returns an empty cache. A capacity below 1 is treated as 1.
func New(capacity int) *Cache {
	if capacity < 1 {
		capacity = 1
	}
	return &Cache{capacity: capacity, order: list.New(), items: map[string]*list.Element{}}
}

// Get returns the value for key and marks it most recently used.
func (c *Cache) Get(key string) (any, bool) {
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*entry).value, true
}

// Set stores value under key, replacing any existing value.
func (c *Cache) Set(key string, value any) {
	if el, ok := c.items[key]; ok {
		el.Value.(*entry).value = value
		c.order.MoveToFront(el)
		return
	}
	if c.order.Len() >= c.capacity {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*entry).key)
	}
	c.items[key] = c.order.PushFront(&entry{key: key, value: value})
}

// Len returns the number of entries.
func (c *Cache) Len() int { return c.order.Len() }

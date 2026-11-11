package sfsymbols

import "container/list"

// lru maps keys to values, holding at most limit of them: putting one more
// drops the least recently used. It is not safe for concurrent use.
type lru[K comparable, V any] struct {
	limit int
	order *list.List // of *entry[K, V], the most recently used first
	items map[K]*list.Element
}

type entry[K comparable, V any] struct {
	key K
	val V
}

func newLRU[K comparable, V any](limit int) *lru[K, V] {
	return &lru[K, V]{limit: limit, order: list.New(), items: map[K]*list.Element{}}
}

// get returns key's value, and marks it the most recently used.
func (c *lru[K, V]) get(key K) (V, bool) {
	if e, ok := c.items[key]; ok {
		c.order.MoveToFront(e)
		return e.Value.(*entry[K, V]).val, true
	}
	var zero V
	return zero, false
}

// put sets key's value, and drops the least recently used past the limit.
func (c *lru[K, V]) put(key K, val V) {
	if e, ok := c.items[key]; ok {
		e.Value.(*entry[K, V]).val = val
		c.order.MoveToFront(e)
		return
	}
	c.items[key] = c.order.PushFront(&entry[K, V]{key, val})
	for c.order.Len() > c.limit {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.items, last.Value.(*entry[K, V]).key)
	}
}

func (c *lru[K, V]) len() int { return c.order.Len() }

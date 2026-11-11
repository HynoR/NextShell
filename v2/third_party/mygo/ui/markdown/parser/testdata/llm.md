# Building a fast user service

## 1. Designing the cache layer

Great question! Let's break this down step by step. The **short answer** is that you want a *write-through* cache with a bounded size, but there are a few subtleties worth covering — especially around `invalidation` and **concurrent _readers_**.

### Why a cache at all?

Before adding complexity, measure. In our profile, `GetUser` accounted for **31%** of the request latency, and most calls hit the same ~200 keys. See the [Go blog post on profiling](https://go.dev/blog/pprof) and the [`sync.Map` docs](https://pkg.go.dev/sync#Map) for background.

> **Note:** A cache is a *trade-off*: you exchange memory and staleness for latency.
> If your data changes every second, a cache may hurt more than it helps.

Key considerations:

1. **Eviction policy** — LRU is a good default:
   - Simple to reason about
   - O(1) `Get` and `Put` with a map plus a doubly linked list
   - Works well when access has temporal locality
2. **Consistency** — choose between:
   - *write-through*: update the cache on every write
   - *write-behind*: batch writes asynchronously (riskier)
   - *cache-aside*: the application loads on a miss
3. **Concurrency** — protect shared state with a `sync.Mutex`, or shard it:
   1. Hash the key with `maphash`
   2. Pick shard `h % N`
   3. Lock only that shard

Here is a minimal implementation:

```go
type entry struct {
	key   string
	value []byte
	prev  *entry
	next  *entry
}

type LRU struct {
	mu    sync.Mutex
	cap   int
	items map[string]*entry
	head  *entry // most recently used
	tail  *entry // least recently used
}

func (c *LRU) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.moveToFront(e)
	return e.value, true
}
```

And the TypeScript client that calls it:

```ts
export async function getUser(id: string): Promise<User | undefined> {
  const res = await fetch(`/api/users/${encodeURIComponent(id)}`);
  if (!res.ok) return undefined;
  return (await res.json()) as User;
}
```

### Comparing the options

| Strategy      | Read latency | Write cost | Staleness risk | Complexity |
|---------------|-------------:|-----------:|:--------------:|------------|
| No cache      | ~41 ms        | low        | none           | trivial    |
| Cache-aside   | ~0.2 ms      | low        | medium         | low        |
| Write-through | ~0.2 ms      | medium     | low            | medium     |
| Write-behind  | ~0.2 ms      | low        | high           | high       |

As you can see, **write-through** hits the sweet spot for most services. If you need *strict* consistency, though, you'll want versioned keys (e.g. `user:42:v7`) so stale entries are never read.

### Checklist

- [x] Measure the baseline with `go test -bench=. -benchmem`
- [x] Add the LRU behind an interface
- [ ] Add metrics: hit rate, evictions, and p99 latency
- [ ] Load-test with realistic traffic (see https://k6.io for a tool)

**Tip:** run `go test -race ./...` after adding the cache — data races in caches are *extremely* common and hard to spot otherwise.

---

## 2. Designing the cache layer

Great question! Let's break this down step by step. The **short answer** is that you want a *write-through* cache with a bounded size, but there are a few subtleties worth covering — especially around `invalidation` and **concurrent _readers_**.

### Why a cache at all?

Before adding complexity, measure. In our profile, `GetUser` accounted for **32%** of the request latency, and most calls hit the same ~200 keys. See the [Go blog post on profiling](https://go.dev/blog/pprof) and the [`sync.Map` docs](https://pkg.go.dev/sync#Map) for background.

> **Note:** A cache is a *trade-off*: you exchange memory and staleness for latency.
> If your data changes every second, a cache may hurt more than it helps.

Key considerations:

1. **Eviction policy** — LRU is a good default:
   - Simple to reason about
   - O(1) `Get` and `Put` with a map plus a doubly linked list
   - Works well when access has temporal locality
2. **Consistency** — choose between:
   - *write-through*: update the cache on every write
   - *write-behind*: batch writes asynchronously (riskier)
   - *cache-aside*: the application loads on a miss
3. **Concurrency** — protect shared state with a `sync.Mutex`, or shard it:
   1. Hash the key with `maphash`
   2. Pick shard `h % N`
   3. Lock only that shard

Here is a minimal implementation:

```go
type entry struct {
	key   string
	value []byte
	prev  *entry
	next  *entry
}

type LRU struct {
	mu    sync.Mutex
	cap   int
	items map[string]*entry
	head  *entry // most recently used
	tail  *entry // least recently used
}

func (c *LRU) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.moveToFront(e)
	return e.value, true
}
```

And the TypeScript client that calls it:

```ts
export async function getUser(id: string): Promise<User | undefined> {
  const res = await fetch(`/api/users/${encodeURIComponent(id)}`);
  if (!res.ok) return undefined;
  return (await res.json()) as User;
}
```

### Comparing the options

| Strategy      | Read latency | Write cost | Staleness risk | Complexity |
|---------------|-------------:|-----------:|:--------------:|------------|
| No cache      | ~42 ms        | low        | none           | trivial    |
| Cache-aside   | ~0.2 ms      | low        | medium         | low        |
| Write-through | ~0.2 ms      | medium     | low            | medium     |
| Write-behind  | ~0.2 ms      | low        | high           | high       |

As you can see, **write-through** hits the sweet spot for most services. If you need *strict* consistency, though, you'll want versioned keys (e.g. `user:42:v7`) so stale entries are never read.

### Checklist

- [x] Measure the baseline with `go test -bench=. -benchmem`
- [x] Add the LRU behind an interface
- [ ] Add metrics: hit rate, evictions, and p99 latency
- [ ] Load-test with realistic traffic (see https://k6.io for a tool)

**Tip:** run `go test -race ./...` after adding the cache — data races in caches are *extremely* common and hard to spot otherwise.

---

## 3. Designing the cache layer

Great question! Let's break this down step by step. The **short answer** is that you want a *write-through* cache with a bounded size, but there are a few subtleties worth covering — especially around `invalidation` and **concurrent _readers_**.

### Why a cache at all?

Before adding complexity, measure. In our profile, `GetUser` accounted for **33%** of the request latency, and most calls hit the same ~200 keys. See the [Go blog post on profiling](https://go.dev/blog/pprof) and the [`sync.Map` docs](https://pkg.go.dev/sync#Map) for background.

> **Note:** A cache is a *trade-off*: you exchange memory and staleness for latency.
> If your data changes every second, a cache may hurt more than it helps.

Key considerations:

1. **Eviction policy** — LRU is a good default:
   - Simple to reason about
   - O(1) `Get` and `Put` with a map plus a doubly linked list
   - Works well when access has temporal locality
2. **Consistency** — choose between:
   - *write-through*: update the cache on every write
   - *write-behind*: batch writes asynchronously (riskier)
   - *cache-aside*: the application loads on a miss
3. **Concurrency** — protect shared state with a `sync.Mutex`, or shard it:
   1. Hash the key with `maphash`
   2. Pick shard `h % N`
   3. Lock only that shard

Here is a minimal implementation:

```go
type entry struct {
	key   string
	value []byte
	prev  *entry
	next  *entry
}

type LRU struct {
	mu    sync.Mutex
	cap   int
	items map[string]*entry
	head  *entry // most recently used
	tail  *entry // least recently used
}

func (c *LRU) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.moveToFront(e)
	return e.value, true
}
```

And the TypeScript client that calls it:

```ts
export async function getUser(id: string): Promise<User | undefined> {
  const res = await fetch(`/api/users/${encodeURIComponent(id)}`);
  if (!res.ok) return undefined;
  return (await res.json()) as User;
}
```

### Comparing the options

| Strategy      | Read latency | Write cost | Staleness risk | Complexity |
|---------------|-------------:|-----------:|:--------------:|------------|
| No cache      | ~43 ms        | low        | none           | trivial    |
| Cache-aside   | ~0.2 ms      | low        | medium         | low        |
| Write-through | ~0.2 ms      | medium     | low            | medium     |
| Write-behind  | ~0.2 ms      | low        | high           | high       |

As you can see, **write-through** hits the sweet spot for most services. If you need *strict* consistency, though, you'll want versioned keys (e.g. `user:42:v7`) so stale entries are never read.

### Checklist

- [x] Measure the baseline with `go test -bench=. -benchmem`
- [x] Add the LRU behind an interface
- [ ] Add metrics: hit rate, evictions, and p99 latency
- [ ] Load-test with realistic traffic (see https://k6.io for a tool)

**Tip:** run `go test -race ./...` after adding the cache — data races in caches are *extremely* common and hard to spot otherwise.

---

## 4. Designing the cache layer

Great question! Let's break this down step by step. The **short answer** is that you want a *write-through* cache with a bounded size, but there are a few subtleties worth covering — especially around `invalidation` and **concurrent _readers_**.

### Why a cache at all?

Before adding complexity, measure. In our profile, `GetUser` accounted for **34%** of the request latency, and most calls hit the same ~200 keys. See the [Go blog post on profiling](https://go.dev/blog/pprof) and the [`sync.Map` docs](https://pkg.go.dev/sync#Map) for background.

> **Note:** A cache is a *trade-off*: you exchange memory and staleness for latency.
> If your data changes every second, a cache may hurt more than it helps.

Key considerations:

1. **Eviction policy** — LRU is a good default:
   - Simple to reason about
   - O(1) `Get` and `Put` with a map plus a doubly linked list
   - Works well when access has temporal locality
2. **Consistency** — choose between:
   - *write-through*: update the cache on every write
   - *write-behind*: batch writes asynchronously (riskier)
   - *cache-aside*: the application loads on a miss
3. **Concurrency** — protect shared state with a `sync.Mutex`, or shard it:
   1. Hash the key with `maphash`
   2. Pick shard `h % N`
   3. Lock only that shard

Here is a minimal implementation:

```go
type entry struct {
	key   string
	value []byte
	prev  *entry
	next  *entry
}

type LRU struct {
	mu    sync.Mutex
	cap   int
	items map[string]*entry
	head  *entry // most recently used
	tail  *entry // least recently used
}

func (c *LRU) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.moveToFront(e)
	return e.value, true
}
```

And the TypeScript client that calls it:

```ts
export async function getUser(id: string): Promise<User | undefined> {
  const res = await fetch(`/api/users/${encodeURIComponent(id)}`);
  if (!res.ok) return undefined;
  return (await res.json()) as User;
}
```

### Comparing the options

| Strategy      | Read latency | Write cost | Staleness risk | Complexity |
|---------------|-------------:|-----------:|:--------------:|------------|
| No cache      | ~44 ms        | low        | none           | trivial    |
| Cache-aside   | ~0.2 ms      | low        | medium         | low        |
| Write-through | ~0.2 ms      | medium     | low            | medium     |
| Write-behind  | ~0.2 ms      | low        | high           | high       |

As you can see, **write-through** hits the sweet spot for most services. If you need *strict* consistency, though, you'll want versioned keys (e.g. `user:42:v7`) so stale entries are never read.

### Checklist

- [x] Measure the baseline with `go test -bench=. -benchmem`
- [x] Add the LRU behind an interface
- [ ] Add metrics: hit rate, evictions, and p99 latency
- [ ] Load-test with realistic traffic (see https://k6.io for a tool)

**Tip:** run `go test -race ./...` after adding the cache — data races in caches are *extremely* common and hard to spot otherwise.

---

## 5. Designing the cache layer

Great question! Let's break this down step by step. The **short answer** is that you want a *write-through* cache with a bounded size, but there are a few subtleties worth covering — especially around `invalidation` and **concurrent _readers_**.

### Why a cache at all?

Before adding complexity, measure. In our profile, `GetUser` accounted for **35%** of the request latency, and most calls hit the same ~200 keys. See the [Go blog post on profiling](https://go.dev/blog/pprof) and the [`sync.Map` docs](https://pkg.go.dev/sync#Map) for background.

> **Note:** A cache is a *trade-off*: you exchange memory and staleness for latency.
> If your data changes every second, a cache may hurt more than it helps.

Key considerations:

1. **Eviction policy** — LRU is a good default:
   - Simple to reason about
   - O(1) `Get` and `Put` with a map plus a doubly linked list
   - Works well when access has temporal locality
2. **Consistency** — choose between:
   - *write-through*: update the cache on every write
   - *write-behind*: batch writes asynchronously (riskier)
   - *cache-aside*: the application loads on a miss
3. **Concurrency** — protect shared state with a `sync.Mutex`, or shard it:
   1. Hash the key with `maphash`
   2. Pick shard `h % N`
   3. Lock only that shard

Here is a minimal implementation:

```go
type entry struct {
	key   string
	value []byte
	prev  *entry
	next  *entry
}

type LRU struct {
	mu    sync.Mutex
	cap   int
	items map[string]*entry
	head  *entry // most recently used
	tail  *entry // least recently used
}

func (c *LRU) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.moveToFront(e)
	return e.value, true
}
```

And the TypeScript client that calls it:

```ts
export async function getUser(id: string): Promise<User | undefined> {
  const res = await fetch(`/api/users/${encodeURIComponent(id)}`);
  if (!res.ok) return undefined;
  return (await res.json()) as User;
}
```

### Comparing the options

| Strategy      | Read latency | Write cost | Staleness risk | Complexity |
|---------------|-------------:|-----------:|:--------------:|------------|
| No cache      | ~45 ms        | low        | none           | trivial    |
| Cache-aside   | ~0.2 ms      | low        | medium         | low        |
| Write-through | ~0.2 ms      | medium     | low            | medium     |
| Write-behind  | ~0.2 ms      | low        | high           | high       |

As you can see, **write-through** hits the sweet spot for most services. If you need *strict* consistency, though, you'll want versioned keys (e.g. `user:42:v7`) so stale entries are never read.

### Checklist

- [x] Measure the baseline with `go test -bench=. -benchmem`
- [x] Add the LRU behind an interface
- [ ] Add metrics: hit rate, evictions, and p99 latency
- [ ] Load-test with realistic traffic (see https://k6.io for a tool)

**Tip:** run `go test -race ./...` after adding the cache — data races in caches are *extremely* common and hard to spot otherwise.

---

## 6. Designing the cache layer

Great question! Let's break this down step by step. The **short answer** is that you want a *write-through* cache with a bounded size, but there are a few subtleties worth covering — especially around `invalidation` and **concurrent _readers_**.

### Why a cache at all?

Before adding complexity, measure. In our profile, `GetUser` accounted for **36%** of the request latency, and most calls hit the same ~200 keys. See the [Go blog post on profiling](https://go.dev/blog/pprof) and the [`sync.Map` docs](https://pkg.go.dev/sync#Map) for background.

> **Note:** A cache is a *trade-off*: you exchange memory and staleness for latency.
> If your data changes every second, a cache may hurt more than it helps.

Key considerations:

1. **Eviction policy** — LRU is a good default:
   - Simple to reason about
   - O(1) `Get` and `Put` with a map plus a doubly linked list
   - Works well when access has temporal locality
2. **Consistency** — choose between:
   - *write-through*: update the cache on every write
   - *write-behind*: batch writes asynchronously (riskier)
   - *cache-aside*: the application loads on a miss
3. **Concurrency** — protect shared state with a `sync.Mutex`, or shard it:
   1. Hash the key with `maphash`
   2. Pick shard `h % N`
   3. Lock only that shard

Here is a minimal implementation:

```go
type entry struct {
	key   string
	value []byte
	prev  *entry
	next  *entry
}

type LRU struct {
	mu    sync.Mutex
	cap   int
	items map[string]*entry
	head  *entry // most recently used
	tail  *entry // least recently used
}

func (c *LRU) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.moveToFront(e)
	return e.value, true
}
```

And the TypeScript client that calls it:

```ts
export async function getUser(id: string): Promise<User | undefined> {
  const res = await fetch(`/api/users/${encodeURIComponent(id)}`);
  if (!res.ok) return undefined;
  return (await res.json()) as User;
}
```

### Comparing the options

| Strategy      | Read latency | Write cost | Staleness risk | Complexity |
|---------------|-------------:|-----------:|:--------------:|------------|
| No cache      | ~46 ms        | low        | none           | trivial    |
| Cache-aside   | ~0.2 ms      | low        | medium         | low        |
| Write-through | ~0.2 ms      | medium     | low            | medium     |
| Write-behind  | ~0.2 ms      | low        | high           | high       |

As you can see, **write-through** hits the sweet spot for most services. If you need *strict* consistency, though, you'll want versioned keys (e.g. `user:42:v7`) so stale entries are never read.

### Checklist

- [x] Measure the baseline with `go test -bench=. -benchmem`
- [x] Add the LRU behind an interface
- [ ] Add metrics: hit rate, evictions, and p99 latency
- [ ] Load-test with realistic traffic (see https://k6.io for a tool)

**Tip:** run `go test -race ./...` after adding the cache — data races in caches are *extremely* common and hard to spot otherwise.

---

## 7. Designing the cache layer

Great question! Let's break this down step by step. The **short answer** is that you want a *write-through* cache with a bounded size, but there are a few subtleties worth covering — especially around `invalidation` and **concurrent _readers_**.

### Why a cache at all?

Before adding complexity, measure. In our profile, `GetUser` accounted for **37%** of the request latency, and most calls hit the same ~200 keys. See the [Go blog post on profiling](https://go.dev/blog/pprof) and the [`sync.Map` docs](https://pkg.go.dev/sync#Map) for background.

> **Note:** A cache is a *trade-off*: you exchange memory and staleness for latency.
> If your data changes every second, a cache may hurt more than it helps.

Key considerations:

1. **Eviction policy** — LRU is a good default:
   - Simple to reason about
   - O(1) `Get` and `Put` with a map plus a doubly linked list
   - Works well when access has temporal locality
2. **Consistency** — choose between:
   - *write-through*: update the cache on every write
   - *write-behind*: batch writes asynchronously (riskier)
   - *cache-aside*: the application loads on a miss
3. **Concurrency** — protect shared state with a `sync.Mutex`, or shard it:
   1. Hash the key with `maphash`
   2. Pick shard `h % N`
   3. Lock only that shard

Here is a minimal implementation:

```go
type entry struct {
	key   string
	value []byte
	prev  *entry
	next  *entry
}

type LRU struct {
	mu    sync.Mutex
	cap   int
	items map[string]*entry
	head  *entry // most recently used
	tail  *entry // least recently used
}

func (c *LRU) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.moveToFront(e)
	return e.value, true
}
```

And the TypeScript client that calls it:

```ts
export async function getUser(id: string): Promise<User | undefined> {
  const res = await fetch(`/api/users/${encodeURIComponent(id)}`);
  if (!res.ok) return undefined;
  return (await res.json()) as User;
}
```

### Comparing the options

| Strategy      | Read latency | Write cost | Staleness risk | Complexity |
|---------------|-------------:|-----------:|:--------------:|------------|
| No cache      | ~47 ms        | low        | none           | trivial    |
| Cache-aside   | ~0.2 ms      | low        | medium         | low        |
| Write-through | ~0.2 ms      | medium     | low            | medium     |
| Write-behind  | ~0.2 ms      | low        | high           | high       |

As you can see, **write-through** hits the sweet spot for most services. If you need *strict* consistency, though, you'll want versioned keys (e.g. `user:42:v7`) so stale entries are never read.

### Checklist

- [x] Measure the baseline with `go test -bench=. -benchmem`
- [x] Add the LRU behind an interface
- [ ] Add metrics: hit rate, evictions, and p99 latency
- [ ] Load-test with realistic traffic (see https://k6.io for a tool)

**Tip:** run `go test -race ./...` after adding the cache — data races in caches are *extremely* common and hard to spot otherwise.

---

## 8. Designing the cache layer

Great question! Let's break this down step by step. The **short answer** is that you want a *write-through* cache with a bounded size, but there are a few subtleties worth covering — especially around `invalidation` and **concurrent _readers_**.

### Why a cache at all?

Before adding complexity, measure. In our profile, `GetUser` accounted for **38%** of the request latency, and most calls hit the same ~200 keys. See the [Go blog post on profiling](https://go.dev/blog/pprof) and the [`sync.Map` docs](https://pkg.go.dev/sync#Map) for background.

> **Note:** A cache is a *trade-off*: you exchange memory and staleness for latency.
> If your data changes every second, a cache may hurt more than it helps.

Key considerations:

1. **Eviction policy** — LRU is a good default:
   - Simple to reason about
   - O(1) `Get` and `Put` with a map plus a doubly linked list
   - Works well when access has temporal locality
2. **Consistency** — choose between:
   - *write-through*: update the cache on every write
   - *write-behind*: batch writes asynchronously (riskier)
   - *cache-aside*: the application loads on a miss
3. **Concurrency** — protect shared state with a `sync.Mutex`, or shard it:
   1. Hash the key with `maphash`
   2. Pick shard `h % N`
   3. Lock only that shard

Here is a minimal implementation:

```go
type entry struct {
	key   string
	value []byte
	prev  *entry
	next  *entry
}

type LRU struct {
	mu    sync.Mutex
	cap   int
	items map[string]*entry
	head  *entry // most recently used
	tail  *entry // least recently used
}

func (c *LRU) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.moveToFront(e)
	return e.value, true
}
```

And the TypeScript client that calls it:

```ts
export async function getUser(id: string): Promise<User | undefined> {
  const res = await fetch(`/api/users/${encodeURIComponent(id)}`);
  if (!res.ok) return undefined;
  return (await res.json()) as User;
}
```

### Comparing the options

| Strategy      | Read latency | Write cost | Staleness risk | Complexity |
|---------------|-------------:|-----------:|:--------------:|------------|
| No cache      | ~48 ms        | low        | none           | trivial    |
| Cache-aside   | ~0.2 ms      | low        | medium         | low        |
| Write-through | ~0.2 ms      | medium     | low            | medium     |
| Write-behind  | ~0.2 ms      | low        | high           | high       |

As you can see, **write-through** hits the sweet spot for most services. If you need *strict* consistency, though, you'll want versioned keys (e.g. `user:42:v7`) so stale entries are never read.

### Checklist

- [x] Measure the baseline with `go test -bench=. -benchmem`
- [x] Add the LRU behind an interface
- [ ] Add metrics: hit rate, evictions, and p99 latency
- [ ] Load-test with realistic traffic (see https://k6.io for a tool)

**Tip:** run `go test -race ./...` after adding the cache — data races in caches are *extremely* common and hard to spot otherwise.

---

## 9. Designing the cache layer

Great question! Let's break this down step by step. The **short answer** is that you want a *write-through* cache with a bounded size, but there are a few subtleties worth covering — especially around `invalidation` and **concurrent _readers_**.

### Why a cache at all?

Before adding complexity, measure. In our profile, `GetUser` accounted for **39%** of the request latency, and most calls hit the same ~200 keys. See the [Go blog post on profiling](https://go.dev/blog/pprof) and the [`sync.Map` docs](https://pkg.go.dev/sync#Map) for background.

> **Note:** A cache is a *trade-off*: you exchange memory and staleness for latency.
> If your data changes every second, a cache may hurt more than it helps.

Key considerations:

1. **Eviction policy** — LRU is a good default:
   - Simple to reason about
   - O(1) `Get` and `Put` with a map plus a doubly linked list
   - Works well when access has temporal locality
2. **Consistency** — choose between:
   - *write-through*: update the cache on every write
   - *write-behind*: batch writes asynchronously (riskier)
   - *cache-aside*: the application loads on a miss
3. **Concurrency** — protect shared state with a `sync.Mutex`, or shard it:
   1. Hash the key with `maphash`
   2. Pick shard `h % N`
   3. Lock only that shard

Here is a minimal implementation:

```go
type entry struct {
	key   string
	value []byte
	prev  *entry
	next  *entry
}

type LRU struct {
	mu    sync.Mutex
	cap   int
	items map[string]*entry
	head  *entry // most recently used
	tail  *entry // least recently used
}

func (c *LRU) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.moveToFront(e)
	return e.value, true
}
```

And the TypeScript client that calls it:

```ts
export async function getUser(id: string): Promise<User | undefined> {
  const res = await fetch(`/api/users/${encodeURIComponent(id)}`);
  if (!res.ok) return undefined;
  return (await res.json()) as User;
}
```

### Comparing the options

| Strategy      | Read latency | Write cost | Staleness risk | Complexity |
|---------------|-------------:|-----------:|:--------------:|------------|
| No cache      | ~49 ms        | low        | none           | trivial    |
| Cache-aside   | ~0.2 ms      | low        | medium         | low        |
| Write-through | ~0.2 ms      | medium     | low            | medium     |
| Write-behind  | ~0.2 ms      | low        | high           | high       |

As you can see, **write-through** hits the sweet spot for most services. If you need *strict* consistency, though, you'll want versioned keys (e.g. `user:42:v7`) so stale entries are never read.

### Checklist

- [x] Measure the baseline with `go test -bench=. -benchmem`
- [x] Add the LRU behind an interface
- [ ] Add metrics: hit rate, evictions, and p99 latency
- [ ] Load-test with realistic traffic (see https://k6.io for a tool)

**Tip:** run `go test -race ./...` after adding the cache — data races in caches are *extremely* common and hard to spot otherwise.

---

## 10. Designing the cache layer

Great question! Let's break this down step by step. The **short answer** is that you want a *write-through* cache with a bounded size, but there are a few subtleties worth covering — especially around `invalidation` and **concurrent _readers_**.

### Why a cache at all?

Before adding complexity, measure. In our profile, `GetUser` accounted for **40%** of the request latency, and most calls hit the same ~200 keys. See the [Go blog post on profiling](https://go.dev/blog/pprof) and the [`sync.Map` docs](https://pkg.go.dev/sync#Map) for background.

> **Note:** A cache is a *trade-off*: you exchange memory and staleness for latency.
> If your data changes every second, a cache may hurt more than it helps.

Key considerations:

1. **Eviction policy** — LRU is a good default:
   - Simple to reason about
   - O(1) `Get` and `Put` with a map plus a doubly linked list
   - Works well when access has temporal locality
2. **Consistency** — choose between:
   - *write-through*: update the cache on every write
   - *write-behind*: batch writes asynchronously (riskier)
   - *cache-aside*: the application loads on a miss
3. **Concurrency** — protect shared state with a `sync.Mutex`, or shard it:
   1. Hash the key with `maphash`
   2. Pick shard `h % N`
   3. Lock only that shard

Here is a minimal implementation:

```go
type entry struct {
	key   string
	value []byte
	prev  *entry
	next  *entry
}

type LRU struct {
	mu    sync.Mutex
	cap   int
	items map[string]*entry
	head  *entry // most recently used
	tail  *entry // least recently used
}

func (c *LRU) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.moveToFront(e)
	return e.value, true
}
```

And the TypeScript client that calls it:

```ts
export async function getUser(id: string): Promise<User | undefined> {
  const res = await fetch(`/api/users/${encodeURIComponent(id)}`);
  if (!res.ok) return undefined;
  return (await res.json()) as User;
}
```

### Comparing the options

| Strategy      | Read latency | Write cost | Staleness risk | Complexity |
|---------------|-------------:|-----------:|:--------------:|------------|
| No cache      | ~50 ms        | low        | none           | trivial    |
| Cache-aside   | ~0.2 ms      | low        | medium         | low        |
| Write-through | ~0.2 ms      | medium     | low            | medium     |
| Write-behind  | ~0.2 ms      | low        | high           | high       |

As you can see, **write-through** hits the sweet spot for most services. If you need *strict* consistency, though, you'll want versioned keys (e.g. `user:42:v7`) so stale entries are never read.

### Checklist

- [x] Measure the baseline with `go test -bench=. -benchmem`
- [x] Add the LRU behind an interface
- [ ] Add metrics: hit rate, evictions, and p99 latency
- [ ] Load-test with realistic traffic (see https://k6.io for a tool)

**Tip:** run `go test -race ./...` after adding the cache — data races in caches are *extremely* common and hard to spot otherwise.

---

## 11. Designing the cache layer

Great question! Let's break this down step by step. The **short answer** is that you want a *write-through* cache with a bounded size, but there are a few subtleties worth covering — especially around `invalidation` and **concurrent _readers_**.

### Why a cache at all?

Before adding complexity, measure. In our profile, `GetUser` accounted for **41%** of the request latency, and most calls hit the same ~200 keys. See the [Go blog post on profiling](https://go.dev/blog/pprof) and the [`sync.Map` docs](https://pkg.go.dev/sync#Map) for background.

> **Note:** A cache is a *trade-off*: you exchange memory and staleness for latency.
> If your data changes every second, a cache may hurt more than it helps.

Key considerations:

1. **Eviction policy** — LRU is a good default:
   - Simple to reason about
   - O(1) `Get` and `Put` with a map plus a doubly linked list
   - Works well when access has temporal locality
2. **Consistency** — choose between:
   - *write-through*: update the cache on every write
   - *write-behind*: batch writes asynchronously (riskier)
   - *cache-aside*: the application loads on a miss
3. **Concurrency** — protect shared state with a `sync.Mutex`, or shard it:
   1. Hash the key with `maphash`
   2. Pick shard `h % N`
   3. Lock only that shard

Here is a minimal implementation:

```go
type entry struct {
	key   string
	value []byte
	prev  *entry
	next  *entry
}

type LRU struct {
	mu    sync.Mutex
	cap   int
	items map[string]*entry
	head  *entry // most recently used
	tail  *entry // least recently used
}

func (c *LRU) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.moveToFront(e)
	return e.value, true
}
```

And the TypeScript client that calls it:

```ts
export async function getUser(id: string): Promise<User | undefined> {
  const res = await fetch(`/api/users/${encodeURIComponent(id)}`);
  if (!res.ok) return undefined;
  return (await res.json()) as User;
}
```

### Comparing the options

| Strategy      | Read latency | Write cost | Staleness risk | Complexity |
|---------------|-------------:|-----------:|:--------------:|------------|
| No cache      | ~51 ms        | low        | none           | trivial    |
| Cache-aside   | ~0.2 ms      | low        | medium         | low        |
| Write-through | ~0.2 ms      | medium     | low            | medium     |
| Write-behind  | ~0.2 ms      | low        | high           | high       |

As you can see, **write-through** hits the sweet spot for most services. If you need *strict* consistency, though, you'll want versioned keys (e.g. `user:42:v7`) so stale entries are never read.

### Checklist

- [x] Measure the baseline with `go test -bench=. -benchmem`
- [x] Add the LRU behind an interface
- [ ] Add metrics: hit rate, evictions, and p99 latency
- [ ] Load-test with realistic traffic (see https://k6.io for a tool)

**Tip:** run `go test -race ./...` after adding the cache — data races in caches are *extremely* common and hard to spot otherwise.

---

## 12. Designing the cache layer

Great question! Let's break this down step by step. The **short answer** is that you want a *write-through* cache with a bounded size, but there are a few subtleties worth covering — especially around `invalidation` and **concurrent _readers_**.

### Why a cache at all?

Before adding complexity, measure. In our profile, `GetUser` accounted for **42%** of the request latency, and most calls hit the same ~200 keys. See the [Go blog post on profiling](https://go.dev/blog/pprof) and the [`sync.Map` docs](https://pkg.go.dev/sync#Map) for background.

> **Note:** A cache is a *trade-off*: you exchange memory and staleness for latency.
> If your data changes every second, a cache may hurt more than it helps.

Key considerations:

1. **Eviction policy** — LRU is a good default:
   - Simple to reason about
   - O(1) `Get` and `Put` with a map plus a doubly linked list
   - Works well when access has temporal locality
2. **Consistency** — choose between:
   - *write-through*: update the cache on every write
   - *write-behind*: batch writes asynchronously (riskier)
   - *cache-aside*: the application loads on a miss
3. **Concurrency** — protect shared state with a `sync.Mutex`, or shard it:
   1. Hash the key with `maphash`
   2. Pick shard `h % N`
   3. Lock only that shard

Here is a minimal implementation:

```go
type entry struct {
	key   string
	value []byte
	prev  *entry
	next  *entry
}

type LRU struct {
	mu    sync.Mutex
	cap   int
	items map[string]*entry
	head  *entry // most recently used
	tail  *entry // least recently used
}

func (c *LRU) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.moveToFront(e)
	return e.value, true
}
```

And the TypeScript client that calls it:

```ts
export async function getUser(id: string): Promise<User | undefined> {
  const res = await fetch(`/api/users/${encodeURIComponent(id)}`);
  if (!res.ok) return undefined;
  return (await res.json()) as User;
}
```

### Comparing the options

| Strategy      | Read latency | Write cost | Staleness risk | Complexity |
|---------------|-------------:|-----------:|:--------------:|------------|
| No cache      | ~52 ms        | low        | none           | trivial    |
| Cache-aside   | ~0.2 ms      | low        | medium         | low        |
| Write-through | ~0.2 ms      | medium     | low            | medium     |
| Write-behind  | ~0.2 ms      | low        | high           | high       |

As you can see, **write-through** hits the sweet spot for most services. If you need *strict* consistency, though, you'll want versioned keys (e.g. `user:42:v7`) so stale entries are never read.

### Checklist

- [x] Measure the baseline with `go test -bench=. -benchmem`
- [x] Add the LRU behind an interface
- [ ] Add metrics: hit rate, evictions, and p99 latency
- [ ] Load-test with realistic traffic (see https://k6.io for a tool)

**Tip:** run `go test -race ./...` after adding the cache — data races in caches are *extremely* common and hard to spot otherwise.

---

## 13. Designing the cache layer

Great question! Let's break this down step by step. The **short answer** is that you want a *write-through* cache with a bounded size, but there are a few subtleties worth covering — especially around `invalidation` and **concurrent _readers_**.

### Why a cache at all?

Before adding complexity, measure. In our profile, `GetUser` accounted for **43%** of the request latency, and most calls hit the same ~200 keys. See the [Go blog post on profiling](https://go.dev/blog/pprof) and the [`sync.Map` docs](https://pkg.go.dev/sync#Map) for background.

> **Note:** A cache is a *trade-off*: you exchange memory and staleness for latency.
> If your data changes every second, a cache may hurt more than it helps.

Key considerations:

1. **Eviction policy** — LRU is a good default:
   - Simple to reason about
   - O(1) `Get` and `Put` with a map plus a doubly linked list
   - Works well when access has temporal locality
2. **Consistency** — choose between:
   - *write-through*: update the cache on every write
   - *write-behind*: batch writes asynchronously (riskier)
   - *cache-aside*: the application loads on a miss
3. **Concurrency** — protect shared state with a `sync.Mutex`, or shard it:
   1. Hash the key with `maphash`
   2. Pick shard `h % N`
   3. Lock only that shard

Here is a minimal implementation:

```go
type entry struct {
	key   string
	value []byte
	prev  *entry
	next  *entry
}

type LRU struct {
	mu    sync.Mutex
	cap   int
	items map[string]*entry
	head  *entry // most recently used
	tail  *entry // least recently used
}

func (c *LRU) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.moveToFront(e)
	return e.value, true
}
```

And the TypeScript client that calls it:

```ts
export async function getUser(id: string): Promise<User | undefined> {
  const res = await fetch(`/api/users/${encodeURIComponent(id)}`);
  if (!res.ok) return undefined;
  return (await res.json()) as User;
}
```

### Comparing the options

| Strategy      | Read latency | Write cost | Staleness risk | Complexity |
|---------------|-------------:|-----------:|:--------------:|------------|
| No cache      | ~53 ms        | low        | none           | trivial    |
| Cache-aside   | ~0.2 ms      | low        | medium         | low        |
| Write-through | ~0.2 ms      | medium     | low            | medium     |
| Write-behind  | ~0.2 ms      | low        | high           | high       |

As you can see, **write-through** hits the sweet spot for most services. If you need *strict* consistency, though, you'll want versioned keys (e.g. `user:42:v7`) so stale entries are never read.

### Checklist

- [x] Measure the baseline with `go test -bench=. -benchmem`
- [x] Add the LRU behind an interface
- [ ] Add metrics: hit rate, evictions, and p99 latency
- [ ] Load-test with realistic traffic (see https://k6.io for a tool)

**Tip:** run `go test -race ./...` after adding the cache — data races in caches are *extremely* common and hard to spot otherwise.

---

## 14. Designing the cache layer

Great question! Let's break this down step by step. The **short answer** is that you want a *write-through* cache with a bounded size, but there are a few subtleties worth covering — especially around `invalidation` and **concurrent _readers_**.

### Why a cache at all?

Before adding complexity, measure. In our profile, `GetUser` accounted for **44%** of the request latency, and most calls hit the same ~200 keys. See the [Go blog post on profiling](https://go.dev/blog/pprof) and the [`sync.Map` docs](https://pkg.go.dev/sync#Map) for background.

> **Note:** A cache is a *trade-off*: you exchange memory and staleness for latency.
> If your data changes every second, a cache may hurt more than it helps.

Key considerations:

1. **Eviction policy** — LRU is a good default:
   - Simple to reason about
   - O(1) `Get` and `Put` with a map plus a doubly linked list
   - Works well when access has temporal locality
2. **Consistency** — choose between:
   - *write-through*: update the cache on every write
   - *write-behind*: batch writes asynchronously (riskier)
   - *cache-aside*: the application loads on a miss
3. **Concurrency** — protect shared state with a `sync.Mutex`, or shard it:
   1. Hash the key with `maphash`
   2. Pick shard `h % N`
   3. Lock only that shard

Here is a minimal implementation:

```go
type entry struct {
	key   string
	value []byte
	prev  *entry
	next  *entry
}

type LRU struct {
	mu    sync.Mutex
	cap   int
	items map[string]*entry
	head  *entry // most recently used
	tail  *entry // least recently used
}

func (c *LRU) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.moveToFront(e)
	return e.value, true
}
```

And the TypeScript client that calls it:

```ts
export async function getUser(id: string): Promise<User | undefined> {
  const res = await fetch(`/api/users/${encodeURIComponent(id)}`);
  if (!res.ok) return undefined;
  return (await res.json()) as User;
}
```

### Comparing the options

| Strategy      | Read latency | Write cost | Staleness risk | Complexity |
|---------------|-------------:|-----------:|:--------------:|------------|
| No cache      | ~54 ms        | low        | none           | trivial    |
| Cache-aside   | ~0.2 ms      | low        | medium         | low        |
| Write-through | ~0.2 ms      | medium     | low            | medium     |
| Write-behind  | ~0.2 ms      | low        | high           | high       |

As you can see, **write-through** hits the sweet spot for most services. If you need *strict* consistency, though, you'll want versioned keys (e.g. `user:42:v7`) so stale entries are never read.

### Checklist

- [x] Measure the baseline with `go test -bench=. -benchmem`
- [x] Add the LRU behind an interface
- [ ] Add metrics: hit rate, evictions, and p99 latency
- [ ] Load-test with realistic traffic (see https://k6.io for a tool)

**Tip:** run `go test -race ./...` after adding the cache — data races in caches are *extremely* common and hard to spot otherwise.

---

## 15. Designing the cache layer

Great question! Let's break this down step by step. The **short answer** is that you want a *write-through* cache with a bounded size, but there are a few subtleties worth covering — especially around `invalidation` and **concurrent _readers_**.

### Why a cache at all?

Before adding complexity, measure. In our profile, `GetUser` accounted for **45%** of the request latency, and most calls hit the same ~200 keys. See the [Go blog post on profiling](https://go.dev/blog/pprof) and the [`sync.Map` docs](https://pkg.go.dev/sync#Map) for background.

> **Note:** A cache is a *trade-off*: you exchange memory and staleness for latency.
> If your data changes every second, a cache may hurt more than it helps.

Key considerations:

1. **Eviction policy** — LRU is a good default:
   - Simple to reason about
   - O(1) `Get` and `Put` with a map plus a doubly linked list
   - Works well when access has temporal locality
2. **Consistency** — choose between:
   - *write-through*: update the cache on every write
   - *write-behind*: batch writes asynchronously (riskier)
   - *cache-aside*: the application loads on a miss
3. **Concurrency** — protect shared state with a `sync.Mutex`, or shard it:
   1. Hash the key with `maphash`
   2. Pick shard `h % N`
   3. Lock only that shard

Here is a minimal implementation:

```go
type entry struct {
	key   string
	value []byte
	prev  *entry
	next  *entry
}

type LRU struct {
	mu    sync.Mutex
	cap   int
	items map[string]*entry
	head  *entry // most recently used
	tail  *entry // least recently used
}

func (c *LRU) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.moveToFront(e)
	return e.value, true
}
```

And the TypeScript client that calls it:

```ts
export async function getUser(id: string): Promise<User | undefined> {
  const res = await fetch(`/api/users/${encodeURIComponent(id)}`);
  if (!res.ok) return undefined;
  return (await res.json()) as User;
}
```

### Comparing the options

| Strategy      | Read latency | Write cost | Staleness risk | Complexity |
|---------------|-------------:|-----------:|:--------------:|------------|
| No cache      | ~55 ms        | low        | none           | trivial    |
| Cache-aside   | ~0.2 ms      | low        | medium         | low        |
| Write-through | ~0.2 ms      | medium     | low            | medium     |
| Write-behind  | ~0.2 ms      | low        | high           | high       |

As you can see, **write-through** hits the sweet spot for most services. If you need *strict* consistency, though, you'll want versioned keys (e.g. `user:42:v7`) so stale entries are never read.

### Checklist

- [x] Measure the baseline with `go test -bench=. -benchmem`
- [x] Add the LRU behind an interface
- [ ] Add metrics: hit rate, evictions, and p99 latency
- [ ] Load-test with realistic traffic (see https://k6.io for a tool)

**Tip:** run `go test -race ./...` after adding the cache — data races in caches are *extremely* common and hard to spot otherwise.

---

## 16. Designing the cache layer

Great question! Let's break this down step by step. The **short answer** is that you want a *write-through* cache with a bounded size, but there are a few subtleties worth covering — especially around `invalidation` and **concurrent _readers_**.

### Why a cache at all?

Before adding complexity, measure. In our profile, `GetUser` accounted for **46%** of the request latency, and most calls hit the same ~200 keys. See the [Go blog post on profiling](https://go.dev/blog/pprof) and the [`sync.Map` docs](https://pkg.go.dev/sync#Map) for background.

> **Note:** A cache is a *trade-off*: you exchange memory and staleness for latency.
> If your data changes every second, a cache may hurt more than it helps.

Key considerations:

1. **Eviction policy** — LRU is a good default:
   - Simple to reason about
   - O(1) `Get` and `Put` with a map plus a doubly linked list
   - Works well when access has temporal locality
2. **Consistency** — choose between:
   - *write-through*: update the cache on every write
   - *write-behind*: batch writes asynchronously (riskier)
   - *cache-aside*: the application loads on a miss
3. **Concurrency** — protect shared state with a `sync.Mutex`, or shard it:
   1. Hash the key with `maphash`
   2. Pick shard `h % N`
   3. Lock only that shard

Here is a minimal implementation:

```go
type entry struct {
	key   string
	value []byte
	prev  *entry
	next  *entry
}

type LRU struct {
	mu    sync.Mutex
	cap   int
	items map[string]*entry
	head  *entry // most recently used
	tail  *entry // least recently used
}

func (c *LRU) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.moveToFront(e)
	return e.value, true
}
```

And the TypeScript client that calls it:

```ts
export async function getUser(id: string): Promise<User | undefined> {
  const res = await fetch(`/api/users/${encodeURIComponent(id)}`);
  if (!res.ok) return undefined;
  return (await res.json()) as User;
}
```

### Comparing the options

| Strategy      | Read latency | Write cost | Staleness risk | Complexity |
|---------------|-------------:|-----------:|:--------------:|------------|
| No cache      | ~56 ms        | low        | none           | trivial    |
| Cache-aside   | ~0.2 ms      | low        | medium         | low        |
| Write-through | ~0.2 ms      | medium     | low            | medium     |
| Write-behind  | ~0.2 ms      | low        | high           | high       |

As you can see, **write-through** hits the sweet spot for most services. If you need *strict* consistency, though, you'll want versioned keys (e.g. `user:42:v7`) so stale entries are never read.

### Checklist

- [x] Measure the baseline with `go test -bench=. -benchmem`
- [x] Add the LRU behind an interface
- [ ] Add metrics: hit rate, evictions, and p99 latency
- [ ] Load-test with realistic traffic (see https://k6.io for a tool)

**Tip:** run `go test -race ./...` after adding the cache — data races in caches are *extremely* common and hard to spot otherwise.

---


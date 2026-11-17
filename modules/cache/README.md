# Cache Module

*Cache module is a part of the `Ginject` framework. It gives every provider and controller a shared key-value cache with expiry and an atomic counter, backed by a bounded in-memory store by default and by any backend you write.*

- [Cache Module](#cache-module)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [`CacheModuleOptions` Parameters](#cachemoduleoptions-parameters)
    - [IsGlobal](#isglobal)
    - [OnInit](#oninit)
    - [Backend](#backend)
  - [`CacheService`](#cacheservice)
  - [`CacheService` Methods](#cacheservice-methods)
    - [Get](#get)
    - [Set](#set)
    - [SetNX](#setnx)
    - [Incr](#incr)
    - [Delete](#delete)
    - [Keys](#keys)
    - [TTL](#ttl)
  - [Errors](#errors)
  - [The `Cache` contract](#the-cache-contract)
  - [Write your own backend](#write-your-own-backend)

## Key Features

- **Ready to use:** `cache.Register(nil)` gives a bounded in-memory cache (65,536 entries, 16 MiB) that starts no goroutine and costs nothing while it is empty.
- **One cache per module value:** every injected `CacheService` from one `Register` result shares one backend, even across apps.
- **Atomic counters:** `Incr` counts correctly under concurrent requests, which `Get` followed by `Set` does not.
- **Pluggable:** any type that implements `Cache` can replace the memory store without changing application code.

## Usage

```go
package main

import (
	"context"
	"time"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/modules/cache"
)

type VisitController struct {
	common.HTTP
	Cache cache.CacheService
}

func (instance VisitController) NewController() core.Controller {
	return instance
}

func (instance VisitController) READ_visits() map[string]int64 {
	visits, err := instance.Cache.Incr(context.Background(), "visits:today", 1, 24*time.Hour)
	if err != nil {
		return map[string]int64{"visits": 0}
	}
	return map[string]int64{"visits": visits}
}

func main() {
	app := core.New()
	app.Create(
		core.ModuleBuilder().
			Imports(cache.Register(nil)).
			Controllers(VisitController{}).
			Build(),
	)
	app.Listen(8080, nil)
}
```

`GET /visits` answers `{"visits":1}`, then `{"visits":2}`, and so on for 24 hours.

## `CacheModuleOptions` Parameters

`Register(nil)` equals `Register(&cache.CacheModuleOptions{})`.

### IsGlobal

Type: `bool`

Default: `false`

Required: `false`

When `true`, `CacheService` can be injected in every module without importing the cache module there.

```go
cache.Register(&cache.CacheModuleOptions{IsGlobal: true})
```

### OnInit

Type: `CacheOnInitFn` (`func()`)

Default: `nil`

Required: `false`

Runs once when the app that imports the module is created.

```go
cache.Register(&cache.CacheModuleOptions{
	OnInit: func() { fmt.Println("cache ready") },
})
```

### Backend

Type: `cache.Cache`

Default: `memorycache.NewMemoryCache(nil)`

Required: `false`

The store behind the service, used as is when set. The default memory cache holds at most 65,536
entries and 16 MiB of keys and values; for other bounds, pass your own:

```go
cache.Register(&cache.CacheModuleOptions{
	Backend: memorycache.NewMemoryCache(&memorycache.MemoryCacheOptions{MaxEntries: 200_000, MaxBytes: 64 << 20}),
})
```

See `memorycache/README.md` for its bounds, eviction and memory cost.

## `CacheService`

```go
type CacheService struct {
	Backend Cache
}
```

The injectable provider. Declare a `cache.CacheService` field in a provider or controller and the
framework fills it. `Register` creates the backend once, when it is called, so every copy of the
service injected from that module value uses the same backend. Creating the module starts no
goroutine and no timer.

## `CacheService` Methods

Each method forwards its arguments to `Backend` and returns the backend's results unchanged. The
rules below are those of the default memory backend, and the contract every backend must meet.

### Get

```go
func (cs *CacheService) Get(ctx context.Context, key string) ([]byte, bool)
```

Returns a copy of the value and `true` for a live entry, `(nil, false)` for a missing key, an
expired entry or an empty key. A value stored as `nil` reads back as an empty, non-nil slice.

```go
raw, ok := instance.Cache.Get(ctx, "user:1")
```

### Set

```go
func (cs *CacheService) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
```

Stores a copy of `val` and replaces the expiry. `ttl` 0 never expires; a negative `ttl` is an error.
Values are bytes: encode structs with `encoding/json` or any serializer.

```go
data, _ := json.Marshal(user)
err := instance.Cache.Set(ctx, "user:1", data, 5*time.Minute)
```

### SetNX

```go
func (cs *CacheService) SetNX(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error)
```

Writes only when the key has no live entry and returns `true` if it wrote. The check and the write
are one atomic step, so concurrent callers get `true` exactly once: use it for locks and run-once
work.

```go
ok, err := instance.Cache.SetNX(ctx, "lock:report", []byte("1"), time.Minute)
```

### Incr

```go
func (cs *CacheService) Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error)
```

Adds `delta` to a counter and returns the new value, atomically. A missing or expired key starts
at `delta` with `ttl`; an existing counter keeps its expiry (`ttl` is ignored), which is what a
fixed time window needs. A counter is 8 bytes, big-endian, so `Get` and `Set` read and write it too.
A key that holds another value returns `ErrNotCounter`.

```go
n, err := instance.Cache.Incr(ctx, "rate:"+clientIP, 1, time.Minute)
```

### Delete

```go
func (cs *CacheService) Delete(ctx context.Context, key string) error
```

Removes the entry. A missing key is not an error; an empty key returns `ErrEmptyKey`.

### Keys

```go
func (cs *CacheService) Keys(ctx context.Context) []string
```

Returns the live keys in no particular order, never `nil`. It may visit every entry: use it in
tools and tests, not on a request path.

### TTL

```go
func (cs *CacheService) TTL(ctx context.Context, key string) (time.Duration, bool)
```

Returns `(remaining, true)` for an entry that expires, `(0, true)` for one that never expires, and
`(0, false)` for a missing key, an expired entry or an empty key.

## Errors

```go
var (
	ErrEmptyKey      = memorycache.ErrEmptyKey
	ErrNegativeTTL   = memorycache.ErrNegativeTTL
	ErrEntryTooLarge = memorycache.ErrEntryTooLarge
	ErrNotCounter    = memorycache.ErrNotCounter
)
```

| Error | When |
|---|---|
| `ErrEmptyKey` | `Set`, `SetNX`, `Incr` or `Delete` got the key `""`. |
| `ErrNegativeTTL` | `Set`, `SetNX` or `Incr` got a `ttl` below zero. Nothing is written. |
| `ErrEntryTooLarge` | The entry is larger than the backend allows (256 KiB of key and value with the default memory cache). |
| `ErrNotCounter` | `Incr` found a live value that is not 8 bytes. |

They are the same values as the `memorycache` errors. Compare them with `errors.Is`:

```go
if err := instance.Cache.Set(ctx, key, val, ttl); errors.Is(err, cache.ErrEntryTooLarge) {
	return err
}
```

## The `Cache` contract

```go
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, bool)
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
	SetNX(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error)
	Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error)
	Delete(ctx context.Context, key string) error
	Keys(ctx context.Context) []string
	TTL(ctx context.Context, key string) (time.Duration, bool)
}
```

A backend meets the rules of the methods above:

- An entry written with `ttl > 0` is live until `ttl` has passed and absent from that instant for every method. `ttl == 0` never expires; `ttl < 0` returns `ErrNegativeTTL`.
- Values are copied in and out: changing a slice passed to a write or returned by `Get` never changes the stored value.
- `SetNX` and `Incr` are atomic per key.
- A counter is 8 bytes, big-endian two's complement, and overflow wraps like `int64`, so `Get`, `Set` and `Incr` mix on one key on every backend.
- Errors are the `cache.Err…` values (or wrap them), checked with `errors.Is`. `ErrEntryTooLarge` only applies to a backend with a size limit.
- Whether a backend evicts entries, and which ones, is its own choice.
- `ctx` is for backends that do I/O; the memory backend ignores it.

## Write your own backend

Implement the seven methods and pass the value as `Backend`. This backend puts every key of an app
under a prefix in another cache; a Redis or Memcached backend has the same shape:

```go
type prefixed struct {
	prefix string
	next   cache.Cache
}

func (p prefixed) Get(ctx context.Context, key string) ([]byte, bool) {
	return p.next.Get(ctx, p.prefix+key)
}

func (p prefixed) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	return p.next.Set(ctx, p.prefix+key, val, ttl)
}

func (p prefixed) SetNX(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error) {
	return p.next.SetNX(ctx, p.prefix+key, val, ttl)
}

func (p prefixed) Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	return p.next.Incr(ctx, p.prefix+key, delta, ttl)
}

func (p prefixed) Delete(ctx context.Context, key string) error {
	return p.next.Delete(ctx, p.prefix+key)
}

func (p prefixed) Keys(ctx context.Context) []string {
	keys := []string{}
	for _, key := range p.next.Keys(ctx) {
		if rest, ok := strings.CutPrefix(key, p.prefix); ok {
			keys = append(keys, rest)
		}
	}
	return keys
}

func (p prefixed) TTL(ctx context.Context, key string) (time.Duration, bool) {
	return p.next.TTL(ctx, p.prefix+key)
}

var Module = cache.Register(&cache.CacheModuleOptions{
	Backend: prefixed{prefix: "shop:", next: memorycache.NewMemoryCache(nil)},
})
```

Application code keeps injecting `cache.CacheService`; only the module's options change.

# Inspect

*Package `inspect` lets developer tools read the run-time state of built-in components (a memory cache, the config module, the HTTP middlewares) without giving those components any exported method for it.*

- [Inspect](#inspect)
  - [Usage](#usage)
  - [Types](#types)
    - [CacheState](#cachestate)
    - [ConfigState](#configstate)
    - [Field](#field)
    - [Snapshot](#snapshot)
    - [Snapshotter](#snapshotter)
  - [Middleware snapshots](#middleware-snapshots)
  - [Variables](#variables)
    - [CacheStateOf](#cachestateof)
    - [ConfigStateOf](#configstateof)
  - [Cost](#cost)

The package is internal: only code in the Ginject module can import it. It holds plain state structs
and one function variable per component. The component's own package sets its variable in `init`
(`memorycache` sets `CacheStateOf`, `modules/config` sets `ConfigStateOf`). There is no registry of
instances: a tool passes the value it found (for example a provider from the container), and nothing
is kept alive or computed until the function is called. The HTTP middlewares take the other road:
their compiled value implements `Snapshotter` directly, since it is already unexported.

## Usage

```go
package main

import (
	"fmt"

	"github.com/dangduoc08/ginject/internal/inspect"
	"github.com/dangduoc08/ginject/memorycache"
)

func main() {
	mc := memorycache.NewMemoryCache(nil)
	if state, ok := inspect.CacheStateOf(mc); ok {
		fmt.Println(state.Entries, state.MaxEntries, state.IsSweeping)
	}
}
```

Output:

```console
0 65536 false
```

## Types

### CacheState

A snapshot of one `*memorycache.MemoryCache`, taken when `CacheStateOf` is called.

| Field | Type | Meaning |
|---|---|---|
| `Entries` | `int` | Entries stored, summed over the shards at read time. Expired entries that are not removed yet are counted. |
| `MaxEntries` | `int` | The effective entry bound (after rounding). |
| `Bytes` | `int64` | Key bytes plus value bytes stored. |
| `MaxBytes` | `int64` | The effective byte bound. |
| `Hits` | `uint64` | `Get` calls that returned `true`. |
| `Misses` | `uint64` | `Get` calls that returned `false` (missing, expired or empty key). |
| `Evictions` | `uint64` | Live entries removed to make room. |
| `Expirations` | `uint64` | Expired entries removed (by the sweep, by eviction, or overwritten in place). |
| `IsSweeping` | `bool` | Whether the sweep timer is armed. |

### ConfigState

The state of one `config.ConfigService`. It never holds a value.

| Field | Type | Meaning |
|---|---|---|
| `Files` | `[]string` | The `.env` files read, in order. Empty when none was read. |
| `Keys` | `[]string` | Every key name of the configuration, sorted. |

### Field

One named value of a snapshot.

| Field | Type | Meaning |
|---|---|---|
| `Key` | `string` | The name, in snake case (`trusted_origins`), or the header name for `helmet` |
| `Value` | `any` | A `string`, `bool`, `int` (`logpipe` only), `int64`, `float64`, `time.Duration` or `[]string` |

### Snapshot

| Field | Type | Meaning |
|---|---|---|
| `Name` | `string` | The component: `proxy`, `hostcheck`, `httpsredirect`, `csrf`, `cors`, `helmet`, `compress`, `logpipe` |
| `Fields` | `[]Field` | Its effective configuration, then its counters |

### Snapshotter

```go
type Snapshotter interface{ Snapshot() Snapshot }
```

Implemented by the compiled value `NewMiddleware` returns. `Snapshot` is safe from any goroutine at
any time: it builds a new value from atomic loads when called, and nothing calls it on the request
path. Returned slices are copies.

```go
mw := proxy.Proxy{TrustedProxies: []string{"10.0.0.0/8"}}.NewMiddleware()
if s, ok := mw.(inspect.Snapshotter); ok {
	fmt.Println(s.Snapshot().Name)
}
```

## Middleware snapshots

| `Name` | Configuration fields | Counters (`int64`) |
|---|---|---|
| `proxy` | `mode` (`proxies` or `hops`), `trusted` (prefixes), `hops`, `header` (`x-forwarded` or `forwarded`) | `resolved` (requests resolved from headers), `untrusted` (requests from untrusted peers) |
| `hostcheck` | `hosts` | `refused` |
| `httpsredirect` | `host`, `port` | `redirected` |
| `csrf` | `trusted_origins` | `refused` |
| `cors` | `allow_origins`, `allow_methods`, `allow_headers`, `expose_headers`, `max_age` (`time.Duration`), `allow_credentials`, `allow_private_network` | `preflights` (answered for a matching origin), `refused` (origins that did not match) |
| `helmet` | one field per configurable header, keyed by header name, with the value sent or `-` when off | none |
| `compress` | `min_size`, `level`, `writer_cap` | `writers_in_use`, `compressed`, `uncompressed_no_writer`, `bytes_in`, `bytes_out` |
| `logpipe` (not a middleware: the stdout sink of the built-in logger, `logpipe.Stdout()`) | `wake_delay` (`time.Duration`), `wake_size`, `batch_size`, `queue_limit`, `soft_limit`, `async` (`int`), then the state `queued`, `writing`, `timer_armed` | `wakes`, `writes`, `dropped_debug`, `dropped_info`, `dropped_warn`, `dropped_error` |

Only `proxy`'s two counters and `cors`'s preflight counter are updated on a request that passes
(one atomic add each); the others count refusals or compressed responses only.

## Variables

### CacheStateOf

```go
var CacheStateOf func(backend any) (CacheState, bool)
```

Returns `(state, true)` for a `*memorycache.MemoryCache` and `(CacheState{}, false)` for anything
else, a custom `cache.Cache` backend and `nil` included.

### ConfigStateOf

```go
var ConfigStateOf func(provider any) (ConfigState, bool)
```

Returns `(state, true)` for a `config.ConfigService` (or a pointer to one) created by
`config.Register`, and `(ConfigState{}, false)` for anything else.

Both variables return `(zero, false)` until the component's package is linked into the program.

## Cost

Nothing runs until a function or `Snapshot` is called. Reading a cache takes each of its 64 shard read locks in
turn and sums the counters; reading the config sorts its key names.

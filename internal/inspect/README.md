# Inspect

*Package `inspect` lets developer tools read the run-time state of built-in components (a memory cache, the config module) without giving those components any exported method for it.*

- [Inspect](#inspect)
  - [Usage](#usage)
  - [Types](#types)
    - [CacheState](#cachestate)
    - [ConfigState](#configstate)
  - [Variables](#variables)
    - [CacheStateOf](#cachestateof)
    - [ConfigStateOf](#configstateof)
  - [Cost](#cost)

The package is internal: only code in the Ginject module can import it. It holds plain state structs
and one function variable per component. The component's own package sets its variable in `init`
(`memorycache` sets `CacheStateOf`, `modules/config` sets `ConfigStateOf`). There is no registry of
instances: a tool passes the value it found (for example a provider from the container), and nothing
is kept alive or computed until the function is called.

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

Nothing runs until a function is called. Reading a cache takes each of its 64 shard read locks in
turn and sums the counters; reading the config sorts its key names.

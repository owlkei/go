# 0015: Config, cache module and a bounded memory cache

Status: Approved
Packages: `memorycache`, `modules/cache`, `modules/config`, `internal/inspect` (new)

Roadmap: `docs/roadmap/0005-middlewares-modules-security.md`, step 11. Written in parallel with 0013
and 0014; what they need from this design is in Coordination.

## Goal
`memorycache` holds a bounded amount of memory whatever keys it is given, costs nothing while it is
empty (no goroutine, no timer), expires entries exactly, and offers an atomic counter. `modules/cache`
states the contract a backend must meet and is tested. `modules/config` parses `.env` files strictly
and safely, never prints a value, bounds what it reads, and is free of data races.

## What is wrong today (measured or read from the code)
| # | Where | Problem |
|---|---|---|
| 1 | `memorycache.NewMemoryCache` | One goroutine and a ticker every `sweepEvery / numShards` (19.5 ms, about 51 wakeups a second) per cache, forever, even when empty. `cache.Register` creates the cache when the package variable is initialized, the throttler one per guard, and neither calls `Stop` |
| 2 | `memorycache` | No bound. 5.2 million distinct 30-byte keys with 8-byte values: 680 MB of heap (measured). It is the throttler's default store |
| 3 | `memorycache` | An empty cache allocates 256 maps (22.8 KB); expiry uses the wall clock (`time.Now().UnixNano()`), so a clock step moves every deadline; a negative TTL makes an entry immortal; the map keeps the caller's key string (a substring pins its whole parent) |
| 4 | `modules/cache` | No tests. The README documents 64 shards, FNV, a 30 s sweep and `cache.ErrEmptyKey`, none of which exist. No atomic counter: `Get` then `Set` loses counts |
| 5 | `config` parser (`DotENV.Unmarshal`) | `URL=postgres://h/db?sslmode=disable` loses the second `=`; `KEY=YWJj==` becomes `YWJj`; `P=pa#ss` becomes `pa`; `P=C:\new` gets a real newline (escapes run on unquoted values); `A="x"y` creates a key `y`; an unterminated quote swallows the rest of the file; `export A=1`, a BOM and invalid lines are dropped silently; single quotes are kept in the value |
| 6 | `config` expansion (`parseParamsToValue`) | `A=${B}`, `B=${C}`, `C=${B}` recurses until the stack overflows (only a direct self reference is caught); `A=${B}${B}` chains grow exponentially with no limit |
| 7 | `config.Register` | `IsIgnoreEnvFile` has no effect when a `.env` exists (`loadConfigOptions` adds the default path, then `!IsIgnoreEnvFile \|\| len(paths) > 0` is true); `os.ReadFile` has no size limit and blocks forever on a FIFO or `/dev/zero`; failures are raw `panic(err)` outside the bootstrap format |
| 8 | `ConfigService` | `Config` is an exported `map[string]any` holding the whole process environment: `%+v` or a JSON log of any struct that holds the service prints every secret. `Set` after startup is a data race (documented, not prevented). `Transform` panics on any non-string value (`val.(string)`), which every nested `Loads` result contains |

## Public API

### `memorycache`
```go
type MemoryCacheOptions struct {
	MaxEntries int   // default 65536; rounded down to a multiple of 64, minimum 64
	MaxBytes   int64 // default 16 << 20; key bytes + value bytes; minimum 64 KiB
}

func NewMemoryCache(opts *MemoryCacheOptions) *MemoryCache // was NewMemoryCache(); nil = defaults

func (mc *MemoryCache) Get(ctx context.Context, key string) ([]byte, bool)
func (mc *MemoryCache) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
func (mc *MemoryCache) SetNX(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error)
func (mc *MemoryCache) Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) // new
func (mc *MemoryCache) Delete(ctx context.Context, key string) error
func (mc *MemoryCache) Keys(ctx context.Context) []string
func (mc *MemoryCache) TTL(ctx context.Context, key string) (time.Duration, bool)

var (
	ErrEmptyKey      = errors.New("memorycache: key must not be empty")
	ErrNegativeTTL   = errors.New("memorycache: ttl must not be negative")         // new
	ErrEntryTooLarge = errors.New("memorycache: entry is larger than the cache allows") // new
	ErrNotCounter    = errors.New("memorycache: value is not a counter")           // new
)
```
Removed: `(*MemoryCache).Stop` (nothing left to stop).

Exported additions and why users need them: `MemoryCacheOptions` (the two bounds are the only
tuning; the throttler and large apps set them), `Incr` (the only correct way to count), three
sentinels (callers must tell the cases apart).

### `modules/cache`
```go
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, bool)
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
	SetNX(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error)
	Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) // new
	Delete(ctx context.Context, key string) error
	Keys(ctx context.Context) []string
	TTL(ctx context.Context, key string) (time.Duration, bool)
}

func (cs *CacheService) Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) // new

var (
	ErrEmptyKey      = memorycache.ErrEmptyKey
	ErrNegativeTTL   = memorycache.ErrNegativeTTL
	ErrEntryTooLarge = memorycache.ErrEntryTooLarge
	ErrNotCounter    = memorycache.ErrNotCounter
)
```
`CacheModuleOptions`, `Register` and `CacheService.Backend` are unchanged. There is no size option on
the module: another size is `Backend: memorycache.NewMemoryCache(&memorycache.MemoryCacheOptions{…})`.
The sentinels are the same values under the `cache` name (a backend returns them; `modules/cache`
imports `memorycache`, so they cannot be declared the other way round).

### `modules/config`
```go
type ConfigService struct {
	State *configState // was: Config map[string]any
}
```
`Get`, `Set`, `Transform`, `NewProvider`, `ConfigModuleOptions`, the three function types and
`Register` keep their signatures. Removed: `ConfigService.Config`, the `DotENV` type and its
`Unmarshal` method (users never needed the parser). `State` is exported only because the container
refuses unexported provider fields (`injectDependencies`, `core/fn.go`); its type is unexported and
has no exported field or method.

New bootstrap panic category: `invalid config: <detail>` (`color.FmtRed`, like the others).

### `internal/inspect` (new, internal)
```go
type CacheState struct {
	Entries, MaxEntries                    int
	Bytes, MaxBytes                        int64
	Hits, Misses, Evictions, Expirations   uint64
	IsSweeping                             bool
}
type ConfigState struct {
	Files []string // the files read, in order
	Keys  []string // sorted key names; never a value
}

var CacheStateOf  func(backend any) (CacheState, bool)   // set by memorycache
var ConfigStateOf func(provider any) (ConfigState, bool) // set by modules/config
```
No public API: see Coordination › Devtools.

## Behavior

### memorycache (M)
Storage: 64 shards (was 256), chosen by `hashKey(key) & 63`. A shard may hold `MaxEntries / 64`
entries and `MaxBytes / 64` bytes, where an entry's bytes are `len(key) + len(val)`.

- **M1** `NewMemoryCache(nil)` equals `NewMemoryCache(&MemoryCacheOptions{})`: 65,536 entries and
  16 MiB. A zero or negative field takes its default; `MaxEntries` is rounded down to a multiple of
  64 with a minimum of 64; `MaxBytes` below 64 KiB becomes 64 KiB.
- **M2** Creating a cache starts no goroutine and no timer and allocates no shard storage
  (`runtime.NumGoroutine` unchanged; the sweep is not running).
- **M3** Liveness: an entry written at time `t` with `ttl > 0` is live while `now - t < ttl` and
  expired from `now - t == ttl` on. `ttl == 0` never expires. `ttl < 0` returns `ErrNegativeTTL`
  and writes nothing (today: immortal). A `ttl` whose deadline overflows `int64` never expires.
  Time is the monotonic clock: a wall-clock change moves no deadline.
- **M4** An expired entry is absent for every operation, whether or not a sweep has run: `Get`
  misses, `TTL` returns `(0, false)`, `Keys` omits it, `SetNX` and `Incr` treat the key as new.
- **M5** `Get` returns a copy of the value and `true` for a live entry, `(nil, false)` otherwise
  (missing, expired, empty key). The value of an empty or nil `val` reads back as an empty non-nil
  slice. Changing the returned slice, or the slice passed to a write, never changes the stored value.
- **M6** `Set` stores the value and replaces the expiry (`ttl` as in M3). Errors, in this order,
  each writing nothing: empty key `ErrEmptyKey`; negative ttl `ErrNegativeTTL`;
  `len(key) + len(val) > MaxBytes / 64` `ErrEntryTooLarge`. Otherwise it returns `nil`, including on
  a full cache (M11): it never blocks on capacity and never fails for lack of room.
- **M7** `SetNX` writes as `Set` and returns `true` only if the key has no live entry; otherwise
  `(false, nil)` and nothing changes (value, expiry). Same errors as `Set`. Check and write are one
  atomic step: N concurrent `SetNX` of one key return `true` exactly once.
- **M8** `Incr` treats the value as a counter: 8 bytes, big-endian two's complement.
  - No live entry: stores `delta` with `ttl` (M3) and returns `delta`.
  - Live counter: adds `delta`, returns the sum, **keeps the expiry** (`ttl` is ignored). Overflow
    wraps like Go's `int64`.
  - Live entry whose value is not 8 bytes: `ErrNotCounter`, nothing changes.
  - Errors as `Set` (empty key, negative ttl). `delta` may be zero or negative.
  - Atomic: N concurrent `Incr(key, 1, ttl)` return each of `1..N` exactly once.
  - `Get` on a counter returns its 8 bytes; `Set` with 8 bytes writes a counter.
- **M9** `Delete` removes the entry; a missing key is not an error; an empty key is `ErrEmptyKey`.
- **M10** `TTL` returns `(remaining, true)` with `0 < remaining <= ttl` for an entry that expires,
  `(0, true)` for one that does not, `(0, false)` for no live entry or an empty key. `Keys` returns
  the live keys in no particular order, never nil.
- **M11** Bounds: at every instant a shard holds at most `MaxEntries / 64` entries and
  `MaxBytes / 64` bytes, so the cache never exceeds `MaxEntries` and `MaxBytes`. A write that would
  exceed a bound first evicts entries of **that shard** until it fits. A write to an existing key
  that does not grow its bytes evicts nothing. The written key is never its own victim.
- **M12** Eviction order (CLOCK, one hand per shard). Each entry has a reference mark: clear when
  the entry is created, set by a `Get` hit and by a `Set`, `SetNX` or `Incr` that writes an existing
  key. The hand walks the shard's entries in a fixed circular order: an expired entry is removed
  (counted as an expiration); a live entry with a clear mark is removed (counted as an eviction); a
  live entry with a set mark has its mark cleared and is skipped. Testable consequences, with keys
  of one shard and a shard of capacity `n`:
  - full shard, `e` entries expired, the rest live: the next `e` inserts of new keys evict no live
    entry;
  - full shard of live entries, `r` of them read or rewritten since they were created, `n - r` not:
    the next `n - r` inserts of new keys remove none of the `r`;
  - a key that is read or written at least once between two visits of the hand is never evicted.
- **M13** Sweep. One `time.AfterFunc` timer per cache, armed only while the cache holds at least one
  entry with an expiry; period 5 s (`sweepEvery`). A pass visits every shard once (one shard lock
  at a time), removes every expired entry, and releases the storage of a shard left with no entry.
  After a pass with no entry with an expiry left, the timer is not re-armed. So:
  - a cache whose entries all have `ttl == 0` never arms the timer;
  - after the last entry with an expiry is written with ttl `d`, at most `d + 5 s` later the cache
    holds no such entry and at most 5 s after that the sweep is not running (zero timers);
  - deleting the last such entry stops the sweep at its next pass, not at once.
- **M14** The timer holds the cache through a `weak.Pointer`: a cache nobody references is
  collected even with a timer pending, and the next firing does nothing and does not re-arm. There
  is no `Stop`.
- **M15** Keys are copied when an entry is created (`strings.Clone`): the cache never keeps a
  caller's backing array alive, so `len(key)` is the memory a key holds.
- **M16** Snapshot (`internal/inspect`): `Entries` and `Bytes` are the sums over shards at read
  time (expired entries not yet removed are counted), `MaxEntries` / `MaxBytes` the effective
  bounds, `Hits` / `Misses` count `Get` calls (hit = M5 returned `true`; an empty key is a miss),
  `Evictions` and `Expirations` count removals as named in M12 and M13 (an expired entry
  overwritten in place counts as an expiration), `IsSweeping` whether the timer is armed.
  `inspect.CacheStateOf(x)` returns `(state, true)` for a `*MemoryCache` and `(zero, false)` for
  anything else.

### `cache.Cache` contract and the module (C)
- **C1** The contract of a backend is M3 to M10 with these substitutions: sentinels are compared
  with `errors.Is` against the `cache.Err…` values; `ErrEntryTooLarge` is returned by a backend that
  has a size limit; whether a backend evicts, and how, is its own business. A counter is 8 bytes
  big-endian on every backend, so `Get`, `Set` and `Incr` mix on one key everywhere. `ctx` is for
  backends that do I/O; `memorycache` ignores it. `Keys` may be expensive (O(entries)): it is for
  tools and tests, not request paths.
- **C2** `cache.Register(nil)` equals `cache.Register(&cache.CacheModuleOptions{})`: a module with
  one provider, `CacheService`, whose `Backend` is `memorycache.NewMemoryCache(nil)`. With
  `Backend` set, that value is used as is. `IsGlobal` and `OnInit` are copied to the module.
- **C3** Calling `Register` starts no goroutine and no timer (it runs when package variables are
  initialized, before `main`).
- **C4** Every `CacheService` injected from one `Register` result shares one backend: a value set
  through one injected copy is read through another, in the same app and in two apps created from
  the same module value.
- **C5** Each `CacheService` method forwards its arguments and returns the backend's results
  unchanged (seven methods, `Incr` included).
- **C6** `cache.ErrEmptyKey == memorycache.ErrEmptyKey`, and the same for the other three.

### config (E)
File format. A file is UTF-8 text; a leading byte-order mark is skipped; lines end with LF or CRLF.

- **E1** A line that is empty or whose first non-blank byte is `#` is ignored. Any other line is
  `[export ]KEY=value`: optional blanks, an optional `export` followed by blanks, a key matching
  `[A-Za-z_][A-Za-z0-9_]*`, optional blanks, `=`, the value. Anything else panics (E9).
- **E2** Unquoted value: the rest of the line with leading and trailing blanks removed. A `#`
  preceded by a blank starts a comment; any other `#` is part of the value. No escape is
  processed. `=` is an ordinary byte. Examples: `URL=postgres://h/db?sslmode=disable` →
  `postgres://h/db?sslmode=disable`; `KEY=YWJj==` → `YWJj==`; `P=pa#ss` → `pa#ss`; `P=pa #ss` →
  `pa`; `P=C:\new` → `C:\new` (backslash and `n`); `A=` → empty string.
- **E3** Double-quoted value (`"` is the first byte of the value): runs to the next unescaped `"`,
  may span lines, keeps inner blanks. Escapes: `\n`, `\r`, `\t`, `\\`, `\"`, `\$`; any other
  backslash stays as written. After the closing quote only blanks and a comment may follow.
- **E4** Single-quoted value: runs to the next `'`, may span lines, no escape, never expanded.
- **E5** A key repeated in one file takes its last value.
- **E6** Expansion, only with `IsExpandVariables`: `${NAME}` in unquoted and double-quoted values is
  replaced by the value of `NAME` in the same file (wherever it is defined), else by the process
  environment variable `NAME`. `\$` in a double-quoted value is a literal `$`. `$NAME` without
  braces is never expanded. Without `IsExpandVariables`, `${NAME}` stays as written.
- **E7** Limits: a file is at most 1 MiB and must be a regular file; a value is at most 64 KiB
  after expansion; a NUL byte is refused. Expansion visits each variable once per chain, so a cycle
  of any length is found without recursion deeper than the number of keys.
- **E8** Paths. With `ENVFilePaths` empty, the default is `.env` in the nearest directory, from the
  working directory upward, that contains `go.mod`, else in the working directory; a missing default
  file is not an error. Every path in `ENVFilePaths` must exist. With `IsIgnoreEnvFile` no file is
  read, default included (fixes row 7); `IsIgnoreEnvFile` together with a non-empty `ENVFilePaths`
  panics.
- **E9** Panics, all at `Register`, all `invalid config: <detail>`:
  - `<path>: <the os error>` (missing, unreadable), `<path>: not a regular file`,
    `<path>: larger than 1 MiB`;
  - `<path>:<line>: expected KEY=value`, `<path>:<line>: invalid key`,
    `<path>:<line>: unterminated quote` (the line where the quote opens),
    `<path>:<line>: unexpected text after the closing quote`, `<path>:<line>: NUL byte`;
  - `<path>:<line>: ${NAME} is not defined`, `<path>:<line>: ${NAME} refers to itself` (any cycle),
    `<path>:<line>: value larger than 64 KiB`;
  - `IsIgnoreEnvFile is set with ENVFilePaths`; `Loads[<i>]: nested deeper than 32 levels`.
- **E10** No secret in output: a panic message, error or log line of this package contains the
  path, the line number and, for E6, the variable **name**; never a value and never the text of a
  line. `fmt.Sprintf("%v")`, `"%+v"`, `"%#v"`, `json.Marshal` and `slog` of a `ConfigService`, or of
  a struct that holds one, contain no key and no value.
- **E11** Precedence, unchanged: process environment, then files in the order given, then `Loads`
  in the order given; the first source that defines a key wins. With `IsOverride` the last source
  that defines it wins. Values from files are not written to the process environment
  (`os.Getenv` does not see them).
- **E12** `Loads`: nested maps and slices are flattened to dotted keys as today (`database.port`,
  `list.0`), the parent keys kept; nesting deeper than 32 levels panics (E9).
- **E13** `Get` returns the value or `nil`; a zero `ConfigService{}` returns `nil`. `Set` stores a
  value. Both are safe from any number of goroutines at any time (`-race` clean); a `Get` that runs
  with a `Set` sees the old or the new value. `Set` on a zero `ConfigService{}` panics
  `invalid config: ConfigService was not created by Register`.
- **E14** Hooks run in order after the merge, each receives the service, and what a hook `Set`s is
  seen by later hooks and by every injected `ConfigService` (they share one state).
- **E15** `Transform` binds string values as today; `bool`, integer and float values are bound from
  their `strconv` text; any other value (map, slice, nil) is skipped. It never panics on a value's
  type (fixes row 8).

## Performance

Measured with prototypes of the four candidates behind one interface
(scratchpad `d0015-cache-proto`, Go 1.27.4, darwin/amd64, i7-9750H, 12 threads; the machine was
shared with another benchmark run, so differences under about 15 % are noise; the builder repeats
them with `-count 5` and `benchstat`). 30-byte keys, 8-byte values, 65,536-entry bound, 64 shards.

**Eviction candidates** (ns/op at GOMAXPROCS 1 / 2 / 12):

| | today (unbounded) | random | soonest expiry of 5 | **CLOCK** | exact LRU |
|---|---|---|---|---|---|
| `Get` hit | 161 / 88 / 32 | 134 / 65 / 26 | 116 / 65 / 26 | 111 / 72 / 27 | 115 / 78 / 43 |
| `Set` existing key | 175 / 119 / 45 | 179 / 134 / 57 | 122 / 95 / 46 | 116 / 83 / 44 | 131 / 88 / 44 |
| `Set` new key, cache full | 871 / 329 / 100 | 589 / 340 / 127 | 699 / 433 / 157 | 430 / 242 / 116 | 419 / 242 / 116 |
| hit ratio, Zipf 1.01 / 1.2, 1 M keys | - | 71.0 % / 92.0 % | 71.0 % / 92.0 % | 75.7 % / 93.5 % | 75.0 % / 93.3 % |
| active counters lost, flood of 25 % / 90 % of capacity between two requests of a client | - | 24 % / 60 % | 21 % / 65 % | 0 % / 0 % | 0 % / 0.2 % |
| same, flood of 100 % / 400 % of capacity | - | 65 % / 96 % | 71 % / 99.7 % | 79 % / 100 % | 83 % / 100 % |
| heap per entry | 113 B | 153 B | 153 B | 150 B | 157 B |
| sweep pass over 65 k entries | 1.35 ms (map) | 1.35 ms | 1.35 ms | 0.125 ms (array) | list walk |

Choice: **CLOCK**. It matches exact LRU on hit ratio and on protecting active keys, reads under the
shard's read lock (LRU needs the write lock on every `Get`: 43 ns against 27 ns at 12 threads), and
its entry array makes the sweep ten times cheaper than iterating a map. Random and soonest-expiry
eviction reset a quarter of the active rate-limit counters under a flood of a quarter of the
capacity; soonest expiry also cannot rank fixed-window keys, which expire together. No policy
protects a key when a full capacity of new keys arrives between two of its requests: that limit is
the throttler's to state (Coordination).

**Per operation, chosen design** (GOMAXPROCS 1 / 2 / 12):

| Operation | ns/op | allocs |
|---|---|---|
| `Get` hit | 111 / 72 / 27 | 1 (the returned copy) |
| `Get` hit with the hit counter (M16) | 122 / 66 / 25 | 1 |
| `Set` existing key, value fits the stored buffer | 116 / 83 / 44 | 0 (today 1) |
| `Set` new key at the bound (evicts one) | 430 / 242 / 116 | 2 (key copy, value) |
| `Incr` existing counter, 4,096 keys | 111 / 74 / 35 | 0 |
| `Incr`, one key from every thread | 91 / 95 / 205 | 0 |
| today's `Get` + `Set` counter (not atomic) | 370 / 192 / 80 | 2 |

About 44 ns of every operation on this machine is the clock read (`time.Since`: 44 ns; today's
`time.Now().UnixNano()`: 90 ns).

**Shards**: 64 instead of 256. Mixed 7:1 workload at GOMAXPROCS 1 / 2 / 12: 121 / 67 / 56 ns with 64,
125 / 74 / 32 ns with 256, 127 / 89 / 77 ns with 16. The gain of 256 exists only on many cores; it
costs four times the idle memory, a first eviction at 84 % of the bound instead of 93 % (per-shard
limits), and a largest entry of `MaxBytes / 256`.

**Idle and memory**
- Empty cache: 0 goroutines, 0 timers, 0 wakeups, about 5 KB (64 shard headers; storage is
  allocated on a shard's first write). Today: 1 goroutine, 51 wakeups a second, 22.8 KB.
- With entries that expire: one timer, one wakeup every 5 s, a pass of about 2 ns per entry
  (0.125 ms for 65,536 entries: 0.0025 % of one core). The firing runs on a short-lived goroutine.
- With only `ttl == 0` entries: the same as empty.
- Bound: measured 150 B of heap per entry for a 30-byte key and an 8-byte value, about 110 B of it
  fixed. Heap at the bound is about `entries × 110 B + bytes × 1.13` (size classes). Defaults:
  65,536 × 110 B + 16 MiB × 1.13, at most about 25 MiB, reached only by a cache that is full of
  large values. 5.2 million distinct keys through the default cache: 65,536 entries, 9.8 MB of
  heap (today 680 MB).
- Config is cold (bootstrap). `ConfigService.Get` is one atomic load and one map lookup, 0 allocs.

**Techniques for approval** (each with the numbers above):
1. **Reusing the stored value's buffer** when the new value fits its capacity (`Set` on an existing
   key, `Incr`): removes the allocation of every counter update (1 → 0 allocs, 175 → 116 ns at
   GOMAXPROCS 1). Constraint: `Get` must copy while it holds the read lock (today it copies after
   unlocking), and nothing may ever return the stored slice.
2. **Padding each shard to a multiple of 64 bytes** so two shards never share a cache line
   (hand-managed layout). No `unsafe`, no `sync.Pool`.

The two regexps of `config/fn.go` become byte loops (type A, cold).

## Concurrency
- **Shard**: `sync.RWMutex` guards the index map, the entry array, the free list, the hand, the byte
  count and the count of entries with an expiry. `Get`, `TTL`, `Keys` and the snapshot take the read
  lock; every write, the eviction and the sweep take the write lock. No operation holds two shard
  locks.
- **Reference mark**: written under the read lock by `Get`, so it is a `uint32` used through
  `sync/atomic`; `Get` stores it only when it is clear (no cache-line write on a hot key).
- **Counters**: hits and misses are one `atomic.Uint64` each **per shard** (no line shared by all
  threads); evictions and expirations are plain integers under the shard's write lock.
- **Sweep state**: a `sync.Mutex` guards `isArmed`, `isDirty` and the timer. A write that takes a
  shard's count of expiring entries from 0 to 1 locks it, sets `isDirty`, and arms the timer if it
  is not armed. A pass clears `isDirty` before visiting the shards and, at its end, re-arms if any
  shard still holds an expiring entry or `isDirty` is set, else clears `isArmed`. Lock order: a
  shard lock is released before the sweep mutex is taken, and the pass takes the sweep mutex only
  while it holds no shard lock. Passes never overlap (the timer is re-armed at the end of a pass).
- **Goroutines**: none owned. The timer's callback runs on a runtime goroutine for the length of
  one pass and ends; it stops for good per M13 and M14.
- **Config**: the state holds an `atomic.Pointer` to an immutable map. `Get` and `Transform` load
  it; `Set` copies the map under a `sync.Mutex` and stores the new pointer (copy on write; `Set` is
  O(keys), meant for hooks).

## Security
- **Memory under hostile keys** (the throttler's store): M11 bounds entries and bytes for any key
  set; M15 stops a key that is a slice of a large header from pinning it; `ErrEntryTooLarge` stops
  one value from emptying a shard. Eviction work per insert is bounded by two turns of one shard
  (at most 2 × `MaxEntries / 64` steps, amortized one).
- **Shard choice** uses `hash/maphash` with a per-process seed (unchanged): a client cannot aim
  keys at one shard.
- **Eviction fails open**: a full cache forgets, it never refuses. For a rate limiter this means a
  flood can reset counters (the table above gives how much); 0014 owns that trade-off (its Q7b).
- **Negative TTL** is an error (M3): a computed remaining time that went below zero no longer makes
  an immortal entry.
- **Config**: strict parsing (E1 to E5) ends silent truncation of secrets (`=`, `#`, backslashes);
  E7 bounds file size, value size and expansion, and refuses FIFOs and devices; E10 keeps values out
  of every message, and the opaque `State` pointer keeps them out of accidental dumps. The `.env`
  file is trusted input (it is the operator's), but a truncated or corrupt one now stops the boot
  with a line number instead of starting with wrong secrets.
- Not done, on purpose: no warning on file permissions, no encryption of values in memory.

## Impact
Breaking (allowed by the roadmap):
- `memorycache.NewMemoryCache()` takes options; `Stop` is gone; a negative ttl is an error; a
  default cache now evicts above 65,536 entries or 16 MiB; one entry is limited to
  `MaxBytes / 64` (256 KiB by default).
- `cache.Cache` gains `Incr`: custom backends must add it.
- `.env` files: `#` inside an unquoted value is kept unless a blank precedes it; escapes work only
  in double quotes; single quotes are removed; an invalid line stops the boot.
- `ConfigService.Config` and `config.DotENV` are gone.

Callers to change: `modules/cache/module.go`; `guards/throttler` (step 10, per 0014: constructor
call, no `Stop`); tests and benchmarks of `memorycache` and the throttler that call
`NewMemoryCache()` / `Stop()`; `sample/confs/conf.module.go` and `sample/shop` compile unchanged.

Docs: new `memorycache/README.md` (structure of `modules/config/README.md`); `modules/cache/README.md`
rewritten from this contract (drop the 64-shard / FNV / 30 s / `common.REST` text);
`modules/config/README.md` (format, limits, precedence, the race note removed, benchmarks
re-measured); `CLAUDE.md` (the `invalid config:` category, the cache line of Built-in Modules);
`.ginject/package-reference.md` (`memorycache`, `modules/cache`, `modules/config`),
`.ginject/conventions.md` (the `Stop` example in Concurrency), `metadata-concurrency-model.json`.

## Test plan
Time-dependent tests run inside `testing/synctest` (fake clock, no sleeps); eviction tests pick keys
of one shard with the package's own `hashKey`. Internal test packages.

`memorycache/memory_cache_test.go`
- M1 `TestNewMemoryCache_Defaults`, `_RoundsMaxEntries`, `_MinimumBounds`, `_NilOptions`
- M2 `TestNewMemoryCache_StartsNoGoroutineOrTimer`
- M3 `TestMemoryCache_ExpiresExactlyAtTTL` (one nanosecond before: live; at ttl: gone),
  `_ZeroTTLNeverExpires`, `_NegativeTTLRejected` (Set, SetNX, Incr), `_HugeTTLNeverExpires`
- M4 `TestMemoryCache_ExpiredEntryAbsentWithoutSweep` (Get, TTL, Keys, SetNX, Incr)
- M5 `TestMemoryCache_GetSet`, `_GetMiss`, `_EmptyKey`, `_NilValue`, `_CopySemantics_Set`, `_CopySemantics_Get`,
  `_CopySemantics_AfterOverwrite` (a slice from `Get` is unchanged by a later `Set` of the key)
- M6 `TestMemoryCache_Set_ReplacesExpiry`, `_Set_ErrorOrder`, `_Set_EntryTooLarge`, `_Set_FullCacheReturnsNil`
- M7 `TestMemoryCache_SetNX_New`, `_Exists`, `_AfterExpiry`, `_ConcurrentOneWinner` (12 goroutines)
- M8 `TestMemoryCache_Incr_Creates`, `_Adds`, `_KeepsExpiry`, `_AfterExpiryStartsOver`, `_NotCounter`,
  `_NegativeAndZeroDelta`, `_Wraps`, `_ReadableByGet`, `_ConcurrentExact` (12 goroutines × 1,000, every
  result unique, final value 12,000)
- M9 `TestMemoryCache_Delete`, `_DeleteMissing`
- M10 `TestMemoryCache_TTLWithExpiry`, `_TTLNoExpiry`, `_TTLMissing`, `_Keys`, `_KeysNeverNil`
- M11 `TestMemoryCache_NeverExceedsMaxEntries` (2 million distinct keys, 12 goroutines),
  `_NeverExceedsMaxBytes`, `_UpdateAtBoundEvictsNothing`, `_GrowingValueEvictsOthersNotItself`
- M12 `TestMemoryCache_EvictsExpiredFirst`, `_KeepsReadEntries`, `_KeepsRewrittenEntries`,
  `_NewEntriesAreFirstVictims`
- M13 `TestMemoryCache_SweepNotArmedWithoutTTL`, `_SweepArmsOnFirstTTL`, `_SweepRemovesExpired`,
  `_SweepStopsWhenNoTTLLeft`, `_SweepRearmsAfterStop`, `_SweepReleasesEmptyShard`,
  `_SweepConcurrentWithWrites` (race)
- M14 `TestMemoryCache_CollectedWithPendingTimer` (`runtime.SetFinalizer` / `runtime.GC`)
- M15 `TestMemoryCache_KeyIsCopied`
- M16 `TestMemoryCache_SnapshotCounts`, `_SnapshotOfOtherTypeIsFalse`
- `TestMemoryCache_ConcurrentMixedNoRace`; the three `TestHashKey_…` tests stay
- Benchmarks (`memory_cache_bench_test.go`, `-cpu 1,2,12`): the existing ones on the new constructor,
  plus `BenchmarkIncr`, `BenchmarkIncr_OneKey`, `BenchmarkSet_Existing`, `BenchmarkSet_NewAtBound`,
  `BenchmarkSweep_Full`, and `BenchmarkHeapAtBound` reporting bytes per entry.

`modules/cache/module_test.go`, `service_test.go` (new)
- C1 the M3 to M10 cases through `CacheService` on the default backend (`TestCacheService_…`)
- C2 `TestRegister_NilOptions`, `_DefaultBackend`, `_CustomBackend`, `_IsGlobalAndOnInit`
- C3 `TestRegister_StartsNoGoroutine`
- C4 `TestRegister_InjectedServicesShareBackend` (an app with two providers), `_TwoAppsShareBackend`
- C5 `TestCacheService_ForwardsToBackend` (a recording fake, seven methods)
- C6 `TestErrors_AreMemoryCacheErrors`

`modules/config` (`dotenv_test.go`, `fn_test.go`, `module_test.go`, `service_test.go`)
- E1 `TestParseDotEnv_Comments`, `_ExportPrefix`, `_BOM`, `_CRLF`, `_LineWithoutEquals`, `_InvalidKey`
- E2 one test per example of the rule (`_EqualsInValue`, `_Base64Padding`, `_HashInValue`,
  `_InlineComment`, `_BackslashKept`, `_EmptyValue`, `_TrimsBlanks`)
- E3 `_DoubleQuoted`, `_DoubleQuotedMultiline`, `_Escapes`, `_UnknownEscapeKept`, `_TextAfterQuote`,
  `_UnterminatedQuote` (line of the opening quote)
- E4 `_SingleQuotedLiteral`, `_SingleQuotedMultiline`; E5 `_DuplicateKeyLastWins`
- E6 `TestExpand_SameFile`, `_ForwardReference`, `_FromEnvironment`, `_Undefined`, `_SelfReference`,
  `_CycleOfThree`, `_SingleQuotedNotExpanded`, `_EscapedDollar`, `_OffKeepsText`
- E7 `TestLoadDotEnv_TooLarge`, `_NotRegularFile` (a directory and a FIFO), `_NULByte`,
  `TestExpand_ValueTooLarge` (a doubling chain of 20 variables)
- E8 `TestRegister_DefaultFileAtModuleRoot`, `_DefaultFileMissing`, `_ExplicitFileMissing`,
  `_IsIgnoreEnvFile`, `_IsIgnoreEnvFileWithPaths`
- E9 each message asserted exactly in the tests above
- E10 `TestConfig_PanicsNeverContainValues` (every failing fixture carries a canary value; the
  recovered text must not contain it), `TestConfigService_NotPrintable` (`%v`, `%+v`, `%#v`,
  `json.Marshal`, `slog` JSON handler, alone and inside a struct)
- E11 `TestRegister_EnvironmentWins`, `_FirstFileWins`, `_IsOverrideLastWins`, `_DoesNotSetProcessEnv`
- E12 `TestFlatten` (kept), `TestRegister_LoadsTooDeep`
- E13 `TestConfigService_GetSet`, `_ZeroValueGet`, `_ZeroValueSetPanics`, `_ConcurrentGetSetNoRace`
- E14 `TestRegister_HooksShareState`
- E15 `TestConfigService_Transform_Strings`, `_Scalars`, `_SkipsMapsAndSlices`
- `inspect.ConfigStateOf`: `TestConfigState_FilesAndSortedKeys`, `_NoValues`

## Coordination

**With 0014 (throttler, step 10).** What the throttler can rely on, against the five needs 0014 lists:
1. Constructor: `memorycache.NewMemoryCache(&memorycache.MemoryCacheOptions{MaxEntries: 32768})`;
   no goroutine, no timer until an entry with a TTL exists (M2, M13), no `Stop` (M14). 0014's T4
   and T5 hold as written. 32,768 entries are 512 per shard; the first eviction comes at about 90 %
   of the bound.
2. `Set` on a full store returns `nil` and never blocks (M6). The victim is an expired entry, else
   by CLOCK (M12): this is 0014's "least recently written" as an approximation, not an exact
   order. A throttler test must assert M12's consequences (active keys survive, expired go first),
   not that the single oldest counter is the one removed. Measured: with any flood below 90 % of the
   capacity between two requests of a client, no active counter is lost; at 100 % and above, most
   are (79 % to 100 %). 0014's Q7b text should carry that limit.
3. Expiry on access: M4.
4. Entry count: `len(Keys(ctx))` in tests; `inspect.CacheStateOf(backend).Entries` for tools.
5. Memory: about 110 B fixed per entry plus key and value bytes rounded to Go's size classes
   (150 B measured for a 30-byte key and an 8-byte counter). 32,768 entries with such keys: about
   4.9 MiB. With 0014's longest key (128 bytes plus its prefix and window, about 160) and a 16-byte
   value: about 300 B, 9.8 MiB, above 0014's "under 8 MiB". To make the figure hard the throttler
   should also pass `MaxBytes: 4 << 20`: heap at most 32,768 × 110 B + 4 MiB × 1.13 = 8.0 MiB.
   Note the entry limit that follows: `MaxBytes / 64` = 64 KiB per entry, far above any counter.

Atomic operations, strategy by strategy (0014 chose its own 64 stripes around `Get` + `Set`, which
stays correct on this design: `Set` on an existing key no longer allocates):
- Fixed window: one `Incr(key, 1, window)` is exact without a stripe, 0 allocs, 111 ns against
  about 250 ns for stripe + `Get` + `Set` (GOMAXPROCS 1; the sum of the measured `Get` and `Set`
  plus a mutex, not measured as one). The TTL applies when the counter is
  created and is kept afterwards (M8), which is what a window needs.
- Sliding window: `Get` of the previous window's counter (8 bytes, big-endian, as the throttler
  encodes today) and one `Incr` of the current one. Both are atomic alone; the pair needs no
  stripe because the previous window is no longer written.
- Token bucket: needs a read-change-write of 16 bytes. This design offers no compare-and-swap
  (Open question 1), so it keeps 0014's stripe around `Get` + `Set`. Measured alternative with a
  compare-and-swap: 228 / 142 / 89 ns and 1 alloc, 298 ns when every thread hits one key.
- Using `Incr` is 0014's choice; nothing here requires it. If the throttler keeps `Backend
  cache.Cache`, custom backends must implement `Incr` either way (it is in the interface).
- A negative `ttl` is now an error: `fixedWindow`'s `max(…, time.Second)` guard already avoids it.

**With 0013.** Nothing shared. `memorycache` and `config` do not touch the request scope.

**Devtools (roadmap decision of 2026-11-17).** `devtool/` reads only bootstrap configuration today
(`devtoolBuilder`: layers, handlers, versioning, schemas); it has no runtime state and no channel
for one. This design adds the read side only:
- `internal/inspect` holds plain state structs and one function variable per component. A package
  sets its variable in `init` (`memorycache` → `CacheStateOf`, `modules/config` → `ConfigStateOf`);
  the devtools later calls `inspect.CacheStateOf(service.Backend)` on what it finds in the
  container. No exported method, no registry of instances (nothing is kept alive), nothing runs
  until the function is called.
- Cost when nobody reads: the four counters. Hits and misses are one atomic add per `Get` on the
  shard's own cache line (measured within noise: 105 → 122, 62 → 66, 23 → 25 ns at GOMAXPROCS
  1 / 2 / 12); evictions and expirations are plain increments under a lock already held. The
  snapshot itself (64 read locks, a sum) is built only when read.
- A custom `cache.Cache` backend reports `(zero, false)`; a public way for third-party backends to
  report state is left to the devtools roadmap.
- 0013 and 0014 must use the same mechanism for their components (throttler rejections, `loadshed`,
  middleware configuration). If they chose another, one of the three is aligned before approval
  (Open question 6).

## Open questions
1. **Atomic operations on `cache.Cache`.** Recommended: add `Incr` only. Counting through a cache
   is common (the module's own README example is a rate counter) and is wrong without it; fixed and
   sliding windows become one lock and zero allocations. No `CompareAndSwap`: only the token bucket
   would use it, 0014's stripes already cover it, and `SetNX` covers locks. Alternatives: add
   neither (smallest interface; the throttler keeps stripes for everything, about 140 ns and one
   lock more per request), or add both.
2. **Bounds and defaults.** Recommended: two fields, `MaxEntries` 65,536 and `MaxBytes` 16 MiB (at
   most about 25 MiB of heap, only when full of large values), no option on `cache.Register`, one
   entry at most `MaxBytes / 64`. Alternative: a single `MaxBytes` that charges a fixed overhead per
   entry (one number to learn, but 0014 asks for an entry count and the charge would be an estimate).
3. **64 shards instead of 256.** Recommended (numbers in Performance): the same speed up to 2
   threads, a quarter of the idle memory, later first eviction, four times the largest entry; it
   costs up to 24 ns per operation at 12 threads.
4. **Eviction policy: CLOCK**, for approval with its two techniques (buffer reuse, shard padding).
   Its known limit is the flood of a full capacity between two requests of a client.
5. **`.env` strictness is a breaking change.** Recommended: strict (E1 to E5, a bad line stops the
   boot). Files that relied on `#` cutting a value without a blank before it, or on escapes in
   unquoted values, change meaning. Alternative: keep skipping invalid lines (silent, as today).
6. **`ConfigService.Config` becomes the opaque `State`.** Recommended: it removes the data race of
   `Set` (copy on write) and the secret dump in one move, with no new method. Alternative: keep the
   exported map and add `String` / `GoString` / `LogValue` redaction (three exported methods, the
   race stays, the map can still be printed directly). If declined, E13's concurrency clause and
   half of E10 go.
7. **Devtools bridge.** Recommended: `internal/inspect` function variables set in `init`, shared
   with 0013 and 0014. Alternative: an exported `Snapshot()` method per component (public API the
   devtools alone needs).
8. **Negative TTL is an error** (`ErrNegativeTTL`). Recommended over today's "never expires";
   alternative: treat it as already expired (write nothing, return `nil`).
9. **Default `.env` lookup** keeps walking up to `go.mod` (E8) so `go test` in a package directory
   finds the project's file. Alternative: the working directory only, as other dotenv loaders do.
10. **Status line.** `docs/design/README.md` defines `Proposed`; this file says `Draft` like 0014,
   as the roadmap run asked while the three designs are reconciled.

## Decisions (2026-11-17)

Approved by the user. All open questions answered as recommended. Reconciled with 0014: the throttler uses `Incr` for the fixed window and passes its own bound (32,768 entries). The user's condition: the public API stays as small and simple as the Product principles require, and every name is documented clearly (the docs will feed a website later).

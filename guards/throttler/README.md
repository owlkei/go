# Throttler Guard

*`throttler` limits how many requests each client can make in a period of time, and answers `429 Too Many Requests` with `Retry-After` once a client goes over its limit.*

- [Throttler Guard](#throttler-guard)
  - [Key Features](#key-features)
  - [Why you need it](#why-you-need-it)
  - [Usage](#usage)
  - [Behind a proxy or load balancer](#behind-a-proxy-or-load-balancer)
  - [`Throttler` Struct](#throttler-struct)
    - [Limit](#limit)
    - [TTL](#ttl)
    - [Strategy](#strategy)
    - [KeyFunc](#keyfunc)
    - [Backend](#backend)
  - [Constants](#constants)
  - [`Throttler` Methods](#throttler-methods)
    - [NewGuard](#newguard)
  - [Response headers](#response-headers)
  - [How the client is identified](#how-the-client-is-identified)
  - [Limits and memory](#limits-and-memory)
    - [Known limits](#known-limits)
  - [Throttler or loadshed](#throttler-or-loadshed)
  - [Benchmarks](#benchmarks)

## Key Features
- Safe by default: the client is its IP address as resolved by the proxy stage (`AppOptions.Proxy`); no request header is read
  by the throttler, so a client cannot buy a new budget by sending a new `X-Forwarded-For`
- IPv6 clients are limited per /64 network, since one end site holds at least a /64 (RFC 6177)
- Exact under concurrency: with `Limit: 100`, 100 requests pass, never 101, whatever the number of
  goroutines
- Bounded memory: the default store keeps at most 32,768 clients (about 5 MiB when full)
- No background goroutine and no timer while the store is empty
- Three strategies: fixed window (default), sliding window, token bucket

## Why you need it

Without a limit, one client can call a login, search or upload route as fast as its connection
allows: password guessing, scraping, or a bill for every downstream call your handler makes
(OWASP API Security Top 10, API4 "Unrestricted resource consumption"). The throttler gives every
client its own budget per route group and refuses the rest with a status code clients and HTTP
libraries understand (RFC 6585 § 4).

## Usage

Bind a `Throttler` value as a guard. Owlky Go calls `NewGuard` once at `Create()`, so every request
to the bound handlers shares one budget per client:

```go
package main

import (
	"time"

	"go.owlky.com/owlky"
	"go.owlky.com/owlky/guards/throttler"
)

type LoginController struct {
	owlky.HTTP
	owlky.Guard
}

func (instance LoginController) NewController() owlky.Controller {
	instance.BindGuard(throttler.Throttler{
		Limit: 10,
		TTL:   time.Minute,
	}, instance.CREATE_login)
	return instance
}

func (instance LoginController) CREATE_login() string {
	return "ok"
}

func main() {
	app := owlky.New(&owlky.AppOptions{Server: &owlky.ServerOptions{Port: 8080}})
	app.Create(
		owlky.ModuleBuilder().
			Controllers(LoginController{}).
			Build(),
	)
	app.Listen()
}
```

To limit the whole app, bind it globally: `app.BindGlobalGuards(throttler.Throttler{})`.

Two bindings with the same options and no `KeyFunc` share one throttler, and so one budget. To give two route
groups separate budgets, give them different options (for example a different `Limit`).

## Behind a proxy or load balancer

Behind a proxy every request arrives from the proxy's address, so every client would share one
budget. List your proxies in `AppOptions.Proxy`; the throttler then limits the real client:

```go
app := owlky.New(&owlky.AppOptions{
	Proxy: &owlky.ProxyOptions{TrustedProxies: []string{"10.0.0.0/8"}},
})
```

The proxy stage reads `X-Forwarded-For` (or `Forwarded`) only from the peers you list, and only the
entries your proxies wrote. Without `Proxy`, the client is the connection's address (`r.RemoteAddr`).

## `Throttler` Struct

### Limit

Type: `int64`

Default: `100`

Required: `false`

The number of requests a client may make in each `TTL`. A zero or negative value means 100.

```go
throttler.Throttler{Limit: 50}
```

### TTL

Type: `time.Duration`

Default: `time.Minute`

Required: `false`

The length of the window (fixed and sliding window), or the time the token bucket takes to refill
completely. A zero or negative value means one minute. Windows are counted in whole seconds: a
`TTL` under one second counts as one second for the fixed and sliding windows.

```go
throttler.Throttler{TTL: 30 * time.Second}
```

### Strategy

Type: `Strategy`

Default: `StrategyFixedWindow`

Required: `false`

The counting algorithm. See [Constants](#constants). An unknown value panics at `Create()`.

```go
throttler.Throttler{Strategy: throttler.StrategyTokenBucket}
```

### KeyFunc

Type: `func(owlky.HTTPContext) string`

Default: the client IP (see [How the client is identified](#how-the-client-is-identified))

Required: `false`

Returns the identity to charge, for example a user or an API key. A key longer than 128 bytes is
stored as `h:` and 16 hex digits of a hash of it, so a client cannot store large keys. Read the
client address with `owlky.ClientIP(c.Request)`, never from a request header yourself: a header is
whatever the client sends.

```go
throttler.Throttler{
	KeyFunc: func(c owlky.HTTPContext) string {
		if user := c.Request.Header.Get("X-User-ID"); user != "" {
			return "user:" + user
		}
		return owlky.ClientIP(c.Request).String()
	},
}
```

### Backend

Type: `cache.Cache`

Default: a `cache.NewMemoryCache` store bounded to 32,768 entries, one per throttler

Required: `false`

Where the counters live. Pass a shared store (such as Redis behind `cache.Cache`) when several
instances of the app must share one budget, or when you need more than 32,768 clients. If the
backend returns an error, the request is allowed: the throttler fails open rather than taking the
app down with its store.

```go
throttler.Throttler{
	Backend: cache.NewMemoryCache(&cache.MemoryCacheOptions{MaxEntries: 262144}),
}
```

## Constants

| Constant | Behavior | Use it for |
|---|---|---|
| `StrategyFixedWindow` (default) | Counts requests per window of `TTL`; the count resets at each window start. One atomic `Incr` per request. | Most routes. A client can make up to twice `Limit` across a window boundary. |
| `StrategySlidingWindow` | Weights the previous window's count by the part of it still inside the last `TTL`, plus the current count. Refused requests are not counted. | Routes where the boundary burst matters (login, password reset). |
| `StrategyTokenBucket` | A bucket of `Limit` tokens refills continuously over `TTL`; each request takes one. | Allowing short bursts while keeping a steady average rate. |

## `Throttler` Methods

### NewGuard

Applies the defaults and returns the compiled guard. Owlky Go calls it for you when you bind a
`Throttler`; call it yourself only to use the guard outside Owlky Go's binding.

**Signature**

```go
func (instance Throttler) NewGuard() any
```

**Returns**

- 1st value: `any`: the compiled guard, a value with `CanActivate(owlky.HTTPContext) bool`

**Rules**

- A zero `Throttler` limits to 100 requests per minute with `StrategyFixedWindow`
- Creating a guard and serving requests starts no goroutine
- The guard allows a request (`true`) or panics with `exception.TooManyRequestsException` (429)
- With one key and one window, any number of concurrent requests get exactly `Limit` allowed, with
  every strategy
- Different keys have independent budgets
- The default key is the client IP: IPv4 as is, IPv6 masked to its /64, the IPv4-mapped IPv6 form
  is the IPv4 address, and every unresolvable peer shares the key `unknown`
- The source port is not part of the key: all connections of one client share one budget
- No request header changes the default key, however many are sent
- `KeyFunc` replaces the default key; keys longer than 128 bytes are hashed, keys of 128 bytes or
  less are kept as they are, and two long keys stay distinct
- Any key, including binary data, is counted without a panic
- A backend error allows the request
- With the default store, a flood of distinct keys never stores more than 32,768 entries: each new
  key past the bound evicts another

## Response headers

| Header | When | Value |
|---|---|---|
| `X-RateLimit-Limit` | every response | `Limit` |
| `X-RateLimit-Remaining` | every response | requests left in the window; `0` once refused |
| `X-RateLimit-Reset` | every response | Unix time (seconds) when the window resets, or when a token is next available |
| `Retry-After` | refusals only | seconds until `X-RateLimit-Reset` (RFC 9110 § 10.2.3) |

A refusal is `429 Too Many Requests` with the fixed message `Too Many Requests`.

## How the client is identified

The default key is `owlky.ClientIP(c.Request)`: the connection's address, or the client that
the proxy stage (`AppOptions.Proxy`) resolved from the headers of a trusted proxy.

- **IPv4:** the address, without the port.
- **IPv6:** the /64 network (`2001:db8:1:2::`). Providers hand every customer a /64 or more, so one
  client can use 2^64 addresses; limiting each address would give it 2^64 budgets.
- **Unknown:** a peer address that cannot be parsed counts under the single key `unknown`.

## Limits and memory

| | Value |
|---|---|
| Clients in the default store | 32,768 (no option: pass your own `Backend` for more) |
| Memory per stored client | about 150 bytes (IPv4 key), 165 bytes (IPv6 key), 280 bytes (128-byte custom key) |
| Memory with a full default store | about 4.7 MiB (IPv4), 5.2 MiB (IPv6), 8.7 MiB with 128-byte custom keys |
| Memory of an idle throttler | about 14 KiB |
| Key length | 128 bytes; longer keys are hashed |
| Goroutines and timers | none while the store is empty; the store's expiry timer runs only while it holds counters |

When the default store is full, a new client evicts an older counter (CLOCK order, an
approximation of least recently used). The evicted client starts again with a full budget: under
more than 32,768 active clients the throttler fails open for the least active ones, rather than
refusing new clients, which a flood of new keys would otherwise lock out.

### Known limits

- **Eviction under a large IPv6 range.** IPv6 clients are counted per /64, and the default store
  keeps 32,768 counters. A client that holds a /48 has 65,536 /64 networks: by rotating among them
  it creates more keys than the store holds, pushes counters out (its own included), and each
  evicted key starts again with a full budget. For routes where that matters (logins, password
  resets), pass a `KeyFunc` that charges something the client can't multiply, such as the account
  name, or a larger `Backend`.
- **Several instances with one shared `Backend`.** `StrategyFixedWindow` counts with the backend's `Incr`,
  so it is exact across instances when the backend's `Incr` is atomic. `StrategySlidingWindow` and
  `StrategyTokenBucket` read the stored value, compute and write it back; the lock that makes this exact
  exists in one process only. Two instances that update one key at the same moment can each
  allow a request that together exceed `Limit`. Use `StrategyFixedWindow` with a shared backend, or accept
  a small overshoot under contention.

## Throttler or loadshed

The throttler limits **each client**: it protects you from one client that asks too much, and it
answers `429`. `AppOptions.LoadShed` limits **the whole server**: it refuses work when the server
is overloaded, whoever sends it, and answers `503`. They solve different problems and work
together: turn `LoadShed` on to keep the server healthy, and the throttler on routes that one client
must not abuse.

## Benchmarks

Measured on an Intel Core i7-9750H @ 2.60 GHz, Go 1.27.4, with
`go test -run='^$' -bench . -benchmem -count 5 -cpu 1,2` (medians). Results depend on the machine
and were captured when this README was written. Every benchmark allows the request with the default
memory store; the header map is a fresh `httptest.ResponseRecorder` map.

| Benchmark | -cpu 1 | -cpu 2 | B/op | allocs/op |
|---|---|---|---|---|
| `CanActivate/FixedWindow` | 424 ns | 360 ns | 104 | 6 |
| `CanActivate/SlidingWindow` | 554 ns | 479 ns | 144 | 8 |
| `CanActivate/TokenBucket` | 553 ns | 475 ns | 128 | 8 |
| `CanActivate_Parallel/FixedWindow` | 418 ns | 251 ns | 104 | 6 |
| `CanActivate_Parallel/SlidingWindow` | 561 ns | 485 ns | 144 | 8 |
| `CanActivate_Parallel/TokenBucket` | 550 ns | 521 ns | 128 | 8 |
| `CanActivate_DefaultKey` (client IP key) | 505 ns | 461 ns | 104 | 6 |

The allocations are the stored key (one), the three response header values and their numbers.
The sliding window and the token bucket also allocate the value they store.

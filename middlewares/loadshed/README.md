# Loadshed Middleware

*`loadshed` keeps your server fast when more requests arrive than it can serve: it lets a bounded number of requests run at once, holds a few more for a moment, and answers the rest `503 Service Unavailable` with `Retry-After` right away.*

- [Loadshed Middleware](#loadshed-middleware)
  - [Key Features](#key-features)
  - [Why you need it](#why-you-need-it)
  - [Usage](#usage)
  - [`LoadShed` Struct](#loadshed-struct)
    - [MaxInFlight](#maxinflight)
    - [MaxQueue](#maxqueue)
  - [`LoadShed` Methods](#loadshed-methods)
    - [NewMiddleware](#newmiddleware)
    - [Use](#use)
  - [What happens to a request](#what-happens-to-a-request)
  - [How the limit adapts](#how-the-limit-adapts)
  - [Defaults and where they come from](#defaults-and-where-they-come-from)
  - [Rules](#rules)
  - [Known limits](#known-limits)
  - [Loadshed or throttler](#loadshed-or-throttler)
  - [Memory and CPU](#memory-and-cpu)
  - [Benchmarks](#benchmarks)

## Key Features
- Nothing to configure: the limit follows the latency your handlers actually have, between bounds
  read from the machine (`GOMAXPROCS`, `GOMEMLIMIT`, the container's memory limit)
- Fails fast: at most 64 requests wait, for at most 100 ms; everything else gets `503` with
  `Retry-After: 1` at once, so clients retry instead of piling up
- A fixed limit when you want one: `LoadShed{MaxInFlight: 200}`
- Every slot is released exactly once, whatever the request did: returned, panicked, answered
  `404`, was refused by a later layer, or aborted
- No goroutine and no timer while the server is idle; an admitted request costs a few atomic
  operations and no allocation

## Why you need it

A server that accepts every request slows down for everyone once it is past its capacity: each
new request takes a share of the CPU, memory and connections the others need, latency climbs
for all of them, and clients time out on work the server finishes anyway. Refusing the extra
requests early, with a status code clients and load balancers understand, keeps the admitted ones
fast (Google SRE Book, chapters 21 and 22, "Handling overload" and "Addressing cascading
failures").

## Usage

Bind `loadshed` first, so a refused request costs as little as possible:

```go
package main

import (
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/middlewares/loadshed"
)

func main() {
	app := core.New()

	app.BindGlobalMiddlewares(loadshed.LoadShed{})

	app.Create(
		core.ModuleBuilder().Build(),
	)

	app.Listen(8080, nil)
}
```

Over capacity, the extra requests get:

```http
HTTP/1.1 503 Service Unavailable
Content-Type: application/json
Retry-After: 1

{"code":503,"error":"Service Unavailable","message":"Server is busy"}
```

Ginject calls `NewMiddleware` once at `Create()`. Two bindings of a `LoadShed` with the same
values share one limiter.

To limit WebSocket handshakes too, pass it to `EnableWS`:

```go
app.EnableWS(&core.WSConfig{Path: "/ws"}, loadshed.LoadShed{})
```

The slot of a handshake is released when the handshake ends, not when the connection closes.

## `LoadShed` Struct

### MaxInFlight
Type: `int`

Default: `0` (adaptive)

Required: `false`

How many requests may run at once. `0` lets the limit adapt by itself (see
[How the limit adapts](#how-the-limit-adapts)); a value from 1 to 1,000,000 is a fixed limit that
never changes. A negative value or a value above 1,000,000 panics at `Create()`.

```go
loadshed.LoadShed{MaxInFlight: 200}
```

Use a fixed limit when you know what your server can take (a load test told you), or for a
module whose routes are much slower or faster than the rest of the app.

### MaxQueue
Type: `int`

Default: `64`

Required: `false`

How many requests may wait for a free slot. `0` means 64; a negative value turns the queue off,
so a request over the limit is refused at once. A value above 65,536 panics at `Create()`. A
waiting request waits at most 100 ms.

```go
loadshed.LoadShed{MaxQueue: -1}
```

The wait and the `Retry-After` value are fixed.

## `LoadShed` Methods

### NewMiddleware
```go
func (instance LoadShed) NewMiddleware() common.MiddlewareFn
```

Validates the options and reads the machine's bounds once; the framework calls it at `Create()`.
It panics with `invalid loadshed: ...` for an option out of range.

### Use
```go
func (instance LoadShed) Use(r *http.Request, w http.ResponseWriter, next ctx.Next)
```

Makes `LoadShed` a `common.MiddlewareFn`. It builds a new limiter on every call, so use it in tests
only; the framework always calls `NewMiddleware`. Called outside an app, it only calls the next
layer.

## What happens to a request

1. A free slot: the request runs. Its slot is released when its exchange ends.
2. No free slot and fewer than `MaxQueue` requests waiting: it waits. The first slot that frees
   goes to the request that has waited longest.
3. The queue is full, or 100 ms passed without a slot: `503`, `Retry-After: 1`, message
   `Server is busy`. No guard, interceptor, handler or exception filter runs.
4. The client went away while waiting (its request context ended): nothing is written.

## How the limit adapts

With `MaxInFlight: 0`, `loadshed` measures how long each request takes and compares it with how
long requests take when the server is not busy. When requests are being held back and they stay
close to that time, the limit grows; when they get slower, it shrinks. While nothing is held back,
the limit stays where it is. This is the gradient rule of Netflix's `concurrency-limits`
(Gradient2) and Envoy's adaptive concurrency filter.

- **Bounds.** The lowest limit is `GOMAXPROCS`. The highest is the memory limit divided by 4 MiB,
  between 64 and 4,096; the memory limit is the smallest of `GOMEMLIMIT` and, on Linux, the
  container's cgroup limit (`memory.max`, or `memory.limit_in_bytes` for cgroup v1). With no memory
  limit, the highest is 256 × `GOMAXPROCS`, between 64 and 4,096. The limit starts at
  8 × `GOMAXPROCS` within those bounds. For example, 1 vCPU and 512 MiB gives 1 / 8 / 128
  (lowest / start / highest), and 2 vCPU and 2 GiB gives 2 / 16 / 512.
- **Samples.** Every finished request is a sample, measured from the moment it got its slot, so
  the time it waited in the queue is left out. Requests that take longer than 30 s (streams,
  downloads) are ignored. Samples are grouped in windows of 4 × the current limit; the limit can
  change when a window closes.
- **Baseline.** The time requests take without contention: the average of samples that finished
  with at most `GOMAXPROCS` requests running, blended with the previous baseline, and lowered
  whenever a window was faster on average. Until a baseline is known, the limit stays where it
  started.
- **Update.** The limit moves only at the end of a window in which a request was queued or
  refused. Without that, the limit held nothing back, so there is nothing to learn. With
  `gradient = min(1.5 × baseline / average, 1)` and `useful = limit × gradient`, the new limit is
  `0.8 × limit + 0.2 × (useful + √useful)`, within the bounds. One window takes at most 20 % off
  the limit, so one slow burst does not empty the server. For example, with a limit of 32 and a
  baseline of 10 ms, a window that averages 10 ms gives 33, and one that averages 60 ms gives 27.
- **Probe.** When requests were queued or refused and the baseline is unknown or older than 30 s,
  `loadshed` lets only `GOMAXPROCS` requests run until 8 of them have finished (or 1 s has passed),
  and takes their average as the new baseline. Probes start at least 30 s apart, and the window in
  which a probe runs does not move the limit. No probe starts while nothing is queued or refused.

All of this runs inside the request that finishes; there is no background goroutine and no timer.

## Defaults and where they come from

| Default | Value | Source |
|---|---|---|
| Refusal | `503` with `Retry-After: 1` | RFC 9110 § 15.6.4 and § 10.2.3 |
| Queue | 64 requests, 100 ms each | Google SRE Book ch. 22: short queues, fail fast |
| Limit | adaptive, gradient against a probed no-load baseline, tolerance 1.5, moved only while requests are held back | Netflix `concurrency-limits` (Gradient2), Envoy adaptive concurrency (minimum-latency probe) |
| Bounds | `GOMAXPROCS` to memory / 4 MiB (64-4,096) | one slot per core at least; about 4 MiB of memory per request in flight at most |
| First limit | 8 × `GOMAXPROCS` | low enough that the limit settles within seconds on a small machine; an I/O-bound app grows it while its first bursts are held back |
| Window | 4 × the current limit samples | enough samples to average out a few slow requests at any limit |

## Rules
- At any instant, at most `limit` requests are between admission and the end of their exchange,
  also under heavy concurrency and while the adaptive limit changes.
- With a fixed limit, the limit is `MaxInFlight` forever.
- Waiting requests are admitted in arrival order.
- A request that finds the queue full or waits 100 ms is refused with `503`, `Retry-After: 1` and
  the body `{"code":503,"error":"Service Unavailable","message":"Server is busy"}`
  (`Content-Type: application/json`). A refused request takes no slot.
- A request whose context ends while it waits gets no response and takes no slot.
- A slot is released after a normal response, a panic, a refusal by a later middleware, a
  middleware that answered by itself, a `404` and `http.ErrAbortHandler`. For a WebSocket
  handshake it is released when the handshake ends.
- Under a burst of 50 × the limit, every response is `200` or `503`, and every slot is free
  afterwards.
- The next layer receives the same `*http.Request` and the same context; the body is never read.
- Idle, there is no goroutine in either mode, and none is left after requests waited.
- Adaptive: the limit starts at 8 × `GOMAXPROCS`, never goes below `GOMAXPROCS` nor above the
  highest bound, and does not move without a baseline. It moves only at the close of a window of
  4 × limit samples in which a request was queued or refused: it shrinks by at most 20 % per slow
  window, grows on fast ones, and stays whatever the latency when nothing was held back. Samples
  over 30 s leave no trace, and the time spent in the queue is not part of a sample.
- Adaptive: a probe starts only when requests were queued or refused, the baseline is unknown or
  older than 30 s, and no probe started in the last 30 s. It admits `GOMAXPROCS` requests, ends
  after 8 of them finished or after 1 s, and the window it ran in leaves the limit as it was. A
  probe in which no request finished leaves the baseline as it was.
- Adaptive, measured on 1 and 2 CPUs under 5 × the capacity: the limit stays within 35 % of its
  median for a CPU-bound handler and 20 % for an I/O-bound one, throughput is at least 95 % of the
  capacity, and the 99th percentile latency is at most 12 × (CPU-bound) and 3 × (I/O-bound) the
  latency without load. At 70 % of the capacity no request is refused.
- An admitted request allocates nothing.

## Known limits
- One limit covers every route it is bound to. A mix of very fast and very slow routes blurs the
  latency signal: bind a fixed `LoadShed` to the module with the slow routes instead.
- The limit follows latency, so a dependency that slows down (a database, an API) lowers it. That
  is the intent: the server stops sending more work to something that is already struggling.
- Health checks are not exempt. A load balancer that sees `503` on its probe takes the instance
  out of rotation, which is what you want while it is overloaded.
- A handler that computes for milliseconds without ever reaching the Go scheduler (no I/O, no
  lock, no channel, no allocation that triggers the garbage collector) can hide overload from any
  in-flight limit, fixed or adaptive. Go runs such requests one after another, so the number in
  flight stays below the limit and the excess waits in Go's run queue instead of being refused.
  `loadshed` starts refusing once enough of them have been preempted to fill the limit, which on
  1 CPU leaves the limit moving by about 35 % around a small value. For such a server, set a fixed
  limit of about twice the CPUs: `LoadShed{MaxInFlight: 2 * runtime.GOMAXPROCS(0)}`. Handlers
  that wait on a database, a socket or a lock reach the scheduler and are not affected.
- `loadshed` protects one process. It does not know about other instances behind your load
  balancer.

## Loadshed or throttler

`loadshed` limits **the whole server**: it refuses work when the server is overloaded, whoever
sends it, and answers `503`. `guards/throttler` limits **each client**: it refuses a client that
asks too much in a period of time, even when the server is idle, and answers `429`. Bind
`loadshed` to keep the server healthy, and the throttler on routes one client must not abuse
(logins, search, uploads).

## Memory and CPU
- Idle: about 200 B per instance (counters, the window, two mutexes, an empty queue). No goroutine,
  no timer.
- Each waiting request: one timer and one channel (about 200 B), at most `MaxQueue` of them.
- Admitted request: a few atomic operations and no allocation. The request that closes a window
  (every 4 × limit requests) also recomputes the adaptive limit.
- Refused request: the cost of the HTTP layer's error response.

## Benchmarks

Machine-dependent; captured on an Intel i7-9750H, Go 1.27.4, when this README was written
(`go test -run='^$' -bench 'Admitted|Shed$' -benchmem -count 5 -cpu 1,2 ./middlewares/loadshed`, median shown).

| Benchmark | GOMAXPROCS 1 | GOMAXPROCS 2 | B/op | allocs/op |
|---|---|---|---|---|
| `LoadShed_Admitted_Fixed` | 26.8 ns | 26.8 ns | 0 | 0 |
| `LoadShed_Admitted_Adaptive` | 40.7 ns | 40.3 ns | 0 | 0 |
| `LoadShed_Admitted_Parallel` | 47.2 ns | 113 ns | 0 | 0 |
| `LoadShed_Shed` | 445 ns | 434 ns | 16 | 1 |
| `LoadShed_App_Admitted` | 1.67 µs | 1.28 µs | 680 | 9 |

`Admitted_*` admit and release one request on the limiter alone, with a fixed or the adaptive limit;
`Parallel` does it from every core at once, so the cores share the counters. `Shed` refuses a
request (the `Retry-After` header is its one allocation). `App_Admitted` serves a whole request
through an app; the allocations are the app's own.
